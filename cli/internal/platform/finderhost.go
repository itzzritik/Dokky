package platform

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
)

const FinderHost = "Dokky"

const (
	finderHostMark   = " # dokky"
	finderHostLine   = "127.0.0.1 Dokky" + finderHostMark
	finderHostScript = `do shell script "sed -i '' '/ # dokky$/d' /etc/hosts && printf '\\n127.0.0.1 Dokky # dokky\\n' >> /etc/hosts" with administrator privileges with prompt "dokky wants to show your drives under Dokky in Finder."`
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

// dokky's lines in the hosts file; anything beyond the one Dokky line is left from per-drive names and points real hosts at this Mac
func dokkyLines() []string {
	b, _ := os.ReadFile(hostsFile)
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasSuffix(strings.TrimRight(l, " \t\r"), finderHostMark) {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}

// Dokky resolves and its line holds nothing else
func FinderHostClean() bool {
	if !FinderHostReady() {
		return false
	}
	lines := dokkyLines()
	return len(lines) == 0 || len(lines) == 1 && lines[0] == finderHostLine
}

// asks while the name is missing or its line carries old drive names, rewriting it to the single Dokky line
func EnsureFinderHost() error {
	if runtime.GOOS != "darwin" || FinderHostClean() {
		return nil
	}
	return askAdmin(finderHostScript)
}

func RemoveFinderHost() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	b, err := os.ReadFile(hostsFile)
	if err != nil || !bytes.Contains(b, []byte(finderHostMark+"\n")) && !bytes.HasSuffix(b, []byte(finderHostMark)) {
		return err
	}
	return askAdmin(removeHostScript)
}
