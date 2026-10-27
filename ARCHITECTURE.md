# Architecture and development

## State ownership

One automatically started, unprivileged daemon per Unix user owns sessions,
numbered windows, split layouts and pane focus. Each pane owns a PTY, child
process, Ghostty terminal instance and native scrollback. Disconnecting or
losing an SSH connection leaves them running.

The daemon's event loop serializes access to session and Ghostty state.
PTY and socket workers perform blocking I/O outside that loop. Clients handle
physical terminal input, rendering, terminal-mode restoration and client-local
menus/scrollback. Window selection, pane focus, layouts and canonical dimensions
are session-wide; session switching and scrollback views are client-specific.

Code is kept in a few packages:

* `cmd/bs`: entry point and link-time release version metadata.
* `internal/mux`: CLI, configuration, daemon, clients, IPC, PTYs and layouts.
* `internal/vt`: isolated Ghostty integration, snapshots and incremental drawing.
* `e2e`: real-binary, process, PTY and rendered-terminal tests.
* `scripts`: pinned native-library builds and target-specific compilation.

## Building

Build prerequisites:

* Go 1.26 or newer, Git, make, a C compiler and pkg-config.
* **Zig 0.17.0**, on PATH or supplied as an absolute `ZIG` path.
* On macOS, Apple's Command Line Tools (`xcode-select --install`).
* Tests additionally require `sh`, `bash`, `vim`, `less`, `htop`, and installed
  `xterm-256color` terminfo.

For example, on Fedora:

```sh
sudo dnf install golang gcc make pkgconf-pkg-config git vim less htop ncurses-term
# Install Zig 0.17.0 from https://ziglang.org/download/ and put it on PATH.
make build
./bs -S work
```

On macOS, install the Command Line Tools and the same prerequisites through
your preferred package manager. Use Zig **0.17.0**, not a newer development
release.

`make build` fetches and builds the pinned native library into `.deps/`.
Go dependencies are pinned in `go.mod`/`go.sum`; the matching native revision
is pinned in `scripts/ghostty.sh`. Ghostty uses baseline CPU settings rather
than the build host's optional instructions. Initial builds need Internet access.
The executable embeds Ghostty statically: copying `bs` to another compatible
machine requires **neither Ghostty nor Zig at runtime**. System libc is
dynamically linked; this is not a fully static libc binary.

To set a release version locally, use `make build VERSION=v0.2.0`;
`./bs -v` then reports `bs v0.2.0`. Ordinary development builds retain their
default version. `VERSION` also works with `scripts/build-target.sh`.

## Process lifecycle

Shells use `$SHELL` (otherwise `/bin/sh`) and receive
`TERM=xterm-256color`, `COLORTERM=truecolor` and `BS_SESSION`.
Each session remembers its creating client's working directory. New windows
and split panes start there, regardless of later shell directory changes or
reattachment from elsewhere. If that directory is gone or no longer a directory,
they start in `$HOME` instead. If HOME is also missing, bs leaves the working
directory unset for the shell to handle; it does not create HOME.
When a pane's shell or its exec-replacement exits, that pane is removed.
Closing the final pane removes its window.
Closing the last window ends its session and disconnects its clients.
When no sessions remain, the daemon exits and removes its socket.
Creating a new session automatically starts it again; `bs -ls` does not
start a daemon just to report an empty session list.

## Windows and panes

Layouts are binary split trees: leaves are panes, and internal nodes contain
an axis and two children. Removing a pane replaces its parent with the surviving
sibling subtree. There is no independent terminal-state or history implementation.

Ordinary new windows start with one pane. Splits divide the focused pane
approximately equally, keep its process, start a new shell, and focus the new
pane. Splits can nest in either direction; single-cell light blue borders meet
at junctions. Input normally goes only to the focused pane. Tab order is a
deterministic tree traversal: left before right and top before bottom, wrapping
at either end.

`--panes` consumes the remaining command arguments and creates one window with
exactly one pane per command, each using the daemon's configured shell with `-c`.
It may be combined only with `-S`. A fitting row count is chosen using terminal
cell proportions, then balanced horizontal/vertical subtrees reuse the existing
layout placement. Geometry and resource limits are checked before any process
starts; each PTY receives its final pane size at launch. The first command is
focused, traversal follows command order, and existing exit/reflow handling
removes completed commands. Partial process-start failures stop panes already
started for that window.

