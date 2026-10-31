package daemon

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/store"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
)

type codedError struct{ code, msg string }

func (e *codedError) Error() string { return e.msg }
func (e *codedError) Code() string  { return e.code }

func (d *Daemon) syncNow(ctx context.Context) {
	d.syncMu.Lock()
	defer d.syncMu.Unlock()
	d.mu.Lock()
	acct, saves := d.data.Account, d.saves
	d.mu.Unlock()
	if acct == nil || acct.Pending {
		return
	}
	snap, err := d.loop.Tick(ctx, client(acct), acct.Rev)
	if d.answer(acct, err) == nil && snap != nil {
		d.apply(acct, saves, *snap)
	}
	d.publish()
}

// a save made during the pull, or a token not yet sent, would be undone; the next pull brings both
func (d *Daemon) apply(acct *store.Account, saves uint64, snap dsync.Snapshot) {
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.mu.Lock()
	if cur := d.data.Account; cur == nil || cur.Token != acct.Token || d.saves != saves || d.loop.Pending() {
		d.mu.Unlock()
		return
	}
	var changes []dsync.Change
	d.data, changes = dsync.Apply(d.data, snap)
	for _, c := range changes {
		switch {
		case c.Kind == "removed" && c.MountID != "":
			delete(d.states, c.MountID)
		case c.NeedsRemount && d.state(c.MountID).state == stateMounted:
			d.state(c.MountID).remount = true
		}
	}
	services := d.data.Services
	d.setSecrets()
	err := d.save()
	d.mu.Unlock()
	if err != nil {
		d.log.Error("save store", "err", err)
	}
	for _, c := range changes {
		i := slices.IndexFunc(services, func(s store.Service) bool { return s.ID == c.ServiceID })
		switch {
		case c.MountID != "":
		case c.Kind == "removed":
			d.eng.DeleteService(c.ServiceID)
		case i >= 0:
			if err := d.eng.PutService(services[i]); err != nil {
				d.log.Error("load service", "service", services[i].Name, "err", err)
			}
		}
	}
}

func (d *Daemon) onToken(serviceID, token string) {
	d.mu.Lock()
	i := slices.IndexFunc(d.data.Services, func(s store.Service) bool { return s.ID == serviceID })
	if i < 0 {
		d.mu.Unlock()
		return
	}
	var t struct {
		Expiry time.Time `json:"expiry"`
	}
	if err := json.Unmarshal([]byte(token), &t); err != nil {
		d.mu.Unlock()
		d.log.Warn("refreshed token has no readable expiry, not saved", "service", serviceID)
		return
	}
	s := d.data.Services[i]
	s.Secrets = maps.Clone(s.Secrets)
	if s.Secrets == nil {
		s.Secrets = map[string]string{}
	}
	s.Secrets["token"] = token
	// copy on write: callers range over the old slice without mu
	d.data.Services = slices.Clone(d.data.Services)
	d.data.Services[i] = s
	d.setSecrets()
	err := d.save()
	acct := d.data.Account
	if acct != nil {
		d.loop.Token(serviceID, token, t.Expiry)
	}
	d.mu.Unlock()
	if err != nil {
		d.log.Error("save store", "err", err)
	}
	// the pull right after brings the version the write-back bumped, so the next edit isn't a conflict
	if acct != nil && !acct.Pending {
		go d.syncNow(context.Background())
	}
}

// caller holds opMu
func (d *Daemon) serverFirst(save func(*dsync.Client) error) error {
	d.mu.Lock()
	acct := d.data.Account
	d.mu.Unlock()
	if acct == nil {
		return nil
	}
	if acct.Pending {
		return &codedError{"login_pending", "finish logging in first: run dokky login"}
	}
	return d.checked(acct, save(client(acct)))
}

// caller holds opMu
func (d *Daemon) checked(acct *store.Account, err error) error {
	if d.note(err) {
		d.wipeLocked(acct)
	}
	return coded(err)
}

// caller must not hold opMu
func (d *Daemon) answer(acct *store.Account, err error) error {
	if d.note(err) {
		d.opMu.Lock()
		d.wipeLocked(acct)
		d.opMu.Unlock()
	}
	return coded(err)
}

