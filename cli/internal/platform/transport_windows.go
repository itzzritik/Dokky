//go:build windows

package platform

import (
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// only this user (or an admin) can own a pipe as their SID
func SocketAddr(string) string {
	sid, err := currentSID()
	if err != nil {
		return `\\.\pipe\dokky`
	}
	return `\\.\pipe\dokky-` + sid.String()
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
