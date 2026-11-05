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
	for _, ok := range []string{"nas", "my-drive2", "Work OneDrive", "Photos ✨", "über", " padded ", strings.Repeat("é", 40)} {
		require.NoError(t, ValidName(ok), ok)
	}
	for _, bad := range []string{"", "   ", "a/b", "a:b", "a\nb", "a\x7fb", strings.Repeat("a", 41)} {
		require.Error(t, ValidName(bad), bad)
	}
}

func TestDefaultMountPoint(t *testing.T) {
	require.Equal(t, filepath.Join("/h", "dokky", "nas"), DefaultMountPoint("/h", "nas", "darwin"))
	require.Equal(t, filepath.Join("/h", "dokky", "Work OneDrive"), DefaultMountPoint("/h", "Work OneDrive", "linux"))
	require.Equal(t, filepath.Join("/h", "dokky", "Notes"), DefaultMountPoint("/h", "Notes. ", "darwin"))
	require.Equal(t, filepath.Join("/h", "dokky", "Drive"), DefaultMountPoint("/h", "..", "linux"))
	require.Equal(t, "*", DefaultMountPoint(`C:\Users\r`, "nas", "windows"))
}

func TestPrepareMountPointCreates(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dokky", "nas")
	require.NoError(t, PrepareMountPoint(p))
	if runtime.GOOS == "windows" {
		require.DirExists(t, filepath.Dir(p))
		require.NoDirExists(t, p, "WinFsp creates the mount folder itself")
		return
	}
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
