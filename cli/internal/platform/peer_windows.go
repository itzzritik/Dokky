//go:build windows

package platform

import "net"

func VerifyPeer(net.Conn) error { return nil }
