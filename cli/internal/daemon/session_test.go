package daemon

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/itzzritik/dokky/cli/internal/sysauth"
	"github.com/stretchr/testify/require"
)

func TestSessionLive(t *testing.T) {
	start := time.Now()
	until := start.Add(api.SessionFor)
	cases := map[string]struct {
		until, now time.Time
		live       bool
	}{
		"never opened":              {now: start},
		"just opened":               {until: until, now: start, live: true},
		"a minute before the end":   {until: until, now: until.Add(-time.Minute), live: true},
		"at the end":                {until: until, now: until},
		"the wall clock is past it": {until: until, now: until.Round(0).Add(time.Hour)},
		"wall time alone, still in": {until: until.Round(0), now: start.Round(0).Add(time.Hour), live: true},
		"wall time alone, run out":  {until: until.Round(0), now: until.Round(0)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, c.live, sessionLive(c.until, c.now))
		})
	}
}

// a session keeper on the shared test vault, its clock and how often it published
func sessionKeeper(t *testing.T) (*Daemon, *testClock, *atomic.Int32) {
	t.Helper()
	d, _, _ := newTestDaemon(t, sample())
	clock := &testClock{t: time.Now()}
	d.vk.now = clock.now
	d.vk.endSession("a fresh start")
	var published atomic.Int32
	d.vk.onPublish(func() { published.Add(1) })
	return d, clock, &published
}

func TestSessionOpenAndReuse(t *testing.T) {
	d, clock, published := sessionKeeper(t)
	k := d.vk
	offerSystem(k, true)
	prompts := 0
	k.prompt = func(context.Context, string) error { prompts++; return nil }
	sys := api.SessionParams{Method: api.AuthSystem}

	require.False(t, vaultOf(d).Session)
	require.Equal(t, api.ErrWrongPassword, codeOf(t, call(t, d, api.MSessionOpen, api.SessionParams{Method: api.AuthPassword, Password: "not it at all"}, nil)))
	require.False(t, k.live())
	require.NoError(t, call(t, d, api.MSessionOpen, api.SessionParams{Method: api.AuthPassword, Password: testPassword}, nil))
	require.True(t, vaultOf(d).Session)
	require.EqualValues(t, 1, published.Load(), "the app hears of it at once")

	require.NoError(t, call(t, d, api.MSessionOpen, sys, nil))
	require.Zero(t, prompts, "a live session is reused with no prompt")
	require.NoError(t, call(t, d, api.MSessionOpen, api.SessionParams{Method: api.AuthSystem, Force: true}, nil))
	require.Equal(t, 1, prompts, "force proves the person again")
	require.NoError(t, call(t, d, api.MVaultUnlock, api.UnlockParams{Method: api.AuthSystem}, nil))
	require.Equal(t, 2, prompts, "vault.unlock always proves")

	clock.add(api.SessionFor - time.Minute)
	require.True(t, k.live())
	clock.add(time.Minute)
	require.False(t, k.live(), "four hours and it ends")
	require.NoError(t, call(t, d, api.MSessionOpen, sys, nil))
	require.Equal(t, 3, prompts)

	n := published.Load()
	require.NoError(t, call(t, d, api.MSessionClose, nil, nil))
	require.False(t, vaultOf(d).Session)
	require.Equal(t, n+1, published.Load())
	require.NoError(t, call(t, d, api.MSessionClose, nil, nil), "closing twice is fine")
	require.Equal(t, n+1, published.Load(), "and says nothing new")
}

func TestSessionEnds(t *testing.T) {
	open := func(t *testing.T, d *Daemon) {
		t.Helper()
		require.NoError(t, call(t, d, api.MSessionOpen, api.SessionParams{Method: api.AuthPassword, Password: testPassword}, nil))
		require.True(t, d.vk.live())
	}
	t.Run("on a screen lock or sleep", func(t *testing.T) {
		d, _, published := sessionKeeper(t)
		open(t, d)
		d.vk.endSession("the screen locked")
		require.False(t, d.vk.live())
		require.EqualValues(t, 2, published.Load())
	})
	t.Run("on a lock of the vault", func(t *testing.T) {
		d, _, _ := sessionKeeper(t)
		open(t, d)
		d.vk.lock()
		require.False(t, d.vk.live())
	})
	t.Run("on a password change, and the new one is typed once", func(t *testing.T) {
		d, _, _ := sessionKeeper(t)
		offerSystem(d.vk, true)
		d.vk.prompt = func(context.Context, string) error { return nil }
		open(t, d)
		const next = "Saffron-Pebble-Drum-64"
		require.NoError(t, call(t, d, api.MVaultChangePassword, api.ChangePasswordParams{Current: testPassword, Next: next}, nil))
		require.False(t, vaultOf(d).Session)
		require.Equal(t, api.ErrPasswordRequired, codeOf(t, call(t, d, api.MSessionOpen, api.SessionParams{Method: api.AuthSystem}, nil)))
		require.NoError(t, call(t, d, api.MSessionOpen, api.SessionParams{Method: api.AuthPassword, Password: next}, nil))
		require.True(t, vaultOf(d).Session)
	})
	t.Run("a prompt answered after the screen locked opens nothing", func(t *testing.T) {
		d, _, _ := sessionKeeper(t)
		k := d.vk
		offerSystem(k, true)
		k.prompt = func(context.Context, string) error {
			k.endSession("the screen locked")
			return nil
		}
		require.Equal(t, api.ErrCancelled, codeOf(t, call(t, d, api.MSessionOpen, api.SessionParams{Method: api.AuthSystem}, nil)))
		require.False(t, k.live())
		k.prompt = func(context.Context, string) error { return nil }
		require.NoError(t, call(t, d, api.MSessionOpen, api.SessionParams{Method: api.AuthSystem}, nil), "the next try works")
		require.True(t, k.live())
	})
}

