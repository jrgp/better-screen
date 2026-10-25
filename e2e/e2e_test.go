package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/jrgp/bs/internal/vt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ghostty "go.mitchellh.com/libghostty"
)

type harness struct {
	t         *testing.T
	bin, home string
	daemon    *exec.Cmd
	log       bytes.Buffer
	stopped   bool
}

func newHarness(t *testing.T, bin string, shell ...string) *harness {
	t.Helper()
	// Keep Unix socket paths below the macOS 104-byte limit.
	home, err := os.MkdirTemp("", "bs-e2e-")
	require.NoError(t, err)
	h := &harness{t: t, bin: bin, home: home}
	h.daemon = exec.Command(bin, "__daemon")
	h.daemon.Env = h.env()
	if len(shell) > 0 {
		h.daemon.Env = append(h.daemon.Env, "SHELL="+shell[0])
	}
	h.daemon.Stderr = &h.log
	require.NoError(t, h.daemon.Start())
	t.Cleanup(func() {
		h.stopDaemon()
		assert.NoFileExists(t, filepath.Join(home, ".bs", "bs.sock"))
		require.NoError(t, os.RemoveAll(home))
	})
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("unix", filepath.Join(home, ".bs", "bs.sock"), 100*time.Millisecond)
		if err != nil {
			return false
		}
		c.Close()
		return true
	}, 5*time.Second, 10*time.Millisecond)
	return h
}

func (h *harness) env() []string {
	return append(os.Environ(), "HOME="+h.home, "SHELL=/bin/sh", "ENV=/dev/null", "BASH_ENV=/dev/null", "HISTFILE=/dev/null", "PS1=bs$ ", "PS2=> ", "TERM=xterm-256color", "LC_ALL=C.UTF-8")
}

func (h *harness) cli(args ...string) string {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.bin, args...)
	cmd.Env = h.env()
	out, err := cmd.CombinedOutput()
	require.NoError(h.t, err, string(out))
	return string(out)
}

func (h *harness) stopDaemon() {
	h.t.Helper()
	if h.stopped {
		return
	}
	h.stopped = true
	require.NoError(h.t, h.daemon.Process.Signal(syscall.SIGTERM))
	done := make(chan error, 1)
	go func() { done <- h.daemon.Wait() }()
	select {
	case err := <-done:
		assert.NoError(h.t, err, h.log.String())
	case <-time.After(8 * time.Second):
		h.daemon.Process.Kill()
		<-done
		h.t.Error("daemon cleanup timed out")
	}
}

type terminalClient struct {
	t                  *testing.T
	cmd                *exec.Cmd
	pty                *os.File
	mu                 sync.Mutex
	screen             *ghostty.Terminal
	raw                bytes.Buffer
	readDone, waitDone chan struct{}
	waitErr            error
}

