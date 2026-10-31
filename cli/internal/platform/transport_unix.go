//go:build !windows

package platform

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

func SocketAddr(stateDir string) string {
	return filepath.Join(stateDir, "dokky.sock")
}

func Listen(addr string) (net.Listener, error) {
	limit := 108
	if runtime.GOOS == "darwin" {
		limit = 104
	}
	if len(addr) >= limit {
		return nil, fmt.Errorf("socket path too long (%d bytes, max %d): %s", len(addr), limit-1, addr)
	}
	if err := secureParent(filepath.Dir(addr)); err != nil {
		return nil, err
	}
	if err := removeStaleSocket(addr); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	if err := os.Chmod(addr, 0o600); err != nil {
		ln.Close()
		return nil, fmt.Errorf("chmod socket %s: %w", addr, err)
	}
	return ln, nil
}

func secureParent(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create socket dir %s: %w", dir, err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Getuid() {
		return fmt.Errorf("socket dir %s is not owned by the current user", dir)
	}
	if fi.Mode().Perm() != 0o700 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("chmod socket dir %s: %w", dir, err)
		}
	}
	return nil
}

func removeStaleSocket(addr string) error {
	fi, err := os.Lstat(addr)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s exists and is not a socket", addr)
	}
	c, err := net.DialTimeout("unix", addr, time.Second)
	if err == nil {
		c.Close()
		return fmt.Errorf("%s is in use by a running daemon", addr)
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("probe socket %s: %w", addr, err)
	}
	return os.Remove(addr)
}

func Dial(addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", addr, timeout)
}
