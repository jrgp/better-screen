# bs - Better Screen

A GNU Screen-style terminal multiplexer for Linux and macOS. Keep shells and
full-screen programs running across SSH disconnects, with familiar Ctrl-A keys.

## Features

* Named sessions and numbered windows, with automatic daemon startup.
* Nested horizontal/vertical splits and independent shells in each pane.
* Multi-attach with shared window selection, layouts and pane focus.
* Interactive session/window switchers and client-specific scrollback.
* Reliable reattachment to Vim, less and htop, with colors and Unicode intact.
* An optional status line showing windows and the current session.

## Install

Download a standalone executable: no Go, Zig or Ghostty installation is needed.

### Linux amd64

```sh
mkdir -p "$HOME/.local/bin" &&
curl -fL https://github.com/jrgp/better-screen/releases/latest/download/bs-linux-amd64 -o "$HOME/.local/bin/bs.new" &&
chmod 755 "$HOME/.local/bin/bs.new" &&
mv -f "$HOME/.local/bin/bs.new" "$HOME/.local/bin/bs" &&
export PATH="$HOME/.local/bin:$PATH" &&
bs -v
```

### macOS arm64 (Apple Silicon)

```sh
mkdir -p "$HOME/.local/bin" &&
curl -fL https://github.com/jrgp/better-screen/releases/latest/download/bs-darwin-arm64 -o "$HOME/.local/bin/bs.new" &&
chmod 755 "$HOME/.local/bin/bs.new" &&
mv -f "$HOME/.local/bin/bs.new" "$HOME/.local/bin/bs" &&
export PATH="$HOME/.local/bin:$PATH" &&
bs -v
```

Add `~/.local/bin` to your shell's startup PATH to keep `bs` available in future
terminals. Existing sessions keep their current daemon until they finish.

## Usage

```sh
bs                   # create and attach a new session
bs -S work           # create a named session
bs -ls               # list sessions
bs -r work           # reattach to a detached session
bs -x work           # attach alongside other clients
bs -x                # attach when exactly one session exists
bs -d -r work        # detach other clients and reattach
bs -v                # print version
```

Unambiguous session-name prefixes work. Detaching leaves programs running.
Exiting the final pane closes its window; the final window closes its session.

### Keys

Press **Ctrl-A**, release it, then:

| Key | Action |
|---|---|
| `c` | Create window |
| `n` / `p` | Next / previous window |
| `0`-`9` | Select numbered window |
| `"` | Window list |
| `s` | Session switcher |
| `\|` / `S` | Split left/right / top/bottom |
| `Tab` / `Shift-Tab` | Next / previous pane |
| `K` | Kill focused pane |
| `k` | Kill whole window |
| `Esc` | Scrollback |
| `F` | Fit session to this terminal |
| `d` | Detach this client |
| `a` | Send literal Ctrl-A |
| `?` | Help |

Menus use arrow keys, Enter to select and Esc to cancel. Session switching
affects only your client; window selection and pane focus are shared.
Scrollback uses Up/Down and Page Up/Page Down; Esc returns to live output.
Text selection and clipboard copying are not implemented.

The first client sets session dimensions; additional clients do not resize it.
Use Ctrl-A F to fit explicitly. Very small terminals temporarily show only the
focused pane; fitting a larger terminal restores the layout.

### Status line

The status line is enabled by default. To disable it, put this in
`~/.bs/config.json`:

```json
{"status_line": false}
```

Configuration loads when the daemon starts. Finish existing sessions before
starting a new daemon to apply changes.

### Build and release

```sh
make build           # build ./bs from source
make release         # increment the latest stable patch tag and push it
```

See [ARCHITECTURE.md](ARCHITECTURE.md) for build prerequisites, internals,
testing and release details, and [AGENTS.md](AGENTS.md) for contributor guidance.
