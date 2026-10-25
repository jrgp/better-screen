# bs - Better Screen

A small GNU Screen-style terminal multiplexer in Go, with static
libghostty-vt terminal emulation. One automatically started, unprivileged
daemon owns sessions, numbered windows, shells, PTYs and scrollback.
Disconnecting or losing an SSH connection leaves them running.

## Build and run

Build prerequisites:

* Go 1.26 or newer, Git, make, a C compiler and pkg-config.
* **Zig 0.17.0**, on PATH or supplied as an absolute `ZIG` path.
* On macOS, Apple's Command Line Tools (`xcode-select --install`).
* Tests additionally require `sh`, `vim`, `less`, `htop`, and installed
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
is pinned in `scripts/ghostty.sh`. Initial builds need Internet access.
The executable embeds Ghostty statically: copying `bs` to another compatible
machine requires **neither Ghostty nor Zig at runtime**. System libc is
dynamically linked; this is not a fully static libc binary.

```sh
./bs                   # new session with an automatically assigned name
./bs -S work           # new named session
./bs -ls               # list sessions, windows and attached-client counts
./bs -r work           # reattach a detached session
./bs -x work           # attach without disconnecting other clients
./bs -d -r work        # detach existing clients, then reattach
./bs -v                # version
```

Exact names take precedence; otherwise an unambiguous name prefix is accepted.
Session names use 1-64 ASCII letters, digits, dots, hyphens or underscores.
Shells use `$SHELL` (otherwise `/bin/sh`), start in HOME, and receive
`TERM=xterm-256color`, `COLORTERM=truecolor` and `BS_SESSION`.
Exiting the last window ends its session.

## Keys

Press **Ctrl-A**, release it, then:

| Key | Action |
|---|---|
| `c` | Create a window |
| `n`, `p` | Next, previous window |
| `0`-`9` | Select a numbered window |
| `"` | Interactive window list |
| `Esc` | Scrollback mode |
| `d` | Detach this client |
| `F` | Fit the session to this client's current dimensions |
| `s` | Interactive session switcher |
| `a` | Send a literal Ctrl-A |
| `?` | Help |

Menus use Up/Down and Enter; Esc cancels. `*` identifies the current session
or window. The session menu shows names, window counts and attachment counts.
It switches only the invoking client, without restarting any process.
Window selection, in contrast, is shared by everyone in that session.

Scrollback uses Up/Down and Page Up/Page Down; Esc returns to live output.
It is client-specific, uses Ghostty's own history, and stays anchored as live
output arrives. If an anchored row is evicted, it advances to the oldest
retained row. **Text selection and clipboard copying are not implemented.**

## Dimensions and rendering

The creating client establishes canonical dimensions. Additional attachments,
SIGWINCH, and disconnects never automatically resize the session.
Ctrl-A F explicitly resizes **all** its windows and their PTYs. Detached
sessions retain their size. Smaller clients see the top-left crop, larger
clients get blank padding, and an off-crop cursor is hidden.

Ghostty's cells are authoritative. Attach, reattach, window changes and menu
exit reconstruct the display without replaying historical PTY bytes or asking
applications to redraw. The native VT formatter preserves attributes,
ANSI/indexed/RGB colors, wide characters, and combining graphemes; a small
presentation layer restores position, visibility, shape and input modes.
OSC palette/default-color overrides are resolved into RGB SGR rather than
mutating the outer terminal's palette. Normal output repaints changed rows,
not the whole screen. Vim, less and htop are exercised by real-process tests.

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

Limits: 32 sessions, 32 windows/session, 128 windows total, 64 connections,
2-500 columns, 2-200 rows, 8 MiB/10,000 lines of history per window
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
go test ./...
go test -race ./...
go vet ./...
```

E2E tests compile and launch the actual binary, actual daemons and actual
PTY-backed clients with isolated HOME directories, explicit deadlines and
cleanup. Ghostty also inspects **rendered physical-terminal output**: assertions
check visible contents, attributes, RGB colors, graphemes and cursor position.
`go test -race` also builds the actual subprocess binary with `-race`.
The Go race detector **does not instrument native Ghostty code**.

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

Linux amd64: full ordinary and race suites pass, including five consecutive
ordinary suite runs and 100 concurrent automatic-startup cycles. A separate real cross-UID check verified filesystem
denial and kernel peer-credential rejection even after deliberately relaxing
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

**Verified here:** Linux amd64 runtime/E2E, and Linux arm64 cross-linking.
The two macOS native static libraries compiled successfully, but final
executable linking and runtime/E2E verification are **not verified** in this
Linux environment: it has no Apple SDK, macOS system frameworks, or macOS
runtime. Initial cross-link attempts failed on missing `libresolv` and
`CoreFoundation`, not on Go source compilation.

`make clean` removes local executables; downloaded toolchains and native
build caches remain in ignored `.deps/`.

See [Ghostty feasibility](docs/ghostty-feasibility.md) for the passed hard
feasibility gate, reproducer, exact native/binding revisions and API evidence.
No Ghostty binding extensions or upstream modifications are needed.

No panes, splits, configuration language, plugins, TCP, recording/replay,
web interface, or Windows support.
