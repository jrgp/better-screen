package mux

import (
	"fmt"
	"slices"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"github.com/jrgp/bs/internal/vt"
)

type rectangle struct{ x, y, cols, rows int }

type layout struct {
	pane          *pane
	vertical      bool
	first, second *layout
}

func (l *layout) panes() []*pane {
	if l.pane != nil {
		return []*pane{l.pane}
	}
	return append(l.first.panes(), l.second.panes()...)
}

func (l *layout) leaf(p *pane) *layout {
	if l.pane == p {
		return l
	}
	if l.pane != nil {
		return nil
	}
	if found := l.first.leaf(p); found != nil {
		return found
	}
	return l.second.leaf(p)
}

func (l *layout) remove(p *pane) *layout {
	if l.pane != nil {
		if l.pane == p {
			return nil
		}
		return l
	}
	l.first, l.second = l.first.remove(p), l.second.remove(p)
	if l.first == nil {
		return l.second
	}
	if l.second == nil {
		return l.first
	}
	return l
}

func (l *layout) minimum() (int, int) {
	if l.pane != nil {
		return 2, 1
	}
	aw, ah := l.first.minimum()
	bw, bh := l.second.minimum()
	if l.vertical {
		return aw + bw + 1, max(ah, bh)
	}
	return max(aw, bw), ah + bh + 1
}

func (l *layout) place(r rectangle, borders [][]byte) {
	if l.pane != nil {
		l.pane.bounds = r
		return
	}
	a, b := r, r
	aw, ah := l.first.minimum()
	bw, bh := l.second.minimum()
	if l.vertical {
		a.cols = min(max((r.cols-1)/2, aw), r.cols-1-bw)
		b.x, b.cols = r.x+a.cols+1, r.cols-a.cols-1
		for y := r.y; y < r.y+r.rows; y++ {
			borders[y][r.x+a.cols] = 1
		}
	} else {
		a.rows = min(max((r.rows-1)/2, ah), r.rows-1-bh)
		b.y, b.rows = r.y+a.rows+1, r.rows-a.rows-1
		for x := r.x; x < r.x+r.cols; x++ {
			borders[r.y+a.rows][x] = 2
		}
	}
	l.first.place(a, borders)
	l.second.place(b, borders)
}

