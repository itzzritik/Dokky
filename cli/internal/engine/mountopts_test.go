package engine

import (
	"testing"

	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/stretchr/testify/require"
)

func TestMountParamsDarwin(t *testing.T) {
	p := MountParams("svc:Media", "/u/dokky/nas", "nas", store.DefaultVFS(), "darwin")
	require.Equal(t, "svc:Media", p["fs"])
	require.Equal(t, "/u/dokky/nas", p["mountPoint"])
	require.Equal(t, "dokky", p["mountType"])
	require.Equal(t, []string{"rsize=1048576", "wsize=1048576", "readahead=16"}, p["mountOpt"].(map[string]any)["ExtraOptions"])
	require.Equal(t, "Nas", p["volname"])
	require.Equal(t, true, p["vfs_fast_fingerprint"])
	require.Equal(t, "full", p["vfs_cache_mode"])
	require.Equal(t, "5m", p["dir_cache_time"])
	require.Equal(t, "16M", p["vfs_read_ahead"])
	require.Equal(t, 0, p["vfs_read_chunk_streams"])
	require.Equal(t, "2M", p["vfs_read_chunk_size"], "doubles per request, so any link fills and seeks start small")
	require.NotContains(t, p, "vfs_refresh", "a background full listing can resurrect entries deleted while it runs")
	require.Equal(t, "50G", p["vfs_cache_max_size"])
	require.Equal(t, "5m", p["vfs_cache_max_age"])
	require.Equal(t, "10s", p["vfs_cache_poll_interval"])
	require.Equal(t, "10G", p["vfs_cache_min_free_space"])
	require.NotContains(t, p, "network_mode")
}

func TestMountParamsLinux(t *testing.T) {
	p := MountParams("svc:", "/u/dokky/nas", "nas", store.DefaultVFS(), "linux")
	require.Equal(t, "mount", p["mountType"])
	require.NotContains(t, p, "mountOpt")
	require.Equal(t, "full", p["vfs_cache_mode"])
}

func TestMountParamsWindowsUsesNetworkModeAndLabel(t *testing.T) {
	p := MountParams("svc:", "*", "nas", store.DefaultVFS(), "windows")
	require.Equal(t, "cmount", p["mountType"])
	require.Equal(t, true, p["network_mode"])
	require.Equal(t, "Nas", p["volname"])
	require.Equal(t, "*", p["mountPoint"])
	require.NotContains(t, p, "mountOpt")
}

func TestValidCacheMaxAge(t *testing.T) {
	for _, ok := range []string{"5m", "15m", "1h", "6h", "24h", "7d"} {
		require.NoError(t, ValidCacheMaxAge(ok))
	}
	for _, bad := range []string{"", "1m", "30m", "2h", "7D"} {
		require.Error(t, ValidCacheMaxAge(bad))
	}
}
