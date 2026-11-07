# Ghostty feasibility gate: PASS

Validated on Linux amd64 with Go 1.26.10, GCC 16, Zig 0.17.0:

* libghostty-vt **1.0.0** (`vt-1.0.0`):
  `0fa3d577a5614150ff8e2c152de5fb9d5c6d2dec`.
* Go bindings: `e247028bdf51b04e9a0fe89459d2abb6e72993a5`.
  Stable-ABI fixes are temporarily pinned to fork commit
  `a418e0549948`; [upstream PR #21](https://github.com/mitchellh/go-libghostty/pull/21)
  updates renamed C constants, a grid-reference pointer argument and
  absent-color result handling without changing the Go API.
* Sources investigated: [Ghostty](https://github.com/ghostty-org/ghostty),
  [Go bindings](https://github.com/mitchellh/go-libghostty),
  [package documentation](https://pkg.go.dev/go.mitchellh.com/libghostty).

## Evidence

`internal/vt/vt_test.go` compiles and exercises the actual static native library.
The initial standalone proof of concept has been retired; its VT ingestion,
state inspection, resizing and colored reconstruction checks are covered by
these tests.

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
| Static native linking | Default bindings use `pkg-config --static libghostty-vt-static`; `ldd ./bs` shows libc only, no Ghostty/Zig runtime |

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

No additional CGO wrapper or modifications to Ghostty are needed. The temporary
binding compatibility fork is documented above; its full tests pass against 1.0.
Native state is not thread-safe: callers must serialize all operations for a
given instance, including formatting and destruction. Independent instances can
operate concurrently. Go's race detector does not instrument native code.

## Reproduce

Install Go >=1.26, a C compiler, pkg-config, make and Zig 0.17.0. Then:

```sh
make build
export PKG_CONFIG_PATH="$PWD/.deps/ghostty-install/lib/pkgconfig"
go test -count=1 -v ./internal/vt
./bs -v
ldd ./bs # Linux; use otool -L on macOS
```

The gate passed **before** implementing daemon/session/client functionality.
The native C ABI is stable from libghostty-vt 1.0. Both native and Go revisions
remain pinned for reproducible builds. The build explicitly stamps version
1.0.0 because its shallow commit checkout has no release tags; tests check the
compiled headers and linked library version. Static Ghostty linking is not
fully static libc linking.
