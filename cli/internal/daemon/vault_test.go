package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/logx"
	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/itzzritik/dokky/cli/internal/sysauth"
	"github.com/itzzritik/dokky/cli/internal/vault"
	"github.com/stretchr/testify/require"
)

func vaultOf(d *Daemon) api.Vault { return d.snapshot().Vault }

// a system prompt a test turns on and off; the state call checks it from another goroutine
type fakeSystem struct{ on atomic.Bool }

func (s *fakeSystem) available(remote bool) (string, bool) {
	if s.on.Load() && !remote {
		return "Touch ID", true
	}
	return "", false
}

func offerSystem(k *keeper, on bool) *fakeSystem {
	s := &fakeSystem{}
	s.on.Store(on)
	k.system = s.available
	return s
}

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) add(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
	return c.t
}

// the vault as the fresh start of a process sees it: on disk, nothing unlocked yet
func diskKeeper(t *testing.T, dir string, change func(*vault.File), deviceKey bool) *keeper {
	t.Helper()
	t.Setenv("DOKKY_KEYRING", "file")
	fx := testVault()
	f := fx.file.Clone()
	if change != nil {
		change(f)
	}
	require.NoError(t, f.Save(dir))
	k := testKeeper(dir)
	if deviceKey {
		require.NoError(t, k.device.Set(fx.dk))
	}
	require.NoError(t, k.load())
	return k
}

func TestStartupPhases(t *testing.T) {
	t.Setenv("DOKKY_KEYRING", "file")
	setup := testKeeper(t.TempDir())
	require.NoError(t, setup.load())
	phase, ak := setup.startPhase()
	require.Equal(t, api.VaultSetup, phase, "no vault.json runs on the old store")
	require.Nil(t, ak)
	st, err := setup.openStore()
	require.NoError(t, err)
	require.NoError(t, st.Save(store.Fresh()))
	v, _ := store.FormatOf(setup.dir)
	require.Equal(t, 1, v)

	cases := map[string]struct {
		change    func(*vault.File)
		deviceKey bool
		want      string
	}{
		"the device key opens":                  {deviceKey: true, want: api.VaultUnlocked},
		"a lost device key waits":               {want: api.VaultLocked},
		"no device wrap waits for the password": {change: passwordOnly, want: api.VaultLocked},
		"a device key that does not open waits": {change: func(f *vault.File) { f.DeviceWrappedKey = otherVault().file.DeviceWrappedKey }, deviceKey: true, want: api.VaultLocked},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			k := diskKeeper(t, t.TempDir(), c.change, c.deviceKey)
			phase, ak := k.startPhase()
			require.Equal(t, c.want, phase)
			require.Equal(t, c.want == api.VaultUnlocked, ak != nil)
			if ak != nil {
				require.Equal(t, testVault().ak, ak)
			}
		})
	}

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "store.enc"), []byte("DKY2 sealed"), 0o600))
	orphan := testKeeper(dir)
	require.NoError(t, orphan.load())
	_, err = orphan.openStore()
	require.ErrorContains(t, err, "vault.json is missing", "a v2 store is never opened as a fresh v1 one")
}

func newLocked(t *testing.T, change func(*vault.File)) (*lockedPhase, *keeper, map[string]ipc.Handler) {
	t.Helper()
	dir := t.TempDir()
	k := diskKeeper(t, dir, change, true)
	shell := newDaemon(newFakeEngine(), nil, store.Fresh(), t.TempDir(), dir, k.log, k)
	shell.notify = func(string, string) error { return nil }
	l := &lockedPhase{e: phaseEnv{p: Paths{State: dir, Cache: dir}, k: k, log: k.log}, shell: shell, opened: make(chan []byte, 1)}
	l.srv = ipc.NewServer(nil, nil)
	return l, k, l.handlers(make(chan struct{}, 1))
}

func invoke(t *testing.T, h map[string]ipc.Handler, method string, params, out any) error {
	t.Helper()
	fn, ok := h[method]
	require.True(t, ok, "no handler for %s", method)
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	res, err := fn(context.Background(), raw)
	if err != nil || out == nil {
		return err
	}
	b, err := json.Marshal(res)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, out))
	return nil
}

