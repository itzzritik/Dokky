package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/platform"
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
		return loginStartReply{}, errors.New("already logged in, sign out in dokky first")
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
	d.vk.op.Lock()
	defer d.vk.op.Unlock()
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.mu.Lock()
	flow, acct := d.flow, d.data.Account
	d.mu.Unlock()
	if acct == nil {
		if flow == nil {
			return loginPollReply{}, errors.New("no login in progress, sign in from dokky")
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
	// the vault comes first: everything this upload sends is sealed with the account's key
	if f := d.vk.current(); f == nil || !f.Account {
		need, err := d.link(ctx, acct)
		if err != nil {
			return loginPollReply{}, d.checked(acct, err)
		}
		if need != "" {
			d.vk.setNeed(need)
			d.publish()
			return loginPollReply{Status: statusVault}, nil
		}
	}

	d.mu.Lock()
	data := d.data
	d.mu.Unlock()
	out, clashes, err := dsync.Upload(ctx, d.client(acct), data)
	if errors.Is(err, dsync.ErrRevoked) {
		return loginPollReply{}, d.checked(acct, err)
	}
	// another device made the account's vault in between
	if errors.Is(err, dsync.ErrKeyMismatch) || errors.Is(err, dsync.ErrNoVault) {
		_ = d.checked(acct, err)
		d.vk.setNeed(api.NeedJoin)
		d.publish()
		return loginPollReply{Status: statusVault}, nil
	}
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
	out, more, err := dsync.Resolve(ctx, d.client(data.Account), data, cl, p.Keep)
	if errors.Is(err, dsync.ErrRevoked) {
		return resolveReply{}, d.checked(data.Account, err)
	}
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
	if err == nil {
		err = d.refuseUnsent(ms, "and let the uploads finish, then resolve again")
	}
	return err
}

func (d *Daemon) refuseUnsent(ms []store.Mount, then string) error {
	counts, err := d.unsentFiles(ms)
	if err != nil {
		return fmt.Errorf("could not check for unsent uploads: %w", err)
	}
	if len(counts) == 0 {
		return nil
	}
	n := 0
	for _, c := range counts {
		n += c
	}
	names := strings.Join(slices.Sorted(maps.Keys(counts)), ", ")
	return fmt.Errorf("%d files on %s are not uploaded yet: mount %s %s", n, names, names, then)
}

func (d *Daemon) refuseUnsentOf(m store.Mount) error {
	d.mu.Lock()
	ms := slices.Clone(d.data.Mounts)
	d.mu.Unlock()
	counts, err := d.unsentFiles(ms)
	if err != nil {
		return fmt.Errorf("could not check for unsent uploads: %w", err)
	}
	if n := counts[m.Name]; n > 0 {
		return &codedError{"unsent", fmt.Sprintf("%d files on %s are not uploaded yet; deleting drops them. Mount %s to let them finish.", n, m.Name, m.Name)}
	}
	return nil
}

// an unmount pauses uploads: rclone keeps each file Dirty in vfsMeta and resumes it on the next mount
func (d *Daemon) unsentFiles(ms []store.Mount) (map[string]int, error) {
	root := filepath.Join(d.cacheDir, "vfsMeta")
	counts := map[string]int{}
	err := filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		switch {
		case errors.Is(err, os.ErrNotExist):
			return nil
		case err != nil || e.IsDir():
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		name := metaOwner(ms, filepath.ToSlash(rel))
		if name == "" {
			return nil
		}
		b, err := os.ReadFile(p)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		var info struct{ Dirty bool }
		if json.Unmarshal(b, &info) == nil && info.Dirty {
			counts[name]++
		}
		return nil
	})
	return counts, err
}

// rel is <service id>/<remote path>/<file>; the longest matching remote path wins, else any mount of the service
func metaOwner(ms []store.Mount, rel string) string {
	svc, file, _ := strings.Cut(rel, "/")
	owner, best := "", -1
	for _, m := range ms {
		if m.ServiceID != svc {
			continue
		}
		score := 0
		if root := strings.Trim(path.Clean("/"+m.RemotePath), "/"); root == "" || strings.HasPrefix(file, root+"/") {
			score = len(root) + 1
		}
		if score > best {
			owner, best = m.Name, score
		}
	}
	return owner
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

// loginCancel drops a sign in still waiting on its master password step. The server forgets this device; nothing
// here changes, the drives and a master password made before the sign in included, since nothing synced yet
func (d *Daemon) loginCancel(ctx context.Context) error {
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.mu.Lock()
	acct, clashing := d.data.Account, len(d.clashes) > 0
	d.mu.Unlock()
	switch {
	case acct == nil || !acct.Pending:
		return errors.New("no sign in is waiting")
	case clashing:
		return errors.New("choose which copies to keep first")
	}
	// a device the server keeps only shows in the account's list until it is removed there
	if err := d.client(acct).Revoke(ctx, "self", "remove"); err != nil && !errors.Is(err, dsync.ErrRevoked) && !errors.Is(err, dsync.ErrUnauthorized) {
		d.log.Warn("cancel sign in: the server still lists this device", "err", err)
	}
	d.mu.Lock()
	d.data.Account, d.flow = nil, nil
	d.setSecrets()
	err := d.save()
	d.mu.Unlock()
	d.vk.setNeed("")
	d.publish()
	d.log.Info("sign in cancelled")
	return err
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

// the server records the removal first; then this device leaves like any other removed device
func (d *Daemon) logout(ctx context.Context, p logoutParams) (logoutReply, error) {
	d.mu.Lock()
	acct, r := d.data.Account, d.data.Removal
	d.mu.Unlock()
	if acct == nil {
		return logoutReply{}, errors.New("not logged in")
	}
	mode := "remove"
	if p.Force {
		mode = "erase"
	}
	if r == nil {
		err := d.client(acct).Revoke(ctx, "self", mode)
		if errors.Is(err, dsync.ErrRevoked) {
			mode = stronger(mode, dsync.RevokedMode(err))
		}
		// already removed, or a login the server never finished: this device leaves on its own
		if err != nil && !errors.Is(err, dsync.ErrRevoked) && !errors.Is(err, dsync.ErrUnauthorized) {
			return logoutReply{}, d.answer(acct, err)
		}
	}
	// the person signed out, so this computer keeps its master password
	d.mu.Lock()
	d.keepVault = true
	d.mu.Unlock()
	d.startLeaving(mode)
	for d.loggedIn() {
		select {
		case <-ctx.Done():
			return logoutReply{Warning: "Removal continues in the background"}, nil
		case <-time.After(uploadWait):
		}
	}
	d.log.Info("logged out")
	return logoutReply{}, nil
}

func (d *Daemon) loggedIn() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.data.Account != nil
}

// once the vault is unlocked the client seals drive logins and says it understands vaults
func (d *Daemon) client(a *store.Account) *dsync.Client {
	c := &dsync.Client{Base: a.APIBase, Token: a.Token}
	if d.vk != nil {
		c.Sealer = d.vk.sealer()
	}
	return c
}

func deviceInfo() dsync.DeviceInfo {
	name := strings.TrimSpace(platform.ComputerName())
	if name == "" {
		name = "dokky"
	}
	if len(name) > maxDeviceName {
		name = strings.ToValidUTF8(name[:maxDeviceName], "")
	}
	return dsync.DeviceInfo{DeviceName: name, OS: runtime.GOOS, Arch: runtime.GOARCH, Version: buildinfo.Version}
}
