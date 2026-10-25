package e2e

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"
)

func runRobustness(t *testing.T, bin string) {
	t.Run("vim-less-htop", func(t *testing.T) {
		for _, app := range []struct{ name, command, marker, quit string }{
			{"vim", "vim -Nu NONE -n fixture.txt", "APPLICATION-FIXTURE-000", ":q!\r"},
			{"less", "less -R fixture.txt", "APPLICATION-FIXTURE-000", "q"},
			{"htop", "htop -C", "Tasks:", "q"},
		} {
			t.Run(app.name, func(t *testing.T) {
				_, err := exec.LookPath(app.name)
				require.NoError(t, err, "E2E prerequisites: install vim, less and htop")
				h := newHarness(t, bin)
				var fixture strings.Builder
				for i := range 100 {
					fmt.Fprintf(&fixture, "APPLICATION-FIXTURE-%03d\n", i)
				}
				require.NoError(t, os.WriteFile(filepath.Join(h.home, "fixture.txt"), []byte(fixture.String()), 0600))
				a := h.client(100, 30, "-S", "application")
				a.await("bs$ ")
				a.command(app.command, app.marker)
				a.write("\x01d")
				a.exited()
				b := h.client(100, 30, "-r", "application")
				b.await(app.marker)
				b.write(app.quit)
				b.await("bs$ ")
			})
		}
	})

	t.Run("terminal-restoration-and-literal-control-a", func(t *testing.T) {
		h := newHarness(t, bin)
		c := h.client(80, 24, "-S", "input")
		c.await("bs$ ")
		c.write("printf 'literal-%s\\n' ok")
		c.write("\x01a")
		c.write("printf 'moved-%s\\n' start; \r")
		c.await("moved-start\nliteral-ok")
		raw, err := term.GetState(int(c.pty.Fd()))
		require.NoError(t, err)
		require.NotNil(t, raw)
		c.write("\x01d")
		c.exited()
		after, err := term.GetState(int(c.pty.Fd()))
		require.NoError(t, err)
		assert.NotEqual(t, raw, after, "client must restore cooked terminal mode on detach")
	})

	t.Run("detached-retains-dimensions", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(110, 32, "-S", "sizing")
		a.await("bs$ ")
		a.command("printf '\\033[20;1Hlower-content\\033[H'", "lower-content")
		b := h.client(80, 24, "-x", "sizing")
		b.await("lower-content")
		b.write("\x01F")
		b.command("printf 'fit-dimensions:'; stty size", "fit-dimensions:24 80")
		a.write("\x01d")
		a.exited()
		b.write("\x01d")
		b.exited()
		c := h.client(120, 40, "-r", "sizing")
		c.await("fit-dimensions:24 80")
		c.command("printf 'retained:'; stty size", "retained:24 80")
		require.NoError(t, pty.Setsize(c.pty, &pty.Winsize{Cols: 60, Rows: 10}))
		c.mu.Lock()
		require.NoError(t, c.screen.Resize(60, 10, 0, 0))
		c.mu.Unlock()
		c.command("printf '\\033[Hcrop-%s\\n' ok", "crop-ok")
	})

	t.Run("history-pruning", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(80, 12, "-S", "prune")
		a.await("bs$ ")
		b := h.client(80, 12, "-x", "prune")
		b.await("bs$ ")
		a.command("i=0; while [ \"$i\" -lt 100 ]; do printf 'old-%04d\\n' \"$i\"; i=$((i+1)); done", "old-0099")
		a.write("\x01\x1b\x1b[5~\x1b[5~")
		a.await("old-0070")
		b.command("i=0; while [ \"$i\" -lt 11000 ]; do printf 'new-%05d\\n' \"$i\"; i=$((i+1)); done; printf 'prune-%s\\n' done", "prune-done")
		a.await("new-")
		a.absent("old-0070")
		a.absent("prune-done")
		a.write("\x1b")
		a.await("prune-done")
	})

	t.Run("background-and-foreground-cleanup", func(t *testing.T) {
		h := newHarness(t, bin)
		c := h.client(80, 24, "-S", "jobs")
		c.await("bs$ ")
		c.command("sleep 600 & printf 'background-%s:%s\\n' pid $!", "background-pid:")
		text := c.text()
		var background int
		_, err := fmt.Sscanf(text[strings.Index(text, "background-pid:"):], "background-pid:%d", &background)
		require.NoError(t, err)
		require.Positive(t, background)
		t.Cleanup(func() {
			if syscall.Kill(background, 0) == nil {
				syscall.Kill(background, syscall.SIGKILL)
			}
		})
		c.command("printf 'foreground-%s\\n' ready; sleep 600", "foreground-ready")
		h.stopDaemon()
		require.Eventually(t, func() bool { return syscall.Kill(background, 0) == syscall.ESRCH }, 5*time.Second, 20*time.Millisecond,
			"background process %d survived daemon shutdown", background)
	})

	t.Run("malformed-and-slow-clients", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(80, 24, "-S", "fast")
		a.await("bs$ ")
		socket := filepath.Join(h.home, ".bs", "bs.sock")
		for _, length := range []uint32{0, 65537, 0xffffffff} {
			c, err := net.DialTimeout("unix", socket, time.Second)
			require.NoError(t, err)
			c.SetDeadline(time.Now().Add(3 * time.Second))
			require.NoError(t, binary.Write(c, binary.BigEndian, length))
			one := make([]byte, 1)
			_, err = c.Read(one)
			require.Error(t, err)
			c.Close()
		}
		slow, err := net.DialTimeout("unix", socket, time.Second)
		require.NoError(t, err)
		defer slow.Close()
		slow.SetWriteDeadline(time.Now().Add(time.Second))
		data, err := json.Marshal(map[string]any{"Version": 1, "Op": "attach", "Name": "fast", "Cols": 80, "Rows": 24, "Multi": true})
		require.NoError(t, err)
		require.NoError(t, binary.Write(slow, binary.BigEndian, uint32(len(data))))
		_, err = slow.Write(data)
		require.NoError(t, err)
		a.command("i=0; while [ \"$i\" -lt 15000 ]; do printf '%070d\\n' \"$i\"; i=$((i+1)); done; printf 'fast-%s\\n' complete", "fast-complete")
		a.command("printf 'responsive-%s\\n' yes", "responsive-yes")
		assert.Contains(t, h.cli("-ls"), "fast")
	})

	t.Run("reject-unsafe-paths", func(t *testing.T) {
		for _, kind := range []string{"directory-symlink", "directory-permissions", "socket-symlink", "socket-file", "socket-permissions"} {
			t.Run(kind, func(t *testing.T) {
				home := t.TempDir()
				target := filepath.Join(home, ".bs")
				switch kind {
				case "directory-symlink":
					require.NoError(t, os.Symlink(t.TempDir(), target))
				case "directory-permissions":
					require.NoError(t, os.Mkdir(target, 0755))
				default:
					require.NoError(t, os.Mkdir(target, 0700))
					socket := filepath.Join(target, "bs.sock")
					if kind == "socket-symlink" {
						require.NoError(t, os.Symlink(filepath.Join(home, "absent"), socket))
					} else if kind == "socket-file" {
						require.NoError(t, os.WriteFile(socket, []byte("not a socket"), 0600))
					} else {
						listener, err := net.Listen("unix", socket)
						require.NoError(t, err)
						defer listener.Close()
						require.NoError(t, os.Chmod(socket, 0666))
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, bin, "-ls")
				cmd.Env = append(os.Environ(), "HOME="+home)
				out, err := cmd.CombinedOutput()
				require.Error(t, err)
				require.NoError(t, ctx.Err())
				assert.Contains(t, string(out), "bs:")
				_, err = os.Lstat(target)
				require.NoError(t, err, "unsafe paths must not be removed")
			})
		}
	})
}
