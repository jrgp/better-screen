package mux

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestStatusCellBounds(t *testing.T) {
	for _, sections := range [][3]string{
		{"", "", ""},
		{"界😀é", "👩🏽‍💻🇺🇳❤️", "終"},
		{"{{highlight \"界😀\"}}", "{{highlight \"😀界é\"}}", "{{highlight \"右界\"}}"},
		{"long left text", "long center text", "long right text"},
		{"A", "界😀👩🏽‍💻界😀", "B"},
	} {
		home := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(home, ".bs"), 0700))
		data, err := json.Marshal(map[string]string{
			"status_line_left": sections[0], "status_line_center": sections[1], "status_line_right": sections[2],
		})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(home, ".bs", "config.json"), data, 0600))
		config, err := loadConfig(home)
		require.NoError(t, err)
		for cols := 1; cols <= 80; cols++ {
			line, err := config.statusLine(nil, "work", cols)
			require.NoError(t, err)
			require.Equal(t, cols, ansi.StringWidth(line), "columns=%d sections=%q output=%q", cols, sections, line)
		}
	}
}

func TestStatusControlFiltering(t *testing.T) {
	input := "safe\x1b[2J\x1b[?1049l\x1b]52;c;evil\a\x1bPpayload\x1b\\\r\n\t\b\x00\x7f\u0085" +
		accent + "active\x1b[39m" + "\x1b[31mplain\x1b[0m\x1b["
	require.Equal(t, "safe"+accent+"active\x1b[39mplain", statusText(input))
}
