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
