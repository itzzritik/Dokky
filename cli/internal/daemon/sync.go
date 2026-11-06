package daemon

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/store"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
)

type codedError struct{ code, msg string }

func (e *codedError) Error() string { return e.msg }
func (e *codedError) Code() string  { return e.code }

func (d *Daemon) syncNow(ctx context.Context) error {
	d.syncMu.Lock()
	defer d.syncMu.Unlock()
	d.mu.Lock()
	acct, saves := d.data.Account, d.saves
	d.mu.Unlock()
	if acct == nil || acct.Pending {
		return nil
	}
	snap, err := d.loop.Tick(ctx, client(acct), acct.Rev)
	err = d.answer(acct, err)
	if err == nil {
		d.synced(ctx, acct)
		if snap != nil {
			d.apply(acct, saves, *snap)
		}
		if d.reportDue.Load() {
			go d.reportDrives()
		}
		go d.flushEvents()
		d.publishEvents()
		if d.srv != nil && d.srv.Watching() {
			go d.refreshPeers()
		}
	}
	d.publish()
	return err
}

func (d *Daemon) synced(ctx context.Context, acct *store.Account) {
	var me dsync.Me
	if acct.Name == "" && acct.Email == "" {
		var err error
		if me, err = client(acct).Me(ctx); err != nil {
			d.log.Warn("read account details", "err", err)
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	cur := d.data.Account
	if cur == nil || cur.Token != acct.Token {
		return
	}
	a := *cur
	a.LastSyncAt = time.Now()
	fetched := me != (dsync.Me{})
	if fetched {
		a.Name, a.Email = me.Name, me.Email
	}
	d.data.Account = &a
	if !fetched {
		return
	}
	if err := d.save(); err != nil {
		d.log.Error("save store", "err", err)
	}
	go d.reportDrives()
}

func (d *Daemon) reportDrives() {
	d.reportMu.Lock()
	defer d.reportMu.Unlock()
	d.mu.Lock()
	acct, ids := d.data.Account, d.mountedIDs()
	d.mu.Unlock()
	if acct == nil || acct.Pending {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	err := client(acct).ReportDrives(ctx, ids)
	d.reportDue.Store(err != nil)
	if err == nil {
		d.mu.Lock()
		d.reported = strings.Join(ids, ",")
		d.mu.Unlock()
	}
	if err != nil {
		d.log.Warn("report drives", "err", err)
	}
}

// a save made during the pull, or a token not yet sent, would be undone; the next pull brings both
func (d *Daemon) apply(acct *store.Account, saves uint64, snap dsync.Snapshot) {
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.mu.Lock()
	if cur := d.data.Account; cur == nil || cur.Token != acct.Token || d.saves != saves || d.loop.Pending() {
		d.mu.Unlock()
		// a save raced this pull; pull again rather than wait for the next timer
		d.kick()
		return
	}
	var changes []dsync.Change
	d.data, changes = dsync.Apply(d.data, snap)
	for _, c := range changes {
		switch {
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
		case c.Kind == "removing":
			go d.drain(c.MountID)
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
		go func() { _ = d.syncNow(context.Background()) }()
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
	d.releaseAll(mounts)
	_ = d.clearCache()
	if err := d.forget(); err != nil {
		d.log.Error("delete store", "err", err)
	}
	d.log.Warn("wiped this device")
}

// caller holds opMu
func (d *Daemon) releaseAll(mounts []store.Mount) {
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
	if err == nil {
		d.mu.Lock()
		d.peers = list
		d.mu.Unlock()
	}
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

func (d *Daemon) renameDevice(ctx context.Context, id, name string) error {
	acct, err := d.account()
	if err != nil {
		return err
	}
	if name = strings.TrimSpace(name); name == "" || utf8.RuneCountInString(name) > maxDeviceName {
		return fmt.Errorf("a computer name must be 1 to %d characters", maxDeviceName)
	}
	if id == "" || id == acct.DeviceID {
		id = "self"
	}
	return d.answer(acct, client(acct).RenameDevice(ctx, id, name))
}

func (d *Daemon) account() (*store.Account, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.data.Account == nil {
		return nil, errors.New("not logged in")
	}
	return d.data.Account, nil
}

// caller holds mu; what the server shows as mounted here, so ejects and failed mounts count too
func (d *Daemon) mountedIDs() []string {
	ids := []string{}
	for id, st := range d.states {
		if st.state == stateMounted {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// a request only when what is mounted changed since the last report
func (d *Daemon) reportIfChanged() {
	d.mu.Lock()
	changed := d.data.Account != nil && strings.Join(d.mountedIDs(), ",") != d.reported
	d.mu.Unlock()
	if changed && !d.reportDue.Load() {
		go d.reportDrives()
	}
}
