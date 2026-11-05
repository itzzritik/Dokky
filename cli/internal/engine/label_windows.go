package engine

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Explorer names a network drive "share (\\server) (X:)" unless the share has a label of its own, which is what renaming the drive in Explorer sets
func labelShare(name string) {
	// the share rclone mounts: the volume name made safe, cut to 32 bytes
	share := strings.TrimSpace(SafeFolder(name, "windows"))
	if len(share) > 32 {
		share = share[:32]
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\MountPoints2\##server#`+share, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.SetStringValue("_LabelFromReg", name)
}
