//go:build windows

package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// only this user (or an admin) can own a pipe as their SID
// pipes are machine-wide, so the state folder is part of the name
func SocketAddr(stateDir string) string {
	h := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(stateDir))))
	name := `\\.\pipe\dokky-` + hex.EncodeToString(h[:4])
	if sid, err := currentSID(); err == nil {
		name += "-" + sid.String()
	}
	return name
}

func Listen(addr string) (net.Listener, error) {
	sid, err := currentSID()
	if err != nil {
		return nil, err
	}
	cfg := &winio.PipeConfig{SecurityDescriptor: "O:" + sid.String() + "D:P(A;;GA;;;" + sid.String() + ")"}
	ln, err := winio.ListenPipe(addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("listen on pipe %s: %w", addr, err)
	}
	return ln, nil
}

func Dial(addr string, timeout time.Duration) (net.Conn, error) {
	conn, err := winio.DialPipe(addr, &timeout)
	if err != nil {
		return nil, err
	}
	if err := verifyPipeOwner(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func currentSID() (*windows.SID, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("current user sid: %w", err)
	}
	return tu.User.Sid, nil
}
