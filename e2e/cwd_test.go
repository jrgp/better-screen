package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func runWorkingDirectories(t *testing.T, bin string) {
	t.Run("session-start-directory-persists-across-windows-panes-and-reattach", func(t *testing.T) {
		h := newHarness(t, bin)
		first := filepath.Join(h.home, "project one-界")
		second := filepath.Join(h.home, "project-two")
		require.NoError(t, os.Mkdir(first, 0700))
		require.NoError(t, os.Mkdir(second, 0700))
		physicalFirst, err := filepath.EvalSymlinks(first)
		require.NoError(t, err)
		physicalSecond, err := filepath.EvalSymlinks(second)
		require.NoError(t, err)
		physicalHome, err := filepath.EvalSymlinks(h.home)
		require.NoError(t, err)
		a := h.clientAt(first, 160, 20, "-S", "first")
		a.await("bs$ ")
		a.command("printf 'initial:'; pwd -P", "initial:"+physicalFirst)
		b := h.clientAt(second, 160, 20, "-S", "second")
		b.await("bs$ ")
		b.command("printf 'independent:'; pwd -P", "independent:"+physicalSecond)
		a.command("cd \"$HOME\"; printf 'changed:'; pwd -P", "changed:"+physicalHome)
		a.write("\x01c")
		a.command("printf 'new-window:'; pwd -P; printf 'env:%s\\n' \"$PWD\"", "new-window:"+physicalFirst)
		a.await("env:" + physicalFirst)
		a.write("\x01|")
		a.command("printf 'split:'; pwd -P", "split:"+physicalFirst)
		a.write("\x01d")
		a.exited()
		r := h.clientAt(second, 160, 20, "-r", "first")
		r.await("split:" + physicalFirst)
		r.write("\x01c")
		r.command("printf 'reattached:'; pwd -P", "reattached:"+physicalFirst)
		b.command("cd \"$HOME\"; printf 'second-%s\\n' changed", "second-changed")
		b.write("\x01c")
		b.command("printf 'second-window:'; pwd -P", "second-window:"+physicalSecond)
	})

	for _, replacement := range []string{"removed", "file", "parent-file"} {
		t.Run("session-start-directory-home-fallback-"+replacement, func(t *testing.T) {
			h := newHarness(t, bin)
			parent := filepath.Join(h.home, "project")
			original := filepath.Join(parent, "start")
			require.NoError(t, os.MkdirAll(original, 0700))
			physicalOriginal, err := filepath.EvalSymlinks(original)
			require.NoError(t, err)
			physicalHome, err := filepath.EvalSymlinks(h.home)
			require.NoError(t, err)
			c := h.clientAt(original, 160, 20, "-S", "fallback")
			c.await("bs$ ")
			c.command("TOKEN=original; printf 'initial:'; pwd -P", "initial:"+physicalOriginal)
			require.NoError(t, os.Remove(original))
			switch replacement {
			case "file":
				require.NoError(t, os.WriteFile(original, []byte("not a directory"), 0600))
			case "parent-file":
				require.NoError(t, os.Remove(parent))
				require.NoError(t, os.WriteFile(parent, []byte("not a directory"), 0600))
			}
			c.write("\x01c")
			c.command("printf 'fallback:'; pwd -P; printf 'env:%s\\n' \"$PWD\"", "fallback:"+physicalHome)
			c.await("env:" + physicalHome)
			c.write("\x01|")
			c.command("printf 'fallback-pane:'; pwd -P", "fallback-pane:"+physicalHome)
			c.write("\x010")
			c.command("printf 'surviving-%s\\n' \"$TOKEN\"", "surviving-original")
			switch replacement {
			case "file":
				require.NoError(t, os.Remove(original))
			case "parent-file":
				require.NoError(t, os.Remove(parent))
			}
			require.NoError(t, os.MkdirAll(original, 0700))
			c.write("\x01c")
			c.command("printf 'restored:'; pwd -P", "restored:"+physicalOriginal)
		})
	}

	t.Run("session-start-directory-missing-home-is-left-to-shell", func(t *testing.T) {
		shell := filepath.Join(t.TempDir(), "shell")
		require.NoError(t, os.WriteFile(shell, []byte("#!/bin/sh\nprintf 'shell-%s\\n' started\nif [ ! -d \"$HOME\" ]; then printf 'home-%s\\n' absent; fi\nexec /bin/sh\n"), 0700))
		h := newHarness(t, bin, shell)
		original := filepath.Join(h.home, "project")
		require.NoError(t, os.Mkdir(original, 0700))
		c := h.clientAt(original, 160, 20, "-S", "missing-home")
		c.await("shell-started")
		c.command("printf 'first-%s\\n' ready", "first-ready")
		moved := h.home + "-moved"
		require.NoError(t, os.Rename(h.home, moved))
		t.Cleanup(func() { require.NoError(t, os.Rename(moved, h.home)) })
		c.write("\x01c")
		c.await("home-absent")
		c.command("printf 'shell-%s\\n' running", "shell-running")
		require.NoDirExists(t, h.home)
		c.write("\x01|")
		c.command("printf 'pane-%s\\n' running", "pane-running")
		require.NoDirExists(t, h.home)
	})
}
