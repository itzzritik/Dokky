package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/store"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
)

const maxDeviceName = 64

var uploadWait = time.Second

type loginFlow struct {
	base, deviceCode string
}

type loginStartReply struct {
	UserCode  string `json:"userCode"`
	VerifyURL string `json:"verifyUrl"`
	Interval  int    `json:"interval"`
	Resume    bool   `json:"resume"`
}

type loginPollReply struct {
	Status  string        `json:"status"`
	Clashes []dsync.Clash `json:"clashes"`
}

type resolveParams struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Keep string `json:"keep"`
}

type resolveReply struct {
	Clashes []dsync.Clash `json:"clashes"`
}

type logoutParams struct {
	Force bool `json:"force"`
}

type logoutReply struct {
	Warning string `json:"warning"`
}

func (d *Daemon) loginStart(ctx context.Context) (loginStartReply, error) {
	d.mu.Lock()
	acct := d.data.Account
	d.mu.Unlock()
	if acct != nil {
		if acct.Pending {
			return loginStartReply{Resume: true}, nil
		}
		return loginStartReply{}, errors.New("already logged in, run dokky logout first")
	}
	base := dsync.APIBase()
	r, err := (&dsync.Client{Base: base}).LoginStart(ctx, deviceInfo())
	if err != nil {
		return loginStartReply{}, err
	}
	d.mu.Lock()
	d.flow = &loginFlow{base: base, deviceCode: r.DeviceCode}
	d.setSecrets()
	d.mu.Unlock()
	if strings.HasPrefix(r.VerifyURL, "https://") || strings.HasPrefix(r.VerifyURL, "http://") {
		go func() {
			if err := d.openURL(r.VerifyURL); err != nil {
				d.log.Warn("open browser", "err", err)
			}
		}()
	}
	return loginStartReply{UserCode: r.UserCode, VerifyURL: r.VerifyURL, Interval: r.Interval}, nil
}

func (d *Daemon) loginPoll(ctx context.Context) (loginPollReply, error) {
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.mu.Lock()
	flow, acct := d.flow, d.data.Account
	d.mu.Unlock()
	if acct == nil {
		if flow == nil {
			return loginPollReply{}, errors.New("no login in progress, run dokky login")
		}
		r, err := (&dsync.Client{Base: flow.base}).LoginPoll(ctx, flow.deviceCode)
		if err != nil {
			return loginPollReply{}, err
		}
		switch r.Status {
		case "pending":
			return loginPollReply{Status: r.Status}, nil
		case "denied", "expired":
			d.mu.Lock()
			d.flow = nil
			d.setSecrets()
			d.mu.Unlock()
			return loginPollReply{Status: r.Status}, nil
		case "approved":
			if r.Token == "" || r.DeviceID == "" {
				return loginPollReply{}, errors.New("the server approved the login without a device token")
			}
		default:
			return loginPollReply{}, fmt.Errorf("unexpected login status %q", r.Status)
		}
		acct = &store.Account{APIBase: flow.base, DeviceID: r.DeviceID, Token: r.Token, Pending: true}
		d.mu.Lock()
		d.flow = nil
		d.data.Account = acct
		d.setSecrets()
		err = d.save()
		d.mu.Unlock()
		if err != nil {
			return loginPollReply{}, err
		}
		d.log.Info("logged in", "device", r.DeviceID)
	}
	if !acct.Pending {
		return loginPollReply{Status: "approved"}, nil
	}

	d.mu.Lock()
	data := d.data
	d.mu.Unlock()
	out, clashes, err := dsync.Upload(ctx, client(acct), data)
	if err != nil {
		clashes = nil
	}
	d.mu.Lock()
	d.data.Services, d.data.Mounts = out.Services, out.Mounts
	d.clashes = clashes
	serr := d.finishLogin(err == nil)
	d.mu.Unlock()
	for _, s := range out.Services {
		if perr := d.eng.PutService(s); perr != nil {
			d.log.Error("load service", "service", s.Name, "err", perr)
		}
	}
	d.publish()
	if err = errors.Join(err, serr); err != nil {
		return loginPollReply{}, err
	}
	return loginPollReply{Status: "approved", Clashes: clashes}, nil
}

