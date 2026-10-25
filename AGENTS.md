# Development principles

## Keep it small and correct

* Choose the simplest correct implementation with the least code. Simplicity
  must not compromise correctness, security or required behavior.
* Investigate existing solutions before writing substantial custom code.
  Prefer established, maintained Go libraries and the standard library over
  reinventing solved problems.
* Use idiomatic modern Go and ordinary constructors with explicit dependencies.
  Keep application state out of mutable package globals.
* Prefer composition. Avoid DI frameworks, unnecessary interfaces, wrappers,
  factories, abstractions and package proliferation. Do not add interfaces
  solely for tests or hypothetical future implementations.
* Preserve existing behavior, keybindings and development tooling. Reuse the
  existing PTY, session, layout and rendering code; avoid unrelated refactors
  and speculative features.
* Surface errors clearly. Do not hide failures behind silent defaults, stubs,
  skipped work or success-shaped responses.

## Preserve the architecture

* Keep the application in Go and Ghostty/CGO integration minimal and isolated.
* The daemon owns authoritative session, window, pane and terminal state.
  Serialize native Ghostty access and keep blocking I/O outside the state loop.
* Use Ghostty for emulation, screen reconstruction and history. Do not build
  another terminal parser, competing state model or scrollback implementation.
* Keep IPC, buffers and resource use bounded. Treat terminal output and IPC as
  untrusted; preserve runtime-directory permissions and peer checks.
* Preserve shared window/layout/focus behavior and client-specific menus,
  session switching and scrollback.

## Verify real behavior

* Use `github.com/stretchr/testify` (`require` and `assert`) for Go tests.
* Prioritize comprehensive E2E tests using the actual compiled binary, real
  daemons, sockets, child processes and PTYs. Do not mock major components.
* Inspect rendered output with an established terminal emulator. Assert actual
  contents, attributes, cursor position, process lifecycle and PTY dimensions,
  not merely successful calls or compilation.
* Use isolated temporary HOME directories, deterministic synchronization,
  explicit timeouts and reliable process/socket cleanup. Avoid timing sleeps.
* Add focused regression coverage for fixes. Run the smallest relevant tests
  first, then broader suites when changes affect shared behavior.
* Never weaken, delete or skip failing tests to get green results. Investigate
  the root cause and fix it. Do not claim untested functionality works.
* Run `make test`, `make test-e2e` and race detection for meaningful runtime
  changes. The Go race detector does not instrument native CGO code.

Direct Go commands need the local Ghostty pkg-config path:

```sh
export PKG_CONFIG_PATH="$PWD/.deps/ghostty-install/share/pkgconfig"
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
```

Use `-count=1`: the E2E tests build subprocess binaries, whose source changes
are not fully tracked by Go's test cache. Format Go changes with `gofmt`.

## Keep history and releases safe

* Commit meaningful, working milestones separately after relevant validation.
* Treat Git history as append-only: no amend, rebase, squash, history resets
  or force-push. Fix mistakes with new commits and preserve unrelated changes.
* Create/push release tags only when requested. Use `make release` to increment
  the latest stable patch tag; never move an existing release tag.
* Keep dependency/toolchain versions pinned and releases limited to the two
  specified raw executables. Do not introduce extra distribution infrastructure.
* Continue through implementation, testing and debugging rather than stopping
  at scaffolding or a partially working result. Report genuine external
  blockers and distinguish verified behavior from untested assumptions.

Read [ARCHITECTURE.md](ARCHITECTURE.md) for the code layout, invariants, build
prerequisites, test coverage and release workflow.
