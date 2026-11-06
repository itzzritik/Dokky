package buildinfo

import (
	"os"
	"strings"
	"time"

	"github.com/coreos/go-semver/semver"
)

var Version = "0.0.0-dev"

// read when the process starts, so a daemon still running older code never reports a binary rebuilt under it
var stamp = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return ""
	}
	return exe + "@" + fi.ModTime().UTC().Format(time.RFC3339Nano)
}()

// this binary's path and build time, which tells one dev build from the next
func Stamp() string { return stamp }

func Dev() bool { return strings.Contains(Version, "dev") }

func Newer(a, than string) bool {
	va, err := semver.NewVersion(a)
	if err != nil {
		return false
	}
	vb, err := semver.NewVersion(than)
	return err == nil && vb.LessThan(*va)
}
