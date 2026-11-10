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

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/store"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
)

type codedError struct{ code, msg string }

func (e *codedError) Error() string { return e.msg }
func (e *codedError) Code() string  { return e.code }

// syncNow pulls, and pushes refreshed tokens, once the vault is unlocked and linked to the account
func (d *Daemon) syncNow(ctx context.Context) error {
	acct := d.acct()
	if acct == nil || acct.Pending || d.closed.Load() {
		return nil
	}
	if err := d.ready(ctx, acct); err != nil {
		d.publish()
		return err
	}
	if !d.canSync() {
		d.publish()
		return nil
	}
	ref, err := d.pull(ctx)
	if err == nil {
		d.follow(ctx, acct, ref)
	}
	return err
}

// ready links an unlinked vault, or in setup learns whether the account already has one
func (d *Daemon) ready(ctx context.Context, acct *store.Account) error {
	k := d.vk
	f := k.current()
	if k.isPaused() || k.state() == api.VaultLocked || (f != nil && f.Account) {
		return nil
	}
	k.op.Lock()
	defer k.op.Unlock()
	need, err := d.link(ctx, acct)
	if err != nil {
		return d.checked(acct, err)
	}
	k.setNeed(need)
	return nil
}

func (d *Daemon) canSync() bool {
	k := d.vk
	f := k.current()
	return k.state() == api.VaultUnlocked && f != nil && f.Account && !k.isPaused()
}

// caller holds mu; why a signed-in device does not sync, "" when it does
func (d *Daemon) syncBlocked() string {
	k := d.vk
	f := k.current()
	switch {
	case k.state() == api.VaultSetup:
		return "Sync waits for a master password: open dokky to create one."
	case k.state() == api.VaultLocked:
		return "Sync waits for dokky to be unlocked."
	case k.isPaused():
		return "Sync waits for your account's master password: open dokky to enter it."
	case !f.Account:
		return "Sync waits to link this computer's master password to your account."
	}
	return ""
}

