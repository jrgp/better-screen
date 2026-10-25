package vt

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	ghostty "go.mitchellh.com/libghostty"
)

func TestUnicodePaneMarginRoundTrip(t *testing.T) {
	source, err := New(40, 15)
	require.NoError(t, err)
	defer source.Close()
	source.VTWrite([]byte("\x1b[?1049h\x1b[2J\x1b[HA😀é界👩🏽‍💻🇺🇳❤️Z\x1b[3;39H😀\x1b[5;40H界"))
	frame, err := Capture(source, 40, 15, 0)
	require.NoError(t, err)
	require.Contains(t, frame.Lines[2], "😀")
	for y, line := range frame.Lines {
		frame.Lines[y] = fmt.Sprintf("\x1b[41G%s", line)
	}
	target, err := New(80, 15)
	require.NoError(t, err)
	defer target.Close()
	target.VTWrite([]byte(Draw(nil, frame)))
	require.Contains(t, strings.Split(plain(t, target), "\n")[2], "😀")
}

func TestUnicodeBytewiseRoundTrip(t *testing.T) {
	for _, text := range []string{"😀", "👍🏽", "👩🏽‍💻", "👨‍👩‍👧‍👦", "🇺🇳", "❤️", "1️⃣", "e\u0301\u0327", "界"} {
		t.Run(text, func(t *testing.T) {
			source, err := New(8, 3)
			require.NoError(t, err)
			defer source.Close()
			for _, b := range []byte("\x1b[1;7H" + text) {
				source.VTWrite([]byte{b})
			}
			ref, err := source.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: 6})
			require.NoError(t, err)
			var want []uint32
			for _, r := range text {
				want = append(want, uint32(r))
			}
			cluster, err := ref.Graphemes()
			require.NoError(t, err)
			require.Equal(t, want, cluster)
			frame, err := Capture(source, 8, 3, 0)
			require.NoError(t, err)
			target, err := New(8, 3, ghostty.WithModeDefault(ghostty.NewMode(2027, false), false))
			require.NoError(t, err)
			defer target.Close()
			target.VTWrite([]byte(Draw(nil, frame)))
			require.Contains(t, plain(t, target), text)
			ref, err = target.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: 6})
			require.NoError(t, err)
			cluster, err = ref.Graphemes()
			require.NoError(t, err)
			require.Equal(t, want, cluster)
		})
	}
}

func TestUnicodeIndependentPaneWidthModes(t *testing.T) {
	frame := Frame{Lines: make([]string, 2)}
	for row, enabled := range []bool{false, true} {
		source, err := New(12, 2, ghostty.WithModeDefault(ghostty.NewMode(2027, false), enabled))
		require.NoError(t, err)
		defer source.Close()
		source.VTWrite([]byte("👍🏽|"))
		part, err := Capture(source, 12, 2, 0)
		require.NoError(t, err)
		frame.Lines[row] = part.Lines[0]
	}
	target, err := New(12, 2)
	require.NoError(t, err)
	defer target.Close()
	target.VTWrite([]byte(Draw(nil, frame)))
	for row, column := range []uint16{4, 2} {
		ref, err := target.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: column, Y: uint32(row)})
		require.NoError(t, err)
		cell, err := ref.Cell()
		require.NoError(t, err)
		cp, err := cell.Codepoint()
		require.NoError(t, err)
		require.Equal(t, uint32('|'), cp)
	}
	source, err := New(12, 2)
	require.NoError(t, err)
	defer source.Close()
	source.VTWrite([]byte("\x1b[?2027l\x1bc"))
	enabled, err := source.Mode(ghostty.NewMode(2027, false))
	require.NoError(t, err)
	require.True(t, enabled, "reset must preserve the grapheme-aware default")
}
