package vt

import (
	"testing"

	"github.com/stretchr/testify/require"
	ghostty "go.mitchellh.com/libghostty"
)

func TestOSCColorOverrides(t *testing.T) {
	source, err := New(30, 4)
	require.NoError(t, err)
	defer source.Close()
	source.VTWrite([]byte("\x1b]4;1;rgb:11/22/33\a\x1b]10;rgb:44/55/66\a\x1b]11;rgb:01/02/03\a\x1b[2J\x1b[2;2H\x1b[31mINDEX\x1b[39mDEFAULT"))
	frame, err := Capture(source, 30, 4, 0)
	require.NoError(t, err)
	target, err := New(30, 4)
	require.NoError(t, err)
	defer target.Close()
	target.VTWrite([]byte(Draw(nil, frame)))
	for _, tc := range []struct {
		x     uint16
		color ghostty.ColorRGB
	}{{1, ghostty.ColorRGB{R: 17, G: 34, B: 51}}, {6, ghostty.ColorRGB{R: 68, G: 85, B: 102}}} {
		ref, err := target.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: tc.x, Y: 1})
		require.NoError(t, err)
		style, err := ref.Style()
		require.NoError(t, err)
		require.Equal(t, tc.color, style.FgColor().RGB)
	}
	require.NotContains(t, Draw(nil, frame), "\x1b]4;", "do not mutate the physical palette")
}
