package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
	ghostty "go.mitchellh.com/libghostty"
)

func runUnicode(t *testing.T, bin string) {
	t.Run("unicode-input-output-graphemes-and-cell-widths", func(t *testing.T) {
		h := newHarness(t, bin)
		c := h.client(80, 16, "-S", "unicode")
		c.await("bs$ ")
		for i, tc := range []struct {
			text  string
			width int
		}{
			{"café", 4},
			{"e\u0301\u0327", 1},
			{"世界日本語한글", 14},
			{"Привет", 6},
			{"مرحبا", 5},
			{"⠿█─│┌┬┐", 7},
			{"∑∈≠≤∞€", 6},
			{"😀", 2},
			{"👍🏽", 2},
			{"👩🏽‍💻", 2},
			{"👨‍👩‍👧‍👦", 2},
			{"🇺🇳", 2},
			{"❤️", 2},
			{"❤︎", 1},
			{"1️⃣", 2},
		} {
			marker := fmt.Sprintf("read-unicode-%d", i)
			c.command(fmt.Sprintf("stty -echo; printf 'read-unicode-%%s\\n' %d; IFS= read -r value; printf '\\033[2J\\033[H\\033[1;38;2;17;88;203m%%s\\033[0m|' \"$value\"; IFS= read -r ack", i), marker)
			// Exercise the physical input path with writes inside UTF-8 scalars.
			for j := range len(tc.text) {
				c.write(tc.text[j : j+1])
			}
			c.write("\r")
			c.await(tc.text + "|")
			require.Equal(t, tc.text+"|", strings.TrimRight(c.row(0), " "))
			func() {
				c.mu.Lock()
				defer c.mu.Unlock()
				x, err := c.screen.CursorX()
				require.NoError(t, err)
				y, err := c.screen.CursorY()
				require.NoError(t, err)
				require.Equal(t, uint16(tc.width+1), x, "%q cursor width", tc.text)
				require.Zero(t, y)
				ref, err := c.screen.GridRef(ghostty.Point{Tag: ghostty.PointTagActive})
				require.NoError(t, err)
				style, err := ref.Style()
				require.NoError(t, err)
				require.True(t, style.Bold())
				require.Equal(t, ghostty.ColorRGB{R: 17, G: 88, B: 203}, style.FgColor().RGB)
				if tc.width <= 2 {
					cluster, err := ref.Graphemes()
					require.NoError(t, err)
					var want []uint32
					for _, r := range tc.text {
						want = append(want, uint32(r))
					}
					require.Equal(t, want, cluster, "%q must remain one grapheme", tc.text)
				}
			}()
			c.write("\r")
		}
		c.write("\x01d")
		c.exited()
		require.Eventually(t, func() bool {
			c.mu.Lock()
			defer c.mu.Unlock()
			mode, err := c.screen.Mode(ghostty.NewMode(2027, false))
			require.NoError(t, err)
			return !mode
		}, 5*time.Second, 15*time.Millisecond, "detach must restore the physical terminal's width mode")
	})

	t.Run("unicode-split-alternate-screen-cropping-resize-and-reattach", func(t *testing.T) {
		h := newHarness(t, bin)
		const text = "A😀é界👩🏽‍💻🇺🇳❤️Z"
		c := h.client(80, 16, "-S", "unicode-panes")
		c.await("bs$ ")
		c.command("printf '\\033[2J\\033[Hleft-%s\\n' '界😀é'", "left-界😀é")
		c.write("\x01|")
		c.command("printf '\\033[?1049h\\033[2J\\033[H%s\\033[3;39H😀\\033[5;40H界' '"+text+"'; sleep 600", text)
		c.border(39, 2, '│')
		c.awaitRow(2, strings.Repeat(" ", 39)+"│"+strings.Repeat(" ", 38)+"😀")
		require.Contains(t, c.region(40, 5, 40, 1), "界")
		small := h.client(42, 12, "-x", "unicode-panes")
		small.await("left-界😀é")
		small.border(39, 0, '│')
		small.awaitRow(0, "left-界😀é"+strings.Repeat(" ", 39-10)+"│A")
		c.write("\x01d")
		c.exited()
		r := h.client(80, 16, "-x", "unicode-panes")
		r.await(text)
		r.awaitRow(2, strings.Repeat(" ", 39)+"│"+strings.Repeat(" ", 38)+"😀")
		require.NoError(t, pty.Setsize(r.pty, &pty.Winsize{Cols: 90, Rows: 18}))
		r.mu.Lock()
		require.NoError(t, r.screen.Resize(90, 18, 0, 0))
		r.mu.Unlock()
		r.write("\x01F")
		r.border(44, 0, '│')
		require.Contains(t, r.region(45, 0, 45, 1), text)
		require.Contains(t, r.region(0, 0, 44, 17), "left-界😀é")
	})

	t.Run("unicode-scrollback-multiattach-and-window-title", func(t *testing.T) {
		h := newHarness(t, bin)
		const text = "😀界é👩🏽‍💻🇺🇳❤️"
		a := h.client(80, 12, "-S", "unicode-history")
		a.await("bs$ ")
		a.command("i=0; while [ \"$i\" -lt 60 ]; do printf 'unicode-%03d:%s\\n' \"$i\" '"+text+"'; i=$((i+1)); done", "unicode-059:"+text)
		b := h.client(80, 12, "-x", "unicode-history")
		b.await("unicode-059:" + text)
		a.write("\x01\x1b\x1b[5~\x1b[5~\x1b[5~\x1b[5~")
		a.await("unicode-010:" + text)
		b.command("printf 'live-%s:%s\\n' ok '"+text+"'", "live-ok:"+text)
		a.absent("live-ok:" + text)
		a.write("\x1b")
		a.await("live-ok:" + text)
		b.command("printf '\\033]2;%s\\007' '"+text+"'; printf 'title-%s\\n' ready", "title-ready")
		b.write("\x01\"")
		b.await("bs - Windows")
		b.await(text)
		require.NotContains(t, b.text(), "\ufffd")
		b.write("\x1b")
		b.await("title-ready")
	})
}