Ctrl-A B toggles a daemon-owned boolean on the current window. When enabled,
keyboard input requests fan out through existing bounded PTY queues to every
current leaf; new panes participate automatically. Prefix commands are separate
IPC operations, not input, and native terminal responses still go only to their
own pane. Broadcast is initially off, survives window switching and reattachment,
and is shared by all clients; it does not affect other windows.

Switching windows preserves each window's layout and focused pane. Exiting or
killing a pane collapses its parent split and expands the surviving subtree;
surviving processes are not restarted. Lowercase `k` still kills the entire
window; uppercase `K` kills just its focused pane.

Window numbers are their positions in the session's ordered window list.
Closing a window renumbers survivors from zero without changing their order;
new windows append to the end. Next/previous wrap through this same order,
matching numeric selection, the picker and the window menu.

If a fitted terminal becomes too small for the tree, only the focused pane
is displayed. Hidden panes stay alive at 1x1; cycling focus still works.
Fitting a larger terminal restores the saved layout. Splits that cannot fit
are rejected with a visible error. There is no manual pane resizing or mouse
pane control.

## Status line and configuration

By default the final physical terminal row is reserved for a status line:
ascending `number:title` window labels on the left, and the session name on the
right. Ghostty's OSC 0/2 title is stored per pane; a window exposes its focused
pane's title. Empty titles fall back to the shell name. No process inspection
or title guessing is involved.
An automatically assigned session identifier is shown for unnamed sessions,
matching `bs -ls` and `bs -x`. The active window label is light blue
(RGB 135, 206, 250), matching split borders; other text uses default colors.
Window switching never adds decorations or moves the picker. Narrow terminals
clip the picker to leave room for the right-aligned identifier.

Configuration lives in `~/.bs/config.json`. `status_line` is a boolean,
defaulting to `true`; `false` returns the row to applications. The other three
settings are Go `text/template` strings:

| Setting | Default |
|---|---|
| `status_line_left` | Window loop shown below |
| `status_line_center` | Empty |
| `status_line_right` | `{{ .Session.Name }}` |

```gotemplate
{{ range .Windows }}{{ if .Active }}{{ highlight (printf "%d:%s" .Number .Title) }}{{ else }}{{ .Number }}:{{ .Title }}{{ end }} {{ end }}
```

Each section receives copied, read-only `.Windows` values (`.Number`, `.Title`,
`.Active`, `.Broadcast`) and `.Session` (`.Name`, `.ID`). `.ID` is the existing
attach identifier; `.Name` is the session name, or that identifier for unnamed sessions.
The `highlight` function applies the existing active-window accent, not a
template-configurable color. The default keeps identical spacing when switching;
custom conditionals should likewise avoid adding active-only decorations.
Templates may use `.Broadcast` to add an indicator; the defaults remain unchanged.

Sections use terminal-cell widths and grapheme-aware clipping. Centering is
relative to the full terminal width, not the space remaining between left and
right. Overlap clips center first, then left, preserving right. A section
extending beyond the terminal is clipped to fit. Templates and application
titles cannot inject terminal controls; only the known highlight styling is
allowed through. Template output is bounded to 64 KiB per section.

Missing keys use defaults; explicitly empty strings render nothing. Templates
are parsed at startup and on explicit reload, not on redraw. Invalid JSON,
unknown settings, invalid types, template parse/execution errors and files over
64 KiB report explicit errors.
Ctrl-A R asks the central daemon to reload the entire configuration. It parses
all templates and checks their execution against every session before replacing
the active configuration in its state-owning event loop. Failures report to the
invoking client without replacing the previous settings. Success immediately
redraws all attached clients; toggling the status line reflows every window and
resizes its PTYs, including inactive windows and detached sessions. A resize
failure restores the previous configuration and attempts to restore all layouts,
reporting any restoration errors. Running processes and pane focus are preserved.
There are no filesystem watchers or automatic reloads.

## Dimensions and rendering

The creating client establishes canonical dimensions. Additional attachments,
SIGWINCH, and disconnects never automatically resize the session.
Ctrl-A F explicitly resizes **all** its windows and their PTYs. Detached
sessions retain their size. Pane PTY sizes exclude borders and the optional
status row. Smaller clients see the top-left crop, larger
clients get blank padding, and an off-crop cursor is hidden.

