//go:build !windows

package platform

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// "macOS 15.2", or the Linux distro's own name like "Ubuntu 24.04.1 LTS"
var OSName = sync.OnceValue(func() string {
	if runtime.GOOS == "darwin" {
		return strings.TrimSpace("macOS " + output("sw_vers", "-productVersion"))
	}
	b, _ := os.ReadFile("/etc/os-release")
	for line := range strings.SplitSeq(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok && strings.Trim(v, `"'`) != "" {
			return strings.Trim(v, `"'`)
		}
	}
	return "Linux"
})

// the name people gave this computer, like "Ritik's MacBook Pro"
func ComputerName() string {
	if runtime.GOOS == "darwin" {
		if n := output("scutil", "--get", "ComputerName"); n != "" {
			return n
		}
	}
	h, _ := os.Hostname()
	return strings.TrimSuffix(strings.TrimSpace(h), ".local")
}

func output(name string, args ...string) string {
	b, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
