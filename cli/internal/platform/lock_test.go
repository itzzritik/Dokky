package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecondLockFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dokky.lock")
	unlock, err := Lock(path)
	require.NoError(t, err)

	_, err = Lock(path)
	require.ErrorIs(t, err, ErrLocked)

	unlock()
	again, err := Lock(path)
	require.NoError(t, err)
	again()
}

func TestLockFileMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix modes only")
	}
	path := filepath.Join(t.TempDir(), "dokky.lock")
	unlock, err := Lock(path)
	require.NoError(t, err)
	defer unlock()
	requireMode(t, path, 0o600)
}

func requireMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, want, fi.Mode().Perm())
}
