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
	// how dokky opens: "" before a master password exists (the old store key), else the vault's unlock mode
	Unlock       string
	FreeBytes    uint64
	MinFreeBytes uint64
	LogPath      string
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
	case "linux", "windows":
		cs = append(cs, Driver(env))
	case "darwin":
		cs = append(cs, checkMac(env))
	}
	cs = append(cs, checkDaemon(env)...)
	cs = append(cs, checkKeychain(env))
	wg.Wait()
	cs = append(cs, net[0], net[1], checkDisk(env))
	return cs
}

func Driver(env Env) Check {
	switch env.GOOS {
	case "linux":
		return checkFuse(env)
	case "windows":
		return checkWinFsp(env)
	}
	return pass("driver", "no driver needed")
}

func OpenReadWrite(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	return f.Close()
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

// before a master password the store key opens the saved drive logins; after, the device key opens the vault
// at startup, and in password mode nothing is kept on the computer
func checkKeychain(env Env) Check {
	const name = "keychain"
	key := "store key"
	if env.Unlock != "" {
		key = "device key"
	}
	switch {
	case env.Unlock == "password":
		return pass(name, "no key is kept on this computer: dokky opens with your master password")
	case errors.Is(env.KeyringErr, store.ErrKeyMissing):
		return Check{Name: name, Detail: env.KeyringErr.Error(), Fix: "restore the store key to the keychain, or move the dokky state folder aside and sign in again"}
	case errors.Is(env.KeyringErr, store.ErrNotFound) && env.Unlock != "":
		return Check{Name: name, Warn: true, Detail: "the device key is missing, so dokky waits for your master password at startup",
			Fix: "open dokky and unlock it with your master password; it saves a new device key"}
	case env.KeyringErr != nil:
		return Check{Name: name, Detail: env.KeyringErr.Error(), Fix: "unlock your keychain, then run dokky doctor again"}
	case env.KeyringFallback:
		return Check{Name: name, Warn: true, Detail: key + " is in a file because Secret Service isn't available",
			Fix: "install and unlock a Secret Service keyring such as gnome-keyring or KWallet"}
	case env.Unlock != "":
		return pass(name, "the device key that opens your vault at startup is in the keychain")
	}
	return pass(name, "the store key that opens your saved drive logins is in the keychain")
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
			Fix: "free up disk space, or pick a shorter cache max age for your mounts in the dokky Settings tab"}
	}
	return pass("disk space", fmt.Sprintf("%s free", free))
}
