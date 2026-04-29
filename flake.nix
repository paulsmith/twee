{
  description = "twee — TUI testing harness for Go (libghostty-vt build env)";

  inputs = {
    nixpkgs.url = "https://channels.nixos.org/nixpkgs-unstable/nixexprs.tar.xz";
    flake-utils.url = "github:numtide/flake-utils";
    zig = {
      url = "github:mitchellh/zig-overlay";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    {
      nixpkgs,
      flake-utils,
      zig,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        # libghostty-vt requires Zig 0.15.2. Match go-libghostty's pin.
        zigPkg =
          if pkgs.stdenv.isDarwin then
            zig.packages.${system}.brew."0.15.2"
          else
            zig.packages.${system}."0.15.2";
      in
      {
        # mkShellNoCC: do not pin a Nix C compiler. libghostty-vt's zig build
        # invokes xcodebuild on macOS to assemble the xcframework, which
        # needs the host's Xcode/Command Line Tools — letting the system
        # toolchain win avoids a clash with Nix's clang wrapper.
        devShells.default = pkgs.mkShellNoCC {
          packages = [
            pkgs.cmake
            pkgs.go
            pkgs.gnumake
            pkgs.pkg-config
            zigPkg
          ];

          shellHook = ''
            # libghostty-vt is built via CMake's FetchContent (see CMakeLists.txt)
            # and emits a pkgconfig under build/_deps/ghostty-src/zig-out.
            export PKG_CONFIG_PATH="$PWD/build/_deps/ghostty-src/zig-out/share/pkgconfig''${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
            export DYLD_LIBRARY_PATH="$PWD/build/_deps/ghostty-src/zig-out/lib''${DYLD_LIBRARY_PATH:+:$DYLD_LIBRARY_PATH}"
            export LD_LIBRARY_PATH="$PWD/build/_deps/ghostty-src/zig-out/lib''${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
          '';
        };
      }
    );
}