Ghostty's cells are authoritative. Attach, reattach, window changes and menu
exit reconstruct the display without replaying historical PTY bytes or asking
applications to redraw. The native VT formatter preserves attributes,
ANSI/indexed/RGB colors, wide characters, and combining graphemes; a small
presentation layer restores position, visibility, shape and input modes.
OSC palette/default-color overrides are resolved into RGB SGR rather than
mutating the outer terminal's palette. Reverse-video effects stay inside
their pane. Normal output repaints changed rows,
not the whole screen. Vim, less and htop are exercised by real-process tests.

Ghostty defaults to Unicode grapheme widths (DEC mode 2027), so skin tones,
joined emoji, flags and combining marks occupy the correct cells. Snapshots
carry each pane's width mode, including in scrollback; client UI uses Unicode
widths, and detaching restores the physical terminal's previous mode. The local
terminal must support these widths and have fonts for the requested glyphs;
bs cannot supply fonts or fix a terminal that ignores mode 2027.

Scrollback uses Ghostty's own history and stays anchored as live output arrives.
If an anchored row is evicted, the client advances to the oldest retained row.
Text selection and clipboard copying are not implemented.

## Runtime and safety

Runtime files live under `~/.bs/` (0700), with `bs.sock` (0600).
Ownership, permissions, file type and symlinks are checked. Linux uses
`SO_PEERCRED`; macOS uses `LOCAL_PEERCRED`. A protected flock file prevents
duplicate daemon starts; there are no PID files or network listeners.
Stale sockets are replaced only after safe validation and connection refusal.
There is no service installation.

Socket and PTY workers perform I/O outside the state-owning event loop.
Only that loop accesses native terminal state. Complete pending frames are
coalesced; a blocked client cannot stall another. IPC uses standard-library
JSON and a four-byte length prefix, with allocation bounds.

