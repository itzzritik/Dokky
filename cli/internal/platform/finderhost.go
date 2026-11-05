package platform

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"unicode"
)

const FinderHost = "Dokky"

const (
	finderHostMark   = " # dokky"
	removeHostScript = `do shell script "sed -i '' '/ # dokky$/d' /etc/hosts" with administrator privileges with prompt "dokky was uninstalled and wants to remove its Dokky name from Finder."`
)

var (
	hostsFile  = "/etc/hosts"
	lookupHost = net.LookupHost
	askAdmin   = func(script string) error { return exec.Command("osascript", "-e", script).Run() }
)

func resolves(host string) bool {
	addrs, err := lookupHost(host)
	return err == nil && slices.Contains(addrs, "127.0.0.1")
}

func FinderHostReady() bool { return resolves(FinderHost) }

// Finder groups network drives by server, so each drive gets a server name of its own, best first:
// its name with no-break spaces (a hosts file splits on plain ones), then a plain ASCII one with hyphens
func DriveHosts(name string) []string {
	clean := func(space rune, keep func(rune) bool) string {
		var b strings.Builder
		for _, r := range name {
			switch {
			case r == ' ':
				b.WriteRune(space)
			case keep(r) || r == '-':
				b.WriteRune(r)
			}
		}
		return strings.Trim(b.String(), "-\u00a0")
	}
	ascii := func(r rune) bool { return r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) }
	pretty, plain := clean('\u00a0', func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }), clean('-', ascii)
	var out []string
	for _, h := range []string{pretty, plain} {
		if h != "" && !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

// the drive's own server name when one resolves, else Dokky, else localhost
func MountHost(name string) string {
	for _, h := range DriveHosts(name) {
		if resolves(h) {
			return h
		}
	}
	if FinderHostReady() {
		return FinderHost
	}
	return "localhost"
}

func FinderHostsReady(names []string) bool {
	if !FinderHostReady() {
		return false
	}
	for _, n := range names {
		if hs := DriveHosts(n); len(hs) > 0 && !slices.ContainsFunc(hs, resolves) {
			return false
		}
	}
	return true
}

// one admin prompt rewrites dokky's line with every drive's names, so removed drives drop out
func EnsureFinderHosts(names []string) error {
	if runtime.GOOS != "darwin" || FinderHostsReady(names) {
		return nil
	}
	hosts := []string{FinderHost}
	for _, n := range names {
		hosts = append(hosts, DriveHosts(n)...)
	}
	line := "127.0.0.1 " + strings.Join(hosts, " ") + finderHostMark
	return askAdmin(`do shell script "sed -i '' '/ # dokky$/d' /etc/hosts && printf '\\n%s\\n' '` + line + `' >> /etc/hosts" with administrator privileges with prompt "dokky wants to show each drive under its own name in Finder."`)
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