// a call that checked the master password or recovery key goes straight in
func TestProofsStartASession(t *testing.T) {
	t.Run("creating the master password", func(t *testing.T) {
		d, _, _ := newSetupDaemon(t, sample())
		d.vk.endSession("a fresh start")
		require.Equal(t, "no_vault", codeOf(t, call(t, d, api.MSessionOpen, api.SessionParams{Method: api.AuthPassword, Password: testPassword}, nil)))
		require.Equal(t, api.ErrWeakPassword, codeOf(t, call(t, d, api.MVaultSetup, api.PasswordParams{Password: "short"}, nil)))
		require.False(t, vaultOf(d).Session)
		require.NoError(t, call(t, d, api.MVaultSetup, api.PasswordParams{Password: testPassword}, nil))
		require.True(t, vaultOf(d).Session)
	})
	t.Run("a recovery while unlocked", func(t *testing.T) {
		d, _, _ := sessionKeeper(t)
		require.NoError(t, call(t, d, api.MVaultRecover, api.RecoverParams{RecoveryKey: testVault().recovery, NewPassword: otherPassword}, nil))
		v := vaultOf(d)
		require.True(t, v.Session)
		require.True(t, v.PasswordDue, "the next unlock still asks for the new password once")
	})
	t.Run("a new recovery key starts the four hours again", func(t *testing.T) {
		d, clock, _ := sessionKeeper(t)
		require.Equal(t, api.ErrSessionRequired, codeOf(t, call(t, d, api.MVaultNewRecoveryKey, api.PasswordParams{Password: testPassword}, nil)))
		require.NoError(t, call(t, d, api.MSessionOpen, api.SessionParams{Method: api.AuthPassword, Password: testPassword}, nil))
		clock.add(3 * time.Hour)
		require.NoError(t, call(t, d, api.MVaultNewRecoveryKey, api.PasswordParams{Password: testPassword}, nil))
		clock.add(2 * time.Hour)
		require.True(t, vaultOf(d).Session)
	})
}

func TestLockedSessionOpensTheVault(t *testing.T) {
	t.Run("session.open", func(t *testing.T) {
		l, k, h := newLocked(t, nil)
		require.Equal(t, api.ErrPasswordRequired, codeOf(t, invoke(t, h, api.MSessionOpen, api.SessionParams{Method: api.AuthSystem}, nil)))
		require.NoError(t, invoke(t, h, api.MSessionOpen, api.SessionParams{Method: api.AuthPassword, Password: testPassword}, nil))
		require.True(t, k.view(false).Session, "the session goes on into the unlocked phase")
		require.Equal(t, testVault().ak, <-l.opened)
		require.NoError(t, invoke(t, h, api.MSessionOpen, api.SessionParams{Method: api.AuthPassword}, nil), "a second call before the switch reuses it")
		require.Empty(t, l.opened, "and hands nothing more over")
	})
	t.Run("vault.unlock", func(t *testing.T) {
		l, k, h := newLocked(t, nil)
		require.NoError(t, invoke(t, h, api.MVaultUnlock, api.UnlockParams{Method: api.AuthPassword, Password: testPassword}, nil))
		require.True(t, k.live())
		require.Equal(t, testVault().ak, <-l.opened)
	})
	t.Run("vault.recover", func(t *testing.T) {
		l, k, h := newLocked(t, nil)
		require.NoError(t, invoke(t, h, api.MVaultRecover, api.RecoverParams{RecoveryKey: testVault().recovery, NewPassword: otherPassword}, nil))
		require.True(t, k.live())
		require.Equal(t, testVault().ak, <-l.opened)
	})
	t.Run("session.close", func(t *testing.T) {
		_, k, h := newLocked(t, nil)
		require.NoError(t, invoke(t, h, api.MSessionClose, nil, nil))
		require.False(t, k.live())
	})
}

