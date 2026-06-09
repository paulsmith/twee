BUILD_DIR := build

# CMake's FetchContent populates ghostty source under build/_deps/ghostty-src.
# Its zig-out is what go-libghostty's pkgconfig points at.
GHOSTTY_ZIG_OUT := $(CURDIR)/$(BUILD_DIR)/_deps/ghostty-src/zig-out
PKG_CONFIG_PATH := $(GHOSTTY_ZIG_OUT)/share/pkgconfig
DYLD_LIBRARY_PATH := $(GHOSTTY_ZIG_OUT)/lib
LD_LIBRARY_PATH := $(GHOSTTY_ZIG_OUT)/lib

STAMP := $(BUILD_DIR)/.ghostty-built

VERSION := $(shell jj log -r @ -T 'change_id.short()' --no-graph 2>/dev/null || echo dev)

.PHONY: all build test coverage smoke clean twee libghostty

all: build

$(STAMP):
	cmake -B $(BUILD_DIR) -DCMAKE_BUILD_TYPE=Release
	cmake --build $(BUILD_DIR)
	@touch $(STAMP)

libghostty: $(STAMP)

twee: $(STAMP)
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) go build -o ./bin/twee \
		-ldflags "-X main.Version=$(VERSION)" ./cmd/twee

build: $(STAMP) twee
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) go build ./...

test: $(STAMP)
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) \
	DYLD_LIBRARY_PATH=$(DYLD_LIBRARY_PATH) \
	LD_LIBRARY_PATH=$(LD_LIBRARY_PATH) \
	go test ./...

coverage: $(STAMP)
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) \
	DYLD_LIBRARY_PATH=$(DYLD_LIBRARY_PATH) \
	LD_LIBRARY_PATH=$(LD_LIBRARY_PATH) \
	go test ./... -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out

smoke: $(STAMP)
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) \
	DYLD_LIBRARY_PATH=$(DYLD_LIBRARY_PATH) \
	LD_LIBRARY_PATH=$(LD_LIBRARY_PATH) \
	go run ./cmd/libghostty-smoke

install:
	nix build
	install -m 0755 ./result/bin/twee $(HOME)/.local/bin

clean:
	rm -rf $(BUILD_DIR)
