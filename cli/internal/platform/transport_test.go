package platform

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testAddr(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return `\\.\pipe\dokky-test-` + filepath.Base(t.TempDir())
	}
	dir, err := os.MkdirTemp("/tmp", "dky")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return SocketAddr(dir)
}

func TestListenDialRoundTrip(t *testing.T) {
	addr := testAddr(t)
	ln, err := Listen(addr)
	require.NoError(t, err)
	defer ln.Close()

	done := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		if err := VerifyPeer(c); err != nil {
			done <- err
			return
		}
		buf := make([]byte, 4)
		if _, err := c.Read(buf); err != nil {
			done <- err
			return
		}
		_, err = c.Write(buf)
		done <- err
	}()

	c, err := Dial(addr, time.Second)
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Write([]byte("ping"))
	require.NoError(t, err)
	buf := make([]byte, 4)
	_, err = c.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "ping", string(buf))
	require.NoError(t, <-done)
}

func TestVerifyPeerRejectsNonUnixConn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pipe ACL enforces the user")
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	require.Error(t, VerifyPeer(a))
}

func TestSocketMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets only")
	}
	addr := testAddr(t)
	ln, err := Listen(addr)
	require.NoError(t, err)
	defer ln.Close()
	fi, err := os.Stat(addr)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

func TestStaleSocketReplaced(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets only")
	}
	addr := testAddr(t)
	stale, err := net.Listen("unix", addr)
	require.NoError(t, err)
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	require.NoError(t, stale.Close())
	_, err = os.Stat(addr)
	require.NoError(t, err)

	ln, err := Listen(addr)
	require.NoError(t, err)
	defer ln.Close()
	c, err := Dial(addr, time.Second)
	require.NoError(t, err)
	c.Close()
}

func TestLiveSocketNotStolen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets only")
	}
	addr := testAddr(t)
	live, err := Listen(addr)
	require.NoError(t, err)
	defer live.Close()

	_, err = Listen(addr)
	require.Error(t, err)

	c, err := Dial(addr, time.Second)
	require.NoError(t, err)
	c.Close()
}

func TestListenPathTooLong(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets only")
	}
	addr := "/tmp/" + strings.Repeat("a", 120) + "/dokky.sock"
	_, err := Listen(addr)
	require.ErrorContains(t, err, "too long")
	require.ErrorContains(t, err, addr)
}

func TestListenTightensParentTo0700(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets only")
	}
	dir, err := os.MkdirTemp("/tmp", "dky")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	require.NoError(t, os.Chmod(dir, 0o755))

	ln, err := Listen(SocketAddr(dir))
	require.NoError(t, err)
	defer ln.Close()
	requireMode(t, dir, 0o700)
}

func TestListenCreatesParent0700(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets only")
	}
	root, err := os.MkdirTemp("/tmp", "dky")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(root) })
	dir := filepath.Join(root, "s")

	ln, err := Listen(SocketAddr(dir))
	require.NoError(t, err)
	defer ln.Close()
	requireMode(t, dir, 0o700)
}

func TestListenRefusesForeignParent(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs a non-root unix user")
	}
	_, err := Listen("/tmp/dokky-test.sock")
	require.ErrorContains(t, err, "/tmp")
	require.ErrorContains(t, err, "not owned")
}
