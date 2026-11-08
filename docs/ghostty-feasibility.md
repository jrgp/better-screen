# Ghostty feasibility gate: PASS

Validated on Linux amd64 with Go 1.26.10, GCC 16, Zig 0.17.0:

* libghostty-vt **1.0.0**:
  [official source tarball](https://release.files.ghostty.org/libghostty-vt/1.0.0/libghostty-vt-1.0.0.tar.gz),
  verified against the SHA-256 pinned in `scripts/ghostty.sh`.
* Official Go bindings: `b13f277ddcb208deea304745dd5240024a0e8be4`,
  with upstream 1.0 compatibility.
* Sources investigated: [Ghostty](https://github.com/ghostty-org/ghostty),
  [Go bindings](https://tangled.org/mitchellh.com/go-libghostty),
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

No additional CGO wrapper, dependency fork or modifications to Ghostty are needed.
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
remain pinned for reproducible builds. The official tarball supplies its release
version metadata; tests check the compiled headers and linked library version.
Static Ghostty linking is not fully static libc linking.
