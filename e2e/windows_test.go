package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runWindowLifecycle(t *testing.T, bin string) {
	t.Run("bare-multi-attach-validation", func(t *testing.T) {
		h := newHarness(t, bin)
		for _, tc := range []struct {
			name string
			args []string
			want string
		}{
			{"no-sessions", []string{"-x"}, "no session matching"},
			{"no-sessions-long-flag", []string{"--x"}, "no session matching"},
			{"list-conflict", []string{"-x", "-ls"}, "use bs"},
			{"create-conflict", []string{"-S", "other", "-x"}, "use bs"},
			{"reattach-conflict", []string{"-x", "-r", "other"}, "use bs"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				c := h.client(80, 24, tc.args...)
				c.await(tc.want)
				select {
				case <-c.waitDone:
					require.Error(t, c.waitErr)
				case <-time.After(5 * time.Second):
					t.Fatal("invalid attach did not exit")
				}
			})
		}
		assert.Contains(t, h.cli("-ls"), "No sessions.")
	})

	t.Run("bare-multi-attach", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(80, 24, "-S", "only")
		a.await("bs$ ")
		a.command("printf 'only-%s\\n' session", "only-session")
		b := h.client(80, 24, "-x")
		b.await("only-session")
		b.command("printf 'joined-%s\\n' existing", "joined-existing")
		a.await("joined-existing")
		b.write("\x01d")
		b.exited()
		a.write("\x01d")
		a.exited()
		c := h.client(80, 24, "-x")
		c.await("joined-existing")
		other := h.client(80, 24, "-S", "other")
		other.await("bs$ ")
		ambiguous := h.client(80, 24, "-x")
		ambiguous.await("ambiguous session")
		select {
		case <-ambiguous.waitDone:
			require.Error(t, ambiguous.waitErr)
		case <-time.After(5 * time.Second):
			t.Fatal("ambiguous -x did not exit")
		}
		assert.Contains(t, h.cli("-ls"), "only")
	})

	t.Run("bash-exit-removes-window-and-last-session", func(t *testing.T) {
		h := newHarness(t, bin, "/bin/bash")
		a := h.client(80, 24, "-S", "exits")
		a.await("bs$ ")
		a.command("printf 'first-%s\\n' shell", "first-shell")
		a.write("\x01c")
		a.command("printf 'second-%s\\n' shell", "second-shell")
		b := h.client(80, 24, "-x", "exits")
		b.await("second-shell")
		a.write("exit\r")
		a.await("first-shell")
		b.await("first-shell")
		assert.Contains(t, h.cli("-ls"), "1 windows")
		a.write("exit\r")
		a.exited()
		b.exited()
		assert.Contains(t, h.cli("-ls"), "No sessions.")
	})

	t.Run("detached-bash-exit-removes-session", func(t *testing.T) {
		h := newHarness(t, bin, "/bin/bash")
		gate := filepath.Join(h.home, "exit-gate")
		require.NoError(t, syscall.Mkfifo(gate, 0600))
		a := h.client(80, 24, "-S", "detached-exit")
		a.await("bs$ ")
		a.command("printf 'detached-%s\\n' ready; read -r answer < exit-gate; exit", "detached-ready")
		a.write("\x01d")
		a.exited()
		assert.Contains(t, h.cli("-ls"), "\tdetached")
		var fd int
		var err error
		require.Eventually(t, func() bool {
			fd, err = syscall.Open(gate, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
			return err != syscall.ENXIO && err != syscall.EINTR
		}, 5*time.Second, 20*time.Millisecond, "shell never opened its exit gate")
		require.NoError(t, err)
		defer syscall.Close(fd)
		_, err = syscall.Write(fd, []byte("exit\n"))
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.Eventually(t, func() bool {
			cmd := exec.CommandContext(ctx, bin, "-ls")
			cmd.Env = h.env()
			out, err := cmd.CombinedOutput()
			return err == nil && strings.Contains(string(out), "No sessions.")
		}, 5*time.Second, 20*time.Millisecond)
	})

	t.Run("exec-replaced-program-exit-removes-session", func(t *testing.T) {
		h := newHarness(t, bin, "/bin/bash")
		a := h.client(80, 24, "-S", "program-exit")
		a.await("bs$ ")
		a.command("exec sh -c 'printf \"program-%s\\n\" ready; read answer'", "program-ready")
		a.write("done\r")
		a.exited()
		assert.Contains(t, h.cli("-ls"), "No sessions.")
	})

	t.Run("kill-current-window-and-last-session", func(t *testing.T) {
		h := newHarness(t, bin, "/bin/bash")
		a := h.client(80, 24, "-S", "kill")
		a.await("bs$ ")
		a.command("printf 'surviving-%s\\n' window", "surviving-window")
		a.write("\x01c")
		a.command("trap '' HUP; printf '%s\\n' $$ > shell.pid; sleep 600 & printf '%s\\n' $! > background.pid; sh -c 'printf \"%s\\n\" $$ > foreground.pid; printf \"busy-%s\\n\" window; exec sleep 600'", "busy-window")
		var pids []int
		for _, file := range []string{"shell.pid", "background.pid", "foreground.pid"} {
			data, err := os.ReadFile(filepath.Join(h.home, file))
			require.NoError(t, err)
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			require.NoError(t, err)
			require.Positive(t, pid)
			pids = append(pids, pid)
		}
		b := h.client(80, 24, "-x", "kill")
		b.await("busy-window")
		a.write("\x01k")
		a.await("surviving-window")
		b.await("surviving-window")
		for _, pid := range pids {
			require.Eventually(t, func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH }, 5*time.Second, 20*time.Millisecond,
				"window process %d survived Ctrl-A k", pid)
		}
		assert.Contains(t, h.cli("-ls"), "1 windows")
		a.write("\x01k")
		a.exited()
		b.exited()
		assert.Contains(t, h.cli("-ls"), "No sessions.")
	})
}
