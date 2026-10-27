package mux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/jrgp/bs/internal/vt"
	"github.com/shirou/gopsutil/v4/process"
	ghostty "go.mitchellh.com/libghostty"
	"golang.org/x/sys/unix"
)

type pane struct {
	term        *ghostty.Terminal
	pty         *os.File
	cmd         *exec.Cmd
	input       chan []byte
	done        chan struct{}
	wait        chan struct{}
	responseErr error
	title       string
	bounds      rectangle
}

type window struct {
	root      *layout
	focused   *pane
	borders   [][]byte
	broadcast bool
}

type session struct {
	name       string
	dir        string
	windows    []*window
	selected   int
	cols, rows int
	dirty      bool
}

type peer struct {
	conn       *net.UnixConn
	out        chan response
	frames     chan response
	done       chan struct{}
	session    *session
	cols, rows int
	offset     int
	force      bool
	anchor     *ghostty.TrackedGridRef
	closing    bool
}

type event struct {
	peer   *peer
	req    *request
	pane   *pane
	data   []byte
	exited bool
	err    error
}

// Server has one state-owning event loop. Socket and PTY workers only perform
// bounded I/O and send events; no native state is accessed by those workers.
type Server struct {
	home, shell string
	log         *log.Logger
	config      Config
	events      chan event
	done        chan struct{}
	sessions    []*session
	peers       map[*peer]bool
}

func NewServer(home, shell string, logger *log.Logger, config Config) *Server {
	return &Server{home: home, shell: shell, log: logger, config: config, events: make(chan event, 256), done: make(chan struct{}), peers: make(map[*peer]bool)}
}

func (s *Server) publish(e event) bool {
	select {
	case s.events <- e:
		return true
	case <-s.done:
		return false
	}
}

func (s *Server) Run(ctx context.Context) error {
	dir, err := runtimeDir(s.home)
	if err != nil {
		return err
	}
	lock, err := secureFile(filepath.Join(dir, "daemon.lock"), unix.O_CREAT|unix.O_RDWR)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil
		}
		return err
	}
	path := filepath.Join(dir, "bs.sock")
	if old, err := checkSocket(path); err == nil {
		if c, err := dial(dir); err == nil {
			c.Close()
			return fmt.Errorf("another daemon is listening")
		} else if !errors.Is(err, syscall.ECONNREFUSED) {
			return err
		}
		current, err := os.Lstat(path)
		if err != nil || !os.SameFile(old, current) {
			return fmt.Errorf("socket changed during stale cleanup")
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Publish a 0600 socket from its first instant of existence. Restore the
	// inherited mask before starting workers or child processes.
	mask := unix.Umask(0177)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	unix.Umask(mask)
	if err != nil {
		return err
	}
	listener.SetUnlinkOnClose(false)
	defer listener.Close()
	socketInfo, err := checkSocket(path)
	if err != nil {
		return err
	}
	defer func() {
		if current, err := os.Lstat(path); err == nil && os.SameFile(socketInfo, current) {
			os.Remove(path)
		}
	}()
	defer func() {
		close(s.done)
		for p := range s.peers {
			s.drop(p)
		}
		for _, session := range s.sessions {
			for _, w := range session.windows {
				for _, p := range w.root.panes() {
					s.stopPane(p)
				}
			}
		}
	}()
	go func() {
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				s.publish(event{err: err})
				return
			}
			if err := checkPeer(conn); err != nil {
				s.log.Print(err)
				conn.Close()
				continue
			}
			if !s.publish(event{peer: &peer{conn: conn, out: make(chan response, 8), frames: make(chan response, 1), done: make(chan struct{})}}) {
				conn.Close()
				return
			}
		}
	}()
	tick := time.NewTicker(33 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case e := <-s.events:
			if e.peer != nil {
				if e.req != nil {
					if s.peers[e.peer] && !e.peer.closing {
						s.handle(e.peer, *e.req)
					}
				} else if e.err != nil {
					s.drop(e.peer)
				} else if len(s.peers) >= maxClients {
					e.peer.conn.Close()
				} else {
					s.peers[e.peer] = true
					go s.readPeer(e.peer)
					go s.writePeer(e.peer)
				}
			} else if e.pane != nil {
				s.paneEvent(e)
			} else if e.err != nil {
				return e.err
			}
			// Let closing peers flush their final responses before shutting down.
			if len(s.sessions) == 0 && len(s.peers) == 0 {
				return nil
			}
		case <-tick.C:
			s.render()
		}
	}
}

