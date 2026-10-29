package mux

import "github.com/charmbracelet/x/ansi"

// Input uses 7-bit prefixes; ansi.CSI and ansi.SS3 are 8-bit controls.
const (
	escape = string(rune(ansi.ESC))
	csi    = escape + "["
	ss3    = escape + "O"

	// x/ansi has no helpers for saving and restoring DEC private modes.
	saveUnicodeCore    = csi + "?2027s"
	restoreUnicodeCore = csi + "?2027r"
)

const (
	opAttach          = "attach"
	opBroadcast       = "broadcast"
	opCatalog         = "catalog"
	opCreate          = "create"
	opDetach          = "detach"
	opEqualize        = "equalize"
	opFit             = "fit"
	opInput           = "input"
	opKill            = "kill"
	opList            = "list"
	opLive            = "live"
	opMoveLeft        = "move-left"
	opMoveRight       = "move-right"
	opNew             = "new"
	opNext            = "next"
	opPaneKill        = "pane-kill"
	opPaneNext        = "pane-next"
	opPanePrev        = "pane-prev"
	opPrev            = "prev"
	opReload          = "reload"
	opScroll          = "scroll"
	opSelect          = "select"
	opSize            = "size"
	opSplitHorizontal = "split-h"
	opSplitVertical   = "split-v"
	opSwitch          = "switch"
)
