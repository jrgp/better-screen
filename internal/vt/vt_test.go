package vt

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	ghostty "go.mitchellh.com/libghostty"
)

func TestRoundTrip(t *testing.T) {
	source, err := New(40, 8)
	require.NoError(t, err)
	defer source.Close()
	target, err := New(45, 10)
	require.NoError(t, err)
	defer target.Close()
	source.VTWrite([]byte("primary\x1b[?1049h\x1b[2J\x1b[2;3H\x1b[1;38;2;255;80;0m世界e\u0301\x1b[0m\x1b[4;6H"))
	require.NoError(t, source.Resize(45, 10, 0, 0))
	frame, err := Capture(source, 45, 10, 0)
	require.NoError(t, err)
	target.VTWrite([]byte(Draw(nil, frame)))
	ref, err := target.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: 2, Y: 1})
	require.NoError(t, err)
	cell, err := ref.Cell()
	require.NoError(t, err)
	cp, err := cell.Codepoint()
	require.NoError(t, err)
	require.Equal(t, uint32('世'), cp)
	style, err := ref.Style()
	require.NoError(t, err)
	require.True(t, style.Bold())
	require.Equal(t, ghostty.ColorRGB{R: 255, G: 80}, style.FgColor().RGB)
	x, err := target.CursorX()
	require.NoError(t, err)
	y, err := target.CursorY()
	require.NoError(t, err)
	require.Equal(t, uint16(5), x)
	require.Equal(t, uint16(3), y)
	source.VTWrite([]byte("\x1b[?1049l"))
	frame, err = Capture(source, 45, 10, 0)
	require.NoError(t, err)
	target.VTWrite([]byte(Draw(nil, frame)))
	require.Contains(t, plain(t, target), "primary")
}

func plain(t *testing.T, term *ghostty.Terminal) string {
	t.Helper()
	f, err := ghostty.NewFormatter(term, ghostty.WithFormatterTrim(true))
	require.NoError(t, err)
	defer f.Close()
	s, err := f.FormatString()
	require.NoError(t, err)
	return s
}

func TestHistoryAndIndependentInstances(t *testing.T) {
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			term, err := New(20, 4)
			require.NoError(t, err)
			defer term.Close()
			for i := range 30 {
				term.VTWrite(fmt.Appendf(nil, "history-%02d\r\n", i))
			}

			f, err := Capture(term, 20, 4, 25)
			require.NoError(t, err)
			require.Contains(t, f.Lines[0], "history-02")
			require.False(t, f.Visible)
		})
	}
	wg.Wait()
}

func TestChangedRowsAndShrink(t *testing.T) {
	screen, err := New(20, 5)
	require.NoError(t, err)
	defer screen.Close()
	first := Frame{Lines: []string{"one", "two", "three", "four"}, CursorStyle: 2, Visible: true}
	screen.VTWrite([]byte(Draw(nil, first)))
	require.Equal(t, "", Draw(&first, first))
	second := first
	second.Lines = []string{"one", "two"}
	screen.VTWrite([]byte(Draw(&first, second)))
	require.NotContains(t, plain(t, screen), "three")
	require.NotContains(t, plain(t, screen), "four")
}

func TestStyledErasesAndWideGraphemes(t *testing.T) {
	source, err := New(20, 4)
	require.NoError(t, err)
	defer source.Close()
	source.VTWrite([]byte("\x1b[41m\x1b[2J\x1b[2;2H\x1b[1;3;4m界e\u0301\x1b[0m\x1b[?25l"))
	f, err := Capture(source, 20, 4, 0)
	require.NoError(t, err)
	target, err := New(20, 4)
	require.NoError(t, err)
	defer target.Close()
	target.VTWrite([]byte(Draw(nil, f)))
	for _, pos := range []ghostty.Point{{Tag: ghostty.PointTagActive, X: 18, Y: 0}, {Tag: ghostty.PointTagActive, X: 1, Y: 1}} {
		ref, err := target.GridRef(pos)
		require.NoError(t, err)
		style, err := ref.Style()
		require.NoError(t, err)
		if pos.Y == 1 {
			require.Equal(t, uint8(1), style.BgColor().Palette)
			require.True(t, style.Bold())
			require.True(t, style.Italic())
			require.Equal(t, ghostty.UnderlineSingle, style.Underline())
		} else {
			cell, err := ref.Cell()
			require.NoError(t, err)
			color, err := cell.ColorPalette()
			require.NoError(t, err)
			require.Equal(t, uint8(1), color)
		}
	}
	ref, err := target.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: 3, Y: 1})
	require.NoError(t, err)
	graphemes, err := ref.Graphemes()
	require.NoError(t, err)
	require.Equal(t, []uint32{'e', 0x301}, graphemes)
	visible, err := target.CursorVisible()
	require.NoError(t, err)
	require.False(t, visible)
}
