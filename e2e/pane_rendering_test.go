package e2e

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ghostty "go.mitchellh.com/libghostty"
)

func runPaneRendering(t *testing.T, bin string) {
	t.Run("pane-scrollback-and-background-exit-preserve-focus", func(t *testing.T) {
		h := newHarness(t, bin)
		gate := filepath.Join(h.home, "background-exit")
		require.NoError(t, syscall.Mkfifo(gate, 0600))
		a := h.client(100, 20, "-S", "history-panes")
		a.await("bs$ ")
		a.command("printf 'background-%s\\n' ready; read answer < background-exit; exit", "background-ready")
		a.write("\x01|")
		a.command("TOKEN=right; i=0; while [ \"$i\" -lt 60 ]; do printf 'history-%03d\\n' \"$i\"; i=$((i+1)); done", "history-059")
		b := h.client(100, 20, "-x", "history-panes")
		b.await("history-059")
		b.write("\x01\x1b\x1b[5~")
		b.await("history-025")
		a.command("printf 'right-live-%s\\n' remains", "right-live-remains")
		b.absent("right-live-remains")
		require.Contains(t, b.region(0, 0, 49, 19), "background-ready")
		b.write("\x1b")
		b.await("right-live-remains")
		var fd int
		var err error
		require.Eventually(t, func() bool {
			fd, err = syscall.Open(gate, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
			return err != syscall.ENXIO && err != syscall.EINTR
		}, 5*time.Second, 15*time.Millisecond)
		require.NoError(t, err)
		defer syscall.Close(fd)
		_, err = syscall.Write(fd, []byte("exit\n"))
		require.NoError(t, err)
		require.Eventually(t, func() bool { return !strings.Contains(a.text(), "│") }, 5*time.Second, 15*time.Millisecond)
		a.command("printf 'focused-%s:' \"$TOKEN\"; stty size", "focused-right:19 100")
		b.await("focused-right:19 100")
	})
	t.Run("pane-local-reverse-video-and-default-colors", func(t *testing.T) {
		h := newHarness(t, bin)
		c := h.client(80, 16, "-S", "reverse")
		c.await("bs$ ")
		c.command("printf '\\033[2J\\033[Hleft-%s\\n' untouched", "left-untouched")
		c.write("\x01|")
		c.command("printf '\\033[?5h\\033[2J\\033[Hreverse-%s\\n' pane", "reverse-pane")
		c.mu.Lock()
		global, err := c.screen.Mode(ghostty.NewMode(5, false))
		require.NoError(t, err)
		c.mu.Unlock()
		require.False(t, global, "a pane must not reverse the physical terminal")
		c.mu.Lock()
		ref, err := c.screen.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: 40})
		require.NoError(t, err)
		initialStyle, err := ref.Style()
		require.NoError(t, err)
		require.True(t, initialStyle.Inverse())
		c.mu.Unlock()
		c.border(39, 0, '│')
		c.command("printf '\\033[2;1H\\033[1;38;2;10;20;30;48;2;70;80;90mRGB-%s\\033[0m' explicit", "RGB-explicit")
		check := func(y int, fg, bg ghostty.ColorRGB) {
			c.mu.Lock()
			defer c.mu.Unlock()
			ref, err := c.screen.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: 40, Y: uint32(y)})
			require.NoError(t, err)
			style, err := ref.Style()
			require.NoError(t, err)
			actualFG, actualBG := style.FgColor().RGB, style.BgColor().RGB
			if style.Inverse() {
				actualFG, actualBG = actualBG, actualFG
			}
			require.Equal(t, fg, actualFG)
			require.Equal(t, bg, actualBG)
		}
		check(1, ghostty.ColorRGB{R: 10, G: 20, B: 30}, ghostty.ColorRGB{R: 70, G: 80, B: 90})
		c.command("printf '\\033]10;rgb:11/22/33\\007\\033]11;rgb:44/55/66\\007\\033[0m\\033[3;1Hdefault-%s' reversed", "default-reversed")
		check(2, ghostty.ColorRGB{R: 68, G: 85, B: 102}, ghostty.ColorRGB{R: 17, G: 34, B: 51})
		check(10, ghostty.ColorRGB{R: 68, G: 85, B: 102}, ghostty.ColorRGB{R: 17, G: 34, B: 51})
		c.border(39, 10, '│')
		c.command("printf '\\033[4;1H\\033[7;38;2;10;20;30;48;2;70;80;90mINVERSE-%s\\033[0m' cell", "INVERSE-cell")
		check(3, ghostty.ColorRGB{R: 70, G: 80, B: 90}, ghostty.ColorRGB{R: 10, G: 20, B: 30})
		require.Contains(t, c.region(0, 0, 39, 15), "left-untouched")
		c.pickerColors(15, 0, 1)
	})
	t.Run("pane-alternate-screen-cursor-and-cropped-clients", func(t *testing.T) {
		h := newHarness(t, bin)
		c := h.client(80, 16, "-S", "cropped")
		c.await("bs$ ")
		c.command("printf '\\033[2J\\033[Hleft-%s\\n' stable", "left-stable")
		c.write("\x01|")
		c.command("printf '\\033[?1049h\\033[2J\\033[3;5H\\033[1;38;2;17;88;203mALT-界é\\033[0m\\033[7;9H'; sleep 600", "ALT-界é")
		c.write("\x01d")
		c.exited()
		r := h.client(60, 12, "-r", "cropped")
		r.await("ALT-界é")
		r.border(39, 10, '│')
		r.mu.Lock()
		x, err := r.screen.CursorX()
		require.NoError(t, err)
		y, err := r.screen.CursorY()
		require.NoError(t, err)
		require.Equal(t, uint16(48), x)
		require.Equal(t, uint16(6), y)
		ref, err := r.screen.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: 44, Y: 2})
		require.NoError(t, err)
		style, err := ref.Style()
		require.NoError(t, err)
		require.True(t, style.Bold())
		require.Equal(t, ghostty.ColorRGB{R: 17, G: 88, B: 203}, style.FgColor().RGB)
		r.mu.Unlock()
		r.awaitRow(11, "0"+strings.Repeat(" ", 52)+"cropped")
		small := h.client(40, 10, "-x", "cropped")
		small.awaitRow(9, "0"+strings.Repeat(" ", 32)+"cropped")
		small.mu.Lock()
		visible, err := small.screen.CursorVisible()
		require.NoError(t, err)
		require.False(t, visible)
		small.mu.Unlock()
		r.write("\x03")
		require.Eventually(t, func() bool { return strings.Contains(r.region(40, 0, 20, 11), "bs$ ") }, 5*time.Second, 15*time.Millisecond)
		r.command("printf '\\033[?1049l\\033[2J\\033[Hprimary-%s\\n' restored", "primary-restored")
		require.Contains(t, r.region(0, 0, 39, 11), "left-stable")
	})
}
