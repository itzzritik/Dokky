package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.json")
	require.NoError(t, WriteFileAtomic(path, []byte("one")))
	require.NoError(t, WriteFileAtomic(path, []byte("two")))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "two", string(got))
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temp file is left behind")

	require.Error(t, WriteFileAtomic(filepath.Join(dir, "missing", "x"), nil))
}
