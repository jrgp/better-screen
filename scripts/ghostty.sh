#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
version=1.0.0
checksum=4a6ef758be721923e1969e7b81b443dcc92d8dd3fb5a39600c03a8817a753676
target="${1:-native}"
prefix="${2:-$PWD/.deps/ghostty-install}"
identity="$version $checksum $target baseline"
if [ -f "$prefix/lib/libghostty-vt.a" ] && [ -f "$prefix/bs-revision" ] &&
    [ "$(cat "$prefix/bs-revision")" = "$identity" ]; then exit 0; fi
mkdir -p .deps
source="$PWD/.deps/libghostty-vt-$version"
if [ ! -f "$source/bs-source-sha256" ] ||
    [ "$(cat "$source/bs-source-sha256")" != "$checksum" ]; then
    archive="$PWD/.deps/libghostty-vt-$version.tar.gz"
    curl -fLsS --retry 3 -o "$archive" \
        "https://release.files.ghostty.org/libghostty-vt/$version/libghostty-vt-$version.tar.gz"
    if command -v sha256sum >/dev/null 2>&1; then
        printf '%s  %s\n' "$checksum" "$archive" | sha256sum -c -
    else
        printf '%s  %s\n' "$checksum" "$archive" | shasum -a 256 -c -
    fi
    tar -xzf "$archive" -C .deps
    printf '%s\n' "$checksum" > "$source/bs-source-sha256"
    rm -f "$archive"
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
cd "$source"
if [ "$("$zig" version)" != 0.17.0 ]; then
    echo "This Ghostty revision requires pinned Zig 0.17.0." >&2
    exit 1
fi
set -- build -j2 -Demit-lib-vt -Doptimize=ReleaseFast -Demit-xcframework=false -Dcpu=baseline --prefix "$prefix"
if [ "$target" != native ]; then set -- "$@" "-Dtarget=$target"; fi
"$zig" "$@"
printf '%s\n' "$identity" > "$prefix/bs-revision"
