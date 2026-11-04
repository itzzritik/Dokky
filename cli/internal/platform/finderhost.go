package platform

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
)

const FinderHost = "Dokky"

const finderHostScript = `do shell script "printf '\\n127.0.0.1 Dokky # dokky\\n' >> /etc/hosts" with administrator privileges with prompt "dokky wants to show your drives as Dokky in Finder."`

var (
	lookupHost = net.LookupHost
	askAdmin   = func(script string) error { return exec.Command("osascript", "-e", script).Run() }
)

func FinderHostReady() bool {
	addrs, err := lookupHost(FinderHost)
	return err == nil && slices.Contains(addrs, "127.0.0.1")
}

// asks once on macOS; a refusal is remembered and mounts fall back to localhost
func EnsureFinderHost(stateDir string) error {
	if runtime.GOOS != "darwin" || FinderHostReady() {
		return nil
	}
	asked := filepath.Join(stateDir, "finder-host-asked")
	if _, err := os.Stat(asked); err == nil {
		return nil
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(asked, nil, 0o600); err != nil {
		return err
	}
	return askAdmin(finderHostScript)
}
