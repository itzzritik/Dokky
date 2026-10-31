package platform

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDirsEndInDokky(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "cfg"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("LocalAppData", filepath.Join(tmp, "local"))

	state, err := StateDir()
	require.NoError(t, err)
	cache, err := CacheDir()
	require.NoError(t, err)
	if runtime.GOOS == "windows" {
		require.Equal(t, filepath.Join(tmp, "local", "dokky"), state)
		require.Equal(t, filepath.Join(tmp, "local", "dokky", "cache"), cache)
		return
	}
	require.Equal(t, "dokky", filepath.Base(state))
	require.Equal(t, "dokky", filepath.Base(cache))
}
