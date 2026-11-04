package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidName(t *testing.T) {
	for _, ok := range []string{"nas", "my-drive2", "a", "9"} {
		require.NoError(t, ValidName(ok), ok)
	}
	for _, bad := range []string{"", "Nas", "a b", "-x", "x-", "a--b", "über", "a_b", strings.Repeat("a", 41)} {
		require.Error(t, ValidName(bad), bad)
	}
	require.NoError(t, ValidName(strings.Repeat("a", 40)))
}

func TestDefaultMountPoint(t *testing.T) {
	require.Equal(t, filepath.Join("/h", "dokky", "Nas"), DefaultMountPoint("/h", "nas", "darwin"))
	require.Equal(t, filepath.Join("/h", "dokky", "Google Drive"), DefaultMountPoint("/h", "google-drive", "linux"))
	require.Equal(t, "Jarvis", Label("jarvis"))
	require.Equal(t, "My 2 Nas", Label("my-2-nas"))
	require.Equal(t, "*", DefaultMountPoint(`C:\Users\r`, "nas", "windows"))
}

func TestPrepareMountPointCreates(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dokky", "nas")
	require.NoError(t, PrepareMountPoint(p))
	st, err := os.Stat(p)
	require.NoError(t, err)
	require.True(t, st.IsDir())
	require.NoError(t, PrepareMountPoint(p))
}

func TestPrepareMountPointRefusesNonEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nas")
	require.NoError(t, os.MkdirAll(p, 0o755))
	f := filepath.Join(p, "keep.txt")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))
	err := PrepareMountPoint(p)
	require.Error(t, err)
	require.Contains(t, err.Error(), p)
	require.FileExists(t, f)
}

func TestPrepareMountPointRefusesFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nas")
	require.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
	err := PrepareMountPoint(p)
	require.Error(t, err)
	require.Contains(t, err.Error(), p)
	require.FileExists(t, p)
}

func TestPrepareMountPointWindowsTargets(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	require.NoError(t, PrepareMountPoint("*"))
	require.NoError(t, PrepareMountPoint("X:"))
	p := filepath.Join(t.TempDir(), "nas")
	require.NoError(t, os.MkdirAll(p, 0o755))
	require.NoError(t, PrepareMountPoint(p))
	require.NoDirExists(t, p)
}

func TestCleanupRemovesEmptyDirAndDokkyParent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dokky")
	p := filepath.Join(root, "nas")
	require.NoError(t, os.MkdirAll(p, 0o755))
	CleanupMountPoint(p)
	require.NoDirExists(t, p)
	require.NoDirExists(t, root)
}

func TestCleanupKeepsNonEmpty(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dokky")
	p := filepath.Join(root, "nas")
	require.NoError(t, os.MkdirAll(p, 0o755))
	f := filepath.Join(p, "keep.txt")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))
	CleanupMountPoint(p)
	require.FileExists(t, f)
}

func TestCleanupKeepsParentWithSibling(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dokky")
	p := filepath.Join(root, "nas")
	other := filepath.Join(root, "other")
	require.NoError(t, os.MkdirAll(p, 0o755))
	require.NoError(t, os.MkdirAll(other, 0o755))
	CleanupMountPoint(p)
	require.NoDirExists(t, p)
	require.DirExists(t, root)
}

func TestCleanupKeepsParentNotNamedDokky(t *testing.T) {
	root := filepath.Join(t.TempDir(), "mnt")
	p := filepath.Join(root, "nas")
	require.NoError(t, os.MkdirAll(p, 0o755))
	CleanupMountPoint(p)
	require.NoDirExists(t, p)
	require.DirExists(t, root)
}

func TestCleanupIgnoresWindowsTargets(t *testing.T) {
	CleanupMountPoint("*")
	CleanupMountPoint("X:")
}

func TestIsMounted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dokky never force-unmounts on Windows")
	}
	require.True(t, IsMounted("/"))
	require.False(t, IsMounted(t.TempDir()))
}
