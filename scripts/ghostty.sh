#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
revision=f54340ad77814fe319d136e5e915d6a48de8dd23
prefix="$PWD/.deps/ghostty-install"
if [ -f "$prefix/lib/libghostty-vt.a" ]; then exit 0; fi
mkdir -p .deps
if [ ! -d .deps/ghostty/.git ]; then
    git init -q .deps/ghostty
    git -C .deps/ghostty remote add origin https://github.com/ghostty-org/ghostty.git
fi
git -C .deps/ghostty fetch -q --depth 1 origin "$revision"
git -C .deps/ghostty checkout -q --detach "$revision"
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
"$zig" build -Demit-lib-vt -Doptimize=ReleaseFast -Demit-xcframework=false --prefix "$prefix"
