package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ghostty "go.mitchellh.com/libghostty"
)

func (c *terminalClient) row(y int) string {
	c.t.Helper()
	rows := strings.Split(c.text(), "\n")
	if y >= len(rows) {
		return ""
	}
	return strings.TrimSuffix(rows[y], "\r")
}

func (c *terminalClient) awaitRow(y int, want string) {
	c.t.Helper()
	if !assert.Eventually(c.t, func() bool { return c.row(y) == want }, 8*time.Second, 15*time.Millisecond) {
		c.t.Fatalf("row %d must be %q; screen: %q", y, want, c.text())
	}
}

func (c *terminalClient) pickerColors(y, active, count int) {
	c.t.Helper()
	tabs := strings.SplitN(c.row(y), "] ", count+1)
	require.Len(c.t, tabs, count+1, "all windows must appear in the picker")
	c.mu.Lock()
	defer c.mu.Unlock()
	x := 0
	for number := range count {
		require.True(c.t, strings.HasPrefix(tabs[number], "["), "window %d must appear in the picker", number)
		width := ansi.StringWidth(tabs[number]) + 1
		for i := range width {
			ref, err := c.screen.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: uint16(x + i), Y: uint32(y)})
			require.NoError(c.t, err)
			style, err := ref.Style()
			require.NoError(c.t, err)
			if number == active {
				require.Equal(c.t, ghostty.ColorRGB{R: 135, G: 206, B: 250}, style.FgColor().RGB)
			} else {
				require.True(c.t, style.IsDefault(), "inactive window %d must use default attributes", number)
			}
		}
		x += width + 1
	}
}

func defaultStatusRow(cols int, name string, titles ...string) string {
	var left strings.Builder
	for _, title := range titles {
		fmt.Fprintf(&left, "[%s] ", title)
	}
	return left.String() + strings.Repeat(" ", cols-ansi.StringWidth(left.String())-ansi.StringWidth(name)) + name
}

func runStatus(t *testing.T, bin string) {
	for _, config := range []string{"", `{"status_line":true}`, `{"status_line":false}`} {
		t.Run("status-configuration-"+config, func(t *testing.T) {
			h := newConfiguredHarness(t, bin, config)
			c := h.client(80, 12, "-S", "work")
			c.await("bs$ ")
			rows := 11
			if strings.Contains(config, "false") {
				rows = 12
			}
			c.command("printf 'dimensions:'; stty size", fmt.Sprintf("dimensions:%d 80", rows))
			c.command("printf '\\033[999;1Hbottom-%s' content", "bottom-content")
			if rows == 11 {
				c.awaitRow(11, defaultStatusRow(80, "work", "sh"))
				require.Contains(t, c.row(10), "bottom-content")
			} else {
				require.Contains(t, c.row(11), "bottom-content")
				require.NotContains(t, c.row(11), "work")
			}
		})
	}
	t.Run("status-picker-colors-order-resize-and-reattach", func(t *testing.T) {
		h := newHarness(t, bin)
		c := h.client(100, 16, "-S", "named")
		c.await("bs$ ")
		for range 11 {
			c.write("\x01c")
			c.await("bs$ ")
		}
		titles := strings.Fields(strings.Repeat("sh ", 12))
		want := defaultStatusRow(100, "named", titles...)
		c.awaitRow(15, want)
		c.pickerColors(15, 11, 12)
		for _, number := range []int{0, 4, 9} {
			c.mu.Lock()
			before := c.raw.Len()
			c.mu.Unlock()
			c.write("\x01" + fmt.Sprint(number))
			require.Eventually(t, func() bool {
				c.mu.Lock()
				defer c.mu.Unlock()
				ref, err := c.screen.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: uint16(number * 5), Y: 15})
				require.NoError(t, err)
				style, err := ref.Style()
				require.NoError(t, err)
				return style.FgColor().RGB == (ghostty.ColorRGB{R: 135, G: 206, B: 250})
			}, 5*time.Second, 15*time.Millisecond)
			require.Equal(t, want, c.row(15))
			c.pickerColors(15, number, 12)
			c.mu.Lock()
			change := c.raw.String()[before:]
			c.mu.Unlock()
			require.NotContains(t, change, "\x1b[2J", "window switches should use incremental rendering")
		}
		c.write("\x01k")
		c.awaitRow(15, defaultStatusRow(100, "named", titles[:11]...))
		c.pickerColors(15, 9, 11)
		c.write("\x01c")
		c.awaitRow(15, want)
		c.pickerColors(15, 11, 12)
		require.NoError(t, pty.Setsize(c.pty, &pty.Winsize{Cols: 85, Rows: 14}))
		c.mu.Lock()
		require.NoError(t, c.screen.Resize(85, 14, 0, 0))
		c.mu.Unlock()
		c.write("\x01F")
		c.command("printf 'new-dimensions:'; stty size", "new-dimensions:13 85")
		c.awaitRow(13, defaultStatusRow(85, "named", titles...))
		c.write("\x01d")
		c.exited()
		r := h.client(85, 14, "-r", "named")
		r.awaitRow(13, defaultStatusRow(85, "named", titles...))
	})
	t.Run("status-physical-resize-without-canonical-fit", func(t *testing.T) {
		h := newHarness(t, bin)
		c := h.client(80, 16, "-S", "resize")
		c.await("bs$ ")
		require.NoError(t, pty.Setsize(c.pty, &pty.Winsize{Cols: 100, Rows: 20}))
		c.mu.Lock()
		require.NoError(t, c.screen.Resize(100, 20, 0, 0))
		c.mu.Unlock()
		c.awaitRow(19, defaultStatusRow(100, "resize", "sh"))
		c.command("printf 'canonical:'; stty size", "canonical:15 80")
		small := h.client(60, 10, "-x", "resize")
		small.awaitRow(9, defaultStatusRow(60, "resize", "sh"))
		c.write("\x01F")
		c.command("printf '\\033[2J\\033[Hfitted:'; stty size", "fitted:19 100")
		small.await("fitted:19 100")
		small.awaitRow(9, defaultStatusRow(60, "resize", "sh"))
	})
	t.Run("status-unnamed-identifier", func(t *testing.T) {
		h := newHarness(t, bin)
		c := h.client(40, 8)
		c.await("bs$ ")
		c.awaitRow(7, defaultStatusRow(40, "1", "sh"))
		require.Contains(t, h.cli("-ls"), "1\t")
	})
}
