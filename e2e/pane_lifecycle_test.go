package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
)

func readPanePID(t *testing.T, home, file string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, file))
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err)
	require.Positive(t, pid)
	return pid
}

func runPaneLifecycle(t *testing.T, bin string) {
	t.Run("nested-pane-exits-collapse-without-restarting-survivors", func(t *testing.T) {
		h := newHarness(t, bin, "/bin/bash")
		c := h.client(80, 16, "-S", "collapse")
		c.await("bs$ ")
		c.command("TOKEN=left; printf '%s\\n' $$ > left.pid; printf 'original-%s\\n' left", "original-left")
		left := readPanePID(t, h.home, "left.pid")
		c.write("\x01|")
		c.command("TOKEN=top; printf '%s\\n' $$ > top.pid; printf 'original-%s\\n' top", "original-top")
		top := readPanePID(t, h.home, "top.pid")
		c.write("\x01S")
		c.command("exec sh -c 'printf \"exec-%s\\n\" bottom; read answer'", "exec-bottom")
		c.border(39, 7, '├')
		c.write("done\r")
		require.Eventually(t, func() bool { return !strings.Contains(c.text(), "─") }, 5*time.Second, 15*time.Millisecond)
		c.command("printf 'same-%s:%s:' \"$TOKEN\" $$; stty size", fmt.Sprintf("same-top:%d:15 40", top))
		c.write("exit\r")
		require.Eventually(t, func() bool { return !strings.Contains(c.text(), "│") }, 5*time.Second, 15*time.Millisecond)
		c.command("printf 'same-%s:%s:' \"$TOKEN\" $$; stty size", fmt.Sprintf("same-left:%d:15 80", left))
		require.ErrorIs(t, syscall.Kill(top, 0), syscall.ESRCH)
		c.write("\x01c")
		c.command("printf 'other-%s\\n' window", "other-window")
		c.write("\x010\x01K")
		c.await("other-window")
		require.Contains(t, h.cli("-ls"), "1 windows")
		require.ErrorIs(t, syscall.Kill(left, 0), syscall.ESRCH)
		c.write("\x01K")
		c.exited()
		h.awaitDaemonExit()
	})

	t.Run("pane-kill-cleans-jobs-but-lowercase-k-kills-whole-window", func(t *testing.T) {
		h := newHarness(t, bin, "/bin/bash")
		c := h.client(100, 20, "-S", "jobs")
		c.await("bs$ ")
		c.command("TOKEN=survivor; printf '%s\\n' $$ > survivor.pid; printf 'survivor-%s\\n' ready", "survivor-ready")
		survivor := readPanePID(t, h.home, "survivor.pid")
		c.write("\x01|")
		c.command("trap '' HUP; printf '%s\\n' $$ > shell.pid; sleep 600 & printf '%s\\n' $! > background.pid; sh -c 'printf \"%s\\n\" $$ > foreground.pid; printf \"busy-%s\\n\" ready; exec sleep 600'", "busy-ready")
		var killed []int
		for _, file := range []string{"shell.pid", "background.pid", "foreground.pid"} {
			killed = append(killed, readPanePID(t, h.home, file))
		}
		c.write("\x01K")
		c.command("printf 'alive-%s:%s\\n' \"$TOKEN\" $$", fmt.Sprintf("alive-survivor:%d", survivor))
		for _, pid := range killed {
			require.Eventually(t, func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH }, 5*time.Second, 15*time.Millisecond)
		}
		c.write("\x01|\x01S")
		c.command("printf 'whole-window-%s\\n' split", "whole-window-split")
		c.write("\x01k")
		c.exited()
		h.awaitDaemonExit()
		require.ErrorIs(t, syscall.Kill(survivor, 0), syscall.ESRCH)
	})

	t.Run("simultaneous-pane-exits-and-kills", func(t *testing.T) {
		h := newHarness(t, bin, "/bin/bash")
		other := h.client(80, 16, "-S", "untouched")
		other.await("bs$ ")
		for round := range 3 {
			c := h.client(100, 24, "-S", fmt.Sprintf("concurrent%d", round))
			c.await("bs$ ")
			c.write("\x01|\x01S\x01\t\x01S")
			var pids, gates []int
			for i := range 4 {
				gate := filepath.Join(h.home, fmt.Sprintf("gate-%d-%d", round, i))
				require.NoError(t, syscall.Mkfifo(gate, 0600))
				file := fmt.Sprintf("pid-%d-%d", round, i)
				c.command(fmt.Sprintf("printf '%%s\\n' $$ > %s; printf 'waiting-%%s\\n' %d; read answer < %s; exit", file, i, filepath.Base(gate)), fmt.Sprintf("waiting-%d", i))
				pids = append(pids, readPanePID(t, h.home, file))
				var fd int
				var err error
				require.Eventually(t, func() bool {
					fd, err = syscall.Open(gate, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
					return err != syscall.ENXIO && err != syscall.EINTR
				}, 5*time.Second, 15*time.Millisecond)
				require.NoError(t, err)
				gates = append(gates, fd)
				t.Cleanup(func() { syscall.Close(fd) })
				if i < 3 {
					c.write("\x01\t")
				}
			}
			c.write("\x01K")
			for _, fd := range gates {
				_, err := syscall.Write(fd, []byte("exit\n"))
				require.True(t, err == nil || err == syscall.EPIPE, "release exit gate: %v", err)
			}
			c.exited()
			for _, pid := range pids {
				require.Eventually(t, func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH }, 5*time.Second, 15*time.Millisecond)
			}
			other.command(fmt.Sprintf("printf 'other-%%s\\n' %d", round), fmt.Sprintf("other-%d", round))
			require.NotContains(t, h.cli("-ls"), fmt.Sprintf("concurrent%d", round))
		}
	})

	t.Run("tiny-canonical-layout-preserves-hidden-processes", func(t *testing.T) {
		h := newHarness(t, bin)
		c := h.client(80, 16, "-S", "tiny")
		c.await("bs$ ")
		c.command("TOKEN=left; printf '%s\\n' $$ > tiny-left.pid; printf 'tiny-%s\\n' left", "tiny-left")
		left := readPanePID(t, h.home, "tiny-left.pid")
		c.write("\x01|")
		c.command("TOKEN=right; printf '%s\\n' $$ > tiny-right.pid; printf 'tiny-%s\\n' right", "tiny-right")
		right := readPanePID(t, h.home, "tiny-right.pid")
		require.NoError(t, pty.Setsize(c.pty, &pty.Winsize{Cols: 4, Rows: 2}))
		c.mu.Lock()
		require.NoError(t, c.screen.Resize(4, 2, 0, 0))
		c.mu.Unlock()
		c.write("\x01F\x01\t\x01\t")
		c.awaitRow(1, "tiny")
		large := h.client(80, 16, "-x", "tiny")
		large.awaitRow(15, "0"+strings.Repeat(" ", 75)+"tiny")
		large.write("\x01|")
		large.await("pane too small to split")
		require.NoError(t, pty.Setsize(c.pty, &pty.Winsize{Cols: 80, Rows: 16}))
		c.mu.Lock()
		require.NoError(t, c.screen.Resize(80, 16, 0, 0))
		c.mu.Unlock()
		c.write("\x01F")
		c.border(39, 0, '│')
		c.command("printf 'restored-%s:%s:' \"$TOKEN\" $$; stty size", fmt.Sprintf("restored-right:%d:15 40", right))
		c.write("\x01\t")
		c.command("printf 'restored-%s:%s:' \"$TOKEN\" $$; stty size", fmt.Sprintf("restored-left:%d:15 39", left))
	})
}
