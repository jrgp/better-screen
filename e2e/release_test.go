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

func TestReleaseTarget(t *testing.T) {
	makefile, err := os.ReadFile("../Makefile")
	require.NoError(t, err)
	for _, tc := range []struct {
		name string
		tags []string
		want string
	}{
		{"first-release", nil, "v0.0.1"},
		{"increment-patch", []string{"v0.0.1"}, "v0.0.2"},
		{"highest-stable-remote-tag", []string{"v1.9.99", "v1.10.9", "v99.0.0-rc.1", "other"}, "v1.10.10"},
		{"dirty-worktree", []string{"v0.0.1"}, ""},
		{"push-failure", []string{"v0.0.1"}, "v0.0.2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			repo, remote := filepath.Join(root, "work"), filepath.Join(root, "origin.git")
			require.NoError(t, os.Mkdir(repo, 0700))
			env := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
			execute := func(args ...string) (string, error) {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, args[0], args[1:]...)
				cmd.Dir, cmd.Env = repo, env
				out, err := cmd.CombinedOutput()
				return string(out), err
			}
			run := func(args ...string) string {
				t.Helper()
				out, err := execute(args...)
				require.NoError(t, err, out)
				return strings.TrimSpace(out)
			}
			run("git", "init", "--bare", remote)
			run("git", "init", "-b", "master")
			run("git", "config", "user.name", "Release test")
			run("git", "config", "user.email", "release@example.invalid")
			require.NoError(t, os.WriteFile(filepath.Join(repo, "Makefile"), makefile, 0600))
			run("git", "add", "Makefile")
			run("git", "commit", "-m", "Initial")
			run("git", "remote", "add", "origin", remote)
			run("git", "push", "origin", "HEAD")
			for _, tag := range tc.tags {
				run("git", "--git-dir", remote, "tag", tag, "refs/heads/master")
			}
			run("git", "commit", "--allow-empty", "-m", "Ready")
			if tc.name == "dirty-worktree" {
				require.NoError(t, os.WriteFile(filepath.Join(repo, "uncommitted"), []byte("pending"), 0600))
			}
			if tc.name == "push-failure" {
				hook := filepath.Join(remote, "hooks", "pre-receive")
				require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\nwhile read old new ref; do\ncase \"$ref\" in refs/tags/*) exit 1;; esac\ndone\n"), 0700))
			}
			out, err := execute("make", "release")
			if tc.want == "" {
				require.Error(t, err, out)
				require.Contains(t, out, "Commit or stash")
				require.Empty(t, run("git", "tag", "--list"))
				return
			}
			if tc.name == "push-failure" {
				require.Error(t, err, out)
				require.Contains(t, out, "Tag "+tc.want+" remains local")
				require.Equal(t, tc.want, run("git", "tag", "--list", tc.want))
				require.Empty(t, run("git", "ls-remote", "--tags", "origin", "refs/tags/"+tc.want))
				return
			}
			require.NoError(t, err, out)
			require.Contains(t, out, "Releasing "+tc.want)
			require.Equal(t, run("git", "rev-parse", "HEAD"), run("git", "rev-parse", tc.want+"^{commit}"))
			tagID := run("git", "rev-parse", tc.want)
			require.Equal(t, tagID+"\trefs/tags/"+tc.want, run("git", "ls-remote", "--tags", "origin", "refs/tags/"+tc.want))
			require.Equal(t, run("git", "rev-parse", "HEAD")+"\trefs/heads/master", run("git", "ls-remote", "origin", "refs/heads/master"))
			require.Equal(t, "tag", run("git", "cat-file", "-t", tc.want))
			require.Empty(t, run("git", "status", "--porcelain"))
		})
	}
}