func (s *Server) readPeer(p *peer) {
	p.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		var r request
		err := receive(p.conn, &r, maxRequest)
		if err != nil {
			s.publish(event{peer: p, err: err})
			return
		}
		p.conn.SetReadDeadline(time.Time{})
		if !s.publish(event{peer: p, req: &r}) {
			return
		}
	}
}

func (s *Server) writePeer(p *peer) {
	for {
		var r response
		select {
		case r = <-p.out:
		default:
			select {
			case r = <-p.out:
			case r = <-p.frames:
			case <-p.done:
				return
			case <-s.done:
				return
			}
		}
		p.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		err := send(p.conn, r)
		if err != nil || r.Done {
			p.conn.Close()
			if err == nil {
				err = io.EOF
			}
			s.publish(event{peer: p, err: err})
			return
		}
	}
}

func (s *Server) reply(p *peer, r response) {
	r.Version = protocolVersion
	if r.Done {
		p.closing = true
		p.clearScroll()
		p.session = nil
	}
	if r.Frame != nil {
		// Frames are complete snapshots, so a pending one may be replaced.
		select {
		case p.frames <- r:
			return
		default:
		}
		select {
		case <-p.frames:
		default:
		}
		select {
		case p.frames <- r:
		default:
		}
		return
	}
	select {
	case p.out <- r:
	default:
		// A slow client is disconnected instead of blocking state ownership.
		s.drop(p)
	}
}

func (s *Server) drop(p *peer) {
	if s.peers[p] {
		close(p.done)
		p.clearScroll()
	}
	delete(s.peers, p)
	p.conn.Close()
}

func (s *Server) fail(p *peer, err error) {
	s.reply(p, response{Error: err.Error(), Done: p.session == nil})
}

func match(sessions []*session, name string) (*session, error) {
	for _, s := range sessions {
		if s.name == name {
			return s, nil
		}
	}
	var found *session
	for _, s := range sessions {
		if strings.HasPrefix(s.name, name) {
			if found != nil {
				return nil, fmt.Errorf("ambiguous session %q", name)
			}
			found = s
		}
	}
	if found == nil {
		return nil, fmt.Errorf("no session matching %q", name)
	}
	return found, nil
}

func validName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

func (s *Server) infos() []sessionInfo {
	infos := make([]sessionInfo, 0, len(s.sessions))
	for _, session := range s.sessions {
		info := sessionInfo{Name: session.name, Windows: len(session.windows)}
		for p := range s.peers {
			if p.session == session {
				info.Attached++
			}
		}
		infos = append(infos, info)
	}
	return infos
}

