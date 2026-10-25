package mux

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/jrgp/bs/internal/vt"
	"github.com/muesli/cancelreader"
	"golang.org/x/term"
)

type client struct {
	conn       *net.UnixConn
	out        io.Writer
	tty        *os.File
	cols, rows int
	current    response
	previous   *vt.Frame
	prefix     bool
	mode       string
	choice     int
	sessions   []sessionInfo
	keys       string
	status     string
}

func (c *client) send(r request) error {
	r.Version = protocolVersion
	c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	return send(c.conn, r)
}

func dimensions(in *os.File) (int, int, error) {
	cols, rows, err := term.GetSize(int(in.Fd()))
	if err != nil {
		return 0, 0, fmt.Errorf("an interactive terminal is required: %w", err)
	}
	if !validSize(cols, rows) {
		return 0, 0, fmt.Errorf("terminal size must be 2..500 columns by 2..200 rows (got %dx%d)", cols, rows)
	}
	return cols, rows, nil
}

func attach(conn *net.UnixConn, in, out *os.File, r request) error {
	cols, rows, err := dimensions(in)
	if err != nil {
		return err
	}
	c := &client{conn: conn, out: out, tty: in, cols: cols, rows: rows}
	r.Cols, r.Rows = cols, rows
	if err := c.send(r); err != nil {
		return err
	}
	var initial response
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if err := receive(conn, &initial, maxMessage); err != nil {
		return err
	}
	conn.SetReadDeadline(time.Time{})
	if initial.Version != protocolVersion {
		return fmt.Errorf("incompatible daemon protocol")
	}
	if initial.Error != "" {
		return errors.New(initial.Error)
	}
	if initial.Frame == nil {
		return fmt.Errorf("daemon did not send terminal state")
	}
	c.current = initial
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(in.Fd()), state)
	if _, err := io.WriteString(out, "\x1b[?2027s\x1b[?1049h"); err != nil {
		return err
	}
	defer io.WriteString(out, "\x1b[0m\x1b[?1l\x1b>\x1b[?5l\x1b[?7h\x1b[?9l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1004l\x1b[?1005l\x1b[?1006l\x1b[?1015l\x1b[?2004l\x1b[?25h\x1b[0 q\x1b[?1049l\x1b[?2027r")
	reader, err := cancelreader.NewReader(in)
	if err != nil {
		return err
	}
	done := make(chan struct{})
	readDone := make(chan struct{})
	defer func() {
		close(done)
		reader.Cancel()
		<-readDone
		reader.Close()
	}()
	input := make(chan []byte, 1)
	failure := make(chan error, 2)
	incoming := make(chan response, 4)
	go func() {
		defer close(readDone)
		buf := make([]byte, 4096)
		for {
			n, err := reader.Read(buf)
			if err != nil {
				select {
				case failure <- err:
				case <-done:
				}
				return
			}
			if n > 0 {
				select {
				case input <- append([]byte(nil), buf[:n]...):
				case <-done:
					return
				}
			}
		}
	}()
	go func() {
		for {
			var r response
			if err := receive(conn, &r, maxMessage); err != nil {
				select {
				case failure <- err:
				case <-done:
				}
				return
			}
			select {
			case incoming <- r:
			case <-done:
				return
			}
			if r.Done {
				return
			}
		}
	}()
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGWINCH, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	var escapeAt, statusAt time.Time
	if err := c.draw(); err != nil {
		return err
	}
	for {
		select {
		case data := <-input:
			finished, err := c.input(data)
			if err != nil {
				return err
			}
			if finished {
				return nil
			}
			if c.keys == "\x1b" || c.mode == "prefix" {
				escapeAt = time.Now()
			}
			if err := c.draw(); err != nil {
				return err
			}
		case r := <-incoming:
			if r.Version != protocolVersion {
				return fmt.Errorf("incompatible daemon protocol")
			}
			if r.Error != "" {
				if r.Done {
					return errors.New(r.Error)
				}
				c.status = r.Error
				statusAt = time.Now()
			}
			if r.Done {
				return nil
			}
			if r.Frame != nil {
				c.current = r
			}
			if r.Sessions != nil {
				c.sessions = r.Sessions
			}
			if r.Windows != nil {
				c.current.Windows = r.Windows
			}
			if err := c.draw(); err != nil {
				return err
			}
		case sig := <-signals:
			if sig != syscall.SIGWINCH {
				return nil
			}
			cols, rows, err := dimensions(in)
			if err != nil {
				c.status = err.Error()
				statusAt = time.Now()
				continue
			}
			c.cols, c.rows, c.previous = cols, rows, nil
			if err := c.send(request{Op: "size", Cols: cols, Rows: rows}); err != nil {
				return err
			}
			if err := c.draw(); err != nil {
				return err
			}
		case err := <-failure:
			return err
		case <-tick.C:
			changed := false
			if (c.keys == "\x1b" || c.mode == "prefix") && time.Since(escapeAt) > 100*time.Millisecond {
				op := "live"
				if c.mode == "prefix" {
					c.mode, op = "scroll", "scroll"
				} else {
					c.mode = ""
				}
				c.keys, c.previous = "", nil
				if err := c.send(request{Op: op}); err != nil {
					return err
				}
				changed = true
			}
			if c.status != "" && !statusAt.IsZero() && time.Since(statusAt) > 3*time.Second {
				c.status, c.previous = "", nil
				changed = true
			}
			if changed {
				if err := c.draw(); err != nil {
					return err
				}
			}
		}
	}
}

