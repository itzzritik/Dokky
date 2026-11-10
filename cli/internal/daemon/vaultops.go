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

const statusVault = "vault" // login.poll: the sign in waits for the master password step in state.vault.need

var errVaultLocked = &codedError{api.ErrLocked, "dokky is locked: unlock it first."}

func (d *Daemon) acct() *store.Account {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.data.Account
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

// the same account key with other wraps: the master password changed on another device, so this one asks for
// the new password once too
func followRemote(f *vault.File, r vault.Remote) (*vault.File, bool) {
	same := f.WrappedKey == r.WrappedKey && f.RecoveryWrappedKey == r.RecoveryWrappedKey && f.KDF.Alg == r.KDF.Alg &&
		slices.Equal(f.KDF.Salt, r.KDF.Salt) && f.KDF.T == r.KDF.T && f.KDF.M == r.KDF.M && f.KDF.P == r.KDF.P
	if same || r.KeyID != f.KeyID || r.Validate() != nil {
		return f, false
	}
	nf := f.Clone()
	nf.KDF, nf.WrappedKey, nf.RecoveryWrappedKey = r.KDF, r.WrappedKey, r.RecoveryWrappedKey
	nf.KDF.Salt = slices.Clone(r.KDF.Salt)
	nf.PasswordOnce = true
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
		return err
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
	dk := k.deviceKey()
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

func (d *Daemon) vaultSettings(p api.VaultSettingsParams) error {
	k := d.vk
	k.op.Lock()
	defer k.op.Unlock()
	f := k.current()
	if f == nil || k.key() == nil {
		return errNoVault
	}
	if p.PasswordDays != nil && !slices.Contains(api.PasswordDayChoices, *p.PasswordDays) {
		return fmt.Errorf("passwordDays must be one of %v", api.PasswordDayChoices)
	}
	nf := f.Clone()
	if p.PasswordDays != nil {
		nf.PasswordDays = *p.PasswordDays
	}
	if err := k.save(nf); err != nil {
		return err
	}
	d.publish()
	return nil
}

// a rewrap only: the account key stays, so other devices keep working; the account changes first. the session
// ends and the next unlock asks for the new password once, so system auth trusts it again only after it is typed
func (d *Daemon) changePassword(ctx context.Context, p api.ChangePasswordParams) error {
	err := d.rewrap(ctx, func(f *vault.File, ak []byte) error {
		next := []byte(p.Next)
		defer vault.Zero(next)
		f.PasswordOnce = true
		return vault.Rewrap(f, ak, next, d.vk.now())
	}, p.Current, p.Next)
	if err == nil {
		d.vk.endSession("the master password changed")
	}
	return err
}

func (d *Daemon) newRecoveryKey(ctx context.Context, password string) (api.RecoveryKeyReply, error) {
	var rec string
	err := d.rewrap(ctx, func(f *vault.File, ak []byte) (err error) {
		rec, err = vault.NewRecovery(f, ak)
		return err
	}, password, "")
	return api.RecoveryKeyReply{RecoveryKey: rec}, err
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
	nf.Typed(k.now())
	if err := change(nf, ak); err != nil {
		return err
	}
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

// recovered opens the vault with the recovery key and rewraps a copy with the new password, which the next
// unlock asks for once
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
	f.PasswordOnce = true
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
