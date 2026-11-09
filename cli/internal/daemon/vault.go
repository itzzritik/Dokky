package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/store"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
	"github.com/itzzritik/dokky/cli/internal/sysauth"
	"github.com/itzzritik/dokky/cli/internal/vault"
)

const (
	maxFails      = 5
	firstCooldown = 30 * time.Second
	maxCooldown   = 5 * time.Minute
	grantFor      = 2 * time.Minute
	systemFor     = 5 * time.Minute
	unlockReason  = "Unlock dokky to mount your drives"
	lockedNote    = "dokky is locked. Open dokky to unlock your drives."
)

// keeper holds the vault for the whole process. the key, wrong password counts and grants outlive a lock and
// unlock, each of which starts a new Daemon. lock order: op, then the Daemon's syncMu, opMu and mu, then mu here
type keeper struct {
	dir     string
	log     *slog.Logger
	device  store.Keyring // this device's key, which opens the vault without a prompt
	legacy  store.Keyring // the old store key, only while the store is v1
	restart store.Keyring // the one-use key a restart hands the account key over with
	prompt  func(ctx context.Context, reason string) error
	system  func(remote bool) (string, bool)
	now     func() time.Time

	// one vault change at a time
	op sync.Mutex

	// a leaf: nothing else is locked while it is held
	mu   sync.Mutex
	file *vault.File // nil until a master password exists
	ak   []byte      // only while unlocked
	need string      // what a sign in, or a device whose key is not the account's, waits for
	// the account's key differs from this device's; sync waits for vault.join
	paused bool
	// the account's vault as the last pull saw it; "" before one
	serverKeyID   string
	serverVersion int
	legacyCount   int
	fails         int
	cooldown      time.Duration
	until         time.Time
	grants        map[string]time.Time
	sysName       string
	sysAt         time.Time
	sysBusy       bool
}

func newKeeper(dir string, log *slog.Logger) *keeper {
	return &keeper{
		dir: dir, log: log, device: store.DeviceKeyring(dir), legacy: store.SystemKeyring(dir), restart: store.RestartKeyring(dir),
		prompt: sysauth.Prompt, system: sysauth.Available, now: time.Now, grants: map[string]time.Time{},
	}
}

func (k *keeper) load() error {
	f, err := vault.Load(k.dir)
	if errors.Is(err, os.ErrNotExist) {
		f, err = nil, nil
	}
	if err != nil {
		return err
	}
	k.mu.Lock()
	k.file = f
	k.mu.Unlock()
	return nil
}

func (k *keeper) state() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.stateLocked()
}

func (k *keeper) stateLocked() string {
	switch {
	case k.file == nil:
		return api.VaultSetup
	case k.ak == nil:
		return api.VaultLocked
	}
	return api.VaultUnlocked
}

// a copy the caller may change and save, or nil in setup
func (k *keeper) current() *vault.File {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.file == nil {
		return nil
	}
	return k.file.Clone()
}

// startPhase decides how the process starts: setup on the old store, unlocked by the device key in auto mode,
// else locked
func (k *keeper) startPhase() (string, []byte) {
	if ak := k.startKey(); ak != nil {
		return api.VaultUnlocked, ak
	}
	return k.state(), nil
}

// the device key opens the vault at startup only in auto mode; anything else waits in the locked phase
func (k *keeper) startKey() []byte {
	f := k.current()
	if f == nil || f.Unlock != vault.UnlockAuto {
		return nil
	}
	ak, err := k.openWithDevice(f)
	if err != nil {
		k.log.Warn("the device key did not open the vault, waiting for the master password", "err", err)
		return nil
	}
	return ak
}

func (k *keeper) openWithDevice(f *vault.File) ([]byte, error) {
	dk, err := k.device.Get()
	if errors.Is(err, store.ErrNotFound) {
		return nil, vault.ErrNoDeviceKey
	}
	if err != nil {
		return nil, err
	}
	defer vault.Zero(dk)
	return vault.OpenWithDevice(f, dk)
}

// the device key, made on first use; nil when this computer has nowhere to keep one
func (k *keeper) deviceKey() []byte {
	dk, err := store.EnsureKey(k.device)
	if err != nil {
		k.log.Warn("no keychain for the device key, dokky will open with the master password only", "err", err)
		return nil
	}
	return dk
}