func TestLockedAnswersOnlyItsMethods(t *testing.T) {
	l, _, h := newLocked(t, nil)
	for _, m := range []string{"mount", "service.put", "login.start", "logout", api.MDriveAdd, api.MActivity, "settings.get", api.MVaultSetup, api.MVaultLock, api.MVaultJoin} {
		require.Equal(t, api.ErrLocked, codeOf(t, invoke(t, h, m, map[string]string{}, nil)), m)
	}
	var s api.State
	require.NoError(t, invoke(t, h, api.MState, nil, &s))
	require.Equal(t, api.VaultLocked, s.Vault.State)
	require.Equal(t, testVault().file.KeyID, s.Vault.KeyID)
	require.Empty(t, s.Drives)
	require.NotEmpty(t, s.Version)
	var st StatusReply
	require.NoError(t, invoke(t, h, "status", nil, &st))
	require.True(t, st.Locked)
	require.Equal(t, api.VaultLocked, st.Vault)
	require.Contains(t, h, "update.check")
	require.Contains(t, h, "daemon.restart")

	require.Equal(t, api.ErrWrongPassword, codeOf(t, invoke(t, h, api.MVaultUnlock, api.UnlockParams{Method: api.AuthPassword, Password: "not it at all"}, nil)))
	require.NoError(t, invoke(t, h, api.MVaultUnlock, api.UnlockParams{Method: api.AuthPassword, Password: testPassword}, nil))
	select {
	case ak := <-l.opened:
		require.Equal(t, testVault().ak, ak)
	case <-time.After(5 * time.Second):
		t.Fatal("the unlock never handed over the key")
	}
}

// argon2's second a try is the only brake: no number of wrong passwords keeps the right one out
func TestWrongPasswordsNeverLockOut(t *testing.T) {
	l, _, h := newLocked(t, nil)
	wrong := api.UnlockParams{Method: api.AuthPassword, Password: "wrong guess 123"}
	for range 8 {
		require.Equal(t, api.ErrWrongPassword, codeOf(t, invoke(t, h, api.MVaultUnlock, wrong, nil)))
	}
	require.NoError(t, invoke(t, h, api.MVaultUnlock, api.UnlockParams{Method: api.AuthPassword, Password: testPassword}, nil))
	require.Equal(t, testVault().ak, <-l.opened)
}

func TestSystemUnlock(t *testing.T) {
	sys := api.UnlockParams{Method: api.AuthSystem}
	t.Run("refused while the password is due, over SSH or with no prompt", func(t *testing.T) {
		_, k, h := newLocked(t, func(f *vault.File) {
			f.PasswordDays, f.PasswordAt = 7, time.Now().Add(-8*24*time.Hour)
		})
		prompted := false
		sys := offerSystem(k, true)
		k.prompt = func(context.Context, string) error { prompted = true; return nil }
		require.True(t, k.view(false).PasswordDue)
		require.Equal(t, api.ErrPasswordRequired, codeOf(t, invoke(t, h, api.MVaultUnlock, api.UnlockParams{Method: api.AuthSystem}, nil)))

		f := k.current()
		f.PasswordAt = time.Now()
		require.NoError(t, k.save(f))
		require.Equal(t, api.ErrPasswordRequired, codeOf(t, invoke(t, h, api.MVaultUnlock, api.UnlockParams{Method: api.AuthSystem, Remote: true}, nil)))
		sys.on.Store(false)
		require.Equal(t, api.ErrPasswordRequired, codeOf(t, invoke(t, h, api.MVaultUnlock, api.UnlockParams{Method: api.AuthSystem}, nil)))
		require.False(t, prompted, "no prompt is shown when it would not count")
	})
	t.Run("a confirmed prompt opens with the device key", func(t *testing.T) {
		l, k, h := newLocked(t, nil)
		offerSystem(k, true)
		var reason string
		k.prompt = func(_ context.Context, r string) error { reason = r; return sysauth.ErrCancelled }
		require.Equal(t, api.ErrCancelled, codeOf(t, invoke(t, h, api.MVaultUnlock, sys, nil)))
		require.Equal(t, unlockReason, reason)
		k.prompt = func(context.Context, string) error { return nil }
		require.NoError(t, invoke(t, h, api.MVaultUnlock, sys, nil))
		require.Equal(t, testVault().ak, <-l.opened)
	})
	t.Run("at login only a notification, never a prompt", func(t *testing.T) {
		promptDelay = time.Millisecond
		t.Cleanup(func() { promptDelay = 5 * time.Second })
		l, k, _ := newLocked(t, nil)
		offerSystem(k, true)
		k.prompt = func(context.Context, string) error { t.Error("prompted at login"); return nil }
		notes := make(chan string, 1)
		l.shell.notify = func(_, body string) error { notes <- body; return nil }
		l.ask(context.Background())
		require.Equal(t, lockedNote, <-notes)
		require.Empty(t, l.opened)
	})
}

// fakeEngine plus what the sampler and space checks ask while a whole phase runs
type phaseEngine struct{ *fakeEngine }

func (phaseEngine) Sample() (int64, []engine.Moving, error)        { return 0, nil, nil }
func (phaseEngine) Finished() ([]engine.Finished, error)           { return nil, nil }
func (phaseEngine) Queue(string) ([]engine.QueueItem, error)       { return nil, nil }
func (phaseEngine) Space(store.Mount) (free, total int64, ok bool) { return 0, 0, false }

