package vt

import (
	"testing"

	"github.com/stretchr/testify/require"
	ghostty "go.mitchellh.com/libghostty"
)

func TestCropWideBoundary(t *testing.T) {
	source, err := New(20, 4)
	require.NoError(t, err)
	defer source.Close()
	source.VTWrite([]byte("abcd界\r\nnext-row"))
	frame, err := Capture(source, 5, 4, 0)
	require.NoError(t, err)
	target, err := New(5, 4)
	require.NoError(t, err)
	defer target.Close()
	target.VTWrite([]byte(Draw(nil, frame)))
	for x, expected := range "abcd" {
		ref, err := target.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: uint16(x)})
		require.NoError(t, err)
		cell, err := ref.Cell()
		require.NoError(t, err)
		cp, err := cell.Codepoint()
		require.NoError(t, err)
		require.Equal(t, uint32(expected), cp)
	}
	ref, err := target.GridRef(ghostty.Point{Tag: ghostty.PointTagActive, X: 4})
	require.NoError(t, err)
	cell, err := ref.Cell()
	require.NoError(t, err)
	cp, err := cell.Codepoint()
	require.NoError(t, err)
	require.Contains(t, []uint32{0, ' '}, cp, "a clipped wide grapheme must not overwrite the previous cell")
	require.Contains(t, plain(t, target), "next-")
}
