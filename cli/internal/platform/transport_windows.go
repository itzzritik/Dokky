//go:build windows

package platform

import (
	"fmt"
	"net"
	"os/user"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func SocketAddr(string) string {
	name := "user"
	if u, err := user.Current(); err == nil {
		name = u.Username[strings.LastIndex(u.Username, `\`)+1:]
	}
	return `\\.\pipe\dokky-` + name
}

func Listen(addr string) (net.Listener, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("current user sid: %w", err)
	}
	sid := tu.User.Sid.String()
	cfg := &winio.PipeConfig{SecurityDescriptor: "O:" + sid + "D:P(A;;GA;;;" + sid + ")"}
	ln, err := winio.ListenPipe(addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("listen on pipe %s: %w", addr, err)
	}
	return ln, nil
}

func Dial(addr string, timeout time.Duration) (net.Conn, error) {
	return winio.DialPipe(addr, &timeout)
}
