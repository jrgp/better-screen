package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
	ghostty "go.mitchellh.com/libghostty"
)

func (c *terminalClient) region(x, y, cols, rows int) string {
	c.t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	a, err := c.screen.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: uint16(x), Y: uint32(y)})
	require.NoError(c.t, err)
	b, err := c.screen.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: uint16(x + cols - 1), Y: uint32(y + rows - 1)})
	require.NoError(c.t, err)
	f, err := ghostty.NewFormatter(c.screen, ghostty.WithFormatterSelection(&ghostty.Selection{Start: *a, End: *b, Rectangle: true}))
	require.NoError(c.t, err)
	defer f.Close()
	text, err := f.FormatString()
	require.NoError(c.t, err)
	return text
}

func (c *terminalClient) border(x, y int, glyph rune) {
	c.t.Helper()
	require.Eventually(c.t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		ref, err := c.screen.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: uint16(x), Y: uint32(y)})
		require.NoError(c.t, err)
		cell, err := ref.Cell()
		require.NoError(c.t, err)
		cp, err := cell.Codepoint()
		require.NoError(c.t, err)
		style, err := ref.Style()
		require.NoError(c.t, err)
		return cp == uint32(glyph) && style.FgColor().RGB == (ghostty.ColorRGB{R: 135, G: 206, B: 250})
	}, 5*time.Second, 15*time.Millisecond, "border at %d,%d must be %c in light blue", x, y, glyph)
}