func TestLockAndUnlockRemountsDrives(t *testing.T) {
	t.Setenv("DOKKY_NO_UPDATE", "1")
	dir := t.TempDir()
	k := diskKeeper(t, dir, nil, true)
	st, err := store.OpenWithKey(dir, testVault().ak)
	require.NoError(t, err)
	require.NoError(t, st.Save(sample("m1")))
	st.Close()
	f := newFakeEngine()
	var cur atomic.Pointer[Daemon]
	e := phaseEnv{p: Paths{State: dir, Cache: dir}, eng: phaseEngine{f}, k: k, home: t.TempDir(), log: k.log, cur: &cur,
		adjust: func(d *Daemon) {
			d.isMounted, d.forceUnmount = f.isMounted, f.forceUnmount
			d.notify = func(string, string) error { return nil }
		}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runPhases(ctx, e) }()
	mounted := func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		_, ok := f.mounted["m1"]
		return ok && len(f.services) == 1
	}
	status := func() StatusReply {
		var s StatusReply
		c, err := ipc.Connect(dir)
		if err != nil {
			return s
		}
		defer c.Close()
		_ = c.Call(context.Background(), "status", nil, &s)
		return s
	}
	require.Eventually(t, mounted, 10*time.Second, 10*time.Millisecond, "auto unlock mounts the flagged drive")
	require.Eventually(t, func() bool { return status().Vault == api.VaultUnlocked }, 10*time.Second, 10*time.Millisecond)

	c, err := ipc.Connect(dir)
	require.NoError(t, err)
	var ie *ipc.Error
	require.ErrorAs(t, c.Call(context.Background(), api.MVaultLock, nil, nil), &ie)
	require.Equal(t, api.ErrSessionRequired, ie.Code, "the start opened the vault, not a session")
	require.NoError(t, c.Call(context.Background(), api.MSessionOpen, api.SessionParams{Method: api.AuthPassword, Password: testPassword}, nil))
	require.NoError(t, c.Call(context.Background(), api.MVaultLock, nil, nil))
	c.Close()
	require.Eventually(t, func() bool { return status().Locked }, 10*time.Second, 10*time.Millisecond)
	f.mu.Lock()
	require.Empty(t, f.mounted, "locking unmounts")
	require.Empty(t, f.services, "and drops every login from rclone")
	f.mu.Unlock()
	require.Nil(t, k.key(), "the key is gone")
	require.Nil(t, cur.Load())
	require.True(t, reopen(t, dir).Local["m1"].Mounted, "the drive stays flagged for the unlock")

	c, err = ipc.Connect(dir)
	require.NoError(t, err)
	require.NoError(t, c.Call(context.Background(), api.MVaultUnlock, api.UnlockParams{Method: api.AuthPassword, Password: testPassword}, nil))
	c.Close()
	require.Eventually(t, mounted, 10*time.Second, 10*time.Millisecond, "unlocking mounts it again")
	require.Eventually(t, func() bool { return status().Vault == api.VaultUnlocked }, 10*time.Second, 10*time.Millisecond)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the daemon did not stop")
	}
}

func TestSetupMovesTheOldStore(t *testing.T) {
	d, f, _ := newSetupDaemon(t, sample("m1"))
	d.start()
	dir := d.vk.dir
	require.FileExists(t, filepath.Join(dir, "store.key"))
	require.Equal(t, api.VaultSetup, vaultOf(d).State)
	require.Equal(t, api.VaultSetup, d.status().Vault)
	require.Equal(t, "no_vault", codeOf(t, call(t, d, api.MVaultLock, nil, nil)))

	require.Equal(t, api.ErrWeakPassword, codeOf(t, call(t, d, api.MVaultSetup, api.PasswordParams{Password: "short"}, nil)))
	var r api.RecoveryKeyReply
	require.NoError(t, call(t, d, api.MVaultSetup, api.PasswordParams{Password: testPassword}, &r))
	require.True(t, strings.HasPrefix(r.RecoveryKey, "DK1-"))
	require.Equal(t, "***", logx.Redact("key " + r.RecoveryKey)[4:], "a recovery key never reaches the log")

	v := vaultOf(d)
	require.Equal(t, api.VaultUnlocked, v.State)
	require.Empty(t, v.Need)
	format, _ := store.FormatOf(dir)
	require.Equal(t, 2, format)
	require.NoFileExists(t, filepath.Join(dir, "store.key"), "the old keychain key is gone")
	require.Contains(t, f.mounted, "m1", "drives stay mounted through the move")

	vf, err := vault.Load(dir)
	require.NoError(t, err)
	require.False(t, vf.Account)
	ak, err := vault.OpenWithRecovery(vf, r.RecoveryKey)
	require.NoError(t, err)
	k := testKeeper(dir)
	require.NoError(t, k.load())
	_, viaDevice := k.startPhase()
	require.Equal(t, ak, viaDevice, "the next start opens with the device key")
	st, err := store.OpenWithKey(dir, ak)
	require.NoError(t, err)
	got, err := st.Load()
	require.NoError(t, err)
	require.Equal(t, sample().Services, got.Services)
	require.True(t, got.Local["m1"].Mounted)

	require.Error(t, call(t, d, api.MVaultSetup, api.PasswordParams{Password: testPassword}, nil), "setup runs once")
}