func (d *Daemon) pull(ctx context.Context) (*dsync.VaultRef, error) {
	d.syncMu.Lock()
	defer d.syncMu.Unlock()
	d.mu.Lock()
	acct, saves := d.data.Account, d.saves
	d.mu.Unlock()
	if acct == nil || acct.Pending {
		return nil, nil
	}
	c := d.client(acct)
	if c.Sealer == nil {
		return nil, nil
	}
	snap, err := d.loop.Tick(ctx, c, acct.Rev)
	if d.vaultTrouble(err) {
		d.note(nil)
		d.publish()
		return nil, nil
	}
	err = d.answer(acct, err)
	var ref *dsync.VaultRef
	if err == nil {
		d.synced(ctx, acct)
		if snap != nil {
			ref = snap.Vault
			d.vk.saw(snap.Vault, len(snap.Legacy))
			d.apply(acct, saves, *snap)
			if len(snap.Legacy) > 0 {
				d.reseal(ctx, c, *snap)
			}
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
	return ref, err
}

// vaultTrouble pauses sync on another key and relinks when the account lost its vault; true when it was one
func (d *Daemon) vaultTrouble(err error) bool {
	k := d.vk
	switch {
	case errors.Is(err, dsync.ErrKeyMismatch):
		k.pause(err)
		return true
	case errors.Is(err, dsync.ErrNoVault):
		// the next sync links it again; a vault change running now decides for itself
		if k.op.TryLock() {
			if f := k.current(); f != nil && f.Account {
				f.Account = false
				if serr := k.save(f); serr != nil {
					d.log.Error("save vault.json", "err", serr)
				}
			}
			k.op.Unlock()
		}
		d.log.Warn("the account has no vault, linking this computer's again", "err", err)
		d.kick()
		return true
	case errors.Is(err, dsync.ErrUpdateRequired):
		d.log.Error("the server asked a vault-aware dokky to update, which should not happen", "err", err)
	}
	return false
}

// reseal seals each service the server still holds with its own key and saves it back: the whole migration.
// one that fails waits for the next pull
func (d *Daemon) reseal(ctx context.Context, c *dsync.Client, snap dsync.Snapshot) {
	left := 0
	for _, id := range snap.Legacy {
		i := slices.IndexFunc(snap.Services, func(s store.Service) bool { return s.ID == id })
		if i < 0 {
			continue
		}
		s := snap.Services[i]
		d.opMu.Lock()
		saved, err := dsync.SaveService(ctx, c, s)
		if err == nil {
			d.mu.Lock()
			if j := slices.IndexFunc(d.data.Services, func(x store.Service) bool { return x.ID == id }); j >= 0 && d.data.Services[j].Version == s.Version {
				d.data.Services = slices.Clone(d.data.Services)
				d.data.Services[j] = saved
				err = d.save()
			}
			d.mu.Unlock()
		}
		d.opMu.Unlock()
		if err != nil {
			left++
			d.vaultTrouble(err)
			d.log.Warn("seal a drive login for the account", "service", s.Name, "err", err)
		}
	}
	d.vk.setLegacy(left)
	if left < len(snap.Legacy) {
		d.log.Info("sealed drive logins with the account key", "count", len(snap.Legacy)-left)
	}
}

// follow takes the account's new wraps when its vault changed: the master password changed on another device
func (d *Daemon) follow(ctx context.Context, acct *store.Account, ref *dsync.VaultRef) {
	k := d.vk
	if !k.versionChanged(ref) || !k.op.TryLock() {
		return
	}
	defer k.op.Unlock()
	remote, err := d.client(acct).GetVault(ctx)
	if err != nil {
		d.log.Warn("read the account's vault", "err", err)
		return
	}
	f := k.current()
	if f == nil || remote.KeyID != f.KeyID {
		return
	}
	if nf, changed := followRemote(f, remote.Remote); changed {
		if err := k.save(nf); err != nil {
			d.log.Error("save vault.json", "err", err)
			return
		}
		d.log.Info("the master password changed on another device")
		k.endSession("the master password changed on another device")
		d.publish()
	}
	k.setServerVersion(remote.Version)
}

func (d *Daemon) synced(ctx context.Context, acct *store.Account) {
	var me dsync.Me
	if acct.Name == "" && acct.Email == "" {
		var err error
		if me, err = d.client(acct).Me(ctx); err != nil {
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
	err := d.client(acct).ReportDrives(ctx, ids)
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
	d.mu.Lock()
	why := d.syncBlocked()
	d.mu.Unlock()
	if why != "" {
		d.kick()
		return &codedError{"vault", why}
	}
	return d.checked(acct, save(d.client(acct)))
}

func (d *Daemon) checked(acct *store.Account, err error) error {
	if d.vaultTrouble(err) {
		d.note(nil)
		d.publish()
		return coded(err)
	}
	if d.note(err) {
		d.removedBy(acct, err)
	}
	return coded(err)
}

func (d *Daemon) answer(acct *store.Account, err error) error {
	return d.checked(acct, err)
}

// true means the server removed this device and it must leave
func (d *Daemon) note(err error) bool {
	d.mu.Lock()
	d.offline, d.signedOut = dsync.Offline(err), errors.Is(err, dsync.ErrUnauthorized)
	d.mu.Unlock()
	switch {
	case err == nil:
	case errors.Is(err, dsync.ErrRevoked):
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
		return &codedError{"revoked", "This device was removed from your dokky account"}
	case errors.Is(err, dsync.ErrUnauthorized):
		return &codedError{"signed_out", "signed out on the server: run dokky logout, then dokky login"}
	case errors.Is(err, dsync.ErrNameTaken):
		return &codedError{"name_taken", "that name is already used in your dokky account"}
	case errors.Is(err, dsync.ErrKeyMismatch):
		return &codedError{"key_mismatch", "This computer's key is not your account's. Enter your account's master password in dokky."}
	case errors.Is(err, dsync.ErrNoVault):
		return &codedError{"no_vault", "Your dokky account has no master password yet; dokky links this computer's when it next syncs."}
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

// caller holds opMu; leaves the device like a fresh install. a device the person signed out keeps its master
// password, unlinked; a removed one, or an uninstall, forgets the vault, the device key and the store
func (d *Daemon) forget() error {
	d.mu.Lock()
	services := d.data.Services
	keep := d.keepVault && d.vk.current() != nil
	d.keepVault = false
	var err error
	if d.st != nil {
		err = d.st.Delete()
	}
	err = errors.Join(err, writePin(d.upd.paths.State, ""))
	if keep {
		f := d.vk.current()
		f.Account = false
		err = errors.Join(err, d.vk.save(f))
		d.vk.linked(dsync.RemoteVault{})
	} else if d.vk.current() != nil {
		err = errors.Join(err, d.vk.erase())
		if d.st != nil {
			d.st.Close()
			d.st = nil
		}
	}
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
	list, err := d.client(acct).Devices(ctx)
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
	err = d.client(acct).Revoke(ctx, id, "remove")
	// already removed, or signed out on the server: this device still leaves
	if id == "self" && (err == nil || errors.Is(err, dsync.ErrRevoked) || errors.Is(err, dsync.ErrUnauthorized)) {
		// the person removed this computer themselves, so it keeps its master password
		d.mu.Lock()
		d.keepVault = true
		d.mu.Unlock()
		mode := "remove"
		if errors.Is(err, dsync.ErrRevoked) {
			mode = dsync.RevokedMode(err)
		}
		d.startLeaving(mode)
		return nil
	}
	return d.answer(acct, err)
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
	return d.answer(acct, d.client(acct).RenameDevice(ctx, id, name))
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
