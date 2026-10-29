package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
	ghostty "go.mitchellh.com/libghostty"
)

func (c *terminalClient) cellColors(x, y int, fg, bg *ghostty.ColorRGB) {
	c.t.Helper()
	wantFG, wantBG := ghostty.StyleColor{Tag: ghostty.StyleColorNone}, ghostty.StyleColor{Tag: ghostty.StyleColorNone}
	if fg != nil {
		wantFG = ghostty.StyleColor{Tag: ghostty.StyleColorRGB, RGB: *fg}
	}
	if bg != nil {
		wantBG = ghostty.StyleColor{Tag: ghostty.StyleColorRGB, RGB: *bg}
	}
	require.Eventually(c.t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		ref, err := c.screen.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: uint16(x), Y: uint32(y)})
		require.NoError(c.t, err)
		style, err := ref.Style()
		require.NoError(c.t, err)
		return style.FgColor() == wantFG && style.BgColor() == wantBG
	}, 5*time.Second, 15*time.Millisecond, "cell (%d,%d) colors", x, y)
}

func runColors(t *testing.T, bin string) {
	t.Run("configurable-ui-colors-and-atomic-reload", func(t *testing.T) {
		settings := map[string]string{
			"status_line_left":  `{{range .Windows}}{{if .Active}}{{highlight (printf "[%d:%s]" .Number .Title)}}{{else}}[{{.Number}}:{{.Title}}]{{end}} {{end}}`,
			"status_line_right": "",
		}
		h := newConfiguredHarness(t, bin, templateConfig(t, settings))
		a := h.client(80, 16, "-S", "colors")
		a.awaitRow(15, "[0:sh] "+strings.Repeat(" ", 73))
		a.write("\x01|")
		a.command("TOKEN=kept; printf '%s\\n' $$ > colors.pid; printf 'color-%s\\n' ready", "color-ready")
		pid := readPanePID(t, h.home, "colors.pid")
		a.write("\x01c")
		a.awaitRow(15, "[0:sh] [1:sh] "+strings.Repeat(" ", 66))
		a.write("\x010")
		a.border(39, 0, '│')
		b := h.client(80, 16, "-x", "colors")
		b.await("color-ready")
		other := h.client(80, 16, "-S", "other")
		other.awaitRow(15, "[0:sh] "+strings.Repeat(" ", 73))
		accent := ghostty.ColorRGB{R: 135, G: 206, B: 250}
		check := func(fg, bg, border *ghostty.ColorRGB) {
			t.Helper()
			for _, c := range []*terminalClient{a, b} {
				c.cellColors(0, 15, fg, bg)
				c.cellColors(6, 15, nil, nil)
				c.cellColors(7, 15, nil, nil)
				c.cellColors(79, 15, nil, nil)
				c.cellColors(39, 0, border, nil)
				c.cellColors(0, 0, nil, nil)
			}
			other.cellColors(0, 15, fg, bg)
			other.cellColors(6, 15, nil, nil)
		}
		check(&accent, nil, &accent)
		path := filepath.Join(h.home, ".bs", "config.json")
		resize := func(c *terminalClient, cols uint16) {
			t.Helper()
			require.NoError(t, pty.Setsize(c.pty, &pty.Winsize{Cols: cols, Rows: 16}))
			c.mu.Lock()
			require.NoError(t, c.screen.Resize(cols, 16, 0, 0))
			c.mu.Unlock()
		}
		reload := func() {
			t.Helper()
			require.NoError(t, os.WriteFile(path, []byte(templateConfig(t, settings)), 0600))
			a.write("\x01R")
		}
		settings["border_color"] = "#123456"
		settings["status_active_tab_color"] = "#FFF"
		settings["status_active_tab_bg"] = "#87CEFA"
		reload()
		white, border := ghostty.ColorRGB{R: 255, G: 255, B: 255}, ghostty.ColorRGB{R: 18, G: 52, B: 86}
		check(&white, &accent, &border)
		resize(b, 5)
		b.write("\x01F")
		b.awaitRow(15, "[0:sh")
		b.cellColors(4, 15, &white, &accent)
		b.cellColors(0, 0, nil, nil)
		resize(b, 7)
		b.write("\x01F")
		b.awaitRow(15, "[0:sh] ")
		b.cellColors(0, 15, &white, &accent)
		b.cellColors(6, 15, nil, nil)
		resize(b, 80)
		b.write("\x01F")
		b.awaitRow(15, "[0:sh] [1:sh] "+strings.Repeat(" ", 66))
		check(&white, &accent, &border)
		resize(a, 400)
		for _, key := range []string{"border_color", "status_active_tab_color", "status_active_tab_bg"} {
			require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(`{%q:"#invalid"}`, key)), 0600))
			a.write("\x01R")
			a.await(key)
			b.cellColors(0, 15, &white, &accent)
			b.cellColors(39, 0, &border, nil)
			other.cellColors(0, 15, &white, &accent)
			a.command("printf 'kept-%s:%s:' \"$TOKEN\" $$; stty size", fmt.Sprintf("kept-kept:%d:15 40", pid))
		}
		resize(a, 80)
		settings["border_color"] = "#aBc"
		settings["status_active_tab_color"] = "#0f0"
		settings["status_active_tab_bg"] = ""
		reload()
		green, pale := ghostty.ColorRGB{G: 255}, ghostty.ColorRGB{R: 170, G: 187, B: 204}
		check(&green, nil, &pale)
		settings["status_active_tab_color"] = ""
		reload()
		check(nil, nil, &pale)
		settings["status_active_tab_bg"] = "#123"
		reload()
		background := ghostty.ColorRGB{R: 17, G: 34, B: 51}
		check(nil, &background, &pale)
		for _, key := range []string{"border_color", "status_active_tab_color", "status_active_tab_bg"} {
			delete(settings, key)
		}
		reload()
		check(&accent, nil, &accent)
		a.command("printf 'final-%s:%s:' \"$TOKEN\" $$; stty size", fmt.Sprintf("final-kept:%d:15 40", pid))
	})
}
