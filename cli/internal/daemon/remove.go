package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/store"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
)

// one background sync is 15 minutes, so a device seen in the last 16 is still on
const peerActive = 16 * time.Minute

func (d *Daemon) checkDrive(ctx context.Context, p idParams) (api.DriveCheck, error) {
	if _, ok := d.findMount(byID(p.ID)); !ok {
		return api.DriveCheck{}, fmt.Errorf("no drive with id %q", p.ID)
	}
	c := api.DriveCheck{Usage: d.usage(p.ID), Others: []api.Peer{}}
	d.mu.Lock()
	acct := d.data.Account
	d.mu.Unlock()
	if acct == nil || acct.Pending {
		return c, nil
	}
	list, err := d.devices(ctx)
	if err != nil {
		return c, err
	}
	for _, x := range list {
		if !x.Current && slices.Contains(x.Mounted, p.ID) {
			c.Others = append(c.Others, api.Peer{Name: x.Name, Active: time.Since(x.LastSeenAt) < peerActive})
		}
	}
	return c, nil
}

// only this device's own use blocks it; other devices finish their work and remove it themselves
func (d *Daemon) removeDrive(ctx context.Context, r api.DriveRemove) (any, error) {
	m, ok := d.findMount(byID(r.ID))
	if !ok {
		return nil, fmt.Errorf("no drive with id %q", r.ID)
	}
	if d.leaving(r.ID) {
		return nil, &codedError{"removing", m.Name + " is being removed"}
	}
	d.opMu.Lock()
	defer d.opMu.Unlock()
	// checked under opMu, so nothing starts between the check and the removal
	switch u := d.usage(r.ID); {
	case u.State == usageBusy:
		return nil, &codedError{"in_use", fmt.Sprintf("%s is in use: %s", m.Name, u.Reason)}
	case u.State == usageStuck && !r.Anyway:
		return nil, &codedError{"stuck", u.Reason + ". Remove anyway to drop them."}
	}
	if err := d.serverFirst(func(c *dsync.Client) error { return notFoundOK(c.DeleteMount(ctx, r.ID)) }); err != nil {
		return nil, err
	}
	err := d.purge(m)
	d.event("drive.removed", m.ID, m.Name, "", true)
	return nil, err
}

func (d *Daemon) leaving(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.data.Local[id].Removing
}

// caller holds opMu; unmounts, then deletes the folder, the cache and the local state
func (d *Daemon) purge(m store.Mount) error {
	d.setProgress(m.ID, 0)
	defer func() {
		d.mu.Lock()
		delete(d.purging, m.ID)
		d.mu.Unlock()
		d.publish()
	}()
	if err := d.unmountLocked(m); err != nil {
		// something still holds it open; a removal goes through anyway
		d.mu.Lock()
		st := d.state(m.ID)
		point, engineID := cmp.Or(st.point, d.mountPoint(m)), cmp.Or(st.name, m.ID)
		d.mu.Unlock()
		if ferr := d.clearPoint(point); ferr != nil {
			return err
		}
		_ = d.clearEngine(engineID)
		d.mu.Lock()
		*st = mountState{state: stateUnmounted}
		d.markUnmounted(m.ID)
		d.mu.Unlock()
	}
	d.mu.Lock()
	point, ours := d.mountPoint(m), d.mountPoint(m) == d.defaultPoint(m.Name)
	all := slices.Clone(d.data.Mounts)
	d.mu.Unlock()
	if ours {
		engine.CleanupMountPoint(point)
	}
	d.setProgress(m.ID, 0.2)
	if err := d.clearDriveCache(m, all); err != nil {
		return err
	}
	d.mu.Lock()
	d.dropMount(m.ID)
	delete(d.unsent, m.ID)
	d.saves++
	err := d.save()
	d.mu.Unlock()
	d.setProgress(m.ID, 1)
	go d.reportDrives()
	return err
}

func (d *Daemon) setProgress(id string, p float64) {
	d.mu.Lock()
	d.purging[id] = p
	d.mu.Unlock()
	d.publish()
}

// a file another drive of the account also shows stays, whichever folder each drive shows
func (d *Daemon) clearDriveCache(m store.Mount, all []store.Mount) error {
	var mine []string
	for _, dir := range []string{"vfs", "vfsMeta"} {
		root := filepath.Join(d.cacheDir, dir)
		err := filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
			switch {
			case errors.Is(err, os.ErrNotExist):
				return nil
			case err != nil || e.IsDir():
				return err
			}
			rel, err := filepath.Rel(root, p)
			if rel = filepath.ToSlash(rel); err == nil && covers(m, rel) && !coveredByOther(m, all, rel) {
				mine = append(mine, p)
			}
			return err
		})
		if err != nil {
			return err
		}
	}
	var errs []error
	last := time.Now()
	for i, p := range mine {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
		if time.Since(last) > 200*time.Millisecond {
			last = time.Now()
			d.setProgress(m.ID, 0.2+0.75*float64(i+1)/float64(len(mine)))
		}
	}
	return errors.Join(errs...)
}

const (
	drainEvery = 5 * time.Second
	drainRetry = 30 * time.Second
)

// removed on another device: let this device's uploads and reads finish, then purge it here
func (d *Daemon) drain(id string) {
	d.mu.Lock()
	if d.draining[id] {
		d.mu.Unlock()
		return
	}
	d.draining[id] = true
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.draining, id)
		d.mu.Unlock()
	}()
	d.publish()
	for {
		m, ok := d.findMount(byID(id))
		if !ok || !d.leaving(id) || d.closed.Load() {
			return
		}
		wait := drainEvery
		// stuck uploads are dropped; real uploads, writes and reads are waited for
		if d.usageOf(id, true).State != usageBusy {
			d.opMu.Lock()
			if d.usageOf(id, true).State == usageBusy {
				d.opMu.Unlock()
				time.Sleep(wait)
				continue
			}
			err := d.purge(m)
			d.opMu.Unlock()
			if err == nil {
				d.log.Info("removed a drive removed on another device", "drive", m.Name)
				d.event("drive.removedOn", m.ID, m.Name, "", true)
				if _, used := d.findMount(func(x store.Mount) bool { return x.ServiceID == m.ServiceID }); !used {
					d.refetchAll()
				}
				return
			}
			d.log.Warn("remove drive", "drive", m.Name, "err", err)
			wait = drainRetry
		}
		time.Sleep(wait)
	}
}

// its account may be gone on the server too; the next sync fetches everything and decides
func (d *Daemon) refetchAll() {
	d.mu.Lock()
	if a := d.data.Account; a != nil && a.Rev != "" {
		cp := *a
		cp.Rev = ""
		d.data.Account = &cp
		if err := d.save(); err != nil {
			d.log.Error("save store", "err", err)
		}
	}
	d.mu.Unlock()
	d.kick()
}

// the other devices and what they have mounted, fetched only while the app is open to show it
func (d *Daemon) refreshPeers() {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if _, err := d.devices(ctx); err != nil {
		d.log.Warn("load devices", "err", err)
		return
	}
	d.publish()
}

// caller holds mu
func (d *Daemon) alsoOn(id string) []string {
	out := []string{}
	for _, x := range d.peers {
		if x.Current || !slices.Contains(x.Mounted, id) {
			continue
		}
		name := strings.TrimSuffix(x.Name, ".local")
		if time.Since(x.LastSeenAt) >= peerActive {
			name += " (offline)"
		}
		out = append(out, name)
	}
	return out
}
