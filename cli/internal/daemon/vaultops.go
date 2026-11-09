package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/store"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
	"github.com/itzzritik/dokky/cli/internal/vault"
)

const (
	statusVault   = "vault" // login.poll: the sign in waits for the master password step in state.vault.need
	confirmReason = "Confirm it is you to change dokky's security"
)

var errVaultLocked = &codedError{api.ErrLocked, "dokky is locked: unlock it first."}

func (d *Daemon) acct() *store.Account {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.data.Account
}

// a wrong password publishes at once, so the app's cooldown countdown is live
func (d *Daemon) told(err error) error {
	var ce *codedError
	if errors.As(err, &ce) && (ce.code == api.ErrWrongPassword || ce.code == api.ErrTooManyAttempts) {
		d.publish()
	}
	return err
}

// vaultSetup creates the master password. a signed-in device whose account already has one is told to join it
func (d *Daemon) vaultSetup(ctx context.Context, password string) (api.RecoveryKeyReply, error) {
	rec, resync, err := d.setupVault(ctx, password)
	if err != nil {
		return api.RecoveryKeyReply{}, err
	}
	d.publish()
	if resync {
		// the account's services were sealed with the server's key until now; the pull seals and uploads them
		if err := d.syncNow(ctx); err != nil {
			d.log.Warn("seal the account's drive logins", "err", err)
		}
	}
	return api.RecoveryKeyReply{RecoveryKey: rec}, nil
}

func (d *Daemon) setupVault(ctx context.Context, password string) (string, bool, error) {
	k := d.vk
	k.op.Lock()
	defer k.op.Unlock()
	if k.current() != nil {
		return "", false, &codedError{"vault_exists", "dokky already has a master password."}
	}
	if err := weak(password); err != nil {
		return "", false, err
	}
	acct := d.acct()
	if acct != nil {
		_, err := d.client(acct).GetVault(ctx)
		switch {
		case err == nil:
			k.setNeed(api.NeedJoin)
			d.publish()
			return "", false, &codedError{"need_join", "Your dokky account already has a master password from another computer. Enter that one instead."}
		case errors.Is(err, dsync.ErrNoVault):
		case acct.Pending || !dsync.Offline(err):
			return "", false, d.checked(acct, err)
		default:
			// offline: the vault is made here and linked to the account once the server answers
			d.log.Info("creating the master password offline, it links to the account later", "err", err)
		}
	}
	pw := []byte(password)
	defer vault.Zero(pw)
	dk := k.deviceKey()
	defer vault.Zero(dk)
	f, ak, rec, err := vault.Create(pw, dk, k.now())
	if err != nil {
		return "", false, err
	}
	// vault.json first: a crash before the store moves leaves one the next start finishes moving
	if err := f.Save(k.dir); err != nil {
		vault.Zero(ak)
		return "", false, err
	}
	d.syncMu.Lock()
	d.opMu.Lock()
	d.mu.Lock()
	st, err := d.stLocked()
	if err == nil {
		err = st.Upgrade(ak)
	}
	if err != nil {
		if v, _ := store.FormatOf(k.dir); v == 2 {
			d.log.Warn("the store moved to the master password, the old key stays", "err", err)
			err = nil
		} else {
			_ = os.Remove(filepath.Join(k.dir, vault.FileName))
		}
	}
	if err == nil {
		k.swap(f, ak)
		k.setNeed("")
	}
	d.mu.Unlock()
	d.opMu.Unlock()
	d.syncMu.Unlock()
	if err != nil {
		vault.Zero(ak)
		return "", false, fmt.Errorf("move the store to the master password: %w", err)
	}
	k.passed()
	d.log.Info("created the master password", "keyId", f.KeyID)
	if acct == nil {
		return rec, false, nil
	}
	need, err := d.link(ctx, acct)
	switch {
	case err != nil:
		// sync links it later; a sign in links it on its next poll
		d.log.Warn("link the master password to the account", "err", err)
	case need != "":
		k.setNeed(need)
	}
	return rec, !acct.Pending && err == nil && need == "", nil
}