func TestSessionPromptCancelled(t *testing.T) {
	d, _, _ := sessionKeeper(t)
	offerSystem(d.vk, true)
	d.vk.prompt = func(context.Context, string) error { return sysauth.ErrCancelled }
	require.Equal(t, api.ErrCancelled, codeOf(t, call(t, d, api.MSessionOpen, api.SessionParams{Method: api.AuthSystem}, nil)))
	require.False(t, d.vk.live())
}

func TestSessionGate(t *testing.T) {
	// reviewed one by one: the lock screen, read-only views that hand out no secret, the launch handshake
	open := []string{"status", api.MState, api.MActivity, api.MEvents, api.MEventsSeen, "oauth.status", "providers", api.MCatalog,
		api.MSyncKick, "sync.now", "update.check", "update.install", "update.restart", "daemon.restart",
		api.MSessionOpen, api.MSessionClose, api.MVaultUnlock, api.MVaultRecover}
	first := []string{"settings.set", "login.start", "login.poll", "login.resolve", "login.cancel", api.MVaultSetup, api.MVaultJoin, api.MVaultJoinRecover}
	table := (&Daemon{}).handlers(nil)
	for _, m := range append(slices.Clone(open), first...) {
		require.Contains(t, table, m, "a listed method that does not exist")
	}
	ran := func(context.Context, json.RawMessage) (any, error) { return "ran", nil }
	methods := append(slices.Collect(maps.Keys(table)), "a.method.added.later")
	for _, state := range []string{api.VaultUnlocked, api.VaultSetup} {
		t.Run(state, func(t *testing.T) {
			d, _, _ := newTestDaemon(t, sample())
			if state == api.VaultSetup {
				d, _, _ = newSetupDaemon(t, sample())
			}
			k := d.vk
			k.endSession("a fresh start")
			for _, m := range methods {
				_, err := sessionGate(k, m, ran)(context.Background(), nil)
				if slices.Contains(open, m) || (state == api.VaultSetup && slices.Contains(first, m)) {
					require.NoError(t, err, m)
					continue
				}
				require.Equal(t, api.ErrSessionRequired, codeOf(t, err), m)
			}
			inSession(k)
			for _, m := range methods {
				_, err := sessionGate(k, m, ran)(context.Background(), nil)
				require.NoError(t, err, "%s with a session", m)
			}
		})
	}
}

func TestDaemonTableIsGated(t *testing.T) {
	d, f, _ := newTestDaemon(t, sample("m1"))
	d.vk.endSession("a fresh start")
	d.start()
	require.Contains(t, f.mounted, "m1", "the daemon's own work needs no session")

	err := call(t, d, "mount.put", store.Mount{ID: "m9", Name: "later", ServiceID: "s1", VFS: store.DefaultVFS()}, nil)
	require.Equal(t, api.ErrSessionRequired, codeOf(t, err))
	require.EqualError(t, err, "Open dokky and unlock it to change this.")
	require.Equal(t, api.ErrSessionRequired, codeOf(t, call(t, d, api.MVaultSettings, api.VaultSettingsParams{}, nil)))
	require.Equal(t, api.ErrSessionRequired, codeOf(t, call(t, d, api.MVaultSetup, api.PasswordParams{Password: testPassword}, nil)), "a vault exists, so setup needs a session too")
	require.NoError(t, call(t, d, "status", nil, nil))
	require.NoError(t, call(t, d, api.MSessionOpen, api.SessionParams{Method: api.AuthPassword, Password: testPassword}, nil))
	require.NoError(t, call(t, d, "mount.put", store.Mount{ID: "m9", Name: "later", ServiceID: "s1", VFS: store.DefaultVFS()}, nil))

	s, _, _ := newSetupDaemon(t, sample())
	s.vk.endSession("a fresh start")
	err = call(t, s, "mount.put", store.Mount{ID: "m9", Name: "later", ServiceID: "s1", VFS: store.DefaultVFS()}, nil)
	require.Equal(t, api.ErrSessionRequired, codeOf(t, err))
	require.EqualError(t, err, "Open dokky and create a master password to change this.")
}

// a lost device key can't open the vault after a prompt, so none is shown
func TestNoPromptWithoutTheDeviceKey(t *testing.T) {
	_, k, h := newLocked(t, nil)
	require.NoError(t, k.device.Delete())
	offerSystem(k, true)
	k.prompt = func(context.Context, string) error { t.Error("prompted with no device key"); return nil }
	require.Equal(t, api.ErrPasswordRequired, codeOf(t, invoke(t, h, api.MSessionOpen, api.SessionParams{Method: api.AuthSystem}, nil)))
}
