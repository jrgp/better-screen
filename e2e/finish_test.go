package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	ghostty "go.mitchellh.com/libghostty"
)

func parentClient(h *harness, tag string, args ...string) *terminalClient {
	h.t.Helper()
	parent := &harness{t: h.t, bin: "/bin/sh", home: h.home}
	script := `tag=$1; shift
stty -g > "$tag.before"
printf '\033[2J\033[HPRIMARY-KEPT\033[16;1HPRIMARY-BOTTOM\033[5;1H'
"$@"
code=$?
printf 'PARENT-%s-ready:%s\r\n' "$tag" "$code"
IFS= read -r restored
[ "$restored" = restore-check ] || exit 97
stty -g > "$tag.after"
printf 'PARENT-%s-restored\r\n' "$tag"
IFS= read -r check
printf '\033[?47h\033[HALT-CHECK'
IFS= read -r ack
printf '\033[?47l'
exit "$code"`
	command := []string{"-c", script, "parent", tag, h.bin}
	return parent.client(80, 16, append(command, args...)...)
}

func restoredParent(t *testing.T, h *harness, c *terminalClient, tag, notice string) {
	t.Helper()
	c.await(notice)
	c.await("PARENT-" + tag + "-ready:0")
	// Darwin's transient PENDIN flag clears when canonical input is processed.
	c.write("restore-check\r")
	c.await("PARENT-" + tag + "-restored")
	require.Equal(t, "PRIMARY-KEPT", strings.TrimSpace(c.row(0)))
	require.Equal(t, "PRIMARY-BOTTOM", strings.TrimSpace(c.row(15)))
	c.absent("pane-artifact")
	c.absent("│")
	c.absent("─")
	c.statusColor(0, 15, false)
	before, err := os.ReadFile(filepath.Join(h.home, tag+".before"))
	require.NoError(t, err)
	after, err := os.ReadFile(filepath.Join(h.home, tag+".after"))
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "restore the parent's exact terminal settings")
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, mode := range []uint16{1, 5, 9, 1000, 1002, 1003, 1004, 1005, 1006, 1015, 1049, 2004, 2027} {
		on, err := c.screen.Mode(ghostty.NewMode(mode, false))
		require.NoError(t, err)
		require.False(t, on, "mode %d must be restored", mode)
	}
	for _, mode := range []uint16{7, 25} {
		on, err := c.screen.Mode(ghostty.NewMode(mode, false))
		require.NoError(t, err)
		require.True(t, on, "mode %d must be restored", mode)
	}
}

func clearedAlternate(t *testing.T, c *terminalClient) {
	t.Helper()
	// Mode 47 reuses the alternate buffer without clearing it.
	c.write("check\r")
	c.await("ALT-CHECK")
	for row := 1; row < 16; row++ {
		require.Empty(t, strings.TrimSpace(c.row(row)), "bs must not leave alternate-screen artifacts on row %d", row)
	}
	c.write("\r")
	c.exited()
}

func runFinish(t *testing.T, bin string) {
	t.Run("detach-notice-restores-parent-and-clears-ui", func(t *testing.T) {
		h := newHarness(t, bin)
		a := parentClient(h, "detach", "-S", "clean")
		a.command("printf '\\033[2J\\033[Hpane-artifact-%s\\n' left", "pane-artifact-left")
		a.write("\x01|")
		a.command("printf '\\033[?1000h\\033[?1006h\\033[?1h\\033[?2004h\\033[2J\\033[Hpane-artifact-%s\\n' right; read wait", "pane-artifact-right")
		a.border(39, 2, '│')
		a.awaitRow(15, defaultStatusRow(80, "clean", "sh"))
		a.write("\x01d")
		restoredParent(t, h, a, "detach", "[bs: detached from session clean]")
		require.Contains(t, h.cli("-ls"), "detached")
		r := h.client(80, 16, "-r", "cle")
		r.await("pane-artifact-left")
		r.await("pane-artifact-right")
		r.border(39, 2, '│')
		r.write("\x01d")
		r.exited()
		clearedAlternate(t, a)
	})

	for _, action := range []struct{ name, keys string }{
		{"shell-exit", "exit\r"}, {"pane-kill", "\x01K"}, {"window-kill", "\x01k"},
	} {
		t.Run("session-exit-notice-clears-ui-"+action.name, func(t *testing.T) {
			h := newHarness(t, bin)
			a := parentClient(h, "exit", "-S", "finished")
			a.command("printf '\\033[2J\\033[Hpane-artifact-%s\\n' left", "pane-artifact-left")
			a.write("\x01|")
			a.command("printf '\\033[2J\\033[Hpane-artifact-%s\\n' right", "pane-artifact-right")
			a.border(39, 2, '│')
			if action.name != "window-kill" {
				a.write(action.keys)
				a.awaitRow(0, "pane-artifact-left")
				a.awaitRow(15, defaultStatusRow(80, "finished", "sh"))
				a.absent("[bs: session finished exited]")
			}
			a.write(action.keys)
			restoredParent(t, h, a, "exit", "[bs: session finished exited]")
			a.absent("detached from session")
			h.awaitDaemonExit()
			clearedAlternate(t, a)
		})
	}

	t.Run("forced-detach-notice-uses-actual-session-name", func(t *testing.T) {
		h := newHarness(t, bin)
		a := parentClient(h, "forced", "-S", "forced")
		a.command("printf 'pane-artifact-%s\\n' original", "pane-artifact-original")
		b := h.client(80, 16, "-d", "-r", "for")
		b.await("pane-artifact-original")
		restoredParent(t, h, a, "forced", "[bs: detached from session forced]")
		b.write("exit\r")
		b.exited()
		b.await("[bs: session forced exited]")
		h.awaitDaemonExit()
		clearedAlternate(t, a)
	})

	t.Run("session-exit-notice-reaches-every-client", func(t *testing.T) {
		h := newHarness(t, bin)
		a := parentClient(h, "first", "-S", "shared")
		a.command("printf 'pane-artifact-%s\\n' shared", "pane-artifact-shared")
		b := parentClient(h, "second", "-x", "sha")
		b.await("pane-artifact-shared")
		b.write("exit\r")
		for i, c := range []*terminalClient{a, b} {
			tag := []string{"first", "second"}[i]
			restoredParent(t, h, c, tag, "[bs: session shared exited]")
			clearedAlternate(t, c)
		}
		h.awaitDaemonExit()
	})

	t.Run("session-exit-notice-before-first-frame", func(t *testing.T) {
		h := newHarness(t, bin)
		a := parentClient(h, "fast", "--panes", "true")
		restoredParent(t, h, a, "fast", "[bs: session 1 exited]")
		h.awaitDaemonExit()
		clearedAlternate(t, a)
	})

	t.Run("detach-notice-after-immediate-session-switch", func(t *testing.T) {
		h := newHarness(t, bin)
		a := parentClient(h, "switch", "-S", "first")
		a.await("bs$ ")
		b := h.client(80, 16, "-S", "second")
		b.await("bs$ ")
		a.write("\x01s")
		a.await("bs - Switch session")
		a.write("\x1b[B\r\x01d")
		restoredParent(t, h, a, "switch", "[bs: detached from session second]")
		a.absent("[bs: detached from session first]")
		b.command("printf 'still-%s\\n' running", "still-running")
		clearedAlternate(t, a)
	})
}