// the fake server learns a key this device just made, as only a test can
func learnKey(fa *fakeAPI, d *Daemon) {
	fa.hook = func() {
		if ak := d.vk.key(); fa.vault != nil && ak != nil && vault.KeyID(fa.ak) != fa.vault.KeyID && vault.KeyID(ak) == fa.vault.KeyID {
			fa.ak = bytes.Clone(ak)
		}
	}
}

func TestSignedInSetupCreatesTheAccountVaultAndSealsEverything(t *testing.T) {
	fa := newFakeAPI(t)
	data := loggedIn(fa.srv.URL)
	data.Services[0].Version = 1
	fa.services["s1"], fa.legacy["s1"] = data.Services[0], true
	for _, m := range data.Mounts {
		m.Version = 1
		fa.mounts[m.ID] = m
	}
	fa.vault, fa.rev = nil, 3
	d, _, st := newSetupDaemon(t, data)
	learnKey(fa, d)

	require.NoError(t, call(t, d, "sync.now", nil, nil))
	require.Empty(t, fa.requests(), "nothing syncs before the master password")
	require.Equal(t, api.NeedCreate, vaultOf(d).Need, "the account has no vault, so this device makes it")
	require.Contains(t, d.status().SyncWaits, "master password")
	require.Equal(t, "vault", codeOf(t, call(t, d, "mount.put", store.Mount{ID: "m9", Name: "later", ServiceID: "s1", VFS: store.DefaultVFS()}, nil)))

	require.NoError(t, call(t, d, api.MVaultSetup, api.PasswordParams{Password: testPassword}, nil))
	keyID := vaultOf(d).KeyID
	fa.set(func() {
		require.NotNil(t, fa.vault)
		require.Equal(t, keyID, fa.vault.KeyID)
		require.False(t, fa.legacy["s1"], "the legacy login was sealed and saved back")
		require.Equal(t, "hunter2-secret", fa.services["s1"].Secrets["pass"])
		for _, b := range fa.bodies {
			require.NotContains(t, b, "hunter2-secret", "no secret leaves the device in the clear")
		}
	})
	require.Contains(t, fa.putLog(), "service nas 1")
	v := vaultOf(d)
	require.True(t, v.Synced)
	require.Zero(t, v.Legacy)
	require.Empty(t, d.status().SyncWaits)
	vf, err := vault.Load(d.vk.dir)
	require.NoError(t, err)
	require.True(t, vf.Account)
	require.Equal(t, int64(2), reopenWith(t, st).Services[0].Version)
}

func reopenWith(t *testing.T, st *store.Store) store.Data {
	t.Helper()
	d, err := st.Load()
	require.NoError(t, err)
	return d
}