// unlocked hands the keeper its key; the keeper owns it from here and zeroes it on lock
func (k *keeper) unlocked(ak []byte) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.ak != nil && &k.ak[0] != &ak[0] {
		vault.Zero(k.ak)
	}
	k.ak = ak
}

func (k *keeper) lock() {
	k.mu.Lock()
	defer k.mu.Unlock()
	vault.Zero(k.ak)
	k.ak = nil
	clear(k.grants)
}

// swap puts a saved file in place, with its key when that changed
func (k *keeper) swap(f *vault.File, ak []byte) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.file = f
	if ak != nil && (k.ak == nil || &k.ak[0] != &ak[0]) {
		vault.Zero(k.ak)
		k.ak = ak
	}
}

// save writes a changed copy of vault.json and puts it in place
func (k *keeper) save(f *vault.File) error {
	if err := f.Save(k.dir); err != nil {
		return err
	}
	k.swap(f, nil)
	return nil
}

func (k *keeper) key() []byte {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.ak
}

// nil unless unlocked. sync seals with the keeper's own key: lock waits for every sync and change first
func (k *keeper) sealer() *dsync.Sealer {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.file == nil || k.ak == nil {
		return nil
	}
	return &dsync.Sealer{AK: k.ak, KeyID: k.file.KeyID}
}

// erase forgets the vault on this device: vault.json and the device key. the store is the caller's
func (k *keeper) erase() error {
	k.mu.Lock()
	vault.Zero(k.ak)
	k.file, k.ak, k.need, k.paused = nil, nil, "", false
	k.serverKeyID, k.serverVersion, k.legacyCount = "", 0, 0
	clear(k.grants)
	k.mu.Unlock()
	err := os.Remove(filepath.Join(k.dir, vault.FileName))
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	k.dropHandoff()
	return errors.Join(err, k.device.Delete())
}

func (k *keeper) setNeed(need string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.need = need
}

// the account's key is not this device's: sync stops until vault.join, and nothing here is wiped
func (k *keeper) pause(why error) {
	k.mu.Lock()
	was := k.paused
	k.paused, k.need = true, api.NeedJoin
	k.mu.Unlock()
	if !was {
		k.log.Warn("sync paused until this device joins the account's vault", "err", why)
	}
}

func (k *keeper) isPaused() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.paused
}

func (k *keeper) saw(ref *dsync.VaultRef, legacy int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if ref != nil {
		k.serverKeyID = ref.KeyID
	}
	k.legacyCount = legacy
}

// whether the server's vault moved on since this process last read it
func (k *keeper) versionChanged(ref *dsync.VaultRef) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return ref != nil && k.file != nil && ref.KeyID == k.file.KeyID && ref.Version != k.serverVersion
}

func (k *keeper) setServerVersion(v int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.serverVersion = v
}

func (k *keeper) setLegacy(n int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.legacyCount = n
}

// view is api.Vault for the state call; signedIn is whether the device has an account
func (k *keeper) view(signedIn bool) api.Vault {
	name := k.systemName()
	now := k.now()
	k.mu.Lock()
	defer k.mu.Unlock()
	v := api.Vault{State: k.stateLocked(), Need: k.need, System: name}
	if now.Before(k.until) {
		v.CooldownUntil = k.until
	}
	f := k.file
	if f == nil {
		return v
	}
	v.Unlock, v.PasswordDays, v.IdleLockMinutes, v.KeyID = f.Unlock, f.PasswordDays, f.IdleLockMinutes, f.KeyID
	v.PasswordDue = f.PasswordDue(now)
	v.Synced = signedIn && f.Account && !k.paused && (k.serverKeyID == "" || k.serverKeyID == f.KeyID)
	if signedIn {
		v.Legacy = k.legacyCount
	}
	return v
}