func (s *Server) handle(p *peer, r request) {
	if r.Version != protocolVersion {
		s.reply(p, response{Error: "incompatible protocol version", Done: true})
		return
	}
	switch r.Op {
	case "list":
		s.reply(p, response{Sessions: s.infos(), Done: true})
	case "catalog":
		if p.session == nil {
			s.fail(p, fmt.Errorf("not attached"))
			return
		}
		s.reply(p, response{Sessions: s.infos(), Windows: s.windowInfos(p.session), Session: p.session.name})
	case "create", "attach", "switch":
		if r.Op != "switch" && p.session != nil {
			s.fail(p, fmt.Errorf("already attached"))
			return
		}
		if r.Op == "switch" && p.session == nil {
			s.fail(p, fmt.Errorf("not attached"))
			return
		}
		if !validSize(r.Cols, r.Rows) {
			s.fail(p, fmt.Errorf("terminal size must be 2..500 columns by 2..200 rows"))
			return
		}
		var target *session
		var err error
		if r.Op == "create" {
			if len(s.sessions) >= maxSessions {
				s.fail(p, fmt.Errorf("session limit reached"))
				return
			}
			if r.Name == "" {
				for n := 1; ; n++ {
					name := fmt.Sprintf("%d", n)
					if _, e := match(s.sessions, name); e != nil {
						r.Name = name
						break
					}
				}
			}
			if !validName(r.Name) {
				s.fail(p, fmt.Errorf("session names must use 1..64 ASCII letters, digits, '.', '-' or '_'"))
				return
			}
			for _, existing := range s.sessions {
				if existing.name == r.Name {
					s.fail(p, fmt.Errorf("session %q already exists", r.Name))
					return
				}
			}
			dir := r.Dir
			if dir == "" {
				dir = s.home
			}
			if !filepath.IsAbs(dir) {
				s.fail(p, fmt.Errorf("session directory must be an absolute path"))
				return
			}
			target = &session{name: r.Name, dir: dir, cols: r.Cols, rows: r.Rows, dirty: true}
			if err = s.newWindow(target, r.Commands...); err == nil {
				s.sessions = append(s.sessions, target)
			}
		} else {
			target, err = match(s.sessions, r.Name)
		}
		if err != nil {
			s.fail(p, err)
			return
		}
		if r.Op == "attach" && !r.Multi && !r.Force {
			for other := range s.peers {
				if other != p && other.session == target {
					s.fail(p, fmt.Errorf("session %q is attached (use -x or -d -r)", target.name))
					return
				}
			}
		}
		if r.Force {
			for other := range s.peers {
				if other != p && other.session == target {
					s.reply(other, response{Done: true})
				}
			}
		}
		p.clearScroll()
		p.session, p.cols, p.rows, p.force = target, r.Cols, r.Rows, true
	case "detach":
		s.reply(p, response{Done: true})
	case "size":
		if !validSize(r.Cols, r.Rows) {
			s.fail(p, fmt.Errorf("terminal size out of range"))
			return
		}
		p.cols, p.rows, p.force = r.Cols, r.Rows, true
	default:
		if p.session == nil {
			s.fail(p, fmt.Errorf("not attached"))
			return
		}
		session := p.session
		w := session.windows[session.selected]
		focused := w.focused
		switch r.Op {
		case "broadcast":
			w.broadcast = !w.broadcast
			session.dirty = true
		case "reload":
			if err := s.reloadConfig(); err != nil {
				s.fail(p, fmt.Errorf("reload configuration: %w", err))
			} else {
				s.render()
			}
		case "input":
			if len(r.Data) > 32768 {
				s.fail(p, fmt.Errorf("input exceeds 32768 bytes"))
				return
			}
			targets := []*pane{focused}
			if w.broadcast {
				targets = w.root.panes()
			}
			for _, target := range targets {
				if !s.queueInput(target, r.Data) {
					s.fail(p, fmt.Errorf("PTY input buffer full"))
					s.drop(p)
					return
				}
			}
		case "new":
			if err := s.newWindow(session); err != nil {
				s.fail(p, err)
				return
			}
			s.resetViews(session)
		case "kill":
			s.removeWindow(session, session.selected)
		case "pane-kill":
			s.paneEvent(event{pane: focused, exited: true})
		case "split-v", "split-h":
			if err := s.split(session, w, r.Op == "split-v"); err != nil {
				s.fail(p, err)
			}
		case "pane-next", "pane-prev":
			panes := w.root.panes()
			i := slices.Index(panes, focused)
			step := 1
			if r.Op == "pane-prev" {
				step = -1
			}
			w.focused = panes[(i+len(panes)+step)%len(panes)]
			s.resetViews(session)
			if err := s.resizeWindow(session, w); err != nil {
				s.fail(p, err)
			}
		case "next", "prev", "select":
			switch r.Op {
			case "next":
				session.selected = (session.selected + 1) % len(session.windows)
			case "prev":
				session.selected = (session.selected + len(session.windows) - 1) % len(session.windows)
			case "select":
				if r.Number < 0 || r.Number >= len(session.windows) {
					s.fail(p, fmt.Errorf("no window %d", r.Number))
					return
				}
				session.selected = r.Number
			}
			s.resetViews(session)
		case "fit":
			if !validSize(r.Cols, r.Rows) {
				s.fail(p, fmt.Errorf("terminal size out of range"))
				return
			}
			p.cols, p.rows = r.Cols, r.Rows
			session.cols, session.rows = p.cols, p.rows
			s.resetViews(session)
			for _, win := range session.windows {
				if err := s.resizeWindow(session, win); err != nil {
					s.fail(p, err)
					return
				}
			}
		case "scroll":
			if err := p.resolveScroll(focused.term); err != nil {
				s.fail(p, err)
				return
			}
			history, err := focused.term.ScrollbackRows()
			if err != nil {
				s.fail(p, err)
				return
			}
			offset := min(max(p.offset+r.Number, 0), int(history))
			p.clearScroll()
			p.offset = offset
			if offset > 0 {
				p.anchor, err = focused.term.TrackGridRef(ghostty.Point{Tag: ghostty.PointTagScreen, Y: uint32(int(history) - offset)})
				if err != nil {
					s.fail(p, err)
					return
				}
			}
			p.force = true
		case "live":
			p.clearScroll()
			p.force = true
		default:
			s.fail(p, fmt.Errorf("unknown operation %q", r.Op))
		}
	}
}

