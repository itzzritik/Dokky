package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/itzzritik/dokky/cli/internal/vault"
	"github.com/stretchr/testify/require"
)

func passwordOnly(f *vault.File) { f.Unlock, f.DeviceWrappedKey = vault.UnlockPassword, "" }

func handoffFiles(t *testing.T, dir string) (note, key bool) {
	t.Helper()
	_, nerr := os.Stat(filepath.Join(dir, handoffFile))
	_, kerr := os.Stat(filepath.Join(dir, "restart.key"))
	return nerr == nil, kerr == nil
}

func ipcCall(t *testing.T, dir, method string, params, out any) error {
	t.Helper()
	c, err := ipc.Connect(dir)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Call(context.Background(), method, params, out)
}

func TestRestartHandsTheKeyToTheNextStart(t *testing.T) {
	t.Setenv("DOKKY_NO_UPDATE", "1")
	dir := t.TempDir()
	k := diskKeeper(t, dir, passwordOnly, false)
	st, err := store.OpenWithKey(dir, testVault().ak)
	require.NoError(t, err)
	require.NoError(t, st.Save(sample("m1")))
	st.Close()
	env := func(k *keeper, f *fakeEngine) phaseEnv {
		return phaseEnv{p: Paths{State: dir, Cache: dir}, eng: phaseEngine{f}, k: k, home: t.TempDir(), log: k.log, cur: &atomic.Pointer[Daemon]{},
			adjust: func(d *Daemon) {
				d.isMounted, d.forceUnmount = f.isMounted, f.forceUnmount
				d.notify = func(string, string) error { return nil }
			}}
	}
	mounted := func(f *fakeEngine) func() bool {
		return func() bool {
			f.mu.Lock()
			defer f.mu.Unlock()
			_, ok := f.mounted["m1"]
			return ok
		}
	}
	vaultIs := func(want string) func() bool {
		return func() bool {
			var s StatusReply
			return ipcCall(t, dir, "status", nil, &s) == nil && s.Vault == want
		}
	}
	unlock := func() {
		require.NoError(t, ipcCall(t, dir, api.MVaultUnlock, api.UnlockParams{Method: api.AuthPassword, Password: testPassword}, nil))
	}

	f1 := newFakeEngine()
	done := make(chan error, 1)
	go func() { done <- runPhases(context.Background(), env(k, f1)) }()
	require.Eventually(t, vaultIs(api.VaultLocked), 10*time.Second, 10*time.Millisecond, "password mode starts locked")
	unlock()
	require.Eventually(t, mounted(f1), 10*time.Second, 10*time.Millisecond)

	require.NoError(t, ipcCall(t, dir, api.MVaultLock, nil, nil))
	require.Eventually(t, vaultIs(api.VaultLocked), 10*time.Second, 10*time.Millisecond)
	note, key := handoffFiles(t, dir)
	require.False(t, note || key, "a lock hands nothing over")
	unlock()
	require.Eventually(t, vaultIs(api.VaultUnlocked), 10*time.Second, 10*time.Millisecond)

	require.NoError(t, ipcCall(t, dir, "daemon.restart", nil, nil))
	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrRestart)
	case <-time.After(10 * time.Second):
		t.Fatal("the daemon did not restart")
	}
	note, key = handoffFiles(t, dir)
	require.True(t, note && key, "the restart left its key for the next start")
	var n handoffNote
	raw, err := os.ReadFile(filepath.Join(dir, handoffFile))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &n))
	require.Equal(t, testVault().file.KeyID, n.KeyID)
	require.NotContains(t, string(raw), string(testVault().ak))

	// the next process: nothing is unlocked by hand and no prompt may show
	next := testKeeper(dir)
	next.prompt = func(context.Context, string) error { t.Error("prompted after a restart"); return nil }
	require.NoError(t, next.load())
	f2 := newFakeEngine()
	ctx, cancel := context.WithCancel(context.Background())
	done = make(chan error, 1)
	go func() { done <- runPhases(ctx, env(next, f2)) }()
	require.Eventually(t, mounted(f2), 10*time.Second, 10*time.Millisecond, "the drives come back unlocked")
	require.Eventually(t, vaultIs(api.VaultUnlocked), 10*time.Second, 10*time.Millisecond)
	note, key = handoffFiles(t, dir)
	require.False(t, note || key, "the handover is used once")
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the daemon did not stop")
	}
	note, key = handoffFiles(t, dir)
	require.False(t, note || key, "a stop hands nothing over")
}

func TestHandoffIgnoredUnlessFreshAndOurs(t *testing.T) {
	cases := map[string]struct {
		after  time.Duration
		change func(*handoffNote)
	}{
		"three minutes old": {after: 3 * time.Minute},
		"from the future":   {after: -time.Minute},
		"another vault":     {change: func(n *handoffNote) { n.KeyID = otherVault().file.KeyID }},
		"damaged":           {change: func(n *handoffNote) { n.Wrapped = otherVault().file.WrappedKey }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			k := diskKeeper(t, dir, passwordOnly, false)
			k.ak = append([]byte(nil), testVault().ak...)
			require.NoError(t, k.handOff())
			if c.change != nil {
				var n handoffNote
				raw, err := os.ReadFile(filepath.Join(dir, handoffFile))
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(raw, &n))
				c.change(&n)
				raw, _ = json.Marshal(n)
				require.NoError(t, os.WriteFile(filepath.Join(dir, handoffFile), raw, 0o600))
			}
			next := testKeeper(dir)
			require.NoError(t, next.load())
			at := time.Now().Add(c.after)
			next.now = func() time.Time { return at }
			require.Nil(t, next.takeHandoff())
			note, key := handoffFiles(t, dir)
			require.False(t, note || key, "removed even when not used")
			phase, _ := next.startPhase()
			require.Equal(t, api.VaultLocked, phase)
		})
	}
	t.Run("fresh and ours opens", func(t *testing.T) {
		dir := t.TempDir()
		k := diskKeeper(t, dir, passwordOnly, false)
		k.ak = append([]byte(nil), testVault().ak...)
		require.NoError(t, k.handOff())
		next := testKeeper(dir)
		require.NoError(t, next.load())
		require.Equal(t, testVault().ak, next.takeHandoff())
		require.Nil(t, next.takeHandoff(), "once only")
	})
	t.Run("auto mode needs none", func(t *testing.T) {
		dir := t.TempDir()
		k := diskKeeper(t, dir, nil, true)
		k.ak = append([]byte(nil), testVault().ak...)
		require.NoError(t, k.handOff())
		note, key := handoffFiles(t, dir)
		require.False(t, note || key)
	})
}
