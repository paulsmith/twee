# Video export for .twee recordings

Date: 2026-06-11
Status: approved

## Goal

Export a `.twee` recording to a video file — animated GIF (pure Go, no
external tools) or MP4/WebM (via an external `ffmpeg` binary) — by replaying
the event stream headlessly and encoding one frame per visible screen change.

## CLI

New verb, registered in `cmd/twee/cmd_export.go` following the existing
dispatch pattern:

```
twee export <bundle.twee> -o <out.gif|out.mp4|out.webm>
    [--speed N]        playback speed multiplier (default 1.0)
    [--max-idle DUR]   cap idle gaps in trace time (default 0 = faithful)
    [--font-size PT]   render font size in points (default 14)
    [--fps-cap N]      max frames per second of logical time (default 30)
    [--ffmpeg PATH]    ffmpeg binary (default: lookup on PATH)
```

- Output format is inferred from the `-o` extension. Unknown extension is an
  error.
- `--max-idle` defaults to 0 (off), unlike `play`'s 2s default. The help text
  notes this intentional difference: exports are faithful to real timing
  unless asked otherwise.
- GIF export requires no ffmpeg. MP4/WebM preflight-check that ffmpeg exists
  before any rendering and fail with a clear message if not.

## Architecture

New package `internal/export`. It reuses:

- `play.OpenBundle` — manifest, decoded events, and `MaxCols`/`MaxRows`
  (already computed; no extra pass needed).
- `vt.Model` — screen reconstruction from raw PTY bytes.
- `render.Render` — snapshot → `*image.RGBA`.
- The output/resize event handling and `engineSnapshot`/`fromVTColor`
  conversion currently private to `internal/play/loop.go` are extracted to a
  shared location rather than duplicated. The wall-clock `loop` itself is
  *not* reused — export is a separate logical-clock walker with no TTY,
  ticker, pause, or keyboard commands, and no end-screen overlay.

```
bundle.twee → OpenBundle → events
                              ↓
            logical-clock replay (vt.Model, fps-cap windows, hash dedup)
                              ↓ (img, duration) per changed frame
                       sink interface
                      ↙             ↘
                gifSink          ffmpegSink
            (image/gif)      (temp PNGs + concat → ffmpeg)
```

## Frame generation

Headless replay over the event list, driven entirely by event timestamps
(logical clock):

1. Feed `output` event bytes into the model; apply `resize` events.
2. **Timing:** each inter-event trace-time gap is capped at `--max-idle`
   (when set), gaps are summed, and the total is divided by `--speed` —
   the same order of operations as `play`.
3. **fps-cap is a merge window, not a duration floor.** Within any
   1/fps-cap span of (post-cap) logical time, events are drained without
   snapshotting; at the window boundary the model is snapshotted once.
   Bursts of output collapse to a single frame whose duration is the
   accumulated window time, so total wall time is preserved. This also
   bounds the snapshot/hash cost to at most fps-cap per second of trace
   time.
4. **Dedup:** the snapshot is hashed (SHA256, as in `loop.go`) **excluding
   cursor position and visibility** — the renderer does not draw the cursor,
   so cursor-only changes would otherwise produce pixel-identical duplicate
   frames. (Exported video has no visible cursor, matching `play`;
   documented in help text.)
5. On hash change, the *previous* frame is closed out with duration =
   current logical time − previous frame time, rendered, and passed to the
   sink.
6. **Trailing frame:** the final screen's duration runs from its frame time
   to the `exit` event timestamp (or last event), capped at 3 seconds
   regardless of `--max-idle`, with a floor of one fps-cap window — so the
   last screen is visible but an idle tail never freezes the video for
   minutes.

## Canvas and letterboxing

`render.Render` stretches content when given explicit pixel dimensions, so
letterboxing is export's responsibility:

- Each snapshot is rendered at fixed `render.Options{SizePt: fontSize}`,
  yielding deterministic `cellW×cols by cellH×rows` dimensions.
- The output canvas is fixed for the whole video: sized for
  `Bundle.MaxCols`/`MaxRows` at the same font size, then **padded to even
  width and height** (required by `yuv420p`).
- Each rendered frame is `draw.Draw`n centered onto a black canvas of that
  size. Mid-recording resizes therefore letterbox naturally.

## Sink interface

```go
type sink interface {
    add(img *image.RGBA, d time.Duration) error
    close() error // finalize encode
}
```

### gifSink (pure stdlib)

- Converts each frame to `*image.Paletted` and appends to a `gif.GIF`,
  encoded at `close()` via `gif.EncodeAll`.
- **Palette:** scan the frame's distinct colors; if ≤256 (the common case
  for terminal content), use an exact-color palette — sharper than
  dithering. Otherwise fall back to `draw.FloydSteinberg` onto
  `palette.Plan9`.
- **Delays:** GIF delays are centiseconds. The rounding remainder is carried
  across frames so timing never drifts cumulatively. Minimum delay is
  clamped to 2cs (browsers treat 0–1 as 100ms).
- **Memory profile:** stdlib `image/gif` has no streaming append API, so all
  paletted frames (1 byte/pixel) are held in memory until `EncodeAll`.
  Change-driven export keeps frame counts modest; if this ever bites, a
  small streaming GIF89a writer on `compress/lzw` (LSB order) is the noted
  future replacement.

### ffmpegSink (external binary)

- Writes `frame-%06d.png` files plus a concat list into an `os.MkdirTemp`
  directory.
- Concat list uses bare relative filenames (`file frame-000001.png` /
  `duration 1.234`), and ffmpeg runs with the temp dir as its working
  directory — no `-safe 0` or path escaping needed.
- **The final `file` entry is repeated after its `duration` directive** —
  the concat demuxer otherwise drops the last frame's duration.
- `close()` runs one ffmpeg invocation:
  - MP4: `ffmpeg -f concat -i list.txt -fps_mode vfr -pix_fmt yuv420p out.mp4`
  - WebM: same with `-c:v libvpx-vp9`
- stderr is captured; on nonzero exit, its tail is surfaced in the error.
  The temp dir is removed on success and kept (with a notice printing its
  path) on failure for debugging.

## Errors

- Missing/invalid `-o` extension: usage error.
- ffmpeg not found for mp4/webm: preflight error before any rendering.
- ffmpeg failure: error including stderr tail and the retained temp dir path.

## Testing

Following the existing fake-driven patterns in `internal/play`:

- **Timing/dedup logic:** synthetic bundles + a fake sink; assert frame
  count, per-frame durations, fps-cap window merging, speed/max-idle math,
  cursor-movement dedup, and trailing-frame cap. Pure logic, no ffmpeg, no
  TTY.
- **gifSink:** encode then decode with `image/gif`; assert frame count,
  delays (including remainder carry), and exact-color palette selection.
- **ffmpegSink:** unit-test the generated concat list (durations, repeated
  final entry); one integration test that runs real ffmpeg, skipped unless
  ffmpeg is on PATH.
- **Canvas:** letterbox placement and even-dimension padding asserted on
  rendered output of a synthetic resize sequence.

## Dependencies

None added to `go.mod`. ffmpeg is an optional runtime external tool.

## Out of scope

- Cursor rendering (renderer limitation shared with `play`).
- Streaming GIF encoder, APNG, SVG, asciicast output.
- Audio, subtitles/captions from input events.
