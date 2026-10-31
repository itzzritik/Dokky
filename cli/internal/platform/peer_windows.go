//go:build windows

package platform

import (
	"errors"
	"fmt"
	"net"

	"golang.org/x/sys/windows"
)

func VerifyPeer(net.Conn) error { return nil }

// another local user could create the pipe first and read what we send
func verifyPipeOwner(conn net.Conn) error {
	h, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return errors.New("cannot check who owns the dokky pipe")
	}
	sd, err := windows.GetSecurityInfo(windows.Handle(h.Fd()), windows.SE_KERNEL_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read the dokky pipe owner: %w", err)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return errors.New("the dokky pipe has no owner")
	}
	me, err := currentSID()
	if err != nil {
		return err
	}
	if !windows.EqualSid(owner, me) {
		return fmt.Errorf("the dokky pipe is owned by %s, not the current user; refusing to connect", owner)
	}
	return nil
}