func TestSetupRefusedWhenTheAccountHasAVault(t *testing.T) {
	fa := newFakeAPI(t)
	d, _, _ := newSetupDaemon(t, loggedIn(fa.srv.URL))
	require.NoError(t, call(t, d, "sync.now", nil, nil))
	require.Equal(t, api.NeedJoin, vaultOf(d).Need, "a signed-in device in setup learns the account already has a vault")
	require.Equal(t, api.VaultSetup, vaultOf(d).State)
	require.Equal(t, "need_join", codeOf(t, call(t, d, api.MVaultSetup, api.PasswordParams{Password: testPassword}, nil)))
	_, err := vault.Load(d.vk.dir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestLoginJoinsTheAccountVault(t *testing.T) {
	fa := newFakeAPI(t, "approved")
	other := otherVault()
	fa.vault, fa.ak = remoteOf(other.file, 4), other.ak
	d, _, _ := newTestDaemon(t, sample())
	noOpen(d)
	dir := d.vk.dir

	require.NoError(t, call(t, d, "login.start", nil, nil))
	var r pollReply
	require.NoError(t, call(t, d, "login.poll", nil, &r))
	require.Equal(t, statusVault, r.Status)
	s := d.snapshot()
	require.True(t, s.Account.Pending)
	require.Equal(t, api.NeedJoin, s.Vault.Need)
	require.Empty(t, fa.putLog(), "nothing uploads before the join")

	require.Equal(t, api.ErrWrongPassword, codeOf(t, call(t, d, api.MVaultJoin, api.PasswordParams{Password: testPassword}, nil)), "this device's own password is not the account's")
	require.NoError(t, call(t, d, api.MVaultJoin, api.PasswordParams{Password: otherPassword}, nil))
	v := vaultOf(d)
	require.Empty(t, v.Need, "need clears before the reply")
	require.Equal(t, other.file.KeyID, v.KeyID)

	vf, err := vault.Load(dir)
	require.NoError(t, err)
	require.True(t, vf.Account)
	require.Equal(t, other.file.WrappedKey, vf.WrappedKey, "the account's wraps replace this device's")
	require.Equal(t, other.file.RecoveryWrappedKey, vf.RecoveryWrappedKey)
	dk, err := store.DeviceKeyring(dir).Get()
	require.NoError(t, err)
	ak, err := vault.OpenWithDevice(vf, dk)
	require.NoError(t, err)
	require.Equal(t, other.ak, ak, "with a fresh device wrap")
	st, err := store.OpenWithKey(dir, other.ak)
	require.NoError(t, err)
	_, err = st.Load()
	require.NoError(t, err, "the store is encrypted with the account's key")
	require.NoFileExists(t, filepath.Join(dir, "store.enc.prev"))
	old, err := store.OpenWithKey(dir, testVault().ak)
	require.NoError(t, err)
	_, err = old.Load()
	require.Error(t, err)

	require.NoError(t, call(t, d, "login.poll", nil, &r))
	require.Equal(t, "approved", r.Status)
	require.Equal(t, []string{"service nas 0", "mount media 0", "mount docs 0"}, fa.putLog())
	require.Equal(t, "hunter2-secret", fa.service("s1").Secrets["pass"], "sealed with the account's key, which the server never sees")
	require.False(t, d.snapshot().Account.Pending)
}

func TestJoinRecoverSetsTheAccountPassword(t *testing.T) {
	fa := newFakeAPI(t)
	other := otherVault()
	fa.vault, fa.ak = remoteOf(other.file, 2), other.ak
	d, _, _ := newSetupDaemon(t, loggedIn(fa.srv.URL))
	require.NoError(t, call(t, d, "sync.now", nil, nil))
	require.Equal(t, api.NeedJoin, vaultOf(d).Need)

	const fresh = "Granite-Willow-Spark-26"
	require.Equal(t, api.ErrBadRecoveryKey, codeOf(t, call(t, d, api.MVaultJoinRecover, api.RecoverParams{RecoveryKey: testVault().recovery, NewPassword: fresh}, nil)))
	require.NoError(t, call(t, d, api.MVaultJoinRecover, api.RecoverParams{RecoveryKey: other.recovery, NewPassword: fresh}, nil))
	fa.set(func() {
		require.Equal(t, 3, fa.vault.Version, "the account's vault took the new password")
		ak, err := vault.OpenRemoteWithPassword(fa.vault.Remote, []byte(fresh))
		require.NoError(t, err)
		require.Equal(t, other.ak, ak)
	})
	v := vaultOf(d)
	require.Equal(t, api.VaultUnlocked, v.State)
	require.Empty(t, v.Need)
	format, _ := store.FormatOf(d.vk.dir)
	require.Equal(t, 2, format)
	require.NoError(t, call(t, d, "sync.now", nil, nil))
	require.True(t, vaultOf(d).Synced)
	require.Contains(t, fa.requests(), "GET /api/v1/sync")
}

func TestSyncSealsWhatItSends(t *testing.T) {
	fa, d, _, _ := synced(t)
	require.NoError(t, call(t, d, "service.put", map[string]any{"id": "s1", "name": "nas", "type": "webdav", "options": map[string]string{"url": "http://edit"}, "secrets": map[string]string{"pass": "fresh-secret-value"}, "version": 1, "editOf": "s1"}, nil))
	require.Equal(t, "fresh-secret-value", fa.service("s1").Secrets["pass"])
	d.onToken("s1", testOAuth)
	require.Eventually(t, func() bool { return fa.service("s1").Secrets["token"] == testOAuth }, 5*time.Second, time.Millisecond)
	fa.set(func() {
		require.NotEmpty(t, fa.bodies)
		for _, b := range fa.bodies {
			for _, plain := range []string{"fresh-secret-value", "http://edit", "access_token", "refresh_token"} {
				require.NotContains(t, b, plain)
			}
		}
		require.Equal(t, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC), fa.expiry["s1"].UTC())
	})
}

func TestLegacyServicesAreSealedAgain(t *testing.T) {
	fa, d, _, st := synced(t)
	fa.set(func() {
		s := fa.services["s1"]
		s.Secrets = map[string]string{"pass": "hunter2-secret", "token": testOAuth}
		fa.services["s1"], fa.legacy["s1"] = s, true
		fa.rev = 9
	})
	require.NoError(t, call(t, d, "sync.now", nil, nil))
	fa.set(func() {
		require.False(t, fa.legacy["s1"])
		require.Equal(t, int64(2), fa.services["s1"].Version)
		require.Equal(t, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC), fa.expiry["s1"].UTC(), "the token's expiry goes along")
	})
	saved := load(t, st)
	require.Equal(t, int64(2), saved.Services[0].Version)
	require.Equal(t, testOAuth, saved.Services[0].Secrets["token"])
	require.Zero(t, vaultOf(d).Legacy)
}

