package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/rclone/rclone/fs"
)

const (
	backendURL  = "https://dokky.ritik.me"
	releasesURL = "https://github.com"
	winfspX64   = `C:\Program Files (x86)\WinFsp\bin\winfsp-x64.dll`
	winfspArm64 = `C:\Program Files (x86)\WinFsp\bin\winfsp-a64.dll`
	minMacMajor = 13
)

var ErrNotRunning = errors.New("dokky daemon is not running")

type Check struct {
	Name   string
	OK     bool
	Warn   bool
	Detail string
	Fix    string
}

type Env struct {
	GOOS            string
	LookPath        func(string) (string, error)
	Stat            func(string) (os.FileInfo, error)
	ReadFile        func(string) ([]byte, error)
	Access          func(path string) error
	HTTPGet         func(url string) error
	MacVersion      func() (string, error)
	DaemonInstalled bool
	DaemonVersion   func() (string, error)
	Version         string
	KeyringFallback bool
	KeyringErr      error
	FreeBytes       uint64
	MinFreeBytes    uint64
	LogPath         string
}

func Run(_ context.Context, env Env) []Check {
	var net [2]Check
	var wg sync.WaitGroup
	for i, u := range []string{backendURL, releasesURL} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			net[i] = reachable(env, u)
		}()
	}

	var cs []Check
	switch env.GOOS {
	case "linux":
		cs = append(cs, checkFuse(env))
	case "windows":
		cs = append(cs, checkWinFsp(env))
	case "darwin":
		cs = append(cs, checkMac(env))
	}
	cs = append(cs, checkDaemon(env)...)
	cs = append(cs, checkKeychain(env))
	wg.Wait()
	cs = append(cs, net[0], net[1], checkDisk(env))
	return cs
}

func pass(name, detail string) Check { return Check{Name: name, OK: true, Detail: detail} }

func fixes(distro string) string {
	apt, dnf, pacman, zypper := "sudo apt install fuse3", "sudo dnf install fuse3", "sudo pacman -S fuse3", "sudo zypper install fuse3"
	for _, id := range strings.Fields(distro) {
		switch id {
		case "debian", "ubuntu":
			return apt
		case "fedora", "rhel", "centos":
			return dnf
		case "arch":
			return pacman
		case "suse", "opensuse", "opensuse-leap", "opensuse-tumbleweed":
			return zypper
		}
	}
	return strings.Join([]string{apt, dnf, pacman, zypper}, "\n")
}

func osReleaseIDs(env Env) string {
	b, err := env.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	var ids []string
	for line := range strings.SplitSeq(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && (k == "ID" || k == "ID_LIKE") {
			ids = append(ids, strings.Trim(v, `"'`))
		}
	}
	return strings.Join(ids, " ")
}

func checkFuse(env Env) Check {
	const name = "fuse3"
	if _, err := env.LookPath("fusermount3"); err != nil {
		return Check{Name: name, Detail: "fusermount3 is not installed", Fix: fixes(osReleaseIDs(env))}
	}
	if err := env.Access("/dev/fuse"); err != nil {
		return Check{Name: name, Detail: "cannot open /dev/fuse: " + err.Error(),
			Fix: "sudo modprobe fuse, then check you can read and write /dev/fuse"}
	}
	return pass(name, "fusermount3 and /dev/fuse are available")
}

func checkWinFsp(env Env) Check {
	const name = "winfsp"
	for _, p := range []string{winfspX64, winfspArm64} {
		if _, err := env.Stat(p); err == nil {
			return pass(name, "WinFsp is installed")
		}
	}
	return Check{Name: name, Detail: "WinFsp is not installed", Fix: "winget install -e --id WinFsp.WinFsp"}
}

func checkMac(env Env) Check {
	const name = "macos version"
	v, err := env.MacVersion()
	if err != nil {
		return Check{Name: name, Warn: true, Detail: "could not read the macOS version: " + err.Error()}
	}
	major, _ := strconv.Atoi(strings.SplitN(v, ".", 2)[0])
	if major < minMacMajor {
		return Check{Name: name, Detail: "macOS " + v + " is too old, dokky needs macOS 13 or newer", Fix: "update macOS to 13 or newer"}
	}
	return pass(name, "macOS "+v)
}

func checkDaemon(env Env) []Check {
	var cs []Check
	if env.DaemonInstalled {
		cs = append(cs, pass("daemon installed", "the background service is set up"))
	} else {
		cs = append(cs, Check{Name: "daemon installed", Warn: true, Detail: "the background service is not set up", Fix: "dokky status"})
	}
	v, err := env.DaemonVersion()
	if err != nil {
		detail := "could not reach the daemon: " + err.Error()
		if errors.Is(err, ErrNotRunning) {
			detail = "the daemon is not running"
		}
		return append(cs, Check{Name: "daemon running", Warn: true, Detail: detail, Fix: "dokky status"})
	}
	cs = append(cs, pass("daemon running", "version "+v))
	if v != env.Version {
		return append(cs, Check{Name: "daemon version", Detail: fmt.Sprintf("daemon is %s, dokky is %s", v, env.Version), Fix: "dokky status"})
	}
	return append(cs, pass("daemon version", "daemon and dokky are both "+v))
}

func checkKeychain(env Env) Check {
	const name = "keychain"
	switch {
	case errors.Is(env.KeyringErr, store.ErrKeyMissing):
		return Check{Name: name, Detail: env.KeyringErr.Error(), Fix: "restore the store key to the keychain, or move the dokky state folder aside and sign in again"}
	case env.KeyringErr != nil:
		return Check{Name: name, Detail: env.KeyringErr.Error(), Fix: "unlock your keychain, then run dokky doctor again"}
	case env.KeyringFallback:
		return Check{Name: name, Warn: true, Detail: "store key is in a file because Secret Service isn't available",
			Fix: "install and unlock a Secret Service keyring such as gnome-keyring or KWallet"}
	}
	return pass(name, "the store key is in the keychain")
}

func reachable(env Env, url string) Check {
	name := strings.TrimPrefix(url, "https://")
	if err := env.HTTPGet(url); err != nil {
		return Check{Name: name, Detail: "cannot reach " + url + ": " + err.Error(), Fix: "check your internet connection, proxy and firewall"}
	}
	return pass(name, "reachable")
}

func checkDisk(env Env) Check {
	free, floor := fs.SizeSuffix(env.FreeBytes).ByteUnit(), fs.SizeSuffix(env.MinFreeBytes).ByteUnit()
	if env.FreeBytes < env.MinFreeBytes {
		return Check{Name: "disk space", Warn: true, Detail: fmt.Sprintf("%s free, below the %s dokky keeps free", free, floor),
			Fix: "free up disk space or lower the cache min free space in the mount settings"}
	}
	return pass("disk space", fmt.Sprintf("%s free", free))
}
