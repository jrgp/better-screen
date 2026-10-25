# Status, splits, and configuration: E2E coverage

All names below are subtests of `TestE2E`. The harness builds the actual `bs`
command and runs real daemons, Unix sockets, shells, job-control groups, and
PTY-backed clients under isolated HOME directories. Ghostty inspects the
physical-terminal output; no terminal parser or major component is mocked.
Tests use explicit deadlines and process/socket cleanup.

| Requirement | Real-binary E2E test | Observable assertions |
|---|---|---|
| Missing/default and explicit enabled configuration | `json-configuration`, `status-configuration-*` | Successful startup; status appears on the final row; application PTY has one fewer row. |
| Disabled status | `status-configuration-*`, `disabled-status-pane-dimensions` | Actual PTY gains its row; applications can write the last physical row, including inside a split. |
| Malformed, invalid-type, unknown, oversized JSON | `json-configuration` | Both CLI and daemon reject with explicit errors; no socket is published. |
| Final-row status placement and application confinement | `status-configuration-*`, `nested-splits-junctions-and-output-confinement` | Cursor-addressed bottom output remains above status; split borders stop above it. |
| Ascending numbering and multi-digit picker | `status-picker-colors-order-resize-and-reattach` | Exact row text through windows 0-11; gap removal and reused numbers remain sorted. |
| Fixed active accent and default inactive foreground | `status-picker-colors-order-resize-and-reattach` | Ghostty cell styles for every digit, including multi-digit windows; only the selected number uses RGB 135/206/250. |
| Switching changes highlight, not picker geometry | `status-picker-colors-order-resize-and-reattach` | Identical plain row before/after selection; changed accent; no whole-screen clear. |
| Named/unnamed right-aligned identifier | `status-unnamed-identifier`, `status-picker-colors-order-resize-and-reattach` | Exact column placement; unnamed ID agrees with `bs -ls` and existing attach matching. |
| Status after creation, closure, reattachment, fit | `status-picker-colors-order-resize-and-reattach` | Exact numbered row after each operation and after native terminal resizing. |
| Physical resizing without changing canonical size | `status-physical-resize-without-canonical-fit` | Status moves to each client's own final row/right edge; `stty size` stays unchanged until Ctrl-A F. |
| Vertical and horizontal splits | `pane-pty-sizes-removal-and-canonical-fit`, `disabled-status-pane-dimensions` | Independent shells, actual reduced PTY sizes, correct divider positions. |
| Nested layout and single-cell junctions | `nested-splits-junctions-and-output-confinement`, `nested-pane-exits-collapse-without-restarting-survivors` | Native cells for vertical/horizontal borders, cross and T junctions, and fixed RGB accent. |
| Independent input routing | `splits-focus-windows-multiattach-and-reattach` | Persistent shell tokens and output occur only in the focused pane's rectangular region. |
| Forward/backward cycling and wraparound | `splits-focus-windows-multiattach-and-reattach`, `nested-splits-junctions-and-output-confinement` | Token checks across forward/backward wrap; fragmented Shift-Tab; deterministic traversal across four panes. |
| New windows are independent; whole-window switching | `splits-focus-windows-multiattach-and-reattach` | New window has one pane; next/previous/select restore the original layout and focused shell token. |
| Shared multi-attach layout and focus | `splits-focus-windows-multiattach-and-reattach` | A second client inputs into the pane selected by the first; both reach identical screen contents. |
| Layout and focus after detach/reattach | `splits-focus-windows-multiattach-and-reattach` | Native border positions and shell tokens persist; another attached client remains functional. |
| Uppercase K kills only focused pane | `pane-kill-cleans-jobs-but-lowercase-k-kills-whole-window` | Survivor PID/token remain; victim shell plus foreground/background jobs disappear. |
| Lowercase k compatibility | `pane-kill-cleans-jobs-but-lowercase-k-kills-whole-window` | A nested multi-pane window closes completely; its processes and daemon are cleaned up. |
| Natural shell and exec-replaced process exits | `nested-pane-exits-collapse-without-restarting-survivors` | Pane vanishes, parent collapses, and survivor PID/token are unchanged with expanded `stty size`. |
| Background exit preserves surviving focus | `pane-scrollback-and-background-exit-preserve-focus` | FIFO-released background pane exits; foreground shell token remains selected after reflow. |
| Final pane/window/session lifecycle | `nested-pane-exits-collapse-without-restarting-survivors`, existing `bash-exit-*`, `kill-current-*` | Final pane removes its window, next window survives, final session disconnects clients and exits daemon with socket removal. |
| Concurrent exits and kills | `simultaneous-pane-exits-and-kills` | Four real pane processes exit against a kill request, repeated three times; all PIDs disappear; a separate session remains usable. |
| PTY sizing after nested splits/removals/fit | `pane-pty-sizes-removal-and-canonical-fit` | Exact `stty size` after each geometry change, with surviving process identity/state. |
| Tiny terminals and impossible splits | `tiny-canonical-layout-preserves-hidden-processes` | Fits down to 4x2, cycles focus, reports impossible split, then restores borders and original PIDs/tokens at larger size. |
| Full-screen applications confined to panes | `fullscreen-applications-inside-panes/{vim,less,htop}` | Real applications restore after detach without disturbing the other pane, borders, or styled Unicode text. |
| UTF-8, wide/combining characters, attributes | `fullscreen-applications-inside-panes/*`, `pane-alternate-screen-cursor-and-cropped-clients` | Visible wide/combining text and native bold/RGB cell attributes after reattachment. |
| Alternate screen and cursor restoration | `pane-alternate-screen-cursor-and-cropped-clients`, `fullscreen-applications-inside-panes/*` | Sleeping alternate-screen state restores without application output; translated cursor coordinates, focus cursor, and primary-screen return are correct. |
| Differently sized attached clients | `pane-alternate-screen-cursor-and-cropped-clients`, `status-physical-resize-without-canonical-fit` | Canonical layout is cropped, status fits physical size, and an off-crop focused cursor is hidden. |
| Pane-local colors and reverse video | `pane-local-reverse-video-and-default-colors` | Physical reverse mode stays off; default/explicit/inverse cell colors and blank background are correct; other pane, borders, and status remain unchanged. |
| Existing client-specific scrollback | `pane-scrollback-and-background-exit-preserve-focus`, existing `scrollback`, `history-pruning` | Only focused pane scrolls for that client; the other client continues live input; native history remains usable. |
| Incremental rendering | `status-picker-colors-order-resize-and-reattach`, existing `incremental-physical-rendering` | No whole-screen clear for window switching or ordinary output; unchanged rows are not repainted. |

## Verification commands

```sh
make test
make test-e2e
export PKG_CONFIG_PATH="$PWD/.deps/ghostty-install/share/pkgconfig"
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
make build
```

Race builds also compile the real subprocess command with `-race`.
The Go race detector does not instrument native Ghostty code. Linux amd64 is
runtime-verified; macOS runtime verification still requires a macOS host.

Existing daemons retain their binary and startup configuration. Finish their
sessions before restarting to load the new binary or configuration. Layouts
are in-memory daemon state, not persistent across daemon restart.
