package mux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfig(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		on         bool
		bad        bool
	}{
		{"missing", "", true, false},
		{"empty-object", "{}", true, false},
		{"maximum-size", "{}" + strings.Repeat(" ", 65534), true, false},
		{"oversized", "{}" + strings.Repeat(" ", 65535), false, true},
		{"enabled", `{"status_line":true}`, true, false},
		{"disabled", `{"status_line":false}`, false, false},
		{"syntax", `{"status_line":`, false, true},
		{"empty-file", "\n", false, true},
		{"trailing-json", `{} {}`, false, true},
		{"trailing-garbage", `{} invalid`, false, true},
		{"type", `{"status_line":"true"}`, false, true},
		{"null-value", `{"status_line":null}`, false, true},
		{"null-object", `null`, false, true},
		{"array", `[]`, false, true},
		{"boolean", `false`, false, true},
		{"unknown", `{"theme":"blue"}`, false, true},
		{"empty-templates", `{"status_line_left":"","status_line_center":"","status_line_right":""}`, true, false},
		{"left-type", `{"status_line_left":true}`, false, true},
		{"center-null", `{"status_line_center":null}`, false, true},
		{"left-null", `{"status_line_left":null}`, false, true},
		{"right-null", `{"status_line_right":null}`, false, true},
		{"right-syntax-disabled", `{"status_line":false,"status_line_right":"{{if}}"}`, false, true},
		{"unknown-function", `{"status_line_left":"{{unknown}}"}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.data != "" {
				require.NoError(t, os.Mkdir(filepath.Join(home, ".bs"), 0700))
				require.NoError(t, os.WriteFile(filepath.Join(home, ".bs", "config.json"), []byte(tc.data), 0600))
			}
			config, err := loadConfig(home)
			if tc.bad {
				require.ErrorContains(t, err, "config.json")
			} else {
				require.NoError(t, err)
				require.NotNil(t, config.StatusLine)
				require.Equal(t, tc.on, *config.StatusLine)
			}
		})
	}
}
