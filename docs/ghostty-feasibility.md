# Ghostty feasibility gate: PASS

Validated on Linux amd64 with Go 1.26.10, GCC 16, Zig 0.17.0:

* Ghostty: `f54340ad77814fe319d136e5e915d6a48de8dd23` (the Go bindings' own pin).
* Go bindings: `df1957cbff56486c4e1ff89711b4dd043e8e38ff`.
* Sources investigated: [Ghostty](https://github.com/ghostty-org/ghostty),
  [Go bindings](https://github.com/mitchellh/go-libghostty),
  [package documentation](https://pkg.go.dev/go.mitchellh.com/libghostty).

## Evidence

`internal/vt/vt_test.go` compiles and exercises the actual static native library.
`cmd/ghostty-poc` is a standalone executable feeding VT, reading state,
resizing, and emitting reconstructed colored terminal output.

| Requirement | Verified API / evidence |
|---|---|
| Independent emulator instances | `NewTerminal`, `Close`; four concurrent independent instances tested |
| PTY byte ingestion | `VTWrite` accepts streaming bytes |
| Cells, attributes, colors | `GridRef`, `Cell`, `Style`; bold RGB cell round-trip assertions |
| Cursor | `CursorX/Y/Visible`, render-state cursor shape and blink |
| Alternate screen | `1049h/l`; restore active alternate display, then recover primary display |
| Scrollback | `ScrollbackRows`, history/screen grid coordinates; history read without moving viewport |
| Resize | `Resize`; round-trip after resizing 40x8 to 45x10 |
| VT reconstruction | Native `FormatterFormatVT`, rectangular selections, explicit screen positioning |
| UTF-8, wide and combining characters | Native grapheme handling; wide Chinese and combining accent input |
| Multiple concurrent instances | Independent goroutines, one owner per instance |
| Static native linking | Default bindings use `pkg-config --static`; `ldd ghostty-poc` shows libc only, no Ghostty/Zig runtime |

## A parser is not a renderer

Ghostty has an actual **VT formatter**, not just a VT parser. Its C API
(`include/ghostty/vt/formatter.h`) and Go `NewFormatter` expose that renderer.
The formatter emits cell styles and graphemes, but a formatted selection alone
is not a complete physical-terminal restore: it does not clear old content,
position each row, manage the client's outer alternate screen, crop to client
dimensions, or synchronize all input modes.

`internal/vt` supplies that small presentation layer. It obtains bounded,
rectangular rows from the native VT formatter, positions them with CUP, and
restores cursor visibility, position, shape and supported input modes.
Subsequent frames redraw only changed rows. No PTY history replay is involved.
Application alternate screens remain internal to Ghostty; the client uses its
own outer alternate screen. Changing the application's active screen therefore
requires no special historical byte stream.

Raw PTY escapes are never forwarded to the physical terminal. Only the
formatter's screen representation and explicitly allowed modes are rendered.
Clipboard, file, window-management and query side effects are not replayed.

No binding extensions or local modifications to either upstream are needed.
Native state is not thread-safe: callers must serialize all operations for a
given instance, including formatting and destruction. Independent instances can
operate concurrently. Go's race detector does not instrument native code.

## Reproduce

Install Go >=1.26, a C compiler, pkg-config, make and Zig 0.17.0. Then:

```sh
make deps
export PKG_CONFIG_PATH="$PWD/.deps/ghostty-install/share/pkgconfig"
go test -v ./internal/vt
go build -o ghostty-poc ./cmd/ghostty-poc
./ghostty-poc
ldd ./ghostty-poc # Linux; use otool -L on macOS
```

The gate passed **before** implementing daemon/session/client functionality.
Ghostty's public API is still unstable, so both native and Go revisions are
pinned together. Static Ghostty linking is not fully static libc linking.
