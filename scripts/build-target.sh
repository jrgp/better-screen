#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
os="${1:?usage: sh scripts/build-target.sh linux|darwin amd64|arm64}"
arch="${2:?usage: sh scripts/build-target.sh linux|darwin amd64|arm64}"
case "$os/$arch" in
    linux/amd64) target=x86_64-linux-gnu ;;
    linux/arm64) target=aarch64-linux-gnu ;;
    darwin/amd64) target=x86_64-macos ;;
    darwin/arm64) target=aarch64-macos ;;
    *) echo "Unsupported target: $os/$arch" >&2; exit 1 ;;
esac
sdk="${SDKROOT:-}"
if [ "$os" = darwin ]; then
    if [ -z "$sdk" ] && command -v xcrun >/dev/null 2>&1; then
        sdk="$(xcrun --sdk macosx --show-sdk-path)"
    fi
    if [ -z "$sdk" ] || [ ! -d "$sdk" ]; then
        echo "macOS linking needs an Apple SDK: build on macOS with Command Line Tools, or set SDKROOT to a licensed SDK." >&2
        exit 1
    fi
fi
if [ -n "${ZIG:-}" ]; then
    zig="$ZIG"
elif command -v zig >/dev/null 2>&1; then
    zig="$(command -v zig)"
else
    zig="$PWD/.deps/zig-x86_64-linux-0.17.0/zig"
fi
prefix="$PWD/.deps/ghostty-$os-$arch"
ZIG="$zig" sh scripts/ghostty.sh "$target" "$prefix"
mkdir -p dist
compiler="$zig cc -target $target"
if [ "$os" = darwin ]; then compiler="$compiler -isysroot $sdk"; fi
CGO_ENABLED=1 GOOS="$os" GOARCH="$arch" CC="$compiler" \
    PKG_CONFIG_PATH="$prefix/share/pkgconfig" \
    go build -tags netgo,osusergo -trimpath -o "dist/bs-$os-$arch" ./cmd/bs