func (c *client) input(data []byte) (bool, error) {
	var plain []byte
	flush := func() error {
		if len(plain) == 0 {
			return nil
		}
		err := c.send(request{Op: "input", Data: plain})
		plain = nil
		return err
	}
	for _, b := range data {
		if c.mode == "prefix" {
			c.keys += string(b)
			if c.keys == "\x1b[" {
				continue
			}
			key := c.keys
			c.mode, c.keys = "", ""
			if key == "\x1b[Z" {
				if err := c.send(request{Op: "pane-prev"}); err != nil {
					return false, err
				}
			} else {
				c.mode, c.previous = "scroll", nil
				if err := c.send(request{Op: "scroll"}); err != nil {
					return false, err
				}
				for _, b := range []byte(key[1:]) {
					if err := c.menuKey(b); err != nil {
						return false, err
					}
				}
			}
			continue
		}
		if c.mode != "" {
			if err := flush(); err != nil {
				return false, err
			}
			if err := c.menuKey(b); err != nil {
				return false, err
			}
			continue
		}
		if c.prefix {
			c.prefix = false
			if err := flush(); err != nil {
				return false, err
			}
			op := ""
			switch b {
			case 'a', 1:
				plain = append(plain, 1)
			case 'c':
				op = "new"
			case 'k':
				op = "kill"
			case 'K':
				op = "pane-kill"
			case '|':
				op = "split-v"
			case 'S':
				op = "split-h"
			case '\t':
				op = "pane-next"
			case 'n', 14:
				op = "next"
			case 'p', 16:
				op = "prev"
			case 'd':
				return true, c.send(request{Op: "detach"})
			case 'F':
				cols, rows, err := dimensions(c.tty)
				if err != nil {
					return false, err
				}
				c.cols, c.rows = cols, rows
				if err := c.send(request{Op: "fit", Cols: cols, Rows: rows}); err != nil {
					return false, err
				}
			case 's', '"', '?':
				c.mode = map[byte]string{'s': "sessions", '"': "windows", '?': "help"}[b]
				c.choice, c.previous = 0, nil
				op = "catalog"
			case 27:
				c.mode, c.keys = "prefix", "\x1b"
			default:
				if b >= '0' && b <= '9' {
					if err := c.send(request{Op: "select", Number: int(b - '0')}); err != nil {
						return false, err
					}
				}
			}
			if op != "" {
				if err := c.send(request{Op: op}); err != nil {
					return false, err
				}
			}
		} else if b == 1 {
			if err := flush(); err != nil {
				return false, err
			}
			c.prefix = true
		} else {
			plain = append(plain, b)
		}
	}
	return false, flush()
}

