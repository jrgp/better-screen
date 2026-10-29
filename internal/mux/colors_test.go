package mux

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestConfigColors(t *testing.T) {
	for _, tc := range []struct {
		name, config, border, tab string
	}{
		{"defaults", `{}`, "\x1b[38;2;135;206;250m", "\x1b[38;2;135;206;250m"},
		{"hex", `{"border_color":"#123","status_active_tab_color":"#aBcD01","status_active_tab_bg":"#09F"}`,
			"\x1b[38;2;17;34;51m", "\x1b[38;2;171;205;1;48;2;0;153;255m"},
		{"black", `{"border_color":"#000000","status_active_tab_color":"#000","status_active_tab_bg":""}`,
			"\x1b[38;2;0;0;0m", "\x1b[38;2;0;0;0m"},
		{"unset-foreground", `{"status_active_tab_color":""}`,
			"\x1b[38;2;135;206;250m", ""},
		{"background-only", `{"status_active_tab_color":"","status_active_tab_bg":"#123"}`,
			"\x1b[38;2;135;206;250m", "\x1b[48;2;17;34;51m"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(home, ".bs"), 0700))
			require.NoError(t, os.WriteFile(filepath.Join(home, ".bs", "config.json"), []byte(tc.config), 0600))
			config, err := loadConfig(home)
			require.NoError(t, err)
			require.Equal(t, tc.border, config.borderStyle)
			require.Equal(t, tc.tab, config.activeTabStyle)
			require.Equal(t, tc.tab+"active\x1b[39;49m", statusText(tc.tab+"active\x1b[39;49m", config.activeTabStyle))
			require.Equal(t, "safex", statusText("safe\x1b[31mx\x1b[2J\x1b]2;title\a", config.activeTabStyle))
			windows := []windowInfo{{Title: "界😀é", Active: true}}
			line, err := config.statusLine(windows, "work", 80)
			require.NoError(t, err)
			require.Contains(t, line, tc.tab+"[界😀é]\x1b[39;49m")
			for cols := 1; cols <= 30; cols++ {
				line, err := config.statusLine(windows, "work", cols)
				require.NoError(t, err)
				require.Equal(t, cols, ansi.StringWidth(line))
			}
		})
	}
}

func TestConfigColorChannels(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(home, ".bs"), 0700))
	for r := range 256 {
		g, b := 255-r, r^0x55
		hex := fmt.Sprintf("#%02x%02x%02x", r, g, b)
		data := fmt.Sprintf(`{"border_color":%q,"status_active_tab_color":%q,"status_active_tab_bg":%q}`, hex, hex, hex)
		require.NoError(t, os.WriteFile(filepath.Join(home, ".bs", "config.json"), []byte(data), 0600))
		config, err := loadConfig(home)
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b), config.borderStyle, hex)
		require.Equal(t, fmt.Sprintf("\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm", r, g, b, r, g, b), config.activeTabStyle, hex)
	}
}

func TestConfigInvalidColors(t *testing.T) {
	for _, key := range []string{"border_color", "status_active_tab_color", "status_active_tab_bg"} {
		for _, value := range []string{
			`null`, `false`, `123`, `[]`, `{}`, `"red"`, `"87cefa"`,
			`"#12"`, `"#1234"`, `"#12345"`, `"#12345678"`, `"#gggggg"`,
			`" #123"`, `"#123 "`, `"\u001b[2J"`,
		} {
			t.Run(key+"/"+value, func(t *testing.T) {
				home := t.TempDir()
				require.NoError(t, os.Mkdir(filepath.Join(home, ".bs"), 0700))
				data := fmt.Sprintf(`{%q:%s}`, key, value)
				require.NoError(t, os.WriteFile(filepath.Join(home, ".bs", "config.json"), []byte(data), 0600))
				_, err := loadConfig(home)
				require.ErrorContains(t, err, key)
			})
		}
	}
	t.Run("border_color/empty", func(t *testing.T) {
		home := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(home, ".bs"), 0700))
		require.NoError(t, os.WriteFile(filepath.Join(home, ".bs", "config.json"), []byte(`{"border_color":""}`), 0600))
		_, err := loadConfig(home)
		require.ErrorContains(t, err, "border_color")
	})
}