func (s *Server) reloadConfig() error {
	config, err := loadConfig(s.home)
	if err != nil {
		return err
	}
	for _, session := range s.sessions {
		if _, err := config.statusLine(s.windowInfos(session), session.name, session.cols); err != nil {
			return fmt.Errorf("session %s: %w", session.name, err)
		}
	}
	previous := s.config
	s.config = config
	if config.StatusLine != previous.StatusLine {
		resize := func() error {
			var result error
			for _, session := range s.sessions {
				for i, w := range session.windows {
					if err := s.resizeWindow(session, w); err != nil {
						result = errors.Join(result, fmt.Errorf("session %s window %d: %w", session.name, i, err))
					}
				}
				s.resetViews(session)
			}
			return result
		}
		if err := resize(); err != nil {
			s.config = previous
			if rollbackErr := resize(); rollbackErr != nil {
				return errors.Join(err, fmt.Errorf("restoring previous layout: %w", rollbackErr))
			}
			return err
		}
	}
	for _, session := range s.sessions {
		session.dirty = true
	}
	return nil
}

func (s *Server) resetViews(session *session) {
	session.dirty = true
	for p := range s.peers {
		if p.session == session {
			p.clearScroll()
			p.force = true
		}
	}
}

func (p *peer) clearScroll() {
	if p.anchor != nil {
		p.anchor.Close()
		p.anchor = nil
	}
	p.offset = 0
}

func (p *peer) resolveScroll(t *ghostty.Terminal) error {
	if p.anchor == nil {
		return nil
	}
	history, err := t.ScrollbackRows()
	if err != nil {
		return err
	}
	if !p.anchor.HasValue() {
		// The old top row was evicted. Continue at the oldest retained row.
		p.offset = int(history)
		if history == 0 {
			p.clearScroll()
			return nil
		}
		return p.anchor.Set(t, ghostty.Point{Tag: ghostty.PointTagScreen})
	}
	point, err := p.anchor.Point(ghostty.PointTagScreen)
	if err != nil {
		return err
	}
	p.offset = max(0, int(history)-int(point.Y))
	return nil
}

func (s *Server) windowInfos(session *session) []windowInfo {
	result := make([]windowInfo, 0, len(session.windows))
	for number, w := range session.windows {
		result = append(result, windowInfo{Number: number, Title: cleanText(w.focused.title), Active: number == session.selected, Broadcast: w.broadcast})
	}
	return result
}

const accent = "\x1b[38;2;135;206;250m"

func (s *Server) contentRows(rows int) int {
	if s.config.StatusLine {
		return rows - 1
	}
	return rows
}

