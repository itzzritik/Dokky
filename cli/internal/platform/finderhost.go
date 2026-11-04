package platform

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
)

const FinderHost = "Dokky"

const (
	finderHostLine   = "127.0.0.1 Dokky # dokky"
	finderHostScript = `do shell script "printf '\\n127.0.0.1 Dokky # dokky\\n' >> /etc/hosts" with administrator privileges with prompt "dokky wants to show your drives as Dokky in Finder."`
	removeHostScript = `do shell script "sed -i '' '/ # dokky$/d' /etc/hosts" with administrator privileges with prompt "dokky was uninstalled and wants to remove its Dokky name from Finder."`
)

var (
	hostsFile  = "/etc/hosts"
	lookupHost = net.LookupHost
	askAdmin   = func(script string) error { return exec.Command("osascript", "-e", script).Run() }
)

func FinderHostReady() bool {
	addrs, err := lookupHost(FinderHost)
	return err == nil && slices.Contains(addrs, "127.0.0.1")
}

// asks on every interactive launch while missing, so a hand-removed line comes back
func EnsureFinderHost() error {
	if runtime.GOOS != "darwin" || FinderHostReady() {
		return nil
	}
	return askAdmin(finderHostScript)
}

func RemoveFinderHost() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	b, err := os.ReadFile(hostsFile)
	if err != nil || !bytes.Contains(b, []byte(finderHostLine)) {
		return err
	}
	return askAdmin(removeHostScript)
}
