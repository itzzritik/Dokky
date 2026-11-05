package engine

import (
	"fmt"
	"slices"

	"github.com/itzzritik/dokky/cli/internal/store"
)

var CacheMaxAges = []string{"5m", "15m", "1h", "6h", "24h", "7d"}

func ValidCacheMaxAge(s string) error {
	if !slices.Contains(CacheMaxAges, s) {
		return fmt.Errorf("cache max age %q must be one of %v", s, CacheMaxAges)
	}
	return nil
}

func MountParams(fsName, mountPoint, volName string, v store.VFS, goos string) map[string]any {
	p := map[string]any{
		"fs":                       fsName,
		"mountPoint":               mountPoint,
		"volname":                  volName,
		"vfs_cache_mode":           v.CacheMode,
		"dir_cache_time":           v.DirCacheTime,
		"vfs_read_ahead":           v.ReadAhead,
		"vfs_read_chunk_streams":   v.ReadChunkStreams,
		"vfs_read_chunk_size":      "2M",
		"vfs_cache_max_size":       v.CacheMaxSize,
		"vfs_cache_max_age":        v.CacheMaxAge,
		"vfs_cache_poll_interval":  v.CachePollInterval,
		"vfs_cache_min_free_space": v.CacheMinFreeSpace,
		"vfs_fast_fingerprint":     v.FastFingerprint,
	}
	switch goos {
	case "darwin":
		p["mountType"] = "dokky"
		p["mountOpt"] = map[string]any{"ExtraOptions": []string{"rsize=1048576", "wsize=1048576", "readahead=16"}}
	case "windows":
		// network mode makes the name a UNC share name
		p["volname"] = SafeFolder(volName, goos)
		p["mountType"] = "cmount"
		p["network_mode"] = true
	default:
		p["mountType"] = "mount"
	}
	return p
}
