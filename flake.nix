{
  description = "twee — TUI testing harness for Go (libghostty-vt build env)";

  inputs = {
    nixpkgs.url = "https://channels.nixos.org/nixpkgs-unstable/nixexprs.tar.xz";
    flake-utils.url = "github:numtide/flake-utils";
    zig = {
      url = "github:mitchellh/zig-overlay";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    ghostty-src = {
      url = "github:ghostty-org/ghostty/2ed382a15566b267c32fae440b065f7844b15bfb";
      flake = false;
    };
  };

  outputs =
    {
      nixpkgs,
      flake-utils,
      zig,
      ghostty-src,
      self,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        lib = pkgs.lib;

        # libghostty-vt requires Zig 0.15.2. Match go-libghostty's pin.
        zigPkg =
          if pkgs.stdenv.isDarwin then
            zig.packages.${system}.brew."0.15.2"
          else
            zig.packages.${system}."0.15.2";

        ghosttyZigCache = pkgs.callPackage "${ghostty-src}/build.zig.zon.nix" {
          zig_0_15 = zigPkg;
        };

        ghostty-vt = pkgs.stdenv.mkDerivation {
          pname = "ghostty-vt";
          version = "2ed382a15566";

          src = ghostty-src;

          nativeBuildInputs = [
            pkgs.cmake
            zigPkg
          ]
          ++ lib.optionals pkgs.stdenv.isDarwin [
            pkgs.cctools
          ];
          buildInputs = lib.optionals pkgs.stdenv.isDarwin [ pkgs.apple-sdk ];

          preConfigure = ''
            export ZIG_GLOBAL_CACHE_DIR="$TMPDIR/zig-global-cache"
            cp -R ${ghosttyZigCache} "$ZIG_GLOBAL_CACHE_DIR"
            chmod -R u+w "$ZIG_GLOBAL_CACHE_DIR"
            export ZIG_LOCAL_CACHE_DIR="$TMPDIR/zig-local-cache"
          ''
          + lib.optionalString pkgs.stdenv.isDarwin ''

            mkdir -p "$TMPDIR/darwin-sdk-bin"
            cat >"$TMPDIR/darwin-sdk-bin/xcode-select" <<'EOF'
            #!/bin/sh
            if [ "$1" = "--print-path" ]; then
              printf '%s\n' '${pkgs.apple-sdk}'
              exit 0
            fi
            exit 1
            EOF
            cat >"$TMPDIR/darwin-sdk-bin/xcrun" <<'EOF'
            #!/bin/sh
            if [ "$1" = "--sdk" ] && [ "$2" = "macosx" ] && [ "$3" = "--show-sdk-path" ]; then
              printf '%s\n' '${pkgs.apple-sdk.sdkroot}'
              exit 0
            fi
            exit 1
            EOF
            cat >"$TMPDIR/darwin-sdk-bin/xcodebuild" <<'EOF'
            #!/bin/sh
            if [ "$1" = "-create-xcframework" ]; then
              while [ "$#" -gt 0 ]; do
                if [ "$1" = "-output" ]; then
                  mkdir -p "$2"
                  exit 0
                fi
                shift
              done
            fi
            exit 1
            EOF
            chmod +x "$TMPDIR/darwin-sdk-bin/xcode-select" "$TMPDIR/darwin-sdk-bin/xcrun" "$TMPDIR/darwin-sdk-bin/xcodebuild"
            export PATH="$TMPDIR/darwin-sdk-bin:$PATH"
            export SDKROOT="${pkgs.apple-sdk.sdkroot}"
          '';

          cmakeFlags = [ "-DCMAKE_BUILD_TYPE=Release" ];

          installPhase = ''
            runHook preInstall
            mkdir -p "$out"
            cp -R "$NIX_BUILD_TOP/source/zig-out/." "$out/"
            for pc in "$out"/share/pkgconfig/*.pc; do
              substituteInPlace "$pc" \
                --replace-fail "prefix=$NIX_BUILD_TOP/source/zig-out" "prefix=$out"
            done
            runHook postInstall
          '';
        };

        version = self.shortRev or self.dirtyShortRev or "dev";
        twee = pkgs.buildGoModule {
          pname = "twee";
          inherit version;

          src = self;
          vendorHash = "sha256-aF6WFeX8X6BajVzS5h+dwFujs/api/EcK/WgrRVxgHw=";

          subPackages = [ "cmd/twee" ];

          nativeBuildInputs = [ pkgs.pkg-config ];
          buildInputs = [ ghostty-vt ];

          ldflags = [ "-X main.Version=${version}" ];
        };
      in
      {
        packages = {
          inherit twee ghostty-vt;
          default = twee;
        };

        formatter = pkgs.nixfmt-tree;

        # mkShellNoCC: do not pin a Nix C compiler. cgo links against the
        # flake-built libghostty-vt with the host toolchain, which avoids
        # a clash with Nix's clang wrapper on macOS.
        devShells.default = pkgs.mkShellNoCC {
          packages = [
            pkgs.go
            pkgs.goreleaser
            pkgs.gnumake
            pkgs.pkg-config
          ];

          shellHook = ''
            # libghostty-vt comes prebuilt from this flake's ghostty-vt
            # package; ordinary go build / go test work in this shell.
            export PKG_CONFIG_PATH="${ghostty-vt}/share/pkgconfig''${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
            export DYLD_LIBRARY_PATH="${ghostty-vt}/lib''${DYLD_LIBRARY_PATH:+:$DYLD_LIBRARY_PATH}"
            export LD_LIBRARY_PATH="${ghostty-vt}/lib''${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
          '';
        };
      }
    );
}