// link ties this device's vault to the account's. an account with no vault takes this device's, so the local
// password becomes the account's; the same key links; another key, or no local vault, leaves the need.
// caller holds k.op
func (d *Daemon) link(ctx context.Context, acct *store.Account) (string, error) {
	k := d.vk
	f := k.current()
	c := d.client(acct)
	remote, err := c.GetVault(ctx)
	switch {
	case errors.Is(err, dsync.ErrNoVault):
		if f == nil {
			return api.NeedCreate, nil
		}
		made, cerr := c.CreateVault(ctx, f.Remote())
		var ve *dsync.VaultExistsError
		switch {
		case errors.As(cerr, &ve) && ve.Current.KeyID == f.KeyID:
			made = ve.Current
		case errors.As(cerr, &ve):
			k.pause(cerr)
			return api.NeedJoin, nil
		case cerr != nil:
			return "", cerr
		}
		d.log.Info("the account now uses this computer's master password", "keyId", f.KeyID)
		remote = made
	case err != nil:
		return "", err
	case f == nil:
		return api.NeedJoin, nil
	case remote.KeyID != f.KeyID:
		k.pause(fmt.Errorf("%w: the account's key is %s, this computer has %s", dsync.ErrKeyMismatch, remote.KeyID, f.KeyID))
		return api.NeedJoin, nil
	default:
		f, _ = followRemote(f, remote.Remote)
	}
	f.Account = true
	if err := k.save(f); err != nil {
		return "", err
	}
	k.linked(remote)
	return "", nil
}

// the same account key with other wraps: the master password changed on another device
func followRemote(f *vault.File, r vault.Remote) (*vault.File, bool) {
	same := f.WrappedKey == r.WrappedKey && f.RecoveryWrappedKey == r.RecoveryWrappedKey && f.KDF.Alg == r.KDF.Alg &&
		slices.Equal(f.KDF.Salt, r.KDF.Salt) && f.KDF.T == r.KDF.T && f.KDF.M == r.KDF.M && f.KDF.P == r.KDF.P
	if same || r.KeyID != f.KeyID || r.Validate() != nil {
		return f, false
	}
	nf := f.Clone()
	nf.KDF, nf.WrappedKey, nf.RecoveryWrappedKey = r.KDF, r.WrappedKey, r.RecoveryWrappedKey
	nf.KDF.Salt = slices.Clone(r.KDF.Salt)
	return nf, true
}

// a join takes the account's key: the store is encrypted again with it and the account's wraps replace these
func (d *Daemon) vaultJoin(ctx context.Context, password string) error {
	return d.join(ctx, func(r dsync.RemoteVault) ([]byte, []byte, error) {
		ak, err := d.vk.checkPassword(r.Remote, password)
		return ak, nil, err
	})
}

// as join, with the recovery key, and the account gets the new password
func (d *Daemon) vaultJoinRecover(ctx context.Context, recoveryKey, newPassword string) error {
	if err := weak(newPassword); err != nil {
		return err
	}
	return d.join(ctx, func(r dsync.RemoteVault) ([]byte, []byte, error) {
		ak, err := vault.OpenRemoteWithRecovery(r.Remote, recoveryKey)
		return ak, []byte(newPassword), badRecovery(err)
	})
}

func (d *Daemon) join(ctx context.Context, open func(dsync.RemoteVault) (ak, newPassword []byte, err error)) error {
	pending, err := d.joinVault(ctx, open)
	if err != nil {
		return d.told(err)
	}
	d.publish()
	if !pending {
		d.kick()
	}
	return nil
}

