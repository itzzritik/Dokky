package doctor

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/stretchr/testify/require"
)

const gib = 1 << 30

func good(goos string) Env {
	return Env{
		GOOS:            goos,
		LookPath:        func(string) (string, error) { return "/usr/bin/x", nil },
		Stat:            func(string) (os.FileInfo, error) { return nil, nil },
		ReadFile:        func(string) ([]byte, error) { return []byte("ID=ubuntu\n"), nil },
		Access:          func(string) error { return nil },
		HTTPGet:         func(string) error { return nil },
		MacVersion:      func() (string, error) { return "14.5", nil },
		DaemonInstalled: true,
		DaemonVersion:   func() (string, error) { return "1.2.3", nil },
		Version:         "1.2.3",
		FreeBytes:       100 * gib,
		MinFreeBytes:    10 * gib,
		LogPath:         "/tmp/dokky.log",
	}
}

func find(t *testing.T, cs []Check, prefix string) Check {
	t.Helper()
	for _, c := range cs {
		if strings.HasPrefix(c.Name, prefix) {
			return c
		}
	}
	require.FailNow(t, "no check named "+prefix)
	return Check{}
}

func TestAllGood(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, c := range Run(t.Context(), good(goos)) {
			require.True(t, c.OK, "%s %s: %s", goos, c.Name, c.Detail)
			require.Empty(t, c.Fix)
		}
	}
}

func TestLinuxMissingFuseShowsDistroFixes(t *testing.T) {
	env := good("linux")
	env.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	env.Access = func(string) error { return os.ErrNotExist }
	env.ReadFile = func(string) ([]byte, error) { return []byte("ID=fedora\nID_LIKE=\"rhel fedora\"\n"), nil }
	c := find(t, Run(t.Context(), env), "fuse")
	require.False(t, c.OK)
	require.Equal(t, "sudo dnf install fuse3", c.Fix)

	env.ReadFile = func(string) ([]byte, error) { return []byte("ID=pop\nID_LIKE=\"ubuntu debian\"\n"), nil }
	require.Equal(t, "sudo apt install fuse3", find(t, Run(t.Context(), env), "fuse").Fix)

	env.ReadFile = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	fix := find(t, Run(t.Context(), env), "fuse").Fix
	for _, cmd := range []string{"sudo apt install fuse3", "sudo dnf install fuse3", "sudo pacman -S fuse3", "sudo zypper install fuse3"} {
		require.Contains(t, fix, cmd)
	}
}

func TestLinuxFuseNeedsDeviceAccess(t *testing.T) {
	env := good("linux")
	env.Access = func(string) error { return os.ErrPermission }
	c := find(t, Run(t.Context(), env), "fuse")
	require.False(t, c.OK)
	require.Contains(t, c.Fix, "/dev/fuse")
}

func TestWindowsMissingWinFsp(t *testing.T) {
	env := good("windows")
	env.Stat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	c := find(t, Run(t.Context(), env), "winfsp")
	require.False(t, c.OK)
	require.Equal(t, `winget install -e --id WinFsp.WinFsp`, c.Fix)
}

func TestOldMacFails(t *testing.T) {
	env := good("darwin")
	env.MacVersion = func() (string, error) { return "12.7.4", nil }
	require.False(t, find(t, Run(t.Context(), env), "macos").OK)
}

func TestLowDiskWarns(t *testing.T) {
	env := good("linux")
	env.FreeBytes = 2 * gib
	c := find(t, Run(t.Context(), env), "disk")
	require.False(t, c.OK)
	require.True(t, c.Warn)
	require.NotEmpty(t, c.Fix)
}

func TestDaemonChecks(t *testing.T) {
	env := good("linux")
	env.DaemonVersion = func() (string, error) { return "", ErrNotRunning }
	c := find(t, Run(t.Context(), env), "daemon running")
	require.True(t, c.Warn)
	require.Equal(t, "dokky status", c.Fix)

	env = good("linux")
	env.DaemonVersion = func() (string, error) { return "1.0.0", nil }
	c = find(t, Run(t.Context(), env), "daemon version")
	require.False(t, c.OK)
	require.False(t, c.Warn)
	require.Contains(t, c.Detail, "1.0.0")
}

func TestKeychain(t *testing.T) {
	env := good("linux")
	env.KeyringFallback = true
	c := find(t, Run(t.Context(), env), "keychain")
	require.True(t, c.Warn)
	require.Contains(t, c.Detail, "Secret Service")

	env.KeyringFallback, env.KeyringErr = false, store.ErrKeyMissing
	c = find(t, Run(t.Context(), env), "keychain")
	require.False(t, c.OK)
	require.False(t, c.Warn)
}

func TestNetworkFailures(t *testing.T) {
	env := good("linux")
	env.HTTPGet = func(u string) error {
		if strings.Contains(u, "github") {
			return errors.New("timeout")
		}
		return nil
	}
	cs := Run(t.Context(), env)
	require.True(t, find(t, cs, "dokky.ritik.me").OK)
	require.False(t, find(t, cs, "github.com").OK)
}

func TestDriverChecksOnlyTheMountDriver(t *testing.T) {
	env := good("linux")
	env.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	require.Equal(t, "sudo apt install fuse3", Driver(env).Fix)

	env = good("windows")
	env.Stat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	require.Equal(t, "winget install -e --id WinFsp.WinFsp", Driver(env).Fix)

	require.True(t, Driver(Env{GOOS: "darwin"}).OK)
}
