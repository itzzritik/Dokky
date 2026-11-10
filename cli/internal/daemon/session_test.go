package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
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
	t.Run("a new recovery key", func(t *testing.T) {
		d, _, _ := sessionKeeper(t)
		require.NoError(t, call(t, d, api.MVaultNewRecoveryKey, api.PasswordParams{Password: testPassword}, nil))
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