func (d *Daemon) joinVault(ctx context.Context, open func(dsync.RemoteVault) ([]byte, []byte, error)) (bool, error) {
	k := d.vk
	k.op.Lock()
	defer k.op.Unlock()
	acct := d.acct()
	if acct == nil {
		return false, errors.New("sign in to your dokky account first")
	}
	if k.state() == api.VaultLocked {
		return false, errVaultLocked
	}
	c := d.client(acct)
	remote, err := c.GetVault(ctx)
	if errors.Is(err, dsync.ErrNoVault) {
		return false, &codedError{"no_vault", "Your dokky account has no master password yet."}
	}
	if err != nil {
		return false, d.checked(acct, err)
	}
	ak, newPassword, err := open(remote)
	defer vault.Zero(newPassword)
	if err != nil {
		return false, err
	}
	local := k.current()
	var dk []byte
	if local == nil || local.Unlock != vault.UnlockPassword {
		dk = k.deviceKey()
	}
	defer vault.Zero(dk)
	nf, err := vault.AdoptRemote(local, remote.Remote, ak, dk, k.now())
	if err == nil && newPassword != nil {
		err = vault.Rewrap(nf, ak, newPassword, k.now())
	}
	if err != nil {
		vault.Zero(ak)
		return false, err
	}
	version := remote.Version
	if newPassword != nil {
		// the account's password changes first: if the server refuses, nothing here changed
		saved, err := k.push(ctx, c, nf, remote.Version)
		if err != nil {
			vault.Zero(ak)
			return false, d.checked(acct, err)
		}
		version = saved.Version
	}
	if err := d.takeKey(nf, ak); err != nil {
		vault.Zero(ak)
		return false, err
	}
	k.linked(dsync.RemoteVault{Remote: nf.Remote(), Version: version})
	k.passed()
	d.log.Info("joined the account's vault", "keyId", nf.KeyID)
	return acct.Pending, nil
}

// takeKey encrypts the store with the account's key and saves its vault.json. a v1 store needs vault.json first,
// a v2 one is encrypted again before vault.json changes; either way a crash leaves something that opens
func (d *Daemon) takeKey(nf *vault.File, ak []byte) error {
	k := d.vk
	d.syncMu.Lock()
	defer d.syncMu.Unlock()
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.mu.Lock()
	defer d.mu.Unlock()
	st, err := d.stLocked()
	if err != nil {
		return err
	}
	if k.state() == api.VaultSetup {
		if err := nf.Save(k.dir); err != nil {
			return err
		}
		if err := st.Upgrade(ak); err != nil {
			if v, _ := store.FormatOf(k.dir); v != 2 {
				_ = os.Remove(filepath.Join(k.dir, vault.FileName))
				return fmt.Errorf("move the store to the account's key: %w", err)
			}
			d.log.Warn("the store moved to the account's key, the old key stays", "err", err)
		}
	} else {
		if err := st.Rekey(ak); err != nil {
			return fmt.Errorf("encrypt the store with the account's key: %w", err)
		}
		if err := nf.Save(k.dir); err != nil {
			return err
		}
		// opening again settles the old file kept beside the new one
		if again, err := store.OpenWithKey(k.dir, ak); err == nil {
			st.Close()
			d.st = again
		} else {
			d.log.Warn("settle the store", "err", err)
		}
	}
	k.swap(nf, ak)
	// a fresh pull reads everything with the account's key
	if a := d.data.Account; a != nil {
		cp := *a
		cp.Rev = ""
		d.data.Account = &cp
		if err := d.save(); err != nil {
			d.log.Error("save store", "err", err)
		}
	}
	return nil
}

// vaultUnlockOpen is vault.unlock while already unlocked: a typed password restarts the password interval
func (d *Daemon) vaultUnlockOpen(ctx context.Context, p api.UnlockParams) error {
	k := d.vk
	defer k.holdFor(p.Method)()
	if k.state() == api.VaultSetup {
		return errNoVault
	}
	ak, err := k.prove(ctx, p, unlockReason)
	vault.Zero(ak)
	if err == nil {
		d.publish()
	}
	return d.told(err)
}

func (d *Daemon) vaultAuthorize(ctx context.Context, p api.UnlockParams) (api.GrantReply, error) {
	k := d.vk
	defer k.holdFor(p.Method)()
	if k.state() != api.VaultUnlocked {
		return api.GrantReply{}, errNoVault
	}
	ak, err := k.prove(ctx, p, confirmReason)
	vault.Zero(ak)
	if err != nil {
		return api.GrantReply{}, d.told(err)
	}
	d.publish()
	return api.GrantReply{Grant: k.grant()}, nil
}

