package engine

import (
	"github.com/moby/sys/mountinfo"
	_ "github.com/rclone/rclone/cmd/mount"
)

func IsMounted(point string) bool {
	infos, err := mountinfo.GetMounts(mountinfo.SingleEntryFilter(realPoint(point)))
	return err == nil && len(infos) > 0
}
