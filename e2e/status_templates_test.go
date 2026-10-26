package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
	ghostty "go.mitchellh.com/libghostty"
)

func templateConfig(t *testing.T, sections map[string]string) string {
	t.Helper()
	data, err := json.Marshal(sections)
	require.NoError(t, err)
	return string(data)
}

func (c *terminalClient) statusColor(x, y int, active bool) {
	c.t.Helper()
	require.Eventually(c.t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		ref, err := c.screen.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: uint16(x), Y: uint32(y)})
		require.NoError(c.t, err)
		style, err := ref.Style()
		require.NoError(c.t, err)
		if active {
			return style.FgColor().RGB == (ghostty.ColorRGB{R: 135, G: 206, B: 250})
		}
		return style.IsDefault()
	}, 5*time.Second, 15*time.Millisecond)
}

func runStatusTemplates(t *testing.T, bin string) {
	t.Run("application-titles-follow-pane-focus-and-clear-to-shell", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(100, 18, "-S", "titles")
		a.awaitRow(17, defaultStatusRow(100, "titles", "sh"))
		a.command("printf '\\033[?2027l\\033]2;%s\\007' 'edit-界👩🏽‍💻'; printf 'title-%s\\n' ready", "title-ready")
		a.awaitRow(17, defaultStatusRow(100, "titles", "edit-界👩🏽‍💻"))
		a.write("\x01|")
		a.awaitRow(17, defaultStatusRow(100, "titles", "sh"))
		a.command("printf '\\033]0;build\\033\\\\'; printf 'build-%s\\n' ready", "build-ready")
		a.awaitRow(17, defaultStatusRow(100, "titles", "build"))
		b := h.client(100, 18, "-x", "titles")
		b.awaitRow(17, defaultStatusRow(100, "titles", "build"))
		a.write("\x01\t")
		for _, c := range []*terminalClient{a, b} {
			c.awaitRow(17, defaultStatusRow(100, "titles", "edit-界👩🏽‍💻"))
		}
		a.command("printf '\\033]2;\\007'; printf 'clear-%s\\n' ready", "clear-ready")
		for _, c := range []*terminalClient{a, b} {
			c.awaitRow(17, defaultStatusRow(100, "titles", "sh"))
		}
		a.write("\x01\t")
		a.awaitRow(17, defaultStatusRow(100, "titles", "build"))
		a.write("\x01c")
		a.awaitRow(17, defaultStatusRow(100, "titles", "build", "sh"))
		before := a.row(17)
		a.write("\x010")
		a.statusColor(0, 17, true)
		require.Equal(t, before, a.row(17), "switching windows must change styling, not spacing")
		a.write("\x01\"")
		a.await("0  build *")
		a.write("\x1b")
		a.await("build-ready")
		a.write("\x01d")
		a.exited()
		r := h.client(100, 18, "-x", "titles")
		r.awaitRow(17, defaultStatusRow(100, "titles", "build", "sh"))
	})

	t.Run("status-template-loops-highlighting-centering-and-resize", func(t *testing.T) {
		config := templateConfig(t, map[string]string{
			"status_line_left":   `{{range .Windows}}{{if .Active}}{{highlight (printf "[%d:%s]" .Number .Title)}}{{else}}[{{.Number}}:{{.Title}}]{{end}} {{end}}`,
			"status_line_center": `{{if eq (len .Windows) 2}}界😀é{{else}}ONE{{end}}`,
			"status_line_right":  `{{.Session.Name}}/{{.Session.ID}}`,
		})
		h := newConfiguredHarness(t, bin, config)
		c := h.client(80, 16, "-S", "work")
		c.awaitRow(15, "[0:sh] "+strings.Repeat(" ", 31)+"ONE"+strings.Repeat(" ", 30)+"work/work")
		c.write("\x01c")
		want := "[0:sh] [1:sh] " + strings.Repeat(" ", 23) + "界😀é" + strings.Repeat(" ", 29) + "work/work"
		c.awaitRow(15, want)
		for x := range 6 {
			c.statusColor(x, 15, false)
			c.statusColor(x+7, 15, true)
		}
		c.write("\x010")
		c.statusColor(0, 15, true)
		require.Equal(t, want, c.row(15))
		c.statusColor(7, 15, false)
		c.write("\x01 ")
		c.statusColor(7, 15, true)
		require.Equal(t, want, c.row(15))
		require.NoError(t, pty.Setsize(c.pty, &pty.Winsize{Cols: 90, Rows: 20}))
		c.mu.Lock()
		require.NoError(t, c.screen.Resize(90, 20, 0, 0))
		c.mu.Unlock()
		c.awaitRow(19, "[0:sh] [1:sh] "+strings.Repeat(" ", 28)+"界😀é"+strings.Repeat(" ", 34)+"work/work")
		c.write("\x01F")
		c.command("printf 'dimensions:'; stty size", "dimensions:19 90")
		c.awaitRow(19, "[0:sh] [1:sh] "+strings.Repeat(" ", 28)+"界😀é"+strings.Repeat(" ", 34)+"work/work")
		require.NoError(t, os.WriteFile(filepath.Join(h.home, ".bs", "config.json"), []byte(`{"status_line_left":"changed"}`), 0600))
		b := h.client(90, 20, "-x", "work")
		b.awaitRow(19, "[0:sh] [1:sh] "+strings.Repeat(" ", 28)+"界😀é"+strings.Repeat(" ", 34)+"work/work")
	})

	for _, tc := range []struct {
		name, config, row string
	}{
		{"missing", "", defaultStatusRow(40, "work", "sh")},
		{"empty-object", "{}", defaultStatusRow(40, "work", "sh")},
		{"empty-left", `{"status_line_left":""}`, strings.Repeat(" ", 36) + "work"},
		{"empty-right", `{"status_line_right":""}`, "0:sh " + strings.Repeat(" ", 35)},
		{"center-only-override", `{"status_line_center":"C"}`, "0:sh " + strings.Repeat(" ", 14) + "C" + strings.Repeat(" ", 16) + "work"},
		{"all-empty", `{"status_line_left":"","status_line_center":"","status_line_right":""}`, strings.Repeat(" ", 40)},
	} {
		t.Run("status-template-defaults-"+tc.name, func(t *testing.T) {
			h := newConfiguredHarness(t, bin, tc.config)
			c := h.client(40, 12, "-S", "work")
			c.await("bs$ ")
			c.awaitRow(11, tc.row)
			c.command("printf 'dimensions:'; stty size", "dimensions:11 40")
		})
	}
	t.Run("status-template-unnamed-session-data", func(t *testing.T) {
		h := newConfiguredHarness(t, bin, `{"status_line_left":"","status_line_right":"{{.Session.Name}}/{{.Session.ID}}"}`)
		c := h.client(40, 12)
		c.awaitRow(11, strings.Repeat(" ", 37)+"1/1")
		require.Contains(t, h.cli("-ls"), "1\t")
	})

	for _, tc := range []struct {
		name, left, center, right, row string
	}{
		{"separate", "LEFT", "MIDDLE", "RIGHT", "LEFT" + strings.Repeat(" ", 13) + "MIDDLE" + strings.Repeat(" ", 12) + "RIGHT"},
		{"clip-center-left", strings.Repeat("L", 20), "abcdef", "RIGHT", strings.Repeat("L", 20) + "def" + strings.Repeat(" ", 12) + "RIGHT"},
		{"clip-center-right", "LEFT", "abcdef", strings.Repeat("R", 20), "LEFT" + strings.Repeat(" ", 13) + "abc" + strings.Repeat("R", 20)},
		{"clip-left-last", strings.Repeat("L", 45), "MIDDLE", "RIGHT", strings.Repeat("L", 35) + "RIGHT"},
		{"right-fills-row", "LEFT", "MIDDLE", strings.Repeat("R", 45), strings.Repeat("R", 40)},
		{"unicode-center-edge", strings.Repeat("L", 20), "界😀é", "RIGHT", strings.Repeat("L", 20) + " é" + strings.Repeat(" ", 13) + "RIGHT"},
		{"unicode-right", "界😀é", "", "界😀", "界😀é" + strings.Repeat(" ", 31) + "界😀"},
	} {
		t.Run("status-template-overlap-"+tc.name, func(t *testing.T) {
			h := newConfiguredHarness(t, bin, templateConfig(t, map[string]string{
				"status_line_left": tc.left, "status_line_center": tc.center, "status_line_right": tc.right,
			}))
			c := h.client(40, 12, "-S", "overlap")
			c.await("bs$ ")
			c.awaitRow(11, tc.row)
			c.command("printf 'content-%s\\n' intact", "content-intact")
			c.awaitRow(11, tc.row)
		})
	}

	t.Run("status-templates-cannot-inject-terminal-controls", func(t *testing.T) {
		h := newConfiguredHarness(t, bin, templateConfig(t, map[string]string{
			"status_line_left":   "LEFT\x1b[2J\x1b[?1049l\n\t\x1b]52;c;ZXZpbA==\a\x1b[31mRED\x1b[0m",
			"status_line_center": `{{highlight "CENTER"}}`,
			"status_line_right":  "RIGHT",
		}))
		c := h.client(80, 16, "-S", "safe")
		c.awaitRow(15, "LEFTRED"+strings.Repeat(" ", 30)+"CENTER"+strings.Repeat(" ", 32)+"RIGHT")
		c.command("printf 'application-%s\\n' intact", "application-intact")
		c.statusColor(37, 15, true)
		c.mu.Lock()
		raw := c.raw.String()
		c.mu.Unlock()
		require.Equal(t, 1, strings.Count(raw, "\x1b[2J"), "only the client's initial clear is allowed")
		require.NotContains(t, raw, "\x1b]52")
		require.NotContains(t, raw, "\x1b[31m")
		require.NotContains(t, raw, "\x1b[?1049l")
	})

	for _, tc := range []struct{ name, text, want string }{
		{"field-error", "{{.Missing}}", "can't evaluate field Missing"},
		{"bounded-output", `{{printf "%70000s" "x"}}`, "output exceeds 64 KiB"},
	} {
		t.Run("status-template-execution-"+tc.name, func(t *testing.T) {
			h := newConfiguredHarness(t, bin, templateConfig(t, map[string]string{"status_line_left": tc.text}))
			c := h.client(80, 16, "-S", "error")
			c.await("status_line_left")
			c.await(tc.want)
			select {
			case <-c.waitDone:
				require.Error(t, c.waitErr)
			case <-time.After(5 * time.Second):
				t.Fatal("invalid status execution did not report an error")
			}
		})
	}

}