func runSplits(t *testing.T, bin string) {
	t.Run("splits-focus-windows-multiattach-and-reattach", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(80, 16, "-S", "panes")
		a.await("bs$ ")
		a.command("TOKEN=left; printf 'left-%s\\n' ready", "left-ready")
		a.write("\x01|")
		a.command("TOKEN=right; printf 'right-%s\\n' ready", "right-ready")
		a.border(39, 0, '│')
		b := h.client(80, 16, "-x", "panes")
		b.await("right-ready")
		b.command("printf 'shared-%s\\n' \"$TOKEN\"", "shared-right")
		a.await("shared-right")
		a.write("\x01\t")
		b.command("printf 'next-%s\\n' \"$TOKEN\"", "next-left")
		a.await("next-left")
		a.write("\x01\t")
		a.command("printf 'wrapped-%s\\n' \"$TOKEN\"", "wrapped-right")
		a.write("\x01\x1b")
		a.write("[")
		a.write("Z")
		a.command("printf 'previous-%s\\n' \"$TOKEN\"", "previous-left")
		a.write("\x01\x1b[Z")
		a.command("printf 'back-wrap-%s\\n' \"$TOKEN\"", "back-wrap-right")
		require.NotContains(t, a.region(0, 0, 39, 15), "back-wrap-right")
		require.Contains(t, a.region(40, 0, 40, 15), "back-wrap-right")
		a.write("\x01c")
		a.command("TOKEN=window1; printf 'single-%s\\n' \"$TOKEN\"", "single-window1")
		require.NotContains(t, a.text(), "│")
		a.write("\x01p")
		a.await("back-wrap-right")
		a.border(39, 0, '│')
		a.command("printf 'preserved-%s\\n' \"$TOKEN\"", "preserved-right")
		b.await("preserved-right")
		a.write("\x01n")
		a.await("single-window1")
		a.command("printf 'whole-%s\\n' \"$TOKEN\"", "whole-window1")
		a.write("\x010")
		a.await("preserved-right")
		a.write("\x01d")
		a.exited()
		b.command("printf 'after-detach-%s\\n' \"$TOKEN\"", "after-detach-right")
		r := h.client(80, 16, "-x", "panes")
		r.await("after-detach-right")
		r.border(39, 14, '│')
		require.Contains(t, r.region(0, 0, 39, 15), "previous-left")
		require.Eventually(t, func() bool { return r.text() == b.text() }, 5*time.Second, 15*time.Millisecond)
	})

	t.Run("nested-splits-junctions-and-output-confinement", func(t *testing.T) {
		h := newHarness(t, bin)
		c := h.client(80, 16, "-S", "nested")
		c.await("bs$ ")
		c.write("\x01|\x01S\x01\t\x01S")
		c.command("TOKEN=bottom-left; printf '\\033[2J\\033[Hbottom-%s\\n' left", "bottom-left")
		c.write("\x01\t")
		c.command("TOKEN=top-right; printf '\\033[2J\\033[Htop-%s\\n' right", "top-right")
		c.write("\x01\t")
		c.command("TOKEN=bottom-right; printf '\\033[2J\\033[Hbottom-%s\\n' right", "bottom-right")
		c.write("\x01\t")
		c.command("TOKEN=top-left; printf '\\033[2J\\033[Htop-%s\\n' left", "top-left")
		c.border(39, 7, '┼')
		c.border(0, 7, '─')
		c.border(79, 7, '─')
		for _, tc := range []struct {
			x, y int
			want string
		}{{0, 0, "top-left"}, {0, 8, "bottom-left"}, {40, 0, "top-right"}, {40, 8, "bottom-right"}} {
			text := c.region(tc.x, tc.y, 39, 7)
			require.Contains(t, text, tc.want)
			for _, other := range []string{"top-left", "top-right", "bottom-left", "bottom-right"} {
				if other != tc.want {
					require.NotContains(t, text, other)
				}
			}
		}
		for _, token := range []string{"bottom-right", "top-right", "bottom-left", "top-left"} {
			c.write("\x01\x1b[Z")
			c.command("printf 'reverse-%s\\n' \"$TOKEN\"", "reverse-"+token)
		}
		c.write("\x01|")
		c.command("printf '\\033[2J\\033[Hnested-%s\\n' fifth", "nested-fifth")
		c.border(19, 7, '┴')
		c.border(19, 0, '│')
		c.awaitRow(15, "0"+strings.Repeat(" ", 73)+"nested")
	})

	t.Run("pane-pty-sizes-removal-and-canonical-fit", func(t *testing.T) {
		h := newHarness(t, bin)
		c := h.client(120, 25, "-S", "sizes")
		c.await("bs$ ")
		c.command("TOKEN=original; printf 'initial:'; stty size", "initial:24 120")
		c.write("\x01|")
		c.command("printf 'right:'; stty size", "right:24 60")
		c.write("\x01S")
		c.command("TOKEN=bottom; printf 'bottom:'; stty size", "bottom:12 60")
		c.write("\x01\t")
		c.command("printf 'left:'; stty size", "left:24 59")
		c.command("printf 'preserved-%s\\n' \"$TOKEN\"", "preserved-original")
		c.write("\x01\t")
		c.command("printf 'top:'; stty size", "top:11 60")
		c.write("\x01K")
		c.command("printf 'expanded-%s:' \"$TOKEN\"; stty size", "expanded-bottom:24 60")
		c.write("\x01K")
		c.command("printf 'full-%s:' \"$TOKEN\"; stty size", "full-original:24 120")
		require.NotContains(t, c.text(), "│")
		require.NoError(t, pty.Setsize(c.pty, &pty.Winsize{Cols: 90, Rows: 17}))
		c.mu.Lock()
		require.NoError(t, c.screen.Resize(90, 17, 0, 0))
		c.mu.Unlock()
		c.write("\x01F\x01|")
		c.command("printf 'fitted-right:'; stty size", "fitted-right:16 45")
		c.write("\x01S")
		c.command("printf 'fitted-bottom:'; stty size", "fitted-bottom:8 45")
		c.write("\x01K")
		c.command("printf 'fitted-expand:'; stty size", "fitted-expand:16 45")
		c.write("\x01\t")
		c.command("printf 'original-still-%s\\n' \"$TOKEN\"", "original-still-original")
		c.write("exit\r")
		require.Eventually(t, func() bool { return !strings.Contains(c.text(), "│") }, 5*time.Second, 15*time.Millisecond)
		c.command("printf 'after-exit:'; stty size", "after-exit:16 90")
		require.NotContains(t, c.text(), "│")
	})

	t.Run("disabled-status-pane-dimensions", func(t *testing.T) {
		h := newConfiguredHarness(t, bin, `{"status_line":false}`)
		c := h.client(80, 16, "-S", "disabled")
		c.await("bs$ ")
		c.write("\x01|\x01S")
		c.command("printf 'no-status:'; stty size", "no-status:8 40")
		c.command("printf '\\033[999;1Hlast-%s' application", "last-application")
		require.Contains(t, c.row(15), "last-application")
		c.border(39, 15, '│')
	})
}
