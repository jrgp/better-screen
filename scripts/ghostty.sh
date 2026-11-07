#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
revision=0fa3d577a5614150ff8e2c152de5fb9d5c6d2dec # vt-1.0.0
target="${1:-native}"
prefix="${2:-$PWD/.deps/ghostty-install}"
if [ -f "$prefix/lib/libghostty-vt.a" ] && [ -f "$prefix/bs-revision" ] &&
    [ "$(cat "$prefix/bs-revision")" = "$revision $target baseline" ]; then exit 0; fi
mkdir -p .deps
if [ ! -d .deps/ghostty/.git ]; then
    git init -q .deps/ghostty
    git -C .deps/ghostty remote add origin https://github.com/ghostty-org/ghostty.git
fi
if [ "$(git -C .deps/ghostty rev-parse HEAD 2>/dev/null || true)" != "$revision" ]; then
    git -C .deps/ghostty fetch -q --depth 1 origin "$revision"
    git -C .deps/ghostty checkout -q --detach "$revision"
fi
if [ -n "${ZIG:-}" ]; then
    zig="$ZIG"
elif command -v zig >/dev/null 2>&1; then
    zig=zig
elif [ -x .deps/zig-x86_64-linux-0.17.0/zig ]; then
    zig="$PWD/.deps/zig-x86_64-linux-0.17.0/zig"
else
    echo "Install Zig 0.17.0 and put it on PATH (or set ZIG)." >&2
    exit 1
fi
cd .deps/ghostty
if [ "$("$zig" version)" != 0.17.0 ]; then
    echo "This Ghostty revision requires pinned Zig 0.17.0." >&2
    exit 1
fi
# The shallow commit checkout has no release tags for version detection.
set -- build -j2 -Demit-lib-vt -Dlib-version-string=1.0.0 -Doptimize=ReleaseFast -Demit-xcframework=false -Dcpu=baseline --prefix "$prefix"
if [ "$target" != native ]; then set -- "$@" "-Dtarget=$target"; fi
"$zig" "$@"
printf '%s %s baseline\n' "$revision" "$target" > "$prefix/bs-revision"