func (d *Daemon) vaultSettings(p api.VaultSettingsParams) error {
	k := d.vk
	k.op.Lock()
	defer k.op.Unlock()
	f, ak := k.current(), k.key()
	if f == nil || ak == nil {
		return errNoVault
	}
	switch {
	case p.Unlock != nil && !vault.ValidUnlock(*p.Unlock):
		return fmt.Errorf("unlock must be %s, %s or %s", api.UnlockAuto, api.UnlockSystem, api.UnlockPassword)
	case p.PasswordDays != nil && !slices.Contains(api.PasswordDayChoices, *p.PasswordDays):
		return fmt.Errorf("passwordDays must be one of %v", api.PasswordDayChoices)
	case p.IdleLockMinutes != nil && !slices.Contains(api.IdleLockChoices, *p.IdleLockMinutes):
		return fmt.Errorf("idleLockMinutes must be one of %v", api.IdleLockChoices)
	}
	if p.Unlock != nil && *p.Unlock == api.UnlockSystem {
		if _, ok := k.system(false); !ok {
			return &codedError{api.ErrSystemUnavailable, "Touch ID, Windows Hello or a system password prompt is not available on this computer."}
		}
	}
	if err := k.useGrant(p.Grant); err != nil {
		return err
	}
	nf := f.Clone()
	if p.PasswordDays != nil {
		nf.PasswordDays = *p.PasswordDays
	}
	if p.IdleLockMinutes != nil {
		nf.IdleLockMinutes = *p.IdleLockMinutes
	}
	dropDevice := false
	if p.Unlock != nil && *p.Unlock != f.Unlock {
		nf.Unlock = *p.Unlock
		if nf.Unlock == vault.UnlockPassword {
			// nothing on this computer opens the vault any more
			nf.DeviceWrappedKey, dropDevice = "", true
		} else {
			dk := k.deviceKey()
			if dk == nil {
				return errors.New("this computer has no keychain to keep a key in, so dokky can only open with the master password")
			}
			err := nf.SetDevice(ak, dk)
			vault.Zero(dk)
			if err != nil {
				return err
			}
		}
	}
	if err := k.save(nf); err != nil {
		return err
	}
	if dropDevice {
		if err := k.device.Delete(); err != nil {
			d.log.Warn("remove the device key", "err", err)
		}
	}
	d.publish()
	return nil
}

// a rewrap only: the account key stays, so other devices keep working; the account changes first
func (d *Daemon) changePassword(ctx context.Context, p api.ChangePasswordParams) error {
	return d.told(d.rewrap(ctx, func(f *vault.File, ak []byte) error {
		next := []byte(p.Next)
		defer vault.Zero(next)
		return vault.Rewrap(f, ak, next, d.vk.now())
	}, p.Current, p.Next))
}

func (d *Daemon) newRecoveryKey(ctx context.Context, password string) (api.RecoveryKeyReply, error) {
	var rec string
	err := d.rewrap(ctx, func(f *vault.File, ak []byte) (err error) {
		rec, err = vault.NewRecovery(f, ak)
		return err
	}, password, "")
	return api.RecoveryKeyReply{RecoveryKey: rec}, d.told(err)
}

// rewrap checks the password, changes a copy of vault.json, saves it on the account's server vault when linked,
// then here. next, when set, is a new password to check first
func (d *Daemon) rewrap(ctx context.Context, change func(*vault.File, []byte) error, password, next string) error {
	k := d.vk
	k.op.Lock()
	defer k.op.Unlock()
	f := k.current()
	if f == nil || k.key() == nil {
		return errNoVault
	}
	if next != "" {
		if err := weak(next); err != nil {
			return err
		}
	}
	ak, err := k.checkPassword(f.Remote(), password)
	if err != nil {
		return err
	}
	defer vault.Zero(ak)
	nf := f.Clone()
	if err := change(nf, ak); err != nil {
		return err
	}
	// the password was just typed
	nf.PasswordAt = k.now().UTC()
	if err := d.pushLinked(ctx, nf); err != nil {
		return err
	}
	return k.save(nf)
}