Limits: 32 sessions, 32 windows/session, 32 panes/window, 128 PTYs total,
64 connections, 2-500 columns, 2-200 rows, 8 MiB/10,000 lines of history per pane
(Ghostty's byte limit is approximate), 64 KiB request frames, 8 MiB response
frames and bounded PTY queues. Blocked socket writes time out after 3 seconds.
Clipboard/file/image escape effects are not forwarded; Kitty file/shared-memory
image access is disabled. Screen hyperlinks and ordinary styled text are
formatted by Ghostty, not passed through as raw application output.

Normal daemon shutdown terminates PTY-session job-control groups and reaps
the shells. Explicitly daemonized processes that leave the PTY's Unix session
are independent processes, just as outside a multiplexer.
Sessions do **not** survive daemon crashes or reboot.
SIGKILL cannot restore a killed client's physical terminal; use `stty sane`
and `reset` if necessary.

## Tests

```sh
make test
make test-e2e

# For direct Go commands:
export PKG_CONFIG_PATH="$PWD/.deps/ghostty-install/share/pkgconfig"
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
```

E2E tests compile and launch the actual binary, actual daemons and actual
PTY-backed clients with isolated HOME directories, explicit deadlines and
cleanup. Ghostty also inspects **rendered physical-terminal output**: assertions
check visible contents, attributes, RGB colors, graphemes and cursor position.
`go test -race` also builds the actual subprocess binary with `-race`.
The Go race detector **does not instrument native Ghostty code**.
Use `-count=1` to avoid cached E2E results: those tests build the command as a
subprocess, so Go's test cache does not track every daemon/client source file.
Both Makefile test targets already disable this cache.

Coverage includes all 15 requested scenarios: command execution; detach and
authoritative reattach; multiple windows; simultaneous clients; shared input
and output; independent detach; session-menu navigation; client-specific
switching; cancel; actual PTY sizes after fitting; scrollback navigation;
alternate-screen restoration; abrupt client death; process cleanup; and
socket permissions/access controls. Additional cases exercise actual Vim,
less and htop, background jobs, history eviction, dynamic colors, terminal-mode
restoration, literal Ctrl-A, malformed/versioned IPC, slow clients, unsafe
paths, CLI errors, concurrent automatic startup, stale sockets and incremental
physical output.

Status, split, and configuration coverage is mapped to real-binary tests in
[the coverage matrix](docs/status-splits-coverage.md). This includes multi-digit
picker colors, independently sized clients, nested junctions, shared focus,
concurrent pane exits/kills, process identity and job cleanup, tiny terminals,
pane-local colors and reverse video, alternate screens, and Vim/less/htop
inside panes.

Linux amd64 and macOS arm64: full uncached ordinary and race suites pass on
native GitHub-hosted runners. Linux also has repeated local layout and
lifecycle stress runs. A separate real Linux cross-UID check verified filesystem
denial and kernel peer-credential rejection after deliberately relaxing
socket permissions.

## Other build targets

The native build supports Linux amd64/arm64 and macOS amd64/arm64. To create a
target-specific binary in `dist/`:

```sh
sh scripts/build-target.sh linux amd64
sh scripts/build-target.sh linux arm64
sh scripts/build-target.sh darwin amd64
sh scripts/build-target.sh darwin arm64
```

Cross builds use Zig as the C compiler and a separately pinned native-library
installation for each target. macOS linking requires a licensed Apple SDK:
on macOS the script discovers it with `xcrun`; elsewhere supply `SDKROOT`.
No SDK or toolchain is required on the destination machine.

**Verified:** Linux amd64 and macOS arm64 native builds, executable/linking
checks, and full ordinary/race E2E suites on GitHub-hosted runners; Linux amd64
also runs locally, and Linux arm64 cross-links. The macOS amd64 native static
library compiled, but its final executable linking and runtime/E2E tests remain
unverified. Cross-linking macOS executables from this Linux machine still
requires an Apple SDK; hosted macOS builds use the runner's native SDK.

`make clean` removes local executables; downloaded toolchains and native
build caches remain in ignored `.deps/`.

See [Ghostty feasibility](docs/ghostty-feasibility.md) for the passed hard
feasibility gate, reproducer, exact native/binding revisions and API evidence.
No Ghostty binding extensions or upstream modifications are needed.

## Publishing a release

[`.github/workflows/release.yml`](.github/workflows/release.yml) runs on pushed
`v*` tags. It builds natively on Ubuntu 24.04 amd64 and macOS 15 arm64 using
Go 1.26.10, Zig 0.17.0, and the existing pinned Ghostty revision. Actions are
pinned to commit SHAs; no additional secrets, services, or release framework
are required.

Both platforms run the complete Go suite, including real-process PTY E2E tests,
and the race suite. Release binaries must execute, report the exact tag, contain
defined Ghostty symbols, and have build metadata matching the tagged commit.
`file`, `readelf`/`ldd`, and `otool -L` validate architecture and reject dynamic
dependencies outside the allowed OS libraries.

Only after both jobs pass are their executables transferred to a publishing
job. It creates a draft, uploads the two raw assets, verifies the exact asset
set, then publishes. Failed builds or validation cannot publish a new release.
Only the publishing job has `contents: write`; builds have read-only access.
Tags containing `-` are marked as prereleases. A failed upload leaves a draft,
not an incomplete public release; rerun the failed job after fixing the cause.
GitHub's automatically provided source-code archive links are not binary assets.

Commit the desired code, then run:

```sh
make release
```

This requires a clean worktree, fetches tags from `origin`, increments the patch
number of the highest stable `vMAJOR.MINOR.PATCH` tag, pushes the current branch,
and creates and pushes an annotated tag at HEAD. For example, `v0.0.1` becomes
`v0.0.2`; without stable tags it starts at `v0.0.1`. Prerelease and unrelated tags
are ignored. GitHub Actions then builds and publishes the release.
If the tag push fails, the error prints the exact push command to retry; the
local tag is preserved.

The release and both executables use the new tag as their version.
Do not move an existing release tag; use a new version for fixes.
Push the workflow to the default branch before the first tag so GitHub registers
it. Tag pushes that happened before registration are not replayed.

To retry an existing tag without deleting or moving it:

```sh
gh workflow run release.yml --ref master -f tag=v0.2.0
```

Omit `-f tag=...` to validate the selected ref on both platforms without
publishing a release or creating a tag; these validation artifacts report `dev`.

[Hosted validation](https://github.com/jrgp/better-screen/actions/runs/40218373737)
passed on both native platforms, including full ordinary/race suites, executable
version/provenance checks, defined Ghostty symbols and OS-only dynamic libraries.
The validation-only run intentionally did not publish a release. Workflow
syntax and publication asset-set guards are also checked locally.

The initial `v0.0.1` tag predates hosted-runner test-fixture corrections. Its
release attempt failed tests and did not publish; use a new tag containing the
fixes rather than moving the existing tag.

No themes, configurable bindings/layouts, manual pane resizing, floating panes,
mouse pane controls, plugins, TCP, recording/replay, web interface, or Windows
support.
