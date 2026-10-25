package mux

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func checkPeer(conn *net.UnixConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var check error
	err = raw.Control(func(fd uintptr) {
		cred, e := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if e != nil {
			check = e
		} else if cred.Uid != uint32(os.Getuid()) {
			check = fmt.Errorf("socket peer has different UID")
		}
	})
	if err != nil {
		return err
	}
	return check
}
