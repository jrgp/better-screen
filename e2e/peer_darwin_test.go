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
		var err error
		pid, err = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		require.NoError(t, err)
	}))
	require.Positive(t, pid)
	return pid
}
