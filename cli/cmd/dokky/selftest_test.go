package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/itzzritik/dokky/cli/internal/vault"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	if os.Getenv("DOKKY_TEST_AS_CLI") == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

func runSelftest(t *testing.T, state string) (string, error) {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(os.Args[0], "selftest", "--state", state)
	cmd.Env = []string{"DOKKY_TEST_AS_CLI=1", "DOKKY_KEYRING=file", "HOME=" + home, "USERPROFILE=" + home, "PATH=" + os.Getenv("PATH")}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestSelftestPasses(t *testing.T) {
	state := t.TempDir()
	t.Setenv("DOKKY_KEYRING", "file")
	s, err := store.Open(state, store.SystemKeyring(state))
	require.NoError(t, err)
	require.NoError(t, s.Save(store.Data{Local: map[string]store.LocalMount{}}))

	out, err := runSelftest(t, state)
	require.NoError(t, err, out)
}

func TestSelftestFailsOnBadStore(t *testing.T) {
	state := t.TempDir()
	t.Setenv("DOKKY_KEYRING", "file")
	_, err := store.Open(state, store.SystemKeyring(state))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(state, "store.enc"), []byte("garbage garbage garbage garbage garbage"), 0o600))

	out, err := runSelftest(t, state)
	require.Error(t, err)
	require.Contains(t, out, "selftest:")
}

func TestSelftestFailsOnMissingKey(t *testing.T) {
	state := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(state, "store.enc"), []byte("x"), 0o600))
	_, err := runSelftest(t, state)
	require.Error(t, err)
}

func TestSelftestWithoutStoreWritesNothing(t *testing.T) {
	state := t.TempDir()
	out, err := runSelftest(t, state)
	require.NoError(t, err, out)
	entries, err := os.ReadDir(state)
	require.NoError(t, err)
	require.Empty(t, entries)
}

// a v2 store needs the master password, which a selftest never has; vault.json and the store's shape are enough
func writeV2State(t *testing.T, state string) {
	t.Helper()
	f, ak, _, err := vault.Create([]byte("correct horse battery"), nil, time.Now())
	require.NoError(t, err)
	require.NoError(t, f.Save(state))
	s, err := store.OpenWithKey(state, ak)
	require.NoError(t, err)
	require.NoError(t, s.Save(store.Data{Local: map[string]store.LocalMount{}}))
}

func TestSelftestPassesOnV2WithoutAnyKey(t *testing.T) {
	state := t.TempDir()
	writeV2State(t, state)
	out, err := runSelftest(t, state)
	require.NoError(t, err, out)
	require.NoFileExists(t, filepath.Join(state, "store.key"), "no keychain key is made for a v2 store")
}

func TestSelftestFailsOnV2WithoutVault(t *testing.T) {
	state := t.TempDir()
	writeV2State(t, state)
	require.NoError(t, os.Remove(filepath.Join(state, "vault.json")))
	out, err := runSelftest(t, state)
	require.Error(t, err)
	require.Contains(t, out, "vault.json is missing")
}

func TestSelftestFailsOnDamagedV2(t *testing.T) {
	state := t.TempDir()
	writeV2State(t, state)
	require.NoError(t, os.WriteFile(filepath.Join(state, "store.enc"), []byte("DKY2\x09short"), 0o600))
	_, err := runSelftest(t, state)
	require.Error(t, err)

	writeV2State(t, state)
	require.NoError(t, os.WriteFile(filepath.Join(state, "vault.json"), []byte(`{"version":9}`), 0o600))
	out, err := runSelftest(t, state)
	require.Error(t, err)
	require.Contains(t, out, "newer dokky")
}