func validatePaneCommands(commands []string) error {
	if len(commands) > maxPanes {
		return fmt.Errorf("pane limit is %d commands", maxPanes)
	}
	for i, command := range commands {
		if strings.TrimSpace(command) == "" || strings.ContainsRune(command, 0) {
			return fmt.Errorf("pane command %d must be nonempty and contain no NUL bytes", i+1)
		}
	}
	return nil
}

func (s *Server) newWindow(session *session, commands ...string) error {
	if err := validatePaneCommands(commands); err != nil {
		return err
	}
	if len(commands) == 0 {
		commands = []string{""}
	}
	total, panes := 0, 0
	for _, s := range s.sessions {
		total += len(s.windows)
		for _, w := range s.windows {
			panes += len(w.root.panes())
		}
	}
	if len(session.windows) >= maxWindows || total >= maxTotalWindows {
		return fmt.Errorf("window limit reached")
	}
	if panes+len(commands) > maxTotalPanes {
		return fmt.Errorf("pane limit reached")
	}
	cols, rows := session.cols, s.contentRows(session.rows)
	bestRows, bestScore := 0, 0
	for r := 1; r <= len(commands); r++ {
		c := (len(commands) + r - 1) / r
		if 3*c-1 > cols || 2*r-1 > rows {
			continue
		}
		score := (cols-c+1)/c - 2*((rows-r+1)/r)
		if score < 0 {
			score = -score
		}
		if bestRows == 0 || score < bestScore {
			bestRows, bestScore = r, score
		}
	}
	if bestRows == 0 {
		return fmt.Errorf("terminal too small for %d panes", len(commands))
	}
	w := &window{root: commandLayout(len(commands), bestRows)}
	slots := w.root.panes()
	w.focused = slots[0]
	w.place(cols, rows)
	var started []*pane
	for i, slot := range slots {
		p, err := s.newPane(session, slot.bounds.cols, slot.bounds.rows, commands[i])
		if err != nil {
			for _, p := range started {
				s.stopPane(p)
			}
			return err
		}
		p.bounds = slot.bounds
		w.root.leaf(slot).pane = p
		started = append(started, p)
	}
	w.focused = started[0]
	session.windows = append(session.windows, w)
	session.selected = len(session.windows) - 1
	session.dirty = true
	return nil
}