// vaultRecover sets a new password with the recovery key while unlocked; the recovery key stays the same
func (d *Daemon) vaultRecover(ctx context.Context, p api.RecoverParams) error {
	k := d.vk
	k.op.Lock()
	defer k.op.Unlock()
	nf, ak, err := k.recovered(p)
	if err != nil {
		return err
	}
	vault.Zero(ak)
	if err := d.pushLinked(ctx, nf); err != nil {
		return err
	}
	if err := k.save(nf); err != nil {
		return err
	}
	d.publish()
	return nil
}

// recovered opens the vault with the recovery key and rewraps a copy with the new password
func (k *keeper) recovered(p api.RecoverParams) (*vault.File, []byte, error) {
	if err := weak(p.NewPassword); err != nil {
		return nil, nil, err
	}
	f := k.current()
	if f == nil {
		return nil, nil, errNoVault
	}
	ak, err := vault.OpenWithRecovery(f, p.RecoveryKey)
	if err != nil {
		return nil, nil, badRecovery(err)
	}
	pw := []byte(p.NewPassword)
	defer vault.Zero(pw)
	if err := vault.Rewrap(f, ak, pw, k.now()); err != nil {
		vault.Zero(ak)
		return nil, nil, err
	}
	k.passed()
	return f, ak, nil
}

// pushLinked saves the wraps on the account's server vault when this device is linked to it. caller holds k.op
func (d *Daemon) pushLinked(ctx context.Context, nf *vault.File) error {
	acct := d.acct()
	if acct == nil || acct.Pending || !nf.Account || d.vk.isPaused() {
		return nil
	}
	_, err := d.vk.push(ctx, d.client(acct), nf, 0)
	return d.checked(acct, err)
}

// push saves nf's wraps as the account's, reading the version first when base is unknown and once more when
// another device saved in between. caller holds k.op
func (k *keeper) push(ctx context.Context, c *dsync.Client, nf *vault.File, base int) (dsync.RemoteVault, error) {
	k.mu.Lock()
	if base == 0 {
		base = k.serverVersion
	}
	k.mu.Unlock()
	for try := 0; ; try++ {
		if base == 0 {
			cur, err := c.GetVault(ctx)
			if err != nil {
				return dsync.RemoteVault{}, err
			}
			if cur.KeyID != nf.KeyID {
				return dsync.RemoteVault{}, fmt.Errorf("%w: the account's key is %s", dsync.ErrKeyMismatch, cur.KeyID)
			}
			base = cur.Version
		}
		saved, err := c.UpdateVault(ctx, base, nf.Remote())
		var ce *dsync.VaultConflictError
		if errors.As(err, &ce) && try == 0 && ce.Current.KeyID == nf.KeyID {
			base = ce.Current.Version
			continue
		}
		if err != nil {
			return dsync.RemoteVault{}, err
		}
		k.setServerVersion(saved.Version)
		return saved, nil
	}
}

// linked records that this device's vault is the account's
func (k *keeper) linked(r dsync.RemoteVault) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.need, k.paused = "", false
	k.serverKeyID, k.serverVersion = r.KeyID, r.Version
}

// vaultLock ends the unlocked phase once the reply is out: drives detach, the key goes
func (d *Daemon) vaultLock() error {
	switch d.vk.state() {
	case api.VaultSetup:
		return &codedError{"no_vault", "Create a master password first, then dokky can lock."}
	case api.VaultLocked:
		return nil
	}
	if d.lockReq == nil {
		return errors.New("this dokky cannot lock")
	}
	requestRestart(d.lockReq)
	return nil
}

// caller holds mu; a store forgotten on erase starts again on the old format, like a fresh install
func (d *Daemon) stLocked() (*store.Store, error) {
	if d.st != nil {
		return d.st, nil
	}
	st, err := store.OpenLegacy(d.vk.dir, d.vk.legacy)
	if err != nil {
		return nil, err
	}
	d.st = st
	return st, nil
}