func (h *harness) client(cols, rows int, args ...string) *terminalClient {
	h.t.Helper()
	cmd := exec.Command(h.bin, args...)
	cmd.Env = h.env()
	screen, err := vt.New(cols, rows)
	require.NoError(h.t, err)
	pt, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	require.NoError(h.t, err)
	c := &terminalClient{t: h.t, cmd: cmd, pty: pt, screen: screen, readDone: make(chan struct{}), waitDone: make(chan struct{})}
	go func() {
		defer close(c.readDone)
		buf := make([]byte, 32768)
		for {
			n, err := pt.Read(buf)
			if n > 0 {
				c.mu.Lock()
				c.screen.VTWrite(buf[:n])
				c.raw.Write(buf[:n])
				c.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { c.waitErr = cmd.Wait(); close(c.waitDone) }()
	h.t.Cleanup(func() {
		select {
		case <-c.waitDone:
		default:
			cmd.Process.Kill()
			<-c.waitDone
		}
		pt.Close()
		<-c.readDone
		c.mu.Lock()
		c.screen.Close()
		c.mu.Unlock()
	})
	return c
}

func (c *terminalClient) write(s string) {
	c.t.Helper()
	_, err := io.WriteString(c.pty, s)
	require.NoError(c.t, err)
}

func (c *terminalClient) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	formatter, err := ghostty.NewFormatter(c.screen, ghostty.WithFormatterTrim(false))
	require.NoError(c.t, err)
	defer formatter.Close()
	text, err := formatter.FormatString()
	require.NoError(c.t, err)
	return text
}

func (c *terminalClient) await(s string) {
	c.t.Helper()
	if !assert.Eventually(c.t, func() bool { return strings.Contains(c.text(), s) }, 8*time.Second, 15*time.Millisecond) {
		text := c.text()
		c.mu.Lock()
		raw := c.raw.String()
		c.mu.Unlock()
		c.t.Fatalf("screen never contained %q; final screen: %q; raw: %q", s, text, raw)
	}
}

func (c *terminalClient) absent(s string) {
	c.t.Helper()
	assert.NotContains(c.t, c.text(), s)
}

func (c *terminalClient) exited() {
	c.t.Helper()
	select {
	case <-c.waitDone:
		c.mu.Lock()
		output := c.raw.String()
		c.mu.Unlock()
		require.NoError(c.t, c.waitErr, output)
	case <-time.After(5 * time.Second):
		c.t.Fatal("client failed to exit")
	}
}

func (c *terminalClient) command(command, marker string) {
	c.t.Helper()
	// The marker isn't present verbatim in the input echo, so observing it
	// proves the shell executed the command rather than merely echoing input.
	c.write(command + "\r")
	c.await(marker)
}

func TestE2E(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bs")
	args := []string{"build", "-o", bin}
	if raceEnabled {
		args = append(args, "-race")
	}
	build := exec.Command("go", append(args, "./cmd/bs")...)
	build.Dir = ".."
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))

	t.Run("commands-restoration-windows", func(t *testing.T) {
		h := newHarness(t, bin)
		c := h.client(80, 24, "-S", "work")
		c.await("bs$ ")
		c.command("printf 'first-%s\\n' marker", "first-marker")
		c.write("\x01c")
		c.await("bs$ ")
		c.command("printf 'second-%s\\n' marker", "second-marker")
		c.absent("first-marker")
		c.write("\x01p")
		c.await("first-marker")
		c.absent("second-marker")
		c.write("\x01n")
		c.await("second-marker")
		c.write("\x010")
		c.await("first-marker")
		c.write("\x01d")
		c.exited()
		assert.Contains(t, h.cli("-ls"), "detached")
		r := h.client(80, 24, "-r", "wor")
		r.await("first-marker")
		r.write("\x01\"")
		r.await("bs - Windows")
		r.write("\x1b[B\r")
		r.await("second-marker")
		r.write("\x01?")
		r.await("bs - Help")
		r.write("\x1b")
		r.await("second-marker")
	})

	t.Run("multi-attach-shared-input", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(80, 24, "-S", "shared")
		a.await("bs$ ")
		b := h.client(80, 24, "-x", "shared")
		b.await("bs$ ")
		a.command("printf 'from-a-%s\\n' ok", "from-a-ok")
		b.await("from-a-ok")
		b.command("printf 'from-b-%s\\n' ok", "from-b-ok")
		a.await("from-b-ok")
		a.write("\x01c")
		a.command("printf 'window-%s\\n' shared", "window-shared")
		b.await("window-shared")
		a.write("\x01d")
		a.exited()
		b.command("printf 'still-%s\\n' alive", "still-alive")
		assert.Contains(t, h.cli("-ls"), "1 attached")
	})

	t.Run("fitting-and-cropping", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(80, 24, "-S", "size")
		a.await("bs$ ")
		b := h.client(100, 30, "-x", "size")
		b.await("bs$ ")
		a.command("printf 'size:'; stty size", "size:24 80")
		b.write("\x01F")
		b.command("printf 'fit:'; stty size", "fit:30 100")
		a.await("fit:30 100")
		require.NoError(t, pty.Setsize(b.pty, &pty.Winsize{Cols: 90, Rows: 28}))
		b.mu.Lock()
		require.NoError(t, b.screen.Resize(90, 28, 0, 0))
		b.mu.Unlock()
		b.command("printf 'unchanged:'; stty size", "unchanged:30 100")
		b.write("\x01F")
		b.command("printf 'refit:'; stty size", "refit:28 90")
		require.NoError(t, pty.Setsize(b.pty, &pty.Winsize{Cols: 85, Rows: 26}))
		b.mu.Lock()
		require.NoError(t, b.screen.Resize(85, 26, 0, 0))
		b.mu.Unlock()
		b.write("\x01F")
		b.command("printf 'immediate-fit:'; stty size", "immediate-fit:26 85")
	})

	t.Run("session-switcher", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(90, 24, "-S", "work")
		a.await("bs$ ")
		a.command("printf 'work-%s\\n' token", "work-token")
		b := h.client(90, 24, "-x", "work")
		b.await("work-token")
		a.write("\x01c")
		a.command("printf 'extra-%s\\n' window", "extra-window")
		a.write("\x010")
		a.await("work-token")
		other := h.client(90, 24, "-S", "personal")
		other.await("bs$ ")
		other.command("printf 'personal-%s\\n' token", "personal-token")
		other.write("\x01d")
		other.exited()
		a.write("\x01s")
		a.await("bs - Switch session")
		a.await("2 attached *")
		a.await("2 windows")
		a.await("detached")
		a.write("\x1b[B\r")
		a.await("personal-token")
		b.absent("personal-token")
		b.command("printf 'work-still-%s\\n' active", "work-still-active")
		a.absent("work-still-active")
		a.write("\x01s")
		a.await("bs - Switch session")
		a.write("\x1b[B")
		a.write("\x1b")
		a.await("personal-token")
		a.write("\x01s")
		a.await("bs - Switch session")
		a.write("\r")
		a.await("work-still-active")
	})

	t.Run("scrollback", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(80, 12, "-S", "history")
		a.await("bs$ ")
		b := h.client(80, 12, "-x", "history")
		b.await("bs$ ")
		a.command("i=0; while [ \"$i\" -lt 60 ]; do printf 'line-%03d\\n' \"$i\"; i=$((i+1)); done", "line-059")
		a.absent("line-010")
		a.write("\x01\x1b\x1b[5~\x1b[5~\x1b[5~\x1b[5~")
		a.await("line-010")
		b.command("printf 'live-%s\\n' continues", "live-continues")
		a.absent("live-continues")
		a.write("\x1b[B")
		a.await("line-017")
		a.absent("line-005")
		a.write("\x1b[A")
		a.await("line-005")
		a.write("\x1b[6~")
		a.await("line-027")
		a.absent("line-010")
		a.write("\x1b")
		a.await("live-continues")
	})

	t.Run("alternate-screen-and-attributes", func(t *testing.T) {
		h := newHarness(t, bin)
		c := h.client(80, 24, "-S", "alt")
		c.await("bs$ ")
		c.command("printf '\\033[?1049h\\033[2J\\033[3;5H\\033[1;38;2;17;88;203mALT-界é\\033[0m\\033[7;9H'; sleep 60", "ALT-界é")
		c.write("\x01d")
		c.exited()
		r := h.client(80, 24, "-r", "alt")
		r.await("ALT-界é")
		r.mu.Lock()
		ref, err := r.screen.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: 4, Y: 2})
		require.NoError(t, err)
		style, err := ref.Style()
		require.NoError(t, err)
		assert.True(t, style.Bold())
		assert.Equal(t, ghostty.ColorRGB{R: 17, G: 88, B: 203}, style.FgColor().RGB)
		x, err := r.screen.CursorX()
		require.NoError(t, err)
		y, err := r.screen.CursorY()
		require.NoError(t, err)
		assert.Equal(t, uint16(8), x)
		assert.Equal(t, uint16(6), y)
		r.mu.Unlock()
		r.write("\x03")
		r.await("bs$ ")
		r.command("printf '\\033[?1049l'; printf 'primary-%s\\n' restored", "primary-restored")
	})

	t.Run("abrupt-client-death-and-forced-attach", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(80, 24, "-S", "durable")
		a.await("bs$ ")
		a.command("printf 'survival-%s\\n' marker", "survival-marker")
		require.NoError(t, a.cmd.Process.Kill())
		<-a.waitDone
		b := h.client(80, 24, "-d", "-r", "durable")
		b.await("survival-marker")
		c := h.client(80, 24, "-d", "-r", "durable")
		c.await("survival-marker")
		b.exited()
		c.command("printf 'after-force-%s\\n' ok", "after-force-ok")
	})

	t.Run("socket-and-process-cleanup", func(t *testing.T) {
		h := newHarness(t, bin)
		a := h.client(80, 24, "-S", "cleanup")
		a.await("bs$ ")
		a.command("printf 'shell-%s:%s\\n' pid $$", "shell-pid:")
		text := a.text()
		var pid int
		i := strings.Index(text, "shell-pid:")
		_, err := fmt.Sscanf(text[i:], "shell-pid:%d", &pid)
		require.NoError(t, err)
		require.Positive(t, pid)
		for path, mode := range map[string]os.FileMode{filepath.Join(h.home, ".bs"): 0700, filepath.Join(h.home, ".bs", "bs.sock"): 0600} {
			info, err := os.Lstat(path)
			require.NoError(t, err)
			assert.Equal(t, mode, info.Mode().Perm())
		}
		a.write("exit\r")
		a.exited()
		require.Eventually(t, func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH }, 3*time.Second, 10*time.Millisecond)
		require.Eventually(t, func() bool { return strings.Contains(h.cli("-ls"), "No sessions.") }, 3*time.Second, 20*time.Millisecond)
	})
	runRobustness(t, bin)
	runStartup(t, bin)
	runWindowLifecycle(t, bin)
}
