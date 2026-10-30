package mux

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaneCommands(t *testing.T) {
	require.NoError(t, validatePaneCommands(nil))
	require.NoError(t, validatePaneCommands([]string{`printf '%s\n' "quoted value"`}))
	for _, command := range []string{"", " \t\n", "echo\x00bad"} {
		require.ErrorContains(t, validatePaneCommands([]string{command}), "pane command 1")
	}
	require.ErrorContains(t, validatePaneCommands(strings.Fields(strings.Repeat("true ", maxPanes+1))), "pane limit")
}

func TestCommandGridRows(t *testing.T) {
	for _, tc := range []struct {
		count, cols, rows, want int
	}{
		{1, 120, 29, 1},
		{2, 120, 29, 1},
		{2, 20, 79, 2},
		{3, 120, 29, 2},
		{4, 80, 23, 2},
		{4, 120, 29, 2},
		{4, 200, 23, 2},
		{4, 20, 79, 2},
		{5, 120, 29, 2},
		{6, 120, 29, 2},
		{9, 120, 29, 3},
		{16, 160, 39, 4},
		{32, 160, 39, 6},
		{4, 2, 20, 4},
		{4, 80, 1, 1},
		{4, 4, 2, 0},
	} {
		t.Run(fmt.Sprintf("%d-in-%dx%d", tc.count, tc.cols, tc.rows), func(t *testing.T) {
			require.Equal(t, tc.want, commandGridRows(tc.count, tc.cols, tc.rows))
		})
	}
}

func TestCommandGridBalancedCells(t *testing.T) {
	for count := 1; count <= maxPanes; count++ {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			rows := commandGridRows(count, 120, 29)
			root := commandLayout(count, rows)
			panes := root.panes()
			w := &window{root: root, focused: panes[0], equalized: true}
			w.place(120, 29)
			widths := make(map[int][]int)
			var heights []int
			for _, p := range panes {
				if len(widths[p.bounds.y]) == 0 {
					heights = append(heights, p.bounds.rows)
				}
				widths[p.bounds.y] = append(widths[p.bounds.y], p.bounds.cols)
			}
			require.Len(t, widths, rows)
			require.LessOrEqual(t, slices.Max(heights)-slices.Min(heights), 1)
			for _, row := range widths {
				require.LessOrEqual(t, slices.Max(row)-slices.Min(row), 1)
			}
		})
	}
}

func TestCommandLayout(t *testing.T) {
	for count := 1; count <= maxPanes; count++ {
		for rows := 1; rows <= count; rows++ {
			root := commandLayout(count, rows)
			panes := root.panes()
			require.Len(t, panes, count)
			cols, height := root.minimum()
			require.Equal(t, 3*((count+rows-1)/rows)-1, cols)
			require.Equal(t, 2*rows-1, height)
			w := &window{root: root, focused: panes[0]}
			w.place(cols, height)
			for _, p := range panes {
				require.GreaterOrEqual(t, p.bounds.cols, 2)
				require.GreaterOrEqual(t, p.bounds.rows, 1)
			}
		}
	}
}

func TestEqualizedLayout(t *testing.T) {
	for _, vertical := range []bool{false, true} {
		for _, firstHeavy := range []bool{false, true} {
			for count := 1; count <= maxPanes; count++ {
				root := &layout{pane: &pane{}}
				for i := 1; i < count; i++ {
					leaf := &layout{pane: &pane{}}
					if firstHeavy {
						root = &layout{vertical: vertical, first: root, second: leaf}
					} else {
						root = &layout{vertical: vertical, first: leaf, second: root}
					}
				}
				panes := root.panes()
				w := &window{root: root, focused: panes[0], equalized: true}
				cols, rows := root.minimum()
				for _, extra := range []int{0, 1, 2, 17, 113} {
					w.place(cols+extra, rows+extra)
					total, smallest, largest := 0, cols+rows+extra, 0
					for _, p := range panes {
						size := p.bounds.rows
						if vertical {
							require.Equal(t, total, p.bounds.x)
							require.Equal(t, rows+extra, p.bounds.rows)
							size = p.bounds.cols
						} else {
							require.Equal(t, total, p.bounds.y)
							require.Equal(t, cols+extra, p.bounds.cols)
						}
						require.GreaterOrEqual(t, p.bounds.cols, 2)
						require.GreaterOrEqual(t, p.bounds.rows, 1)
						total += size + 1
						smallest, largest = min(smallest, size), max(largest, size)
					}
					require.LessOrEqual(t, largest-smallest, 1)
					if vertical {
						require.Equal(t, cols+extra, total-1)
					} else {
						require.Equal(t, rows+extra, total-1)
					}
				}
			}
		}
	}
}

func TestEqualizedMixedLayout(t *testing.T) {
	a, b, c, d, e, f := &pane{}, &pane{}, &pane{}, &pane{}, &pane{}, &pane{}
	w := &window{
		root: &layout{
			vertical: true,
			first: &layout{
				first:  &layout{pane: a},
				second: &layout{first: &layout{pane: b}, second: &layout{pane: c}},
			},
			second: &layout{
				vertical: true,
				first:    &layout{pane: d},
				second:   &layout{first: &layout{pane: e}, second: &layout{pane: f}},
			},
		},
		focused: e, equalized: true,
	}
	w.place(120, 23)
	for _, tc := range []struct {
		pane   *pane
		bounds rectangle
	}{
		{a, rectangle{0, 0, 39, 7}},
		{b, rectangle{0, 8, 39, 7}},
		{c, rectangle{0, 16, 39, 7}},
		{d, rectangle{40, 0, 39, 23}},
		{e, rectangle{80, 0, 40, 11}},
		{f, rectangle{80, 12, 40, 11}},
	} {
		require.Equal(t, tc.bounds, tc.pane.bounds)
	}
	require.Same(t, e, w.focused)
	w.place(3, 2)
	for _, p := range w.root.panes() {
		if p == e {
			require.Equal(t, rectangle{cols: 3, rows: 2}, p.bounds)
		} else {
			require.Equal(t, rectangle{}, p.bounds)
		}
	}
	w.place(120, 23)
	require.Equal(t, rectangle{80, 12, 40, 11}, f.bounds)
	require.Same(t, e, w.focused)
}
