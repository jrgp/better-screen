// Package vt confines terminal emulation to Ghostty. Callers serialize access.
package vt

import (
	"fmt"
	"strings"

	ghostty "go.mitchellh.com/libghostty"
)

// Frame is a bounded, presentation-only snapshot, not a replay of PTY bytes.
type Frame struct {
	Lines       []string
	X, Y        int
	Visible     bool
	CursorStyle int
	Modes       []int
	Keypad      bool
}

func New(cols, rows int, options ...ghostty.TerminalOption) (*ghostty.Terminal, error) {
	options = append(options,
		ghostty.WithSize(uint16(cols), uint16(rows)),
		ghostty.WithMaxScrollbackBytes(8<<20),
		ghostty.WithMaxScrollbackLines(10000),
		ghostty.WithKittyClipboardWriteMaxBytes(4096),
	)
	t, err := ghostty.NewTerminal(options...)
	if err != nil {
		return nil, err
	}
	limit := uint64(0)
	for _, err := range []error{
		t.SetKittyImageStorageLimit(&limit),
		t.SetKittyImageMediumFile(false),
		t.SetKittyImageMediumSharedMem(false),
		t.SetKittyImageMediumTempFile(nil),
	} {
		if err != nil {
			t.Close()
			return nil, err
		}
	}
	return t, nil
}

// Capture uses Ghostty's VT formatter for colors, attributes and graphemes.
// History is addressed by grid coordinates, without moving the shared viewport.
func Capture(t *ghostty.Terminal, cols, rows, offset int) (Frame, error) {
	width, err := t.Cols()
	if err != nil {
		return Frame{}, err
	}
	height, err := t.Rows()
	if err != nil {
		return Frame{}, err
	}
	history, err := t.ScrollbackRows()
	if err != nil {
		return Frame{}, err
	}
	cols = min(cols, int(width))
	rows = min(rows, int(height))
	offset = min(max(offset, 0), int(history))
	f := Frame{Lines: make([]string, rows)}
	for y := range rows {
		tag, row := ghostty.PointTagActive, y
		if offset > 0 {
			tag, row = ghostty.PointTagScreen, int(history)-offset+y
		}
		a, err := t.GridRef(ghostty.Point{Tag: tag, Y: uint32(row)})
		if err != nil {
			return Frame{}, err
		}
		b, err := t.GridRef(ghostty.Point{Tag: tag, X: uint16(cols - 1), Y: uint32(row)})
		if err != nil {
			return Frame{}, err
		}
		selection := &ghostty.Selection{Start: *a, End: *b, Rectangle: true}
		formatter, err := ghostty.NewFormatter(t,
			ghostty.WithFormatterFormat(ghostty.FormatterFormatVT),
			ghostty.WithFormatterSelection(selection),
			ghostty.WithFormatterTrim(false),
		)
		if err != nil {
			return Frame{}, err
		}
		line, err := formatter.FormatString()
		formatter.Close()
		if err != nil {
			return Frame{}, err
		}
		f.Lines[y] = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	}
	x, err := t.CursorX()
	if err != nil {
		return Frame{}, err
	}
	y, err := t.CursorY()
	if err != nil {
		return Frame{}, err
	}
	visible, err := t.CursorVisible()
	if err != nil {
		return Frame{}, err
	}
	f.X, f.Y = int(x), int(y)
	f.Visible = visible && offset == 0 && f.X < cols && f.Y < rows
	rs, err := ghostty.NewRenderState()
	if err != nil {
		return Frame{}, err
	}
	defer rs.Close()
	if err := rs.Update(t); err != nil {
		return Frame{}, err
	}
	cursor, err := rs.Cursor()
	if err != nil {
		return Frame{}, err
	}
	switch cursor.VisualStyle {
	case ghostty.CursorVisualStyleBar:
		f.CursorStyle = 6
	case ghostty.CursorVisualStyleUnderline:
		f.CursorStyle = 4
	default:
		f.CursorStyle = 2
	}
	if cursor.Blinking {
		f.CursorStyle--
	}
	// Only input/presentation modes belong on the physical terminal. Never emit
	// application queries, clipboard writes, window operations, or file effects.
	for _, mode := range []int{1, 5, 9, 1000, 1002, 1003, 1004, 1005, 1006, 1015, 2004} {
		on, err := t.Mode(ghostty.NewMode(uint16(mode), false))
		if err != nil {
			return Frame{}, err
		}
		if on && offset == 0 {
			f.Modes = append(f.Modes, mode)
		}
	}
	f.Keypad, err = t.Mode(ghostty.NewMode(66, false))
	return f, err
}

// Draw updates changed rows only. Autowrap is disabled so the bottom-right cell
// cannot scroll the physical terminal. CUP clears pending-wrap state.
func Draw(previous *Frame, f Frame) string {
	var b strings.Builder
	b.WriteString("\x1b[?25l\x1b[?7l\x1b[0m")
	if previous == nil {
		b.WriteString("\x1b[?6l\x1b[?69l\x1b[r\x1b[2J")
	}
	for y, line := range f.Lines {
		if previous != nil && y < len(previous.Lines) && line == previous.Lines[y] {
			continue
		}
		fmt.Fprintf(&b, "\x1b[%d;1H\x1b[0m\x1b[2K%s", y+1, line)
	}
	for _, mode := range []int{1, 5, 9, 1000, 1002, 1003, 1004, 1005, 1006, 1015, 2004} {
		on := false
		for _, m := range f.Modes {
			if m == mode {
				on = true
			}
		}
		state := "l"
		if on {
			state = "h"
		}
		fmt.Fprintf(&b, "\x1b[?%d%s", mode, state)
	}
	if f.Keypad {
		b.WriteString("\x1b=")
	} else {
		b.WriteString("\x1b>")
	}
	fmt.Fprintf(&b, "\x1b[0m\x1b[%d q\x1b[%d;%dH", f.CursorStyle, f.Y+1, f.X+1)
	if f.Visible {
		b.WriteString("\x1b[?25h")
	}
	return b.String()
}
