//go:build windows && cmount

package engine

import _ "github.com/rclone/rclone/cmd/cmount"

// WinFsp mounts die with the process, so none is ever stale
func IsMounted(string) bool { return false }
