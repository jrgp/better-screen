package mux

import (
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
