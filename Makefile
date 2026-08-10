# All source builds run inside the Nix dev shell (`nix develop`, or
# `direnv allow` once), which provides go, pkg-config, and the prebuilt
# libghostty-vt from the flake. `make install` instead builds the
# *committed* tree via `nix build` — uncommitted changes only show up
# in `make twee`.

VERSION := $(shell jj log -r @ -T 'change_id.short()' --no-graph 2>/dev/null || echo dev)
LDFLAGS := -X main.Version=$(VERSION)

.PHONY: all build test coverage install clean check-env

all: build

check-env:
	@pkg-config --exists libghostty-vt || { \
		echo "libghostty-vt is not on the pkg-config path." >&2; \
		echo "Enter the dev shell first: nix develop (or direnv allow)." >&2; \
		exit 1; }

build: check-env
	go build -o build/twee -ldflags "$(LDFLAGS)" ./cmd/twee

test: check-env
	go test ./...

check: test
	go fix -diff ./...
	go vet ./...
	golangci-lint run

coverage: check-env
	go test ./... -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out

install: build
	install -m 0755 build/twee $(HOME)/.local/bin

clean:
	-rm -f build/* coverage.out