func TestKeyMismatchPausesSync(t *testing.T) {
	fa, d, f, st := synced(t)
	other := otherVault()
	fa.set(func() {
		fa.vault, fa.ak = remoteOf(other.file, 1), other.ak
		fa.rev = 7
	})
	before := load(t, st)
	require.NoError(t, call(t, d, "sync.now", nil, nil))
	v := vaultOf(d)
	require.Equal(t, api.NeedJoin, v.Need)
	require.False(t, v.Synced)
	require.Contains(t, d.status().SyncWaits, "account's master password")
	require.Equal(t, before, load(t, st), "nothing here is wiped")
	require.Contains(t, f.mounted, "m1")
	require.False(t, d.status().Offline)

	n := len(fa.requests())
	require.NoError(t, call(t, d, "sync.now", nil, nil))
	require.Len(t, fa.requests(), n, "a paused device does not pull")
	require.Equal(t, "vault", codeOf(t, call(t, d, "mount.put", store.Mount{ID: "m9", Name: "later", ServiceID: "s1", VFS: store.DefaultVFS()}, nil)))

	require.NoError(t, call(t, d, api.MVaultJoin, api.PasswordParams{Password: otherPassword}, nil))
	require.Empty(t, vaultOf(d).Need)
	require.NoError(t, call(t, d, "sync.now", nil, nil))
	require.True(t, vaultOf(d).Synced)
	require.Equal(t, int64(1), load(t, d.st).Services[0].Version)
}

func TestPasswordDaysSetting(t *testing.T) {
	d, _, _ := newTestDaemon(t, sample())
	for _, bad := range []int{0, 3, 14} {
		require.Error(t, call(t, d, api.MVaultSettings, api.VaultSettingsParams{PasswordDays: &bad}, nil), "only the listed choices")
	}
	days := 15
	require.NoError(t, call(t, d, api.MVaultSettings, api.VaultSettingsParams{PasswordDays: &days}, nil))
	require.Equal(t, 15, vaultOf(d).PasswordDays)
	vf, err := vault.Load(d.vk.dir)
	require.NoError(t, err)
	require.Equal(t, 15, vf.PasswordDays)
	require.NotContains(t, d.handlers(nil), "vault.authorize", "grants are gone")
}

// the password once makes a device key again for a vault without one, whatever an older dokky was set to
func TestPasswordMendsTheDeviceKey(t *testing.T) {
	l, k, h := newLocked(t, passwordOnly)
	require.NoError(t, k.device.Delete())
	require.NoError(t, invoke(t, h, api.MVaultUnlock, api.UnlockParams{Method: api.AuthPassword, Password: testPassword}, nil))
	require.Equal(t, testVault().ak, <-l.opened)
	vf, err := vault.Load(k.dir)
	require.NoError(t, err)
	require.NotEmpty(t, vf.DeviceWrappedKey)
	next := testKeeper(k.dir)
	require.NoError(t, next.load())
	phase, ak := next.startPhase()
	require.Equal(t, api.VaultUnlocked, phase, "the next start opens alone")
	require.Equal(t, testVault().ak, ak)
}

func TestChangePasswordGoesToTheAccountFirst(t *testing.T) {
	fa, d, _, _ := synced(t)
	dir := d.vk.dir
	const next = "Saffron-Pebble-Drum-64"
	require.Equal(t, api.ErrWrongPassword, codeOf(t, call(t, d, api.MVaultChangePassword, api.ChangePasswordParams{Current: "not my password", Next: next}, nil)))
	require.Equal(t, api.ErrWeakPassword, codeOf(t, call(t, d, api.MVaultChangePassword, api.ChangePasswordParams{Current: testPassword, Next: "weak"}, nil)))

	fa.set(func() { fa.vault.KeyID = otherVault().file.KeyID })
	require.Error(t, call(t, d, api.MVaultChangePassword, api.ChangePasswordParams{Current: testPassword, Next: next}, nil))
	vf, err := vault.Load(dir)
	require.NoError(t, err)
	require.Equal(t, testVault().file.WrappedKey, vf.WrappedKey, "a refused change leaves this device alone")

	fa.set(func() { fa.vault = remoteOf(testVault().file, 1) })
	d.vk.linked(*fa.vault)
	require.NoError(t, call(t, d, api.MVaultChangePassword, api.ChangePasswordParams{Current: testPassword, Next: next}, nil))
	vf, err = vault.Load(dir)
	require.NoError(t, err)
	fa.set(func() {
		require.Equal(t, 2, fa.vault.Version)
		require.Equal(t, vf.WrappedKey, fa.vault.WrappedKey)
		require.Equal(t, testVault().file.RecoveryWrappedKey, fa.vault.RecoveryWrappedKey, "the recovery key keeps working")
	})
	_, err = vault.OpenWithPassword(vf, []byte(next))
	require.NoError(t, err)
	_, err = vault.OpenWithRecovery(vf, testVault().recovery)
	require.NoError(t, err)

	var r api.RecoveryKeyReply
	require.Equal(t, api.ErrSessionRequired, codeOf(t, call(t, d, api.MVaultNewRecoveryKey, api.PasswordParams{Password: next}, &r)), "the change ended the session")
	require.NoError(t, call(t, d, api.MSessionOpen, api.SessionParams{Method: api.AuthPassword, Password: next}, nil))
	require.NoError(t, call(t, d, api.MVaultNewRecoveryKey, api.PasswordParams{Password: next}, &r))
	vf, err = vault.Load(dir)
	require.NoError(t, err)
	_, err = vault.OpenWithRecovery(vf, testVault().recovery)
	require.Error(t, err, "the old recovery key stops working")
	_, err = vault.OpenWithRecovery(vf, r.RecoveryKey)
	require.NoError(t, err)
	fa.set(func() { require.Equal(t, vf.RecoveryWrappedKey, fa.vault.RecoveryWrappedKey) })
}

