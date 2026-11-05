package daemon

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/logx"
	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/rclone/rclone/fs"
)

const (
	stepSignIn = iota
	stepReach
	stepOpen
)

func (d *Daemon) progress(p api.AddProgress) {
	if d.srv == nil {
		return
	}
	raw, _ := json.Marshal(p)
	d.srv.Publish(ipc.Event{Event: api.EAddProgress, Data: raw})
}

// anything this call saved goes again on failure or cancel
func (d *Daemon) addDrive(ctx context.Context, p api.NewDrive) (dr api.Drive, err error) {
	name := strings.TrimSpace(p.Name)
	if err := engine.ValidName(name); err != nil {
		return dr, err
	}
	d.mu.Lock()
	err = d.folderClash("", name)
	if err == nil && p.ServiceID != "" {
		err = d.sameRemote(store.Mount{ServiceID: p.ServiceID, RemotePath: p.SubPath})
	}
	d.mu.Unlock()
	if err != nil {
		return dr, err
	}
	var newService, newMount string
	step := stepSignIn
	defer func() {
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			err = errors.New("adding the drive was cancelled")
		}
		// undo forgets the new account's secrets, so they are masked here first
		var extra []string
		if p.Service != nil {
			if split, serr := splitSecrets(store.Service{Type: p.Service.Type, Options: p.Service.Options}); serr == nil {
				extra = slices.Collect(maps.Values(split.Secrets))
			}
		}
		err = errors.New(logx.Redact(err.Error(), extra...))
		d.progress(api.AddProgress{Step: step, Error: err.Error()})
		undo, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		if newMount != "" {
			_ = d.deleteMount(undo, newMount, true)
		}
		if newService != "" {
			_ = d.deleteService(undo, newService)
		}
	}()

	d.progress(api.AddProgress{Step: stepSignIn})
	var svc store.Service
	if p.ServiceID != "" {
		var ok bool
		if svc, ok = d.findService(p.ServiceID); !ok {
			return dr, fmt.Errorf("no account with id %q", p.ServiceID)
		}
	} else {
		if p.Service == nil {
			return dr, errors.New("choose where your files live")
		}
		svc = store.Service{ID: uuid.NewString(), Name: cmp.Or(strings.TrimSpace(p.Service.Name), name), Type: p.Service.Type, Options: p.Service.Options}
		if r := d.testService(svc); !r.OK {
			return dr, errors.New(r.Error)
		}
	}
	if err = ctx.Err(); err != nil {
		return dr, err
	}

	step = stepReach
	d.progress(api.AddProgress{Step: stepReach})
	if p.ServiceID == "" {
		if err = d.putService(ctx, svc); err != nil {
			return dr, err
		}
		newService = svc.ID
		svc, _ = d.findService(svc.ID)
	}
	if err = d.eng.Reachable(svc, p.SubPath); err != nil {
		if strings.Contains(err.Error(), fs.ErrorDirNotFound.Error()) {
			err = fmt.Errorf("there is no folder %s on %s", p.SubPath, svc.Name)
		}
		return dr, err
	}
	m := store.Mount{ID: uuid.NewString(), Name: name, ServiceID: svc.ID, RemotePath: strings.Trim(p.SubPath, "/"), VFS: store.DefaultVFS(), CreatedAt: time.Now().UTC()}
	if err = ctx.Err(); err == nil {
		err = d.putMount(ctx, m)
	}
	if err != nil {
		return dr, err
	}
	newMount = m.ID

	step = stepOpen
	d.progress(api.AddProgress{Step: stepOpen})
	if p.AtLogin {
		if err = d.mount(m.ID); err != nil {
			return dr, err
		}
	}
	if err = ctx.Err(); err != nil {
		return dr, err
	}
	d.progress(api.AddProgress{Step: stepOpen, Done: true})
	return d.driveView(m.ID)
}

func (d *Daemon) updateDrive(ctx context.Context, u api.DriveUpdate) (api.Drive, error) {
	m, ok := d.findMount(byID(u.ID))
	if !ok {
		return api.Drive{}, fmt.Errorf("no drive with id %q", u.ID)
	}
	old := m
	m.Name = cmp.Or(strings.TrimSpace(u.Name), m.Name)
	if u.SubPath != nil {
		m.RemotePath = strings.Trim(*u.SubPath, "/")
		d.mu.Lock()
		err := d.sameRemote(m)
		d.mu.Unlock()
		if err != nil {
			return api.Drive{}, err
		}
	}
	if u.CacheMaxSize != "" {
		var size fs.SizeSuffix
		if err := size.Set(u.CacheMaxSize); err != nil {
			return api.Drive{}, fmt.Errorf("cache size %q: try a size like 10G", u.CacheMaxSize)
		}
		m.VFS.CacheMaxSize = u.CacheMaxSize
	}
	m.VFS.CacheMaxAge = cmp.Or(u.CacheMaxAge, m.VFS.CacheMaxAge)
	if m != old {
		if err := d.putMount(ctx, m); err != nil {
			return api.Drive{}, err
		}
	}
	if u.LocalFolder != nil {
		if err := d.setLocal(u.ID, strings.TrimSpace(*u.LocalFolder)); err != nil {
			return api.Drive{}, err
		}
	}
	if u.AtLogin != nil {
		d.mu.Lock()
		d.setMounted(u.ID, *u.AtLogin)
		err := d.save()
		d.mu.Unlock()
		if err != nil {
			return api.Drive{}, err
		}
		d.publish()
	}
	return d.driveView(u.ID)
}

