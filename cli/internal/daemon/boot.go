package daemon

import (
	"time"

	"github.com/shirou/gopsutil/v4/host"
)

const loginWindow = 10 * time.Minute

func bootedRecently() bool {
	boot, err := host.BootTime()
	return err == nil && time.Since(time.Unix(int64(boot), 0)) < loginWindow
}