func TestPasswordChangedElsewhereIsFollowed(t *testing.T) {
	fa, d, _, _ := synced(t)
	require.NoError(t, call(t, d, "sync.now", nil, nil))
	const next = "Juniper-Lantern-Fox-38"
	changed := testVault().file.Clone()
	require.NoError(t, vault.Rewrap(changed, testVault().ak, []byte(next), time.Now()))
	fa.set(func() {
		fa.vault = remoteOf(changed, 5)
		fa.rev = 11
	})
	require.NoError(t, call(t, d, "sync.now", nil, nil))
	vf, err := vault.Load(d.vk.dir)
	require.NoError(t, err)
	require.Equal(t, changed.WrappedKey, vf.WrappedKey)
	require.NotEmpty(t, vf.DeviceWrappedKey, "this device still opens on its own")
	require.True(t, vf.PasswordOnce, "the new password is asked for here too")
	require.NoError(t, call(t, d, api.MVaultUnlock, api.UnlockParams{Method: api.AuthPassword, Password: next}, nil))
}

// system auth slides the password window, saved once an hour; a changed password is typed once before it counts
func TestPasswordWindow(t *testing.T) {
	d, _, _ := newTestDaemon(t, sample())
	k, dir := d.vk, d.vk.dir
	clock := &testClock{t: time.Now()}
	k.now = clock.now
	offerSystem(k, true)
	k.prompt = func(context.Context, string) error { return nil }
	sys := api.UnlockParams{Method: api.AuthSystem}
	stamp := func() time.Time {
		vf, err := vault.Load(dir)
		require.NoError(t, err)
		return vf.SystemAt
	}

	require.NoError(t, call(t, d, api.MVaultUnlock, sys, nil))
	first := stamp()
	require.WithinDuration(t, clock.now(), first, time.Second)
	clock.add(30 * time.Minute)
	require.NoError(t, call(t, d, api.MVaultUnlock, sys, nil))
	require.Equal(t, first, stamp(), "not saved again within the hour")
	clock.add(6 * 24 * time.Hour)
	require.NoError(t, call(t, d, api.MVaultUnlock, sys, nil))
	require.True(t, stamp().After(first))
	clock.add(6 * 24 * time.Hour)
	require.False(t, vaultOf(d).PasswordDue, "twelve days on, the last system auth keeps the seven day window open")
	require.NoError(t, call(t, d, api.MVaultUnlock, sys, nil))

	const next = "Saffron-Pebble-Drum-64"
	require.NoError(t, call(t, d, api.MVaultChangePassword, api.ChangePasswordParams{Current: testPassword, Next: next}, nil))
	require.True(t, vaultOf(d).PasswordDue)
	err := call(t, d, api.MVaultUnlock, sys, nil)
	require.Equal(t, api.ErrPasswordRequired, codeOf(t, err))
	require.ErrorContains(t, err, "changed")
	require.Equal(t, api.ErrWrongPassword, codeOf(t, call(t, d, api.MVaultUnlock, api.UnlockParams{Method: api.AuthPassword, Password: testPassword}, nil)))
	require.NoError(t, call(t, d, api.MVaultUnlock, api.UnlockParams{Method: api.AuthPassword, Password: next}, nil))
	require.False(t, vaultOf(d).PasswordDue)
	require.NoError(t, call(t, d, api.MVaultUnlock, sys, nil), "system auth counts again")

	require.NoError(t, call(t, d, api.MVaultRecover, api.RecoverParams{RecoveryKey: testVault().recovery, NewPassword: testPassword}, nil))
	require.True(t, vaultOf(d).PasswordDue, "a recovery asks for the new password once too")
}

