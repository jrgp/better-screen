export PKG_CONFIG_PATH := $(CURDIR)/.deps/ghostty-install/share/pkgconfig:$(PKG_CONFIG_PATH)
export VERSION

.PHONY: build deps test test-e2e clean
build: deps
	go build -tags netgo,osusergo -trimpath -ldflags "$${VERSION:+-X main.version=$$VERSION}" -o bs ./cmd/bs

deps:
	sh scripts/ghostty.sh

test: deps
	go test -count=1 ./...

test-e2e: deps
	go test -count=1 -v ./e2e

clean:
	rm -f bs ghostty-poc dist/bs-linux-amd64 dist/bs-linux-arm64 dist/bs-darwin-amd64 dist/bs-darwin-arm64
