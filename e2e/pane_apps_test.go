package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ghostty "go.mitchellh.com/libghostty"
)

func runPaneApplications(t *testing.T, bin string) {
	t.Run("fullscreen-applications-inside-panes", func(t *testing.T) {
		for _, app := range []struct{ name, command, marker, quit string }{
			{"vim", "vim -Nu NONE -n pane-fixture.txt", "PANE-APPLICATION-000", ":q!\r"},
			{"less", "less -R pane-fixture.txt", "PANE-APPLICATION-000", "q"},
			{"htop", "htop -C", "Tasks:", "q"},
		} {
			t.Run(app.name, func(t *testing.T) {
				_, err := exec.LookPath(app.name)
				require.NoError(t, err)
				h := newHarness(t, bin)
				var fixture strings.Builder
				for i := range 100 {
					fmt.Fprintf(&fixture, "PANE-APPLICATION-%03d\n", i)
				}
				require.NoError(t, os.WriteFile(filepath.Join(h.home, "pane-fixture.txt"), []byte(fixture.String()), 0600))
				c := h.client(100, 30, "-S", "apps")
				c.await("bs$ ")
				c.write("\x01|")
				c.command(app.command, app.marker)
				c.write("\x01\t")
				c.command("printf '\\033[2J\\033[2;3H\\033[1;38;2;17;88;203mSAFE-界é\\033[0m\\033[5;7H'; sleep 600", "SAFE-界é")
				c.write("\x01\t")
				c.write("\x01d")
				c.exited()
				r := h.client(100, 30, "-r", "apps")
				r.await(app.marker)
				r.await("SAFE-界é")
				r.border(49, 0, '│')
				r.border(49, 28, '│')
				require.Contains(t, r.region(0, 0, 49, 29), "SAFE-界é")
				require.NotContains(t, r.region(0, 0, 49, 29), app.marker)
				require.Contains(t, r.region(50, 0, 50, 29), app.marker)
				r.mu.Lock()
				ref, err := r.screen.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: 2, Y: 1})
				require.NoError(t, err)
				style, err := ref.Style()
				require.NoError(t, err)
				require.True(t, style.Bold())
				require.Equal(t, ghostty.ColorRGB{R: 17, G: 88, B: 203}, style.FgColor().RGB)
				x, err := r.screen.CursorX()
				require.NoError(t, err)
				require.GreaterOrEqual(t, x, uint16(50))
				r.mu.Unlock()
				r.write(app.quit)
				require.Eventually(t, func() bool { return strings.Contains(r.region(50, 0, 50, 29), "bs$ ") }, 5*time.Second, 15*time.Millisecond)
				r.write("\x01\t")
				require.Eventually(t, func() bool {
					r.mu.Lock()
					defer r.mu.Unlock()
					x, err := r.screen.CursorX()
					require.NoError(t, err)
					y, err := r.screen.CursorY()
					require.NoError(t, err)
					return x == 6 && y == 4
				}, 5*time.Second, 15*time.Millisecond)
				r.write("\x03")
				require.Eventually(t, func() bool { return strings.Contains(r.region(0, 0, 49, 29), "bs$ ") }, 5*time.Second, 15*time.Millisecond)
				r.command("printf 'left-still-%s\\n' alive", "left-still-alive")
			})
		}
	})
}