// the name of the system prompt, never waiting on the check: the first one can take seconds on Windows
func (k *keeper) systemName() string {
	k.mu.Lock()
	stale := !k.sysBusy && (k.sysAt.IsZero() || k.now().Sub(k.sysAt) >= systemFor)
	if stale {
		k.sysBusy = true
	}
	name := k.sysName
	k.mu.Unlock()
	if stale {
		go func() {
			n, ok := k.system(false)
			if !ok {
				n = ""
			}
			k.mu.Lock()
			k.sysName, k.sysAt, k.sysBusy = n, k.now(), false
			k.mu.Unlock()
		}()
	}
	return name
}

// wrong passwords: five in a row start a cooldown of 30 seconds, doubling each time up to 5 minutes
func (k *keeper) cooling() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if wait := k.until.Sub(k.now()); wait > 0 {
		return &codedError{api.ErrTooManyAttempts, "Too many wrong passwords. Try again in " + plainDuration(wait) + "."}
	}
	return nil
}

func (k *keeper) failed() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.fails++
	if k.fails < maxFails {
		return
	}
	k.fails = 0
	k.cooldown = min(max(k.cooldown*2, firstCooldown), maxCooldown)
	k.until = k.now().Add(k.cooldown)
}

func (k *keeper) passed() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.fails, k.cooldown, k.until = 0, 0, time.Time{}
}

func plainDuration(d time.Duration) string {
	if s := int((d + time.Second - 1) / time.Second); s < 60 {
		return fmt.Sprintf("%d seconds", s)
	}
	m := int((d + time.Minute - 1) / time.Minute)
	if m == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", m)
}

var (
	errNoVault   = &codedError{"no_vault", "dokky has no master password yet"}
	errWrongPass = &codedError{api.ErrWrongPassword, "That master password is not right."}
)

// checkPassword opens the account key with the master password, counting wrong ones; r is this device's vault
// or, joining, the account's
func (k *keeper) checkPassword(r vault.Remote, password string) ([]byte, error) {
	if err := k.cooling(); err != nil {
		return nil, err
	}
	pw := []byte(password)
	defer vault.Zero(pw)
	ak, err := vault.OpenRemoteWithPassword(r, pw)
	if errors.Is(err, vault.ErrWrongPassword) {
		k.failed()
		if cerr := k.cooling(); cerr != nil {
			return nil, cerr
		}
		return nil, errWrongPass
	}
	if err != nil {
		return nil, err
	}
	k.passed()
	return ak, nil
}

// systemRefused says why the system prompt may not stand in for the password right now
func (k *keeper) systemRefused(f *vault.File, remote bool) error {
	why := ""
	switch {
	case f == nil:
		return errNoVault
	case f.PasswordDue(k.now()):
		why = fmt.Sprintf("It has been %d days: type your master password this time.", f.PasswordDays)
	case remote:
		why = "Over SSH, type your master password."
	case f.DeviceWrappedKey == "":
		why = "dokky is set to open with the master password only."
	}
	if why != "" {
		return &codedError{api.ErrPasswordRequired, why}
	}
	if _, ok := k.system(remote); !ok {
		return &codedError{api.ErrPasswordRequired, "No system prompt is available here: type your master password."}
	}
	return nil
}

// checkSystem shows the system prompt, then opens the account key with the device key
func (k *keeper) checkSystem(ctx context.Context, f *vault.File, remote bool, reason string) ([]byte, error) {
	if err := k.systemRefused(f, remote); err != nil {
		return nil, err
	}
	switch err := k.prompt(ctx, reason); {
	case errors.Is(err, sysauth.ErrCancelled):
		return nil, &codedError{api.ErrCancelled, "The system prompt was cancelled."}
	case errors.Is(err, sysauth.ErrUnavailable):
		return nil, &codedError{api.ErrSystemUnavailable, "The system prompt could not open: type your master password."}
	case err != nil:
		return nil, &codedError{api.ErrSystemUnavailable, "The system prompt failed: " + err.Error()}
	}
	ak, err := k.openWithDevice(f)
	if err != nil {
		return nil, &codedError{api.ErrPasswordRequired, "The key saved on this computer no longer opens dokky: type your master password."}
	}
	return ak, nil
}

// holdFor takes the vault lock for a typed password, which saves vault.json; a system prompt can wait minutes
// for the person and changes nothing, so it never holds up a password typed meanwhile. returns the release
func (k *keeper) holdFor(method string) func() {
	if method == api.AuthSystem {
		return func() {}
	}
	k.op.Lock()
	return k.op.Unlock
}

