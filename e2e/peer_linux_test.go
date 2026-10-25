package e2e

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func serverPID(t *testing.T, path string) int {
	t.Helper()
	conn, err := net.Dial("unix", path)
	require.NoError(t, err)
	defer conn.Close()
	raw, err := conn.(*net.UnixConn).SyscallConn()
	require.NoError(t, err)
	var pid int
	require.NoError(t, raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		require.NoError(t, err)
		pid = int(cred.Pid)
	}))
	require.Positive(t, pid)
	return pid
}
