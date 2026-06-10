# Releasing

GitHub releases are built by GoReleaser from `.github/workflows/release.yml`.
The workflow builds each release target on a native runner, uploads the archives
as workflow artifacts, then publishes one GitHub release with a combined
checksum file.

1. Create and push a semver tag:

   ```sh
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

2. The release workflow installs Nix and enters the repo's Nix dev shell,
   which provides `libghostty-vt` prebuilt from the flake, then packages
   release archives.

3. The downloadable assets are Linux x86_64 and macOS arm64 archives. The
   binaries are built with go-libghostty's `static` build tag so users do not
   need to install `libghostty-vt` separately.

Release-target smoke test on a native target:

```sh
nix develop --command goreleaser check
TARGET=linux_amd64_v1 nix develop --command goreleaser build --single-target --snapshot --clean --output dist/twee
# or, on macOS arm64:
TARGET=darwin_arm64 nix develop --command goreleaser build --single-target --snapshot --clean --output dist/twee
```