// true means the server revoked this device and it must be wiped
func (d *Daemon) note(err error) bool {
	d.mu.Lock()
	d.offline, d.signedOut = dsync.Offline(err), errors.Is(err, dsync.ErrUnauthorized)
	d.mu.Unlock()
	switch {
	case err == nil:
	case errors.Is(err, dsync.ErrRevoked):
		d.log.Warn("this device was removed from the account, wiping it")
		return true
	case errors.Is(err, dsync.ErrUpgrade):
		d.log.Warn("the dokky server needs a newer version, checking for one", "err", err)
		d.upd.upgrade.Store(true)
	default:
		d.log.Warn("dokky server", "err", err)
	}
	return false
}

func coded(err error) error {
	var ce *dsync.ConflictError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &ce):
		return &codedError{"conflict", withoutSecrets(ce.Current)}
	case errors.Is(err, dsync.ErrRevoked):
		return &codedError{"revoked", "this device was removed from your dokky account, so dokky wiped it"}
	case errors.Is(err, dsync.ErrUnauthorized):
		return &codedError{"signed_out", "signed out on the server: run dokky logout, then dokky login"}
	case errors.Is(err, dsync.ErrUpgrade):
		return &codedError{"upgrade_required", "the dokky server needs a newer dokky"}
	case errors.Is(err, dsync.ErrNameTaken):
		return &codedError{"name_taken", "that name is already used in your dokky account"}
	case dsync.Offline(err):
		return &codedError{"offline", "could not reach the dokky server, nothing was changed; try again when online: " + err.Error()}
	}
	return err
}

// the newer item is shown in the TUI, so secret values stay in the daemon
func withoutSecrets(raw json.RawMessage) string {
	var item map[string]any
	if json.Unmarshal(raw, &item) != nil {
		return "{}"
	}
	if secrets, ok := item["secrets"].(map[string]any); ok {
		for k := range secrets {
			secrets[k] = ""
		}
	}
	b, _ := json.Marshal(item)
	return string(b)
}

func notFoundOK(err error) error {
	if errors.Is(err, dsync.ErrNotFound) {
		return nil
	}
	return err
}

// caller holds opMu; a revoked device unmounts at once and keeps nothing, unsent uploads included
func (d *Daemon) wipeLocked(acct *store.Account) {
	d.mu.Lock()
	cur, mounts := d.data.Account, slices.Clone(d.data.Mounts)
	d.mu.Unlock()
	if cur == nil || cur.Token != acct.Token {
		return
	}
	for _, m := range mounts {
		if err := d.release(m); err != nil {
			d.mu.Lock()
			point := cmp.Or(d.state(m.ID).point, d.mountPoint(m))
			d.mu.Unlock()
			if ferr := d.clearPoint(point); ferr != nil {
				d.log.Error("unmount for wipe", "name", m.Name, "err", errors.Join(err, ferr))
				continue
			}
			engine.CleanupMountPoint(point)
		}
	}
	_ = d.clearCache()
	if err := d.forget(); err != nil {
		d.log.Error("delete store", "err", err)
	}
	d.log.Warn("wiped this device")
}

func (d *Daemon) clearCache() error {
	var errs []error
	for _, dir := range []string{"vfs", "vfsMeta"} {
		if err := os.RemoveAll(filepath.Join(d.cacheDir, dir)); err != nil {
			d.log.Warn("remove cache", "err", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// caller holds opMu; leaves the device like a fresh install
func (d *Daemon) forget() error {
	d.mu.Lock()
	services := d.data.Services
	err := errors.Join(d.st.Delete(), writePin(d.upd.paths.State, ""))
	d.data = store.Fresh()
	d.states, d.edits = map[string]*mountState{}, map[string]string{}
	d.flow, d.clashes = nil, nil
	d.offline, d.signedOut = false, false
	d.loop.Reset()
	d.setSecrets()
	d.mu.Unlock()
	for _, s := range services {
		d.eng.DeleteService(s.ID)
	}
	d.publish()
	return err
}

func (d *Daemon) devices(ctx context.Context) ([]dsync.Device, error) {
	acct, err := d.account()
	if err != nil {
		return nil, err
	}
	list, err := client(acct).Devices(ctx)
	return list, d.answer(acct, err)
}

func (d *Daemon) revokeDevice(ctx context.Context, id string) error {
	acct, err := d.account()
	if err != nil {
		return err
	}
	if id == acct.DeviceID {
		id = "self"
	}
	if err := d.answer(acct, client(acct).Revoke(ctx, id)); err != nil {
		return err
	}
	if id == "self" {
		d.opMu.Lock()
		d.wipeLocked(acct)
		d.opMu.Unlock()
	}
	return nil
}

func (d *Daemon) account() (*store.Account, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.data.Account == nil {
		return nil, errors.New("not logged in")
	}
	return d.data.Account, nil
}
