//go:build darwin || linux

package lockwatch

import (
	"bufio"
	"os/exec"
)

// scan runs a monitor and calls onLock for each line it prints that match says is a lock, until it exits
func scan(cmd *exec.Cmd, match func(string) bool, onLock func()) {
	out, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	lines := bufio.NewScanner(out)
	for lines.Scan() {
		if match(lines.Text()) {
			onLock()
		}
	}
	_ = cmd.Wait()
}
