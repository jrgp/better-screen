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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runWindowLifecycle(t *testing.T, bin string) {
	t.Run("space-next-window-binding", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(80, 16, "-S", "space")
		a.await("bs$ ")
		a.write("\x01 ")
		a.write("\x01\x00")
		a.command("printf 'single-%s\\n' window", "single-window")
		for number := range 3 {
			if number > 0 {
				a.write("\x01c")
			}
			a.command(fmt.Sprintf("printf 'window-%%s\\n' %d", number), fmt.Sprintf("window-%d", number))
		}
		b := h.client(80, 16, "-x", "space")
		b.await("window-2")
		for _, step := range []struct {
			keys string
			want int
		}{{"\x01 ", 0}, {"\x01n", 1}, {"\x01 ", 2}, {"\x01 ", 0}, {"\x01p", 2},
			{"\x01\x00", 0}, {"\x01\x00", 1}, {"\x01\x00", 2}} {
			for i := range len(step.keys) {
				a.write(step.keys[i : i+1])
			}
			for _, c := range []*terminalClient{a, b} {
				c.await(fmt.Sprintf("window-%d", step.want))
				c.pickerColors(15, step.want, 3)
			}
		}
		a.write("\x01\x00")
		for _, c := range []*terminalClient{a, b} {
			c.await("window-0")
			c.pickerColors(15, 0, 3)
		}
		a.command("printf 'normal-%s\\n' 'space preserved'", "normal-space preserved")
		b.await("normal-space preserved")
		a.write("\x01?")
		a.await("n/Space  next")
	})

	for _, action := range []struct {
		name, keys string
	}{{"shell-exit", "exit\r"}, {"window-kill", "\x01k"}, {"last-pane-kill", "\x01K"}} {
		t.Run("window-renumbering-"+action.name, func(t *testing.T) {
			h := newHarness(t, bin)
			a := h.client(80, 16, "-S", "order")
			a.await("bs$ ")
			create := func(label string) {
				a.command("TOKEN="+label+"; printf '\\033]2;order-%s\\007\\033[2J\\033[Hwindow-%s\\n' \"$TOKEN\" \"$TOKEN\"", "window-"+label)
			}
			create("A")
			for _, label := range []string{"B", "C", "D", "E"} {
				a.write("\x01c")
				create(label)
			}
			b := h.client(80, 16, "-x", "order")
			b.await("window-E")
			expect := func(active int, labels ...string) {
				t.Helper()
				var titles []string
				for _, label := range labels {
					titles = append(titles, "order-"+label)
				}
				row := defaultStatusRow(80, "order", titles...)
				for _, c := range []*terminalClient{a, b} {
					c.await("window-" + labels[active])
					c.awaitRow(15, row)
					c.pickerColors(15, active, len(labels))
				}
			}
			a.write("\x012")
			expect(2, "A", "B", "C", "D", "E")
			a.write(action.keys)
			labels := []string{"A", "B", "D", "E"}
			expect(2, labels...)
			for _, active := range []int{3, 0, 1, 2} {
				a.write("\x01n")
				expect(active, labels...)
			}
			for _, active := range []int{1, 0, 3, 2} {
				a.write("\x01p")
				expect(active, labels...)
			}
			a.write("\x01c")
			create("F")
			labels = append(labels, "F")
			expect(4, labels...)
			for active := range labels {
				a.write("\x01" + fmt.Sprint(active))
				expect(active, labels...)
			}
			a.write("\x01\"")
			a.await("bs - Windows")
			for i, label := range labels {
				prefix, suffix := "  ", ""
				if i == 0 {
					prefix = "> "
				}
				if i == 4 {
					suffix = " *"
				}
				a.awaitRow(i+1, fmt.Sprintf("%s%d  order-%s%s", prefix, i, label, suffix))
			}
			a.write("\x1b[B\x1b[B\r")
			expect(2, labels...)
			a.write("\x01d")
			a.exited()
			a = h.client(80, 16, "-x", "order")
			expect(2, labels...)
			a.write("\x010")
			expect(0, labels...)
			a.write(action.keys)
			labels = []string{"B", "D", "E", "F"}
			expect(0, labels...)
			a.write("\x01p")
			expect(3, labels...)
			a.write(action.keys)
			labels = []string{"B", "D", "E"}
			expect(2, labels...)
			a.write("\x01c")
			create("G")
			labels = append(labels, "G")
			expect(3, labels...)
			a.write("\x01n")
			expect(0, labels...)
			a.write("\x01p")
			expect(3, labels...)
		})
	}

	t.Run("background-window-exit-preserves-selection-and-renumbers", func(t *testing.T) {
		h := newHarness(t, bin)
		c := h.client(80, 16, "-S", "background")
		c.await("bs$ ")
		c.command("printf '%s\\n' $$ > first.pid; exec sh -c 'printf \"background-%s\\n\" ready; read answer'", "background-ready")
		for _, label := range []string{"B", "C"} {
			c.write("\x01c")
			c.command("TOKEN="+label+"; printf '\\033[2J\\033[Hwindow-%s\\n' \"$TOKEN\"", "window-"+label)
		}
		pidText, err := os.ReadFile(filepath.Join(h.home, "first.pid"))
		require.NoError(t, err)
		pid, err := strconv.Atoi(strings.TrimSpace(string(pidText)))
		require.NoError(t, err)
		require.Positive(t, pid)
		require.NoError(t, syscall.Kill(pid, syscall.SIGTERM))
		c.awaitRow(15, defaultStatusRow(80, "background", "sh", "sh"))
		c.pickerColors(15, 1, 2)
		c.command("printf 'selected-%s\\n' \"$TOKEN\"", "selected-C")
		c.write("\x01n")
		c.await("window-B")
		c.pickerColors(15, 0, 2)
		c.write("\x01p")
		c.await("selected-C")
		c.pickerColors(15, 1, 2)
	})

	t.Run("daemon-survives-remaining-and-detached-sessions", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(80, 24, "-S", "keep")
		a.await("bs$ ")
		b := h.client(80, 24, "-S", "close")
		b.await("bs$ ")
		b.write("\x01k")
		b.exited()
		a.command("printf 'remaining-%s\\n' session", "remaining-session")
		a.write("\x01d")
		a.exited()
		assert.Contains(t, h.cli("-ls"), "keep\t1 windows\tdetached")
		c := h.client(80, 24, "-r", "keep")
		c.await("remaining-session")
		c.write("exit\r")
		c.exited()
		h.awaitDaemonExit()
		assert.Contains(t, h.cli("-ls"), "No sessions.")
		require.NoFileExists(t, filepath.Join(h.home, ".bs", "bs.sock"))
	})

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
		h.awaitDaemonExit()
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
		h.awaitDaemonExit()
		assert.Contains(t, h.cli("-ls"), "No sessions.")
	})

	t.Run("exec-replaced-program-exit-removes-session", func(t *testing.T) {
		h := newHarness(t, bin, "/bin/bash")
		a := h.client(80, 24, "-S", "program-exit")
		a.await("bs$ ")
		a.command("exec sh -c 'printf \"program-%s\\n\" ready; read answer'", "program-ready")
		a.write("done\r")
		a.exited()
		h.awaitDaemonExit()
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
		h.awaitDaemonExit()
		assert.Contains(t, h.cli("-ls"), "No sessions.")
	})
}