func (d *Daemon) removeDrive(ctx context.Context, r api.DriveRemove) (any, error) {
	m, ok := d.findMount(byID(r.ID))
	if !ok {
		return nil, fmt.Errorf("no drive with id %q", r.ID)
	}
	if err := d.deleteMount(ctx, r.ID, false); err != nil || !r.ForgetAccount {
		return nil, err
	}
	if _, used := d.findMount(func(x store.Mount) bool { return x.ServiceID == m.ServiceID }); used {
		return nil, nil
	}
	return nil, d.deleteService(ctx, m.ServiceID)
}

func (d *Daemon) connectDrive(p idParams) (api.Drive, error) {
	if err := d.mount(p.ID); err != nil {
		return api.Drive{}, err
	}
	return d.driveView(p.ID)
}

func (d *Daemon) disconnectDrive(p idParams) (api.Drive, error) {
	if err := d.unmount(p.ID); err != nil {
		return api.Drive{}, err
	}
	dr, err := d.driveView(p.ID)
	if err != nil {
		// a drive removed on another computer goes once it is disconnected here
		return api.Drive{ID: p.ID, State: api.Removed}, nil
	}
	return dr, nil
}

func (d *Daemon) driveView(id string) (api.Drive, error) {
	m, ok := d.findMount(byID(id))
	if !ok {
		return api.Drive{}, fmt.Errorf("no drive with id %q", id)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.drive(m), nil
}

func (d *Daemon) accounts() []api.AccountRef {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]api.AccountRef, 0, len(d.data.Services))
	for _, s := range d.data.Services {
		a := api.AccountRef{ID: s.ID, Name: s.Name, Type: s.Type}
		for _, m := range d.data.Mounts {
			if m.ServiceID == s.ID {
				a.Drives++
			}
		}
		out = append(out, a)
	}
	return out
}

// caller holds mu
func (d *Daemon) folderClash(id, name string) error {
	folder := engine.SafeFolder(name, runtime.GOOS)
	for _, x := range d.data.Mounts {
		if x.ID != id && strings.EqualFold(engine.SafeFolder(x.Name, runtime.GOOS), folder) {
			return fmt.Errorf("another drive already uses the folder %s", folder)
		}
	}
	return nil
}

// rclone finds a mount's cache by account and folder, so two drives cannot share both
func (d *Daemon) sameRemote(m store.Mount) error {
	for _, x := range d.data.Mounts {
		if x.ID != m.ID && x.ServiceID == m.ServiceID && strings.Trim(x.RemotePath, "/") == strings.Trim(m.RemotePath, "/") {
			return fmt.Errorf("%s already shows that folder of the account", x.Name)
		}
	}
	return nil
}

// rclone cannot drop a live drive's cache, so connected drives go off and come back around it
func (d *Daemon) clearCachedFiles() error {
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.mu.Lock()
	ms := slices.Clone(d.data.Mounts)
	var live []store.Mount
	for _, m := range ms {
		if st := d.states[m.ID]; st != nil && st.name != "" {
			live = append(live, m)
		}
	}
	d.mu.Unlock()
	counts, err := d.unsentFiles(ms)
	if err != nil {
		return fmt.Errorf("could not check for files not uploaded yet: %w", err)
	}
	switch n := sum(counts); {
	case n == 1:
		return errors.New("1 file is not uploaded yet, clear the cache when it is")
	case n > 1:
		return fmt.Errorf("%d files are not uploaded yet, clear the cache when they are", n)
	}
	for _, m := range live {
		if err := d.unmountLocked(m); err != nil {
			return err
		}
	}
	err = d.clearCache()
	for _, m := range live {
		if merr := d.mountLocked(m); merr != nil && err == nil {
			err = merr
		}
	}
	return err
}

func sum(counts map[string]int) int {
	n := 0
	for _, c := range counts {
		n += c
	}
	return n
}