func (d *Daemon) loginResolve(ctx context.Context, p resolveParams) (resolveReply, error) {
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.mu.Lock()
	data, i := d.data, slices.IndexFunc(d.clashes, func(c dsync.Clash) bool { return c.Kind == p.Kind && c.Name == p.Name })
	var cl dsync.Clash
	if i >= 0 {
		cl = d.clashes[i]
	}
	d.mu.Unlock()
	if data.Account == nil || !data.Account.Pending || i < 0 {
		return resolveReply{}, fmt.Errorf("no %s named %q is waiting to be resolved", p.Kind, p.Name)
	}
	var moving []store.Mount
	for _, m := range data.Mounts {
		if (cl.Kind == dsync.KindService && m.ServiceID == cl.LocalID) || (cl.Kind == dsync.KindMount && p.Keep == dsync.KeepAccount && m.ID == cl.LocalID) {
			moving = append(moving, m)
		}
	}
	if err := d.uploadsIdle(moving); err != nil {
		return resolveReply{}, err
	}
	out, more, err := dsync.Resolve(ctx, client(data.Account), data, cl, p.Keep)
	if err != nil {
		return resolveReply{}, err
	}
	// a file closed during the network step queues under the old service too
	if err := d.uploadsIdle(moving); err != nil {
		return resolveReply{}, err
	}
	d.mu.Lock()
	local, hadLocal := d.data.Local[cl.LocalID]
	d.mu.Unlock()
	for _, m := range moving {
		if err := d.release(m); err != nil {
			return resolveReply{}, err
		}
	}

	d.mu.Lock()
	d.data.Services, d.data.Mounts = out.Services, out.Mounts
	if cl.Kind == dsync.KindMount {
		if hadLocal {
			d.data.Local[cl.RemoteID] = local
		}
		if st, ok := d.states[cl.LocalID]; ok && p.Keep == dsync.KeepLocal {
			d.states[cl.RemoteID] = st
		}
		delete(d.data.Local, cl.LocalID)
		delete(d.states, cl.LocalID)
	}
	d.clashes = append(slices.Delete(slices.Clone(d.clashes), i, i+1), more...)
	reply := resolveReply{Clashes: slices.Clone(d.clashes)}
	err = d.finishLogin(true)
	d.mu.Unlock()

	for _, s := range out.Services {
		if !slices.ContainsFunc(data.Services, func(o store.Service) bool { return o.ID == s.ID }) {
			if perr := d.eng.PutService(s); perr != nil {
				d.log.Error("load service", "service", s.Name, "err", perr)
			}
		}
	}
	for _, o := range data.Services {
		if !slices.ContainsFunc(out.Services, func(s store.Service) bool { return s.ID == o.ID }) {
			d.eng.DeleteService(o.ID)
		}
	}
	d.publish()
	return reply, err
}

// rclone keys the upload queue by service id, so moving a busy mount strands it; caller holds opMu
func (d *Daemon) uploadsIdle(ms []store.Mount) error {
	name, n, err := d.busyMount(ms)
	if err == nil && n > 0 {
		err = fmt.Errorf("%d uploads on %s still running; resolve again when they finish", n, name)
	}
	return err
}

