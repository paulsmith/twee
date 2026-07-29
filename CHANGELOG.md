# Changelog

## v0.2.0 - 2026-07-29

- Reworked the CLI around consistent long-form flags. Child commands now follow
  `--`, text waits use `--pattern`, and coordinates and dimensions use named
  flags such as `--x`, `--y`, `--cols`, and `--rows`.
- Replaced legacy JSONL recordings with self-contained `.twee` trace bundles and
  added whole-session recording with `start --trace`.
- Added `twee export` for GIF, MP4, WebM, and self-contained HTML replays, with
  cropping, input overlays, quality presets, atomic output, and permission
  preservation.
- Added selectable Kitty, iTerm2, and Sixel graphics backends to `twee play`,
  along with safer terminal handoff and a playback end screen.
- Added `twee bundle info` and `twee bundle validate` for inspecting trace
  metadata and checking bundle integrity and event validity.
- Added `twee do` to run JSON operation scripts against an existing named
  session.
- Expanded session lifecycle controls with `start --force`, `stop --all`, and a
  configurable stop grace period.
- Made session teardown and recovery more reliable: traces finalize before the
  daemon exits, finalized bundle paths are reported, stale sessions are cleaned
  up, and `status` remembers how a completed session ended.
- Improved wait behavior with multiline regular expressions, a distinct
  `SESSION_ENDED` error when a session exits, and clearer timeout diagnostics.
- Stabilized JSON responses for cells and regions with snake_case fields and
  complete color and text-style data; unknown RPC argument keys are now
  rejected instead of ignored.
- Fixed client-relative path handling for daemon-side inputs and outputs,
  including screenshots, traces, and `diff --against`.
- Fixed playback and export rendering of 256-color, italic, and strikethrough
  content, plus several export cleanup and corrupt-input failure paths.
- Consolidated source builds around the Nix flake and expanded release
  automation to produce Linux x86_64 and macOS arm64 archives with checksums.