// prove checks the person by password or system prompt and returns the account key. a typed password restarts
// the password interval, so passwordAt moves forward
func (k *keeper) prove(ctx context.Context, p api.UnlockParams, reason string) ([]byte, error) {
	f := k.current()
	if f == nil {
		return nil, errNoVault
	}
	switch p.Method {
	case api.AuthPassword:
		ak, err := k.checkPassword(f.Remote(), p.Password)
		if err != nil {
			return nil, err
		}
		f.PasswordAt = k.now().UTC()
		k.mendDevice(f, ak)
		if err := k.save(f); err != nil {
			k.log.Warn("save vault.json", "err", err)
		}
		return ak, nil
	case api.AuthSystem:
		return k.checkSystem(ctx, f, p.Remote, reason)
	}
	return nil, fmt.Errorf("method must be %q or %q", api.AuthPassword, api.AuthSystem)
}

// a device key lost from the keychain is made again once the password is typed, so the next start opens alone
func (k *keeper) mendDevice(f *vault.File, ak []byte) {
	if f.Unlock == vault.UnlockPassword {
		return
	}
	if opened, err := k.openWithDevice(f); err == nil {
		vault.Zero(opened)
		return
	}
	dk := k.deviceKey()
	if dk == nil {
		return
	}
	defer vault.Zero(dk)
	if err := f.SetDevice(ak, dk); err != nil {
		k.log.Warn("save a new device key", "err", err)
		return
	}
	k.log.Info("saved a new device key")
}

func (k *keeper) grant() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	g := hex.EncodeToString(b)
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()
	for id, at := range k.grants {
		if now.After(at) {
			delete(k.grants, id)
		}
	}
	k.grants[g] = now.Add(grantFor)
	return g
}

// a grant works once, within two minutes
func (k *keeper) useGrant(g string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	at, ok := k.grants[g]
	delete(k.grants, g)
	if g == "" || !ok || k.now().After(at) {
		return &codedError{api.ErrGrantRequired, "Confirm it is you first."}
	}
	return nil
}

func weak(pw string) error {
	if !vault.CheckPassword(pw) {
		return &codedError{api.ErrWeakPassword, "Use at least " + strconv.Itoa(vault.MinPassword) + " characters."}
	}
	return nil
}

func badRecovery(err error) error {
	if errors.Is(err, vault.ErrBadRecoveryKey) {
		return &codedError{api.ErrBadRecoveryKey, upperFirst(err.Error()) + "."}
	}
	return err
}

func upperFirst(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}

// a notification from the locked daemon; nobody may see it on a server, which is fine
func notifyLocked(log *slog.Logger, notify func(title, body string) error) {
	if notify == nil {
		notify = platform.Notify
	}
	if err := notify("dokky", lockedNote); err != nil {
		log.Debug("notify", "err", err)
	}
}

// openStore opens the store the vault says: the old keychain key in setup, else the account key, finishing a
// move to the account key that a crash cut short
func (k *keeper) openStore() (*store.Store, error) {
	k.mu.Lock()
	f, ak := k.file, k.ak
	k.mu.Unlock()
	v, err := store.FormatOf(k.dir)
	if err != nil {
		return nil, err
	}
	switch {
	case f == nil && v == 2:
		return nil, fmt.Errorf("store.enc is locked with a master password but %s is missing: put %s back, or move store.enc aside to start again", vault.FileName, vault.FileName)
	case f == nil:
		return store.OpenLegacy(k.dir, k.legacy)
	case ak == nil:
		return nil, errors.New("the vault is locked")
	case v == 1:
		st, err := store.OpenLegacy(k.dir, k.legacy)
		if err != nil {
			return nil, err
		}
		if err := st.Upgrade(ak); err != nil {
			if v, _ := store.FormatOf(k.dir); v != 2 {
				return nil, fmt.Errorf("move the store to the master password: %w", err)
			}
			k.log.Warn("the store moved to the master password, the old key stays", "err", err)
		}
		return st, nil
	}
	return store.OpenWithKey(k.dir, ak)
}
