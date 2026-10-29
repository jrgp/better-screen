package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func runConfigReload(t *testing.T, bin string) {
	t.Run("config-reload-templates-all-sessions-and-clients", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(80, 16, "-S", "work")
		a.command("TOKEN=original; printf '%s\\n' $$ > original.pid; printf 'original-%s\\n' ready", "original-ready")
		pid := readPanePID(t, h.home, "original.pid")
		a.write("\x01c")
		a.command("printf 'second-%s\\n' window", "second-window")
		a.write("\x010")
		a.await("original-ready")
		other := h.client(60, 12, "-S", "personal")
		other.command("printf 'other-%s\\n' session", "other-session")
		path := filepath.Join(h.home, ".bs", "config.json")
		socket, err := os.Stat(filepath.Join(h.home, ".bs", "bs.sock"))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, []byte(templateConfig(t, map[string]string{
			"status_line_left":   `{{highlight "NEW"}}{{range .Windows}} {{.Number}}{{end}}`,
			"status_line_center": "界",
			"status_line_right":  "{{.Session.Name}}",
		})), 0600))
		b := h.client(100, 20, "-x", "work")
		b.awaitRow(19, defaultStatusRow(100, "work", "sh", "sh"))
		a.write("\x01R")
		a.awaitRow(15, "NEW 0 1"+strings.Repeat(" ", 32)+"界"+strings.Repeat(" ", 35)+"work")
		b.awaitRow(19, "NEW 0 1"+strings.Repeat(" ", 42)+"界"+strings.Repeat(" ", 45)+"work")
		other.awaitRow(11, "NEW 0"+strings.Repeat(" ", 24)+"界"+strings.Repeat(" ", 21)+"personal")
		for _, tc := range []struct {
			client *terminalClient
			row    int
		}{{a, 15}, {b, 19}, {other, 11}} {
			tc.client.statusColor(0, tc.row, true)
		}
		a.command("printf 'preserved-%s:%s\\n' \"$TOKEN\" $$", fmt.Sprintf("preserved-original:%d", pid))
		b.await(fmt.Sprintf("preserved-original:%d", pid))
		other.command("printf 'other-still-%s\\n' running", "other-still-running")
		current, err := os.Stat(filepath.Join(h.home, ".bs", "bs.sock"))
		require.NoError(t, err)
		require.True(t, os.SameFile(socket, current), "reload must not replace the daemon socket")
		a.write("\x01?")
		a.await("R  reload configuration")
	})

	t.Run("config-reload-status-row-resizes-all-panes-and-windows", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(80, 18, "-S", "panes")
		a.command("TOKEN=background; printf '%s\\n' $$ > background.pid; printf 'background-%s\\n' ready", "background-ready")
		background := readPanePID(t, h.home, "background.pid")
		a.write("\x01c")
		a.command("TOKEN=left; printf '%s\\n' $$ > left.pid; printf 'left-%s\\n' ready", "left-ready")
		left := readPanePID(t, h.home, "left.pid")
		a.write("\x01|")
		a.command("TOKEN=top; printf '%s\\n' $$ > top.pid; printf 'top-%s\\n' ready", "top-ready")
		top := readPanePID(t, h.home, "top.pid")
		a.write("\x01S")
		a.command("TOKEN=bottom; printf '%s\\n' $$ > bottom.pid; printf 'bottom-%s\\n' ready", "bottom-ready")
		bottom := readPanePID(t, h.home, "bottom.pid")
		b := h.client(80, 18, "-x", "panes")
		b.await("bottom-ready")
		other := h.client(60, 14, "-S", "other")
		other.command("TOKEN=other; printf '%s\\n' $$ > other.pid; printf 'other-%s\\n' ready", "other-ready")
		otherPID := readPanePID(t, h.home, "other.pid")
		detached := h.client(40, 10, "-S", "detached")
		detached.command("TOKEN=detached; printf '%s\\n' $$ > detached.pid; printf 'detached-%s\\n' ready", "detached-ready")
		detachedPID := readPanePID(t, h.home, "detached.pid")
		detached.write("\x01d")
		detached.exited()
		path := filepath.Join(h.home, ".bs", "config.json")
		require.NoError(t, os.WriteFile(path, []byte(`{"status_line":false}`), 0600))
		a.write("\x01R")
		a.command("printf 'disabled-%s:%s:' \"$TOKEN\" $$; stty size", fmt.Sprintf("disabled-bottom:%d:9 40", bottom))
		for _, c := range []*terminalClient{a, b} {
			c.border(39, 8, '├')
			c.border(39, 17, '│')
		}
		a.command("printf '\\033[999;1Hbottom-%s' content", "bottom-content")
		b.await("bottom-content")
		require.Contains(t, a.row(17), "bottom-content")
		require.Contains(t, b.row(17), "bottom-content")
		for _, pane := range []struct {
			token string
			pid   int
			size  string
		}{{"left", left, "18 39"}, {"top", top, "8 40"}, {"bottom", bottom, "9 40"}} {
			a.write("\x01\t")
			a.command("printf 'disabled-%s:%s:' \"$TOKEN\" $$; stty size", fmt.Sprintf("disabled-%s:%d:%s", pane.token, pane.pid, pane.size))
		}
		a.write("\x010")
		a.command("printf 'disabled-%s:%s:' \"$TOKEN\" $$; stty size", fmt.Sprintf("disabled-background:%d:18 80", background))
		other.command("printf 'disabled-%s:%s:' \"$TOKEN\" $$; stty size", fmt.Sprintf("disabled-other:%d:14 60", otherPID))
		detached = h.client(40, 10, "-x", "detached")
		detached.command("printf 'disabled-%s:%s:' \"$TOKEN\" $$; stty size", fmt.Sprintf("disabled-detached:%d:10 40", detachedPID))
		require.NoError(t, os.WriteFile(path, []byte(`{"status_line":true}`), 0600))
		other.write("\x01R")
		a.awaitRow(17, defaultStatusRow(80, "panes", "sh", "sh"))
		b.awaitRow(17, defaultStatusRow(80, "panes", "sh", "sh"))
		other.awaitRow(13, defaultStatusRow(60, "other", "sh"))
		detached.awaitRow(9, defaultStatusRow(40, "detached", "sh"))
		a.command("printf 'enabled-%s:%s:' \"$TOKEN\" $$; stty size", fmt.Sprintf("enabled-background:%d:17 80", background))
		a.write("\x011")
		a.command("printf 'enabled-%s:%s:' \"$TOKEN\" $$; stty size", fmt.Sprintf("enabled-bottom:%d:8 40", bottom))
		for _, c := range []*terminalClient{a, b} {
			c.border(39, 8, '├')
			c.border(39, 16, '│')
			c.pickerColors(17, 1, 2)
		}
		for _, pane := range []struct {
			token string
			pid   int
			size  string
		}{{"left", left, "17 39"}, {"top", top, "8 40"}, {"bottom", bottom, "8 40"}} {
			a.write("\x01\t")
			a.command("printf 'enabled-%s:%s:' \"$TOKEN\" $$; stty size", fmt.Sprintf("enabled-%s:%d:%s", pane.token, pane.pid, pane.size))
		}
		other.command("printf 'enabled-%s:%s:' \"$TOKEN\" $$; stty size", fmt.Sprintf("enabled-other:%d:13 60", otherPID))
		detached.command("printf 'enabled-%s:%s:' \"$TOKEN\" $$; stty size", fmt.Sprintf("enabled-detached:%d:9 40", detachedPID))
	})

	t.Run("config-reload-invalid-config-keeps-working-state", func(t *testing.T) {
		h := newConfiguredHarness(t, bin, `{"status_line_left":"WORKING","status_line_right":""}`)
		a := h.client(500, 16, "-S", "rollback")
		a.command("TOKEN=kept; printf '%s\\n' $$ > kept.pid; printf 'kept-%s\\n' ready", "kept-ready")
		pid := readPanePID(t, h.home, "kept.pid")
		b := h.client(100, 16, "-x", "rollback")
		b.awaitRow(15, "WORKING"+strings.Repeat(" ", 93))
		other := h.client(100, 12, "-S", "other")
		other.awaitRow(11, "WORKING"+strings.Repeat(" ", 93))
		path := filepath.Join(h.home, ".bs", "config.json")
		for i, tc := range []struct{ config, want string }{
			{`{"status_line":false,`, "unexpected EOF"},
			{`{"status_line":false,"unknown":true}`, `unknown field "unknown"`},
			{`{"status_line":"false"}`, "cannot unmarshal string"},
			{`{"status_line":false,"status_line_left":"{{"}`, "status_line_left"},
			{`{"status_line":false,"status_line_center":"{{"}`, "status_line_center"},
			{`{"status_line":false,"status_line_right":"{{"}`, "status_line_right"},
			{`{"status_line":false,"status_line_left":"{{.Missing}}"}`, "can't evaluate field Missing"},
			{`{"status_line":false,"status_line_center":"{{.BadCenter}}"}`, "can't evaluate field BadCenter"},
			{`{"status_line":false,"status_line_right":"{{.BadRight}}"}`, "can't evaluate field BadRight"},
			{`{"status_line":false,"status_line_left":"{{if eq .Session.Name \"other\"}}{{.BadOther}}{{end}}"}`, "session other"},
			{`{"status_line":false,"status_line_right":"{{printf \"%70000s\" \"x\"}}"}`, "output exceeds 64 KiB"},
			{`{"status_line":false,"status_line_center":null}`, "must be a string"},
		} {
			require.NoError(t, os.WriteFile(path, []byte(tc.config), 0600))
			a.write("\x01R")
			a.await(tc.want)
			a.command(fmt.Sprintf("printf 'rollback-%d-%%s:%%s:' \"$TOKEN\" $$; stty size", i), fmt.Sprintf("rollback-%d-kept:%d:15 500", i, pid))
			b.awaitRow(15, "WORKING"+strings.Repeat(" ", 93))
			other.awaitRow(11, "WORKING"+strings.Repeat(" ", 93))
		}
		require.NoError(t, os.WriteFile(path, []byte(`{"status_line_left":"RECOVERED","status_line_right":""}`), 0600))
		a.write("\x01R")
		a.awaitRow(15, "RECOVERED"+strings.Repeat(" ", 491))
		b.awaitRow(15, "RECOVERED"+strings.Repeat(" ", 91))
		other.awaitRow(11, "RECOVERED"+strings.Repeat(" ", 91))
	})

	t.Run("config-reload-defaults-and-explicit-empty-sections", func(t *testing.T) {
		h := newConfiguredHarness(t, bin, `{"status_line":false,"status_line_left":"CUSTOM","status_line_center":"CENTER","status_line_right":"RIGHT"}`)
		c := h.client(40, 12, "-S", "defaults")
		c.command("printf 'initial:'; stty size", "initial:12 40")
		path := filepath.Join(h.home, ".bs", "config.json")
		for _, tc := range []struct{ config, row string }{
			{`{"status_line_left":"","status_line_right":""}`, strings.Repeat(" ", 40)},
			{`{"status_line_center":"","status_line_right":""}`, "0:sh " + strings.Repeat(" ", 35)},
			{`{"status_line_left":"","status_line_center":"C"}`, strings.Repeat(" ", 19) + "C" + strings.Repeat(" ", 12) + "defaults"},
			{`{}`, defaultStatusRow(40, "defaults", "sh")},
			{`{"status_line_left":"LAST","status_line_right":""}`, "LAST" + strings.Repeat(" ", 36)},
		} {
			require.NoError(t, os.WriteFile(path, []byte(tc.config), 0600))
			c.write("\x01R")
			c.awaitRow(11, tc.row)
		}
		require.NoError(t, os.Remove(path))
		c.write("\x01R")
		c.awaitRow(11, defaultStatusRow(40, "defaults", "sh"))
		c.command("printf 'default:'; stty size", "default:11 40")
	})
}
