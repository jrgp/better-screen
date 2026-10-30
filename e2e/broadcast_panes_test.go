package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func runBroadcastPanes(t *testing.T, bin string) {
	t.Run("broadcast-default-status-labels", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(100, 16, "-S", "labels")
		a.command("printf '\\033]2;first\\007first-%s\\n' ready", "first-ready")
		b := h.client(100, 16, "-x", "labels")
		b.awaitRow(15, defaultStatusRow(100, "labels", "first"))
		a.write("\x01B")
		for _, c := range []*terminalClient{a, b} {
			c.awaitRow(15, defaultStatusRow(100, "labels", "Broadcast | first"))
			c.pickerColors(15, 0, 1)
		}
		a.write("\x01c")
		a.command("printf '\\033]2;second\\007second-%s\\n' ready", "second-ready")
		for _, c := range []*terminalClient{a, b} {
			c.awaitRow(15, defaultStatusRow(100, "labels", "Broadcast | first", "second"))
			c.pickerColors(15, 1, 2)
		}
		a.write("\x01B")
		for _, c := range []*terminalClient{a, b} {
			c.awaitRow(15, defaultStatusRow(100, "labels", "Broadcast | first", "Broadcast | second"))
		}
		b.write("\x010\x01B")
		for _, c := range []*terminalClient{a, b} {
			c.awaitRow(15, defaultStatusRow(100, "labels", "first", "Broadcast | second"))
			c.pickerColors(15, 0, 2)
		}
		a.write("\x01d")
		a.exited()
		r := h.client(100, 16, "-x", "labels")
		r.awaitRow(15, defaultStatusRow(100, "labels", "first", "Broadcast | second"))
		r.write("\x011\x01B")
		for _, c := range []*terminalClient{b, r} {
			c.awaitRow(15, defaultStatusRow(100, "labels", "first", "second"))
			c.pickerColors(15, 1, 2)
		}
	})

	t.Run("broadcast-window-state-focus-new-panes-and-multiattach", func(t *testing.T) {
		h := newConfiguredHarness(t, bin, `{"status_line_left":"{{range .Windows}}{{.Number}}={{.Broadcast}} {{end}}","status_line_right":""}`, "/bin/bash")
		a := h.client(100, 24, "-S", "broadcast")
		a.command("TOKEN=left; printf 'left-%s\\n' ready", "left-ready")
		a.write("\x01|")
		a.command("TOKEN=right; printf 'right-%s\\n' ready", "right-ready")
		b := h.client(100, 24, "-x", "broadcast")
		b.await("right-ready")
		a.command("printf 'focus-only-%s\\n' \"$TOKEN\"", "focus-only-right")
		require.NotContains(t, a.region(0, 0, 49, 23), "focus-only")
		a.write("\x01B")
		for _, c := range []*terminalClient{a, b} {
			c.awaitRow(23, "0=true "+strings.Repeat(" ", 93))
		}
		a.command("printf 'fanout-%s\\n' \"$TOKEN\"", "fanout-right")
		for _, c := range []*terminalClient{a, b} {
			c.await("fanout-left")
			c.await("fanout-right")
		}
		b.command("printf 'other-client-%s\\n' \"$TOKEN\"", "other-client-right")
		a.await("other-client-left")
		a.write("\x01c")
		a.command("TOKEN=other; printf 'other-window-%s\\n' ready", "other-window-ready")
		a.awaitRow(23, "0=true 1=false "+strings.Repeat(" ", 85))
		b.write("\x010")
		a.await("other-client-right")
		a.command("printf 'restored-%s\\n' \"$TOKEN\"", "restored-right")
		a.await("restored-left")
		b.write("\x01S")
		for _, c := range []*terminalClient{a, b} {
			c.border(49, 11, '├')
		}
		a.command("printf 'joined-%s\\n' ready", "joined-ready")
		require.Eventually(t, func() bool {
			return strings.Count(a.text(), "joined-ready") == 3 && strings.Count(b.text(), "joined-ready") == 3
		}, 5*time.Second, 15*time.Millisecond)
		b.write("\x01B")
		for _, c := range []*terminalClient{a, b} {
			c.awaitRow(23, "0=false 1=false "+strings.Repeat(" ", 84))
		}
		b.write("\x01\t")
		b.command("printf 'shared-focus-%s\\n' \"$TOKEN\"", "shared-focus-left")
		a.await("shared-focus-left")
		a.command("printf 'disabled-%s\\n' \"$TOKEN\"", "disabled-left")
		require.NotContains(t, a.region(50, 0, 50, 23), "disabled-")
		a.write("\x01B\x01d")
		a.exited()
		r := h.client(100, 24, "-x", "broadcast")
		r.awaitRow(23, "0=true 1=false "+strings.Repeat(" ", 85))
		r.command("printf 'reattached-%s\\n' alive", "reattached-alive")
		require.Eventually(t, func() bool {
			return strings.Count(r.text(), "reattached-alive") == 3
		}, 5*time.Second, 15*time.Millisecond)
	})

	t.Run("broadcast-exact-raw-input-excludes-prefix-commands", func(t *testing.T) {
		h := newConfiguredHarness(t, bin, `{"status_line_left":"{{range .Windows}}{{if .Broadcast}}ON{{else}}OFF{{end}}{{end}}","status_line_right":""}`)
		a := h.client(100, 20, "-S", "bytes")
		payload := "é界\x03\x04\x1a\x00\x1b[A\r\x7f\t\xff\x01"
		for i, name := range []string{"left", "right"} {
			if i != 0 {
				a.write("\x01|")
			}
			a.command(fmt.Sprintf("stty raw -echo; printf 'raw-%%s-ready\\r\\n' %s; dd bs=1 count=%d of=%s.bytes 2>/dev/null; stty sane; printf '\\r\\nraw-%%s-done\\n' %s; read ack", name, len(payload), name, name), "raw-"+name+"-ready")
		}
		a.write("\x01")
		a.write("B")
		a.awaitRow(19, "ON"+strings.Repeat(" ", 98))
		a.write("\x01\t\x01F\x01R\x01n\x01p\x010\x01\x00")
		a.write("\x01?")
		a.await("B  broadcast input")
		a.write("\x1b")
		a.await("raw-left-ready")
		a.write(payload[:len(payload)-1])
		a.write("\x01a")
		for _, name := range []string{"left", "right"} {
			a.await("raw-" + name + "-done")
			data, err := os.ReadFile(filepath.Join(h.home, name+".bytes"))
			require.NoError(t, err)
			require.Equal(t, []byte(payload), data, "raw input for %s", name)
		}
	})

	t.Run("command-panes-balanced-grid", func(t *testing.T) {
		for _, tc := range []struct {
			count, cols, rows, gridCols, gridRows int
		}{
			{4, 80, 24, 2, 2},
			{4, 120, 30, 2, 2},
			{4, 200, 24, 2, 2},
			{6, 120, 30, 3, 2},
			{9, 120, 30, 3, 3},
			{16, 160, 40, 4, 4},
		} {
			t.Run(fmt.Sprintf("%d-in-%dx%d", tc.count, tc.cols, tc.rows), func(t *testing.T) {
				h := newHarness(t, bin)
				args := []string{"--panes"}
				for i := range tc.count {
					args = append(args, fmt.Sprintf("stty size > grid-%d; printf '[%02d]\\n'; read action", i, i))
				}
				a := h.client(tc.cols, tc.rows, args...)
				for i := range tc.count {
					label := fmt.Sprintf("[%02d]", i)
					a.await(label)
					col, row := i%tc.gridCols, i/tc.gridCols
					x, y := (tc.cols+1)*col/tc.gridCols, tc.rows*row/tc.gridRows
					width := (tc.cols+1)*(col+1)/tc.gridCols - 1 - x
					height := tc.rows*(row+1)/tc.gridRows - 1 - y
					require.Contains(t, a.region(x, y, width, height), label)
					data, err := os.ReadFile(filepath.Join(h.home, fmt.Sprintf("grid-%d", i)))
					require.NoError(t, err)
					require.Equal(t, fmt.Sprintf("%d %d", height, width), strings.Join(strings.Fields(string(data)), " "))
				}
				for row := 1; row < tc.gridRows; row++ {
					for col := 1; col < tc.gridCols; col++ {
						a.border((tc.cols+1)*col/tc.gridCols-1, tc.rows*row/tc.gridRows-1, '┼')
					}
				}
				a.write("\x01Bquit\r")
				a.exited()
				h.awaitDaemonExit()
			})
		}
	})

	t.Run("command-panes-shell-quoting-initial-sizes-and-exit-reflow", func(t *testing.T) {
		h := newHarness(t, bin, "/bin/bash")
		args := []string{"-S", "commands", "--panes"}
		for i := range 5 {
			command := `printf -v value '%s|%s' "arg with spaces" '$not expanded'; [[ "$value" == 'arg with spaces|$not expanded' ]] || exit 9; `
			command += fmt.Sprintf(`printf '%%s\n' "$value" > value-%d; printf '%%s\n' $$ > pane-%d.pid; stty size > size-%d; printf '\033]2;cmd-%d\007command-%%s-ready\n' %d; while IFS= read -r action; do case "$action" in quit) exit 0;; *) printf 'reply-%d:%%s\n' "$action";; esac; done`, i, i, i, i, i, i)
			args = append(args, command)
		}
		a := h.client(120, 30, args...)
		var pids []int
		for i, size := range []string{"14 39", "14 39", "14 40", "14 59", "14 60"} {
			a.await(fmt.Sprintf("command-%d-ready", i))
			pids = append(pids, readPanePID(t, h.home, fmt.Sprintf("pane-%d.pid", i)))
			data, err := os.ReadFile(filepath.Join(h.home, fmt.Sprintf("value-%d", i)))
			require.NoError(t, err)
			require.Equal(t, "arg with spaces|$not expanded\n", string(data))
			data, err = os.ReadFile(filepath.Join(h.home, fmt.Sprintf("size-%d", i)))
			require.NoError(t, err)
			require.Equal(t, size, strings.Join(strings.Fields(string(data)), " "), "initial PTY size for command %d", i)
		}
		a.border(39, 14, '┴')
		a.border(59, 14, '┬')
		a.border(79, 14, '┴')
		for i := 1; i <= 5; i++ {
			a.write("\x01\t")
			a.awaitRow(29, defaultStatusRow(120, "commands", fmt.Sprintf("cmd-%d", i%5)))
		}
		a.write("focused\r")
		a.await("reply-0:focused")
		a.absent("reply-1:focused")
		a.write("quit\r")
		a.awaitRow(29, defaultStatusRow(120, "commands", "cmd-1"))
		require.Eventually(t, func() bool { return syscall.Kill(pids[0], 0) == syscall.ESRCH }, 5*time.Second, 15*time.Millisecond)
		for i := 1; i < len(pids); i++ {
			require.Equal(t, pids[i], readPanePID(t, h.home, fmt.Sprintf("pane-%d.pid", i)))
			require.NoError(t, syscall.Kill(pids[i], 0), "surviving command %d must not restart", i)
		}
		a.border(59, 0, '│')
		a.write("survived\r")
		a.await("reply-1:survived")
		for i := 1; i < 5; i++ {
			a.write("quit\r")
			if i < 4 {
				a.awaitRow(29, defaultStatusRow(120, "commands", fmt.Sprintf("cmd-%d", i+1)))
			}
		}
		a.exited()
		h.awaitDaemonExit()
		for _, pid := range pids {
			require.ErrorIs(t, syscall.Kill(pid, 0), syscall.ESRCH)
		}
	})

	t.Run("command-panes-practical-limit-and-broadcast-exit-cleanup", func(t *testing.T) {
		h := newHarness(t, bin)
		args := []string{"--panes"}
		for i := range 32 {
			args = append(args, fmt.Sprintf(`printf '%%s\n' $$ > max-%d.pid; printf 'pane-%%s-ready\n' %d; while IFS= read -r value; do [ "$value" = quit ] && exit 0; printf 'ack-%d:%%s\n' "$value"; done`, i, i, i))
		}
		a := h.client(160, 40, args...)
		for i := range 32 {
			a.await(fmt.Sprintf("pane-%d-ready", i))
		}
		files, err := filepath.Glob(filepath.Join(h.home, "max-*.pid"))
		require.NoError(t, err)
		require.Len(t, files, 32)
		a.write("\x01B")
		a.write("ping\r")
		for i := range 32 {
			a.await(fmt.Sprintf("ack-%d:ping", i))
		}
		a.awaitRow(39, defaultStatusRow(160, "1", "Broadcast | sh"))
		a.write("quit\r")
		a.exited()
		h.awaitDaemonExit()
		for i := range 32 {
			require.ErrorIs(t, syscall.Kill(readPanePID(t, h.home, fmt.Sprintf("max-%d.pid", i)), 0), syscall.ESRCH)
		}
	})

	t.Run("command-panes-fast-exits-close-without-extra-shell", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(80, 16, "--panes", "true", "true", "true")
		a.exited()
		h.awaitDaemonExit()
	})

	t.Run("command-panes-reject-too-small-before-launch", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(2, 2, "--panes", "touch should-not-start; read x", "touch should-not-start; read x")
		select {
		case <-a.waitDone:
			require.Error(t, a.waitErr)
		case <-time.After(5 * time.Second):
			t.Fatal("invalid layout did not fail")
		}
		select {
		case <-a.readDone:
		case <-time.After(5 * time.Second):
			t.Fatal("failed client's output did not close")
		}
		a.mu.Lock()
		raw := a.raw.String()
		a.mu.Unlock()
		require.Contains(t, raw, "terminal too small for 2 panes")
		require.NoFileExists(t, filepath.Join(h.home, "should-not-start"))
		h.awaitDaemonExit()
	})

	t.Run("command-panes-cli-validation", func(t *testing.T) {
		for _, tc := range []struct {
			args []string
			want string
		}{
			{[]string{"--panes"}, "at least one command"},
			{[]string{"--panes", ""}, "pane command 1 must be nonempty"},
			{[]string{"--panes", " \t\n"}, "pane command 1 must be nonempty"},
			{[]string{"-r", "work", "--panes", "read x"}, "may only be combined with -S"},
			{[]string{"-x", "--panes", "read x"}, "may only be combined with -S"},
			{[]string{"-d", "-r", "work", "--panes", "read x"}, "may only be combined with -S"},
			{[]string{"-ls", "--panes", "read x"}, "may only be combined with -S"},
			{[]string{"-v", "--panes", "read x"}, "may only be combined with -S"},
			{append([]string{"--panes"}, strings.Fields(strings.Repeat("true ", 33))...), "pane limit is 32"},
			{[]string{"echo not-a-pane-command"}, "unexpected arguments"},
		} {
			home := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			cmd := exec.CommandContext(ctx, bin, tc.args...)
			cmd.Env = append(os.Environ(), "HOME="+home)
			output, err := cmd.CombinedOutput()
			cancel()
			require.Error(t, err, "%q", tc.args)
			require.Contains(t, string(output), tc.want)
			require.NoFileExists(t, filepath.Join(home, ".bs", "bs.sock"))
		}
	})
}
