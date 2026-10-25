package vt

import (
	"testing"

	"github.com/stretchr/testify/require"
	ghostty "go.mitchellh.com/libghostty"
)

func TestReverseScreenIsLocalAndPreservesExplicitColors(t *testing.T) {
	for _, tc := range []struct {
		name, sgr string
		fg, bg    int
		inverse   bool
	}{
		{"basic", "1;31;44", 1, 4, false},
		{"bright", "91;104", 9, 12, false},
		{"indexed", "38;5;123;48;5;45", 123, 45, false},
		{"cell-inverse", "7;31;44", 4, 1, true},
		{"inverse-off", "7;31;44;27", 1, 4, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := New(16, 3)
			require.NoError(t, err)
			defer source.Close()
			source.VTWrite([]byte("\x1b[?5h\x1b[" + tc.sgr + "mX"))
			palette, err := source.ColorPalette()
			require.NoError(t, err)
			require.NotNil(t, palette)
			frame, err := Capture(source, 16, 3, 0)
			require.NoError(t, err)
			target, err := New(16, 3)
			require.NoError(t, err)
			defer target.Close()
			target.VTWrite([]byte(Draw(nil, frame)))
			on, err := target.Mode(ghostty.NewMode(5, false))
			require.NoError(t, err)
			require.False(t, on)
			ref, err := target.GridRef(ghostty.Point{Tag: ghostty.PointTagActive})
			require.NoError(t, err)
			style, err := ref.Style()
			require.NoError(t, err)
			require.Equal(t, !tc.inverse, style.Inverse())
			fg, bg := style.FgColor().RGB, style.BgColor().RGB
			if style.Inverse() {
				fg, bg = bg, fg
			}
			require.Equal(t, palette[tc.fg], fg)
			require.Equal(t, palette[tc.bg], bg)
		})
	}
}
