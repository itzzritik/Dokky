//go:build darwin

package platform

import (
	"errors"
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func VerifyPeer(conn net.Conn) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return errors.New("peer check needs a unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return err
	}
	var uid uint32
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, e := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if e != nil {
			credErr = e
			return
		}
		uid = cred.Uid
	}); err != nil {
		return err
	}
	if credErr != nil {
		return credErr
	}
	if uid != uint32(os.Getuid()) {
		return fmt.Errorf("peer uid %d is not the current user", uid)
	}
	return nil
}
