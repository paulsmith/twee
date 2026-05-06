# Releasing

GitHub releases are published by GoReleaser from `.github/workflows/release.yml`.

1. Create and push a semver tag:

   ```sh
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

2. The release workflow installs Nix, enters the repo's Nix dev shell, builds
   `libghostty-vt`, then publishes the release assets with GoReleaser.

3. The current downloadable asset is a Linux x86_64 archive. The binary is built
   with go-libghostty's `static` build tag so users do not need to install
   `libghostty-vt` separately.

Release-target smoke test on Linux x86_64:

```sh
nix develop --command goreleaser check
nix develop --command goreleaser release --snapshot --clean --skip=publish
```
