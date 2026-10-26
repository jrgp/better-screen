export PKG_CONFIG_PATH := $(CURDIR)/.deps/ghostty-install/share/pkgconfig:$(PKG_CONFIG_PATH)
export VERSION

.PHONY: build deps test test-e2e release clean
build: deps
	go build -tags netgo,osusergo -trimpath -ldflags "$${VERSION:+-X main.version=$$VERSION}" -o bs ./cmd/bs

deps:
	sh scripts/ghostty.sh

test: deps
	go test -count=1 ./...

test-e2e: deps
	go test -count=1 -v ./e2e

release:
	@set -eu; \
	if [ -n "$$(git status --porcelain)" ]; then \
		echo "Commit or stash changes before releasing." >&2; exit 1; \
	fi; \
	git fetch origin --tags; \
	tags="$$(git tag --list --sort=-version:refname)"; \
	latest="$$(printf '%s\n' "$$tags" | awk '/^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$$/ {print; exit}')"; \
	latest="$${latest:-v0.0.0}"; \
	next="$${latest%.*}.$$(( $${latest##*.} + 1 ))"; \
	git push origin HEAD; \
	echo "Releasing $$next"; \
	git tag -a "$$next" -m "Release $$next"; \
	git push origin "refs/tags/$$next" || { \
		echo "Tag $$next remains local; retry: git push origin refs/tags/$$next" >&2; \
		exit 1; \
	}

clean:
	rm -f bs dist/bs-linux-amd64 dist/bs-linux-arm64 dist/bs-darwin-amd64 dist/bs-darwin-arm64