func (s *Server) newPane(session *session, cols, rows int, command string) (*pane, error) {
	total := 0
	for _, session := range s.sessions {
		for _, w := range session.windows {
			total += len(w.root.panes())
		}
	}
	if total >= maxTotalPanes {
		return nil, fmt.Errorf("pane limit reached")
	}
	dir := ""
	for _, candidate := range []string{session.dir, s.home} {
		info, err := os.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("access session directory %q: %w", candidate, err)
		}
		if info.IsDir() {
			dir = candidate
			break
		}
	}
	w := &pane{input: make(chan []byte, 64), done: make(chan struct{}), wait: make(chan struct{}), title: filepath.Base(s.shell)}
	term, err := vt.New(cols, rows, ghostty.WithWritePty(func(_ *ghostty.Terminal, data []byte) {
		if !s.queueInput(w, append([]byte(nil), data...)) {
			w.responseErr = fmt.Errorf("terminal response buffer full")
		}
	}))
	if err != nil {
		return nil, err
	}
	w.term = term
	cmd := exec.Command(s.shell)
	if command != "" {
		cmd.Args = append(cmd.Args, "-c", command)
	}
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor", "BS_SESSION="+session.name)
	w.cmd = cmd
	w.pty, err = pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		term.Close()
		return nil, err
	}
	go func() {
		buffer := make([]byte, 32768)
		for {
			n, err := w.pty.Read(buffer)
			if n > 0 && !s.publish(event{pane: w, data: append([]byte(nil), buffer[:n]...)}) {
				return
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		for {
			select {
			case data := <-w.input:
				if _, err := w.pty.Write(data); err != nil {
					s.publish(event{pane: w, exited: true, err: err})
					return
				}
			case <-w.done:
				return
			}
		}
	}()
	go func() {
		err := cmd.Wait()
		close(w.wait)
		s.publish(event{pane: w, exited: true, err: err})
	}()
	return w, nil
}

func (s *Server) queueInput(w *pane, data []byte) bool {
	select {
	case w.input <- data:
		return true
	default:
		return false
	}
}

func (s *Server) stopPane(w *pane) {
	close(w.done)
	// A PTY's jobs share its Unix session, not necessarily its process group.
	// Enumerate through gopsutil instead of maintaining a platform-specific
	// process-table parser. Kill jobs before the shell so it can reap them.
	pids, err := process.Pids()
	if err != nil {
		s.log.Printf("enumerating window jobs: %v", err)
	}
	jobs := make([]int, 0)
	groups := make(map[int]bool)
	for _, pid := range pids {
		sid, err := unix.Getsid(int(pid))
		if err != nil || sid != w.cmd.Process.Pid || int(pid) == w.cmd.Process.Pid {
			continue
		}
		jobs = append(jobs, int(pid))
		group, err := unix.Getpgid(int(pid))
		if err == nil && group != w.cmd.Process.Pid {
			groups[group] = true
		}
	}
	for group := range groups {
		unix.Kill(-group, unix.SIGKILL)
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for len(jobs) > 0 && time.Now().Before(deadline) {
		jobs = slices.DeleteFunc(jobs, func(pid int) bool { return unix.Kill(pid, 0) == unix.ESRCH })
		if len(jobs) > 0 {
			time.Sleep(5 * time.Millisecond)
		}
	}
	unix.Kill(-w.cmd.Process.Pid, unix.SIGHUP)
	w.pty.Close()
	select {
	case <-w.wait:
	case <-time.After(500 * time.Millisecond):
		unix.Kill(-w.cmd.Process.Pid, unix.SIGKILL)
		<-w.wait
	}
	for _, pid := range jobs {
		if sid, err := unix.Getsid(pid); err == nil && sid == w.cmd.Process.Pid {
			unix.Kill(pid, unix.SIGKILL)
		}
	}
	w.term.Close()
}

func (s *Server) paneEvent(e event) {
	for _, session := range s.sessions {
		for wi, w := range session.windows {
			panes := w.root.panes()
			pi := slices.Index(panes, e.pane)
			if pi < 0 {
				continue
			}
			if e.exited {
				if e.err != nil {
					s.log.Printf("session %s window %d pane %d: %v", session.name, wi, pi, e.err)
				}
				if len(panes) == 1 {
					s.removeWindow(session, wi)
				} else {
					s.resetViews(session)
					s.stopPane(e.pane)
					w.root = w.root.remove(e.pane)
					if w.focused == e.pane {
						remaining := w.root.panes()
						w.focused = remaining[min(pi, len(remaining)-1)]
					}
					if err := s.resizeWindow(session, w); err != nil {
						s.log.Printf("reflowing panes: %v", err)
						for p := range s.peers {
							if p.session == session {
								s.fail(p, err)
							}
						}
					}
				}
			} else {
				e.pane.term.VTWrite(e.data)
				if e.pane.responseErr != nil {
					s.paneEvent(event{pane: e.pane, exited: true, err: e.pane.responseErr})
					return
				}
				title, err := e.pane.term.Title()
				if err != nil {
					s.log.Print(err)
				} else {
					e.pane.title = title
					if cleanText(title) == "" {
						e.pane.title = filepath.Base(s.shell)
					}
				}
				session.dirty = true
			}
			return
		}
	}
}

func (s *Server) render() {
	for p := range s.peers {
		if p.session == nil || (!p.force && !p.session.dirty) {
			continue
		}
		session := p.session
		frame, err := s.capture(p)
		if err != nil {
			s.fail(p, err)
			continue
		}
		s.reply(p, response{Frame: &frame, Session: session.name, Window: session.selected, Windows: s.windowInfos(session)})
		p.force = false
	}
	for _, session := range s.sessions {
		session.dirty = false
	}
}