func (c *client) menuKey(b byte) error {
	c.keys += string([]byte{b})
	key := c.keys
	if key == "\x1b" || key == "\x1b[" || key == "\x1bO" {
		return nil
	}
	c.keys = ""
	if key == "\x1b[A" || key == "\x1bOA" || key == "k" {
		if c.mode == "scroll" {
			return c.send(request{Op: "scroll", Number: 1})
		}
		c.choice = max(0, c.choice-1)
	} else if key == "\x1b[B" || key == "\x1bOB" || key == "j" {
		if c.mode == "scroll" {
			return c.send(request{Op: "scroll", Number: -1})
		}
		c.choice = min(max(0, c.menuCount()-1), c.choice+1)
	} else if key == "\x1b[5" || key == "\x1b[6" {
		c.keys = key
		return nil
	} else if key == "\x1b[5~" && c.mode == "scroll" {
		return c.send(request{Op: "scroll", Number: max(1, c.rows-1)})
	} else if key == "\x1b[6~" && c.mode == "scroll" {
		return c.send(request{Op: "scroll", Number: -max(1, c.rows-1)})
	} else if key == "\r" || key == "\n" {
		var err error
		switch c.mode {
		case "sessions":
			if c.choice < len(c.sessions) {
				err = c.send(request{Op: "switch", Name: c.sessions[c.choice].Name, Cols: c.cols, Rows: c.rows})
			}
		case "windows":
			if c.choice < len(c.current.Windows) {
				err = c.send(request{Op: "select", Number: c.current.Windows[c.choice].Number})
			}
		}
		c.mode, c.previous = "", nil
		return err
	} else if strings.HasPrefix(key, "\x1b") || key == "q" {
		c.mode, c.previous = "", nil
		return c.send(request{Op: "live"})
	}
	return nil
}

func (c *client) menuCount() int {
	if c.mode == "sessions" {
		return len(c.sessions)
	}
	return len(c.current.Windows)
}

func cleanText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(s))
}

func (c *client) draw() error {
	var f vt.Frame
	if c.current.Frame == nil {
		return nil
	}
	f = *c.current.Frame
	if c.mode == "sessions" || c.mode == "windows" || c.mode == "help" {
		f = vt.Frame{Lines: make([]string, c.rows), CursorStyle: 2}
		lines := []string{"bs - Switch session"}
		if c.mode == "sessions" {
			for i, info := range c.sessions {
				prefix := "  "
				if i == c.choice {
					prefix = "> "
				}
				current := ""
				if info.Name == c.current.Session {
					current = " *"
				}
				state := "detached"
				if info.Attached > 0 {
					state = fmt.Sprintf("%d attached", info.Attached)
				}
				lines = append(lines, fmt.Sprintf("%s%-20s %d windows  %s%s", prefix, info.Name, info.Windows, state, current))
			}
		} else if c.mode == "windows" {
			lines[0] = "bs - Windows"
			for i, w := range c.current.Windows {
				prefix := "  "
				if i == c.choice {
					prefix = "> "
				}
				current := ""
				if w.Number == c.current.Window {
					current = " *"
				}
				lines = append(lines, fmt.Sprintf("%s%d  %s%s", prefix, w.Number, cleanText(w.Title), current))
			}
		} else {
			lines = []string{"bs - Help", "Ctrl-A followed by:", "c  create window     n/p  next/previous", "0-9  select window   \"  window list", "k  kill window       K  kill focused pane", "|  left/right split  S  top/bottom split", "Tab / Shift-Tab  next/previous pane", "s  switch session    Esc  scrollback", "F  fit to this client   d  detach", "a  literal Ctrl-A    ?  help"}
		}
		// Scroll menus within the available rows, keeping the selection visible.
		start := max(0, c.choice-max(0, c.rows-4))
		f.Lines[0] = lines[0]
		for y := 1; y < c.rows-1 && start+y < len(lines); y++ {
			f.Lines[y] = ansi.Truncate(lines[start+y], c.cols, "")
		}
		f.Lines[c.rows-1] = ansi.Truncate("Up/Down navigate  Enter switch  Esc cancel   * current", c.cols, "")
	} else if c.mode == "scroll" {
		f.Visible = false
		f.Modes = nil
		f.Keypad = false
	}
	if c.status != "" {
		f.Lines = append([]string(nil), f.Lines...)
		if len(f.Lines) > 0 {
			f.Lines[len(f.Lines)-1] = ansi.Truncate("[bs: "+cleanText(c.status)+"]", c.cols, "")
		}
	}
	f.Lines = f.Lines[:min(len(f.Lines), c.rows)]
	if c.previous != nil && len(c.previous.Lines) > c.rows {
		c.previous = nil
	}
	output := vt.Draw(c.previous, f)
	if _, err := io.WriteString(c.out, output); err != nil {
		return err
	}
	c.previous = &f
	return nil
}
