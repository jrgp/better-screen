export PKG_CONFIG_PATH := $(CURDIR)/.deps/ghostty-install/share/pkgconfig:$(PKG_CONFIG_PATH)

.PHONY: build deps test test-e2e clean
build: deps
	go build -o bs ./cmd/bs

deps:
	sh scripts/ghostty.sh

test: deps
	go test ./...

test-e2e: deps
	go test -count=1 -v ./e2e

clean:
	rm -f bs ghostty-poc