func TestRecoverWhileUnlocked(t *testing.T) {
	d, _, _ := newTestDaemon(t, sample())
	const next = "Harbor-Thistle-Moon-71"
	require.Equal(t, api.ErrBadRecoveryKey, codeOf(t, call(t, d, api.MVaultRecover, api.RecoverParams{RecoveryKey: "DK1-0000", NewPassword: next}, nil)))
	require.NoError(t, call(t, d, api.MVaultRecover, api.RecoverParams{RecoveryKey: testVault().recovery, NewPassword: next}, nil))
	vf, err := vault.Load(d.vk.dir)
	require.NoError(t, err)
	_, err = vault.OpenWithPassword(vf, []byte(next))
	require.NoError(t, err)
	_, err = vault.OpenWithRecovery(vf, testVault().recovery)
	require.NoError(t, err, "the recovery key stays the same")
}

func TestLogoutKeepsTheVaultAndRemovalErasesIt(t *testing.T) {
	fa := newFakeAPI(t)
	d, _, _ := newTestDaemon(t, loggedIn(fa.srv.URL))
	d.start()
	dir := d.vk.dir
	require.NoError(t, call(t, d, "logout", map[string]bool{"force": true}, nil))
	vf, err := vault.Load(dir)
	require.NoError(t, err, "signing out keeps the master password")
	require.False(t, vf.Account)
	require.FileExists(t, filepath.Join(dir, "device.key"))
	require.Equal(t, api.VaultUnlocked, vaultOf(d).State)

	srv := newFakeAPI(t)
	e, _, _ := newTestDaemon(t, loggedIn(srv.srv.URL))
	e.start()
	e.startLeaving("erase")
	require.Eventually(t, func() bool { return !e.status().LoggedIn }, 10*time.Second, 5*time.Millisecond)
	_, err = vault.Load(e.vk.dir)
	require.ErrorIs(t, err, os.ErrNotExist, "a removed device forgets the vault")
	require.NoFileExists(t, filepath.Join(e.vk.dir, "device.key"))
	require.Equal(t, api.VaultSetup, vaultOf(e).State)
}

func TestErrorsNeverCarrySecrets(t *testing.T) {
	err := errors.New("bad key " + testVault().recovery)
	require.NotContains(t, logx.Redact(err.Error()), testVault().recovery)
	spaced := strings.ToLower(strings.ReplaceAll(testVault().recovery, "-", " "))
	require.NotContains(t, logx.Redact(spaced), spaced[8:])
}

func TestLoginCreatesTheAccountVault(t *testing.T) {
	t.Run("a device in setup creates the password, then the poll uploads sealed", func(t *testing.T) {
		fa := newFakeAPI(t, "approved")
		fa.vault = nil
		d, _, _ := newSetupDaemon(t, sample())
		noOpen(d)
		learnKey(fa, d)
		require.NoError(t, call(t, d, "login.start", nil, nil))
		var r pollReply
		require.NoError(t, call(t, d, "login.poll", nil, &r))
		require.Equal(t, statusVault, r.Status)
		s := d.snapshot()
		require.True(t, s.Account.Pending)
		require.Equal(t, api.NeedCreate, s.Vault.Need)
		require.Equal(t, api.VaultSetup, s.Vault.State)

		var rk api.RecoveryKeyReply
		require.NoError(t, call(t, d, api.MVaultSetup, api.PasswordParams{Password: testPassword}, &rk))
		require.NotEmpty(t, rk.RecoveryKey)
		s = d.snapshot()
		require.Empty(t, s.Vault.Need, "need clears before the reply")
		require.True(t, s.Account.Pending, "the upload is the poll's job")
		fa.set(func() { require.Equal(t, s.Vault.KeyID, fa.vault.KeyID, "this device's password became the account's") })

		require.NoError(t, call(t, d, "login.poll", nil, &r))
		require.Equal(t, "approved", r.Status)
		require.Equal(t, []string{"service nas 0", "mount media 0", "mount docs 0"}, fa.putLog())
		require.Equal(t, "hunter2-secret", fa.service("s1").Secrets["pass"])
		require.False(t, d.snapshot().Account.Pending)
		require.True(t, d.snapshot().Vault.Synced)
	})
	t.Run("a device with a password gives it to an account without one", func(t *testing.T) {
		fa := newFakeAPI(t, "approved")
		fa.vault = nil
		d, _, _ := newTestDaemon(t, sample())
		noOpen(d)
		r := login(t, d)
		require.Empty(t, r.Clashes)
		fa.set(func() {
			require.Equal(t, testVault().file.KeyID, fa.vault.KeyID)
			require.Equal(t, testVault().file.WrappedKey, fa.vault.WrappedKey)
		})
		require.Empty(t, d.snapshot().Vault.Need, "no prompt: the local password is the account's now")
		require.Equal(t, "hunter2-secret", fa.service("s1").Secrets["pass"])
		vf, err := vault.Load(d.vk.dir)
		require.NoError(t, err)
		require.True(t, vf.Account)
	})
}
