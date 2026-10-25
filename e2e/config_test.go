package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func runConfiguration(t *testing.T, bin string) {
	t.Run("json-configuration", func(t *testing.T) {
		for _, tc := range []struct {
			name, data, want string
			bad              bool
		}{
			{"missing", "", "No sessions.", false},
			{"enabled", `{"status_line":true}`, "No sessions.", false},
			{"disabled", `{"status_line":false}`, "No sessions.", false},
			{"malformed", `{"status_line":`, "config.json", true},
			{"wrong-type", `{"status_line":"false"}`, "must be a boolean", true},
			{"unknown", `{"color":"blue"}`, "unknown setting", true},
			{"oversized", strings.Repeat(" ", 65537), "exceeds 64 KiB", true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				home := t.TempDir()
				require.NoError(t, os.Mkdir(filepath.Join(home, ".bs"), 0700))
				if tc.data != "" {
					require.NoError(t, os.WriteFile(filepath.Join(home, ".bs", "config.json"), []byte(tc.data), 0600))
				}
				for _, op := range []string{"-ls", "__daemon"} {
					if op == "__daemon" && !tc.bad {
						continue
					}
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					cmd := exec.CommandContext(ctx, bin, op)
					cmd.Env = append(os.Environ(), "HOME="+home)
					out, err := cmd.CombinedOutput()
					cancel()
					if tc.bad {
						require.Error(t, err)
					} else {
						require.NoError(t, err)
					}
					require.Contains(t, string(out), tc.want)
				}
				require.NoFileExists(t, filepath.Join(home, ".bs", "bs.sock"))
			})
		}
	})
}
