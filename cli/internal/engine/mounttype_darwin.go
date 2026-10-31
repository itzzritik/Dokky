package engine

import (
	"time"

	_ "github.com/rclone/rclone/cmd/nfsmount"
	"github.com/rclone/rclone/cmd/serve/nfs"
	"golang.org/x/sys/unix"
)

// rclone keeps one global nfs unmount hook that each mount overwrites, so ejecting one mount would stop the last one
func init() {
	takeUnmountHook = func(e *Engine) func() {
		own := nfs.OnUnmountFunc
		nfs.OnUnmountFunc = func() { go e.reapUnmounted() }
		return own
	}
}

func (e *Engine) reapUnmounted() {
	for range 50 {
		if points, err := osMountPoints(); err == nil && e.reap(points) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (e *Engine) reap(points map[string]bool) bool {
	var hooks []func()
	e.mu.Lock()
	for name, lm := range e.mounted {
		if lm.onUnmount != nil && !points[realPoint(lm.point)] {
			hooks = append(hooks, lm.onUnmount)
			delete(e.mounted, name)
		}
	}
	e.mu.Unlock()
	for _, h := range hooks {
		h()
	}
	return len(hooks) > 0
}

func IsMounted(point string) bool {
	points, err := osMountPoints()
	return err == nil && points[realPoint(point)]
}

func osMountPoints() (map[string]bool, error) {
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}
	buf := make([]unix.Statfs_t, n+8)
	n, err = unix.Getfsstat(buf, unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}
	points := make(map[string]bool, n)
	for _, s := range buf[:n] {
		points[unix.ByteSliceToString(s.Mntonname[:])] = true
	}
	return points, nil
}
