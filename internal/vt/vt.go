// Package vt confines terminal emulation to Ghostty. Callers serialize access.
package vt

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
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
	Style       string
}

func New(cols, rows int, options ...ghostty.TerminalOption) (*ghostty.Terminal, error) {
	options = append([]ghostty.TerminalOption{ghostty.WithModeDefault(ghostty.NewMode(2027, false), true)}, options...)
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
	reverse, err := t.Mode(ghostty.NewMode(5, false))
	if err != nil {
		return Frame{}, err
	}
	if reverse {
		f.Style = ansi.SGR(ansi.AttrReverse)
	}
	graphemes, err := t.Mode(ghostty.NewMode(2027, false))
	if err != nil {
		return Frame{}, err
	}
	widthMode := ansi.ResetModeUnicodeCore
	if graphemes {
		widthMode = ansi.SetModeUnicodeCore
	}
	palette, err := t.ColorPalette()
	if err != nil {
		return Frame{}, err
	}
	defaultPalette, err := t.ColorPaletteDefault()
	if err != nil {
		return Frame{}, err
	}
	overrides := make(map[int]ghostty.ColorRGB)
	if palette != nil && defaultPalette != nil {
		for i, color := range *palette {
			if reverse || color != defaultPalette[i] {
				overrides[i] = color
			}
		}
	}
	defaults := make(map[string]string)
	foreground, background := t.ColorForeground, t.ColorBackground
	if reverse {
		foreground, background = background, foreground
	}
	for _, item := range []struct {
		code string
		get  func() (*ghostty.ColorRGB, error)
	}{{"39", foreground}, {"49", background}} {
		color, err := item.get()
		if err != nil {
			return Frame{}, err
		}
		if color != nil {
			channel := "38"
			if item.code == "49" {
				channel = "48"
			}
			if reverse {
				channel = reverseColorCode(channel)
			}
			value := fmt.Sprintf("%s;2;%d;%d;%d", channel, color.R, color.G, color.B)
			defaults[item.code] = value
			f.Style += ansi.Style{value}.String()
		}
	}
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
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		f.Lines[y] = widthMode + resolveColors(line, overrides, defaults, reverse)
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
	for _, mode := range []int{1, 9, 1000, 1002, 1003, 1004, 1005, 1006, 1015, 2004} {
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
	if previous != nil && slices.Equal(previous.Lines, f.Lines) && previous.Style == f.Style && previous.X == f.X && previous.Y == f.Y && previous.Visible == f.Visible && previous.CursorStyle == f.CursorStyle && slices.Equal(previous.Modes, f.Modes) && previous.Keypad == f.Keypad {
		return ""
	}
	var b strings.Builder
	reset := ansi.SGR(ansi.AttrReset)
	b.WriteString(ansi.HideCursor + ansi.ResetModeAutoWrap + ansi.SetModeUnicodeCore + reset)
	if previous == nil {
		b.WriteString(ansi.ResetModeOrigin + ansi.ResetModeLeftRightMargin +
			ansi.SetTopBottomMargins(0, 0) + ansi.EraseEntireScreen)
	}
	for y, line := range f.Lines {
		if previous != nil && previous.Style == f.Style && y < len(previous.Lines) && line == previous.Lines[y] {
			continue
		}
		b.WriteString(ansi.CursorPosition(1, y+1) + reset + f.Style + ansi.EraseEntireLine + line)
	}
	if previous != nil {
		for y := len(f.Lines); y < len(previous.Lines); y++ {
			b.WriteString(ansi.CursorPosition(1, y+1) + reset + ansi.EraseEntireLine)
		}
	}
	for _, mode := range []int{1, 5, 9, 1000, 1002, 1003, 1004, 1005, 1006, 1015, 2004} {
		on := slices.Contains(f.Modes, mode)
		if previous != nil && slices.Contains(previous.Modes, mode) == on {
			continue
		}
		if on {
			b.WriteString(ansi.SetMode(ansi.DECMode(mode)))
		} else {
			b.WriteString(ansi.ResetMode(ansi.DECMode(mode)))
		}
	}
	if previous == nil || previous.Keypad != f.Keypad {
		if f.Keypad {
			b.WriteString(ansi.KeypadApplicationMode)
		} else {
			b.WriteString(ansi.KeypadNumericMode)
		}
	}
	b.WriteString(reset + ansi.SetCursorStyle(f.CursorStyle) + ansi.CursorPosition(f.X+1, f.Y+1))
	if f.Visible {
		b.WriteString(ansi.ShowCursor)
	}
	return b.String()
}

// Resolve OSC color overrides into SGR RGB, never changing the outer terminal's
// global palette. Ghostty still does all cell/style formatting; x/ansi identifies
// complete sequences before the formatter's known SGR color tokens are replaced.
func resolveColors(line string, palette map[int]ghostty.ColorRGB, defaults map[string]string, reverse bool) string {
	if len(palette) == 0 && len(defaults) == 0 && !reverse {
		return line
	}
	var out strings.Builder
	for len(line) > 0 {
		seq, _, n, _ := ansi.DecodeSequence(line, 0, nil)
		line = line[n:]
		if ansi.HasCsiPrefix(seq) && seq[0] == ansi.ESC && strings.HasSuffix(seq, "m") {
			params := strings.Split(seq[2:len(seq)-1], ";")
			var result []string
			for i := 0; i < len(params); i++ {
				value := params[i]
				if value == "0" || value == "" {
					result = append(result, "0")
					if reverse {
						result = append(result, "7")
					}
					for _, key := range []string{"39", "49"} {
						if s, ok := defaults[key]; ok {
							result = append(result, s)
						}
					}
				} else if replacement, ok := defaults[value]; ok {
					result = append(result, replacement)
				} else if (value == "38" || value == "48" || value == "58") && i+2 < len(params) && params[i+1] == "5" {
					if reverse {
						value = reverseColorCode(value)
					}
					index, err := strconv.Atoi(params[i+2])
					if color, ok := palette[index]; err == nil && ok {
						result = append(result, fmt.Sprintf("%s;2;%d;%d;%d", value, color.R, color.G, color.B))
					} else {
						result = append(result, value, params[i+1], params[i+2])
					}
					i += 2
				} else if (value == "38" || value == "48" || value == "58") && i+4 < len(params) && params[i+1] == "2" {
					if reverse {
						value = reverseColorCode(value)
					}
					result = append(result, value)
					result = append(result, params[i+1:i+5]...)
					i += 4
				} else {
					if reverse {
						n, err := strconv.Atoi(value)
						index, channel := -1, ""
						if err == nil {
							switch {
							case n >= 30 && n <= 37:
								index, channel = n-30, "48"
							case n >= 40 && n <= 47:
								index, channel = n-40, "38"
							case n >= 90 && n <= 97:
								index, channel = n-90+8, "48"
							case n >= 100 && n <= 107:
								index, channel = n-100+8, "38"
							}
						}
						if color, ok := palette[index]; ok {
							value = fmt.Sprintf("%s;2;%d;%d;%d", channel, color.R, color.G, color.B)
						} else {
							value = reverseColorCode(value)
						}
					}
					result = append(result, value)
				}
			}
			out.WriteString(ansi.Style(result).String())
		} else {
			out.WriteString(seq)
		}
	}
	return out.String()
}

// DECSCNM swaps default colors, not explicit RGB colors. Swapping SGR color
// channels and toggling cell inverse reproduces it inside one pane, even when
// the physical terminal's default RGB colors are unknown.
func reverseColorCode(value string) string {
	switch value {
	case "7":
		return "27"
	case "27":
		return "7"
	case "38":
		return "48"
	case "48":
		return "38"
	case "39":
		return "49"
	case "49":
		return "39"
	}
	return value
}
