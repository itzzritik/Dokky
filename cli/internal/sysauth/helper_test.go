package sysauth

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// a stand-in helper that answers like the real one
func withHelper(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	dir := t.TempDir()
	oldBin, oldDir, oldPath := helperBin, helperDir, helperPath
	helperBin, helperDir = []byte(script), func() (string, error) { return dir, nil }
	helperPath = sync.OnceValue(installHelper)
	t.Cleanup(func() { helperBin, helperDir, helperPath = oldBin, oldDir, oldPath })
	return filepath.Join(dir, "dokky-auth")
}

func TestHelperNamesDokkyInThePrompt(t *testing.T) {
	path := withHelper(t, "#!/bin/sh\n[ \"$1\" = check ] && echo touchid || echo ok\n")
	c := macCommand("prompt", hostile)
	require.Equal(t, path, c.path, "the helper asks, so macOS names dokky")
	require.Equal(t, []string{"prompt", hostile}, c.args, "the reason is an argument")
	require.Equal(t, []string{"check"}, macCommand("check", "ignored").args)

	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), fi.Mode().Perm())

	name, ok := macAvailable(context.Background())
	require.True(t, ok)
	require.Equal(t, "Touch ID", name)
	require.NoError(t, macPrompt(context.Background(), "unlock dokky"))
}

func TestHelperIsRewrittenWhenDokkyCarriesAnother(t *testing.T) {
	path := withHelper(t, "#!/bin/sh\necho none\n")
	require.NoError(t, os.WriteFile(path, []byte("an older helper"), 0o700))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))
	require.Equal(t, path, helperPath())
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, helperBin, b)
}

func TestNoHelperAsksThroughOsascript(t *testing.T) {
	withHelper(t, "")
	require.Equal(t, osascript, macCommand("check", "").path)
}