// fails closed: stale counts must never pass as idle; caller holds opMu
func (d *Daemon) busyMount(ms []store.Mount) (string, int, error) {
	if err := d.refreshStats(); err != nil {
		return "", 0, fmt.Errorf("could not check uploads: %w", err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, m := range ms {
		if st := d.state(m.ID); st.state == stateMounted {
			if n := d.stats[st.name].UploadsQueued + d.stats[st.name].UploadsInProgress; n > 0 {
				return m.Name, n, nil
			}
		}
	}
	return "", 0, nil
}

// caller holds mu
func (d *Daemon) finishLogin(uploaded bool) error {
	if uploaded && len(d.clashes) == 0 && d.data.Account != nil && d.data.Account.Pending {
		a := *d.data.Account
		a.Pending = false
		d.data.Account = &a
	}
	d.setSecrets()
	return d.save()
}

func (d *Daemon) logout(ctx context.Context, p logoutParams) (logoutReply, error) {
	if !d.loggedIn() {
		return logoutReply{}, errors.New("not logged in")
	}
	for {
		if !p.Force {
			if err := d.waitUploads(ctx); err != nil {
				return logoutReply{}, err
			}
		}
		d.opMu.Lock()
		if p.Force {
			break
		}
		// an upload queued while waiting for opMu would be deleted with the cache
		d.mu.Lock()
		all := slices.Clone(d.data.Mounts)
		d.mu.Unlock()
		_, n, err := d.busyMount(all)
		if err != nil {
			d.opMu.Unlock()
			return logoutReply{}, fmt.Errorf("%w; nothing was removed", err)
		}
		if n == 0 {
			break
		}
		d.opMu.Unlock()
	}
	defer d.opMu.Unlock()
	d.mu.Lock()
	acct, mounts := d.data.Account, slices.Clone(d.data.Mounts)
	d.mu.Unlock()
	if acct == nil {
		return logoutReply{}, errors.New("not logged in")
	}
	for _, m := range mounts {
		if err := d.release(m); err != nil {
			return logoutReply{}, fmt.Errorf("could not unmount %s, close its open files and try again: %w", m.Name, err)
		}
	}

	var warnings []string
	for _, dir := range []string{"vfs", "vfsMeta"} {
		if err := os.RemoveAll(filepath.Join(d.cacheDir, dir)); err != nil {
			d.log.Warn("remove cache", "err", err)
			warnings = append(warnings, fmt.Sprintf("could not remove the cache: %v", err))
		}
	}
	// the device is wiped even when the caller is gone or offline
	if err := client(acct).Revoke(context.WithoutCancel(ctx), "self"); err != nil && !errors.Is(err, dsync.ErrRevoked) && !errors.Is(err, dsync.ErrUnauthorized) {
		d.log.Warn("revoke device", "err", err)
		warnings = append(warnings, fmt.Sprintf("could not reach dokky to remove this device, remove it at %s/devices", acct.APIBase))
	}
	reply := logoutReply{Warning: strings.Join(warnings, "\n")}

	d.mu.Lock()
	services := d.data.Services
	err := d.st.Delete()
	d.data = store.Data{Local: map[string]store.LocalMount{}}
	d.states, d.edits = map[string]*mountState{}, map[string]string{}
	d.flow, d.clashes = nil, nil
	d.setSecrets()
	d.mu.Unlock()
	for _, s := range services {
		d.eng.DeleteService(s.ID)
	}
	d.log.Info("logged out")
	d.publish()
	return reply, err
}

func (d *Daemon) waitUploads(ctx context.Context) error {
	for {
		n := 0
		for _, m := range d.status().Mounts {
			n += m.UploadsQueued + m.UploadsInProgress
		}
		if n == 0 {
			return nil
		}
		d.publish()
		select {
		case <-ctx.Done():
			return fmt.Errorf("logout stopped with %d uploads still waiting", n)
		case <-time.After(uploadWait):
		}
	}
}

func (d *Daemon) loggedIn() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.data.Account != nil
}

func client(a *store.Account) *dsync.Client {
	return &dsync.Client{Base: a.APIBase, Token: a.Token}
}

func deviceInfo() dsync.DeviceInfo {
	name, _ := os.Hostname()
	name = strings.TrimSpace(name)
	if name == "" {
		name = "dokky"
	}
	if len(name) > maxDeviceName {
		name = name[:maxDeviceName]
	}
	return dsync.DeviceInfo{DeviceName: name, OS: runtime.GOOS, Arch: runtime.GOARCH, Version: buildinfo.Version}
}
