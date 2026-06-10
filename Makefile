# All source builds run inside the Nix dev shell (`nix develop`, or
# `direnv allow` once), which provides go, pkg-config, and the prebuilt
# libghostty-vt from the flake. `make install` instead builds the
# *committed* tree via `nix build` — uncommitted changes only show up
# in `make twee`.

VERSION := $(shell jj log -r @ -T 'change_id.short()' --no-graph 2>/dev/null || echo dev)

.PHONY: all build test coverage smoke twee install clean check-env

all: build

check-env:
	@pkg-config --exists libghostty-vt || { \
		echo "libghostty-vt is not on the pkg-config path." >&2; \
		echo "Enter the dev shell first: nix develop (or direnv allow)." >&2; \
		exit 1; }

twee: check-env
	go build -o ./bin/twee -ldflags "-X main.Version=$(VERSION)" ./cmd/twee

build: twee
	go build ./...

test: check-env
	go test ./...

coverage: check-env
	go test ./... -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out

smoke: check-env
	go run ./cmd/libghostty-smoke

install:
	nix build
	install -m 0755 ./result/bin/twee $(HOME)/.local/bin

clean:
	rm -rf bin build coverage.out