func (s *Server) resizeWindow(session *session, w *window) error {
	cols, rows := session.cols, s.contentRows(session.rows)
	w.borders = make([][]byte, rows)
	for y := range w.borders {
		w.borders[y] = make([]byte, cols)
	}
	minCols, minRows := w.root.minimum()
	for _, p := range w.root.panes() {
		p.bounds = rectangle{}
	}
	if cols < minCols || rows < minRows {
		// Keep the tree and its processes intact when it cannot fit. Only the
		// focused pane is visible until a larger canonical size is fitted.
		w.focused.bounds = rectangle{cols: cols, rows: rows}
	} else {
		w.root.place(rectangle{cols: cols, rows: rows}, w.borders)
	}
	for _, p := range w.root.panes() {
		cols, rows := max(1, p.bounds.cols), max(1, p.bounds.rows)
		oldCols, err := p.term.Cols()
		if err != nil {
			return err
		}
		oldRows, err := p.term.Rows()
		if err != nil {
			return err
		}
		if int(oldCols) == cols && int(oldRows) == rows {
			continue
		}
		if err := pty.Setsize(p.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
			return err
		}
		if err := p.term.Resize(uint16(cols), uint16(rows), 0, 0); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) split(session *session, w *window, vertical bool) error {
	if len(w.root.panes()) >= maxPanes {
		return fmt.Errorf("pane limit reached")
	}
	r := w.focused.bounds
	minCols, minRows := w.root.minimum()
	if session.cols < minCols || s.contentRows(session.rows) < minRows ||
		(vertical && r.cols < 5) || (!vertical && r.rows < 3) {
		return fmt.Errorf("pane too small to split")
	}
	cols, rows := r.cols, r.rows
	if vertical {
		cols /= 2
	} else {
		rows /= 2
	}
	p, err := s.newPane(session, cols, rows)
	if err != nil {
		return err
	}
	leaf := w.root.leaf(w.focused)
	*leaf = layout{vertical: vertical, first: &layout{pane: w.focused}, second: &layout{pane: p}}
	w.focused = p
	s.resetViews(session)
	return s.resizeWindow(session, w)
}

func borderGlyph(grid [][]byte, x, y int) rune {
	mask := 0
	if y > 0 && grid[y-1][x] != 0 {
		mask |= 1
	}
	if y+1 < len(grid) && grid[y+1][x] != 0 {
		mask |= 2
	}
	if x > 0 && grid[y][x-1] != 0 {
		mask |= 4
	}
	if x+1 < len(grid[y]) && grid[y][x+1] != 0 {
		mask |= 8
	}
	switch mask {
	case 5:
		return '┘'
	case 6:
		return '┐'
	case 9:
		return '└'
	case 10:
		return '┌'
	case 7:
		return '┤'
	case 11:
		return '├'
	case 13:
		return '┴'
	case 14:
		return '┬'
	case 15:
		return '┼'
	}
	if grid[y][x] == 1 {
		return '│'
	}
	return '─'
}

func (s *Server) capture(p *peer) (vt.Frame, error) {
	session := p.session
	w := session.windows[session.selected]
	if err := p.resolveScroll(w.focused.term); err != nil {
		return vt.Frame{}, err
	}
	f := vt.Frame{Lines: make([]string, p.rows)}
	for _, pane := range w.root.panes() {
		r := pane.bounds
		cols, rows := min(r.cols, p.cols-r.x), min(r.rows, s.contentRows(p.rows)-r.y)
		visible := cols > 0 && rows > 0
		if !visible && pane != w.focused {
			continue
		}
		offset := 0
		if pane == w.focused {
			offset = p.offset
		}
		part, err := vt.Capture(pane.term, max(1, cols), max(1, rows), offset)
		if err != nil {
			return vt.Frame{}, err
		}
		if pane == w.focused {
			f.X, f.Y = r.x+part.X, r.y+part.Y
			f.Visible = visible && part.Visible
			f.CursorStyle, f.Modes, f.Keypad = part.CursorStyle, part.Modes, part.Keypad
		}
		if !visible {
			continue
		}
		for y, line := range part.Lines {
			segment := fmt.Sprintf("\x1b[%dG\x1b[0m%s%s\x1b[0m%s", r.x+1, part.Style, line, part.Style)
			if padding := cols - ansi.StringWidth(line); padding > 0 {
				segment += fmt.Sprintf("\x1b[%dX", padding)
			}
			f.Lines[r.y+y] += segment + "\x1b[0m"
		}
	}
	for y, row := range w.borders {
		if y >= s.contentRows(p.rows) {
			break
		}
		for x, cell := range row {
			if x >= p.cols {
				break
			}
			if cell != 0 {
				f.Lines[y] += fmt.Sprintf("\x1b[%dG\x1b[0m%s%c\x1b[0m", x+1, accent, borderGlyph(w.borders, x, y))
			}
		}
	}
	if s.config.StatusLine {
		f.Lines[p.rows-1] = s.statusLine(session, p.cols)
	}
	return f, nil
}

func (s *Server) removeWindow(session *session, wi int) {
	s.resetViews(session)
	for _, p := range session.windows[wi].root.panes() {
		s.stopPane(p)
	}
	session.windows = slices.Delete(session.windows, wi, wi+1)
	if len(session.windows) == 0 {
		for p := range s.peers {
			if p.session == session {
				s.reply(p, response{Done: true})
			}
		}
		i := slices.Index(s.sessions, session)
		s.sessions = slices.Delete(s.sessions, i, i+1)
	} else {
		if wi < session.selected {
			session.selected--
		}
		session.selected = min(session.selected, len(session.windows)-1)
		s.resetViews(session)
	}
}
