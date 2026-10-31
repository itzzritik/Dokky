package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/itzzritik/dokky/cli/internal/store"
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
