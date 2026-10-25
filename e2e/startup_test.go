package e2e

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runStartup(t *testing.T, bin string) {
	t.Run("version-without-home", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, "-v")
		cmd.Env = append(os.Environ(), "HOME=")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		assert.Equal(t, "bs 0.1.0\n", string(out))
	})
	t.Run("automatic-daemon-and-stale-socket", func(t *testing.T) {
		home, err := os.MkdirTemp("", "bs-auto-")
		require.NoError(t, err)
		h := &harness{t: t, bin: bin, home: home}
		socket := filepath.Join(home, ".bs", "bs.sock")
		t.Cleanup(func() {
			if c, err := net.DialTimeout("unix", socket, time.Second); err == nil {
				c.Close()
				pid := serverPID(t, socket)
				require.NoError(t, syscall.Kill(pid, syscall.SIGTERM))
				require.Eventually(t, func() bool { _, err := os.Lstat(socket); return os.IsNotExist(err) }, 5*time.Second, 10*time.Millisecond)
			}
			require.NoError(t, os.RemoveAll(home))
		})
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, bin, "-ls")
				cmd.Env = h.env()
				out, err := cmd.CombinedOutput()
				assert.NoError(t, err, string(out))
				assert.Contains(t, string(out), "No sessions.")
			})
		}
		wg.Wait()
		pid := serverPID(t, socket)
		assert.Contains(t, h.cli("-v"), "bs 0.1.0")
		require.NoError(t, syscall.Kill(pid, syscall.SIGKILL))
		require.Eventually(t, func() bool {
			c, err := net.DialTimeout("unix", socket, 50*time.Millisecond)
			if err != nil {
				return true
			}
			c.Close()
			return false
		}, 3*time.Second, 10*time.Millisecond)
		require.FileExists(t, socket)
		assert.Contains(t, h.cli("-ls"), "No sessions.")
		assert.NotEqual(t, pid, serverPID(t, socket))
		a := h.client(80, 24)
		a.await("bs$ ")
		b := h.client(80, 24)
		b.await("bs$ ")
		assert.Contains(t, h.cli("-ls"), "1\t")
		assert.Contains(t, h.cli("-ls"), "2\t")
		a.write("\x01d")
		a.exited()
		b.write("\x01d")
		b.exited()
	})

	t.Run("cli-errors-and-versioned-ipc", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(80, 24, "-S", "work")
		a.await("bs$ ")
		b := h.client(80, 24, "-S", "worker")
		b.await("bs$ ")
		for _, tc := range []struct {
			args    []string
			message string
		}{
			{[]string{"-r", "wor"}, "ambiguous session"},
			{[]string{"-r", "work"}, "is attached"},
			{[]string{"-S", "work"}, "already exists"},
			{[]string{"-r", "missing"}, "no session matching"},
			{[]string{"-S", "bad/name"}, "session names"},
		} {
			c := h.client(80, 24, tc.args...)
			c.await(tc.message)
			select {
			case <-c.waitDone:
				require.Error(t, c.waitErr)
			case <-time.After(4 * time.Second):
				t.Fatal("rejected client did not exit")
			}
		}
		conn, err := net.DialTimeout("unix", filepath.Join(h.home, ".bs", "bs.sock"), time.Second)
		require.NoError(t, err)
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		data, err := json.Marshal(map[string]any{"Version": 999, "Op": "list"})
		require.NoError(t, err)
		require.NoError(t, binary.Write(conn, binary.BigEndian, uint32(len(data))))
		_, err = conn.Write(data)
		require.NoError(t, err)
		var size uint32
		require.NoError(t, binary.Read(conn, binary.BigEndian, &size))
		require.Less(t, size, uint32(4096))
		reply := make([]byte, size)
		_, err = io.ReadFull(conn, reply)
		require.NoError(t, err)
		assert.Contains(t, string(reply), "incompatible protocol version")
		a.command("printf 'valid-%s\\n' connection", "valid-connection")
	})

	t.Run("incremental-physical-rendering", func(t *testing.T) {
		h := newHarness(t, bin)
		c := h.client(80, 24, "-S", "incremental")
		c.await("bs$ ")
		c.mu.Lock()
		before := c.raw.Len()
		c.mu.Unlock()
		c.command("printf 'incremental-%s\\n' output", "incremental-output")
		c.mu.Lock()
		output := c.raw.String()[before:]
		c.mu.Unlock()
		assert.NotContains(t, output, "\x1b[2J", "ordinary output must not clear the screen")
		assert.Less(t, strings.Count(output, "\x1b[2K"), 10, "only changed rows should be repainted")
	})
}
