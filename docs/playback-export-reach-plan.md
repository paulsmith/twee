# Playback and export reach implementation plan

## Goal and constraints

Make recordings useful to people who cannot use Kitty playback, while making
the existing fast, robust headless exporter easy to use for CI failure
artifacts. Deliver the work in independently useful increments:

1. Document a GIF artifact recipe now.
2. Add a self-contained HTML export for broadly shareable local replay.
3. Separate playback from the Kitty graphics protocol and add automatic
   backend selection.
4. Add iTerm2, then Sixel, only after their protocol and redraw behavior meet
   the shared playback contract.

Keep the existing headless logical-clock exporter separate from interactive
playback's wall-clock loop. They solve different timing problems and must not
be coupled merely because both render terminal snapshots.

## Phase 1: CI GIF artifacts

Publish `docs/ci-artifacts.md` and link it from the README's single-shot
scripts section. The documented recipe must:

- run `twee run --script ... --trace-out ... -- <app>`;
- preserve a failing scenario's exit status while continuing to export;
- export an existing trace to a GIF, normally with `--input-overlay` and a
  bounded `--max-idle`;
- upload only the GIF with `actions/upload-artifact@v7` under `if: always()`;
- add the action's `artifact-url` to `$GITHUB_STEP_SUMMARY` only when it was
  produced; and
- warn that a raw `.twee` bundle may reveal typed input, output, command
  arguments, or environment-derived metadata, so it is opt-in and needs a
  deliberate retention/access decision.

Acceptance: the YAML parses, the failing-scenario path still runs export and
upload, and a successful scenario with a broken export fails rather than
silently producing no diagnostic.

## Phase 2: self-contained HTML export

Extend `twee export` to dispatch `.html` output:

```text
twee export session.twee -o replay.html
twee export session.twee -o replay.html --input-overlay --max-idle 2s
```

Use `export`, not `play --to-html`: HTML is an offline artifact and should
share export's crop, timing, frame-rate, font, and input-overlay options. Keep
`play` interactive and TTY-oriented.

### Implementation shape

- Add an HTML export sink and embed its HTML, CSS, and JavaScript template in
  the Go binary.
- Reuse the existing replay walker, canvas composition, crop, timing,
  idle-compression, frame-rate cap, and overlay code. Do not implement a
  separate JavaScript terminal emulator.
- Encode each composed RGBA frame as a PNG data URL, with its duration. Stream
  encoded frame records through a temporary output and atomically rename only
  after successful completion, so malformed bundles never replace a good
  artifact with a partial one.
- Give export sinks explicit finish/abort cleanup semantics. Ensure errors
  remove temporary output and preserve a pre-existing target.
- Embed only whitelisted replay data. Exclude manifest environment, hostname,
  process identifiers, raw terminal events, and arbitrary metadata.
- Emit a restrictive CSP and include no external scripts, fonts, images, or
  network requests. The output must work from `file://`.

The first viewer should be deliberately small: canvas rendering, play/pause,
restart, previous/next frame, timeline scrubbing, 0.25x--4x speed, keyboard
controls where appropriate, elapsed/total time, and the latest input overlay.
Use cumulative frame durations and binary search for seeking.

PNG frames preserve the current renderer exactly and avoid duplicating VT
rendering in JavaScript. Record HTML size as a benchmark concern; investigate
delta frames or a video-backed variant only if representative traces make the
single-file form impractical.

Acceptance: generated files contain no external URLs or sensitive manifest
fields; every embedded PNG decodes; duration totals and seeking are correct;
corrupt bundles return clean errors without panics or committed partial files;
and representative artifacts open from `file://` in current Chrome, Firefox,
and Safari.

## Phase 3: playback backend abstraction and auto detection

Refactor `play` before adding another terminal protocol:

```text
twee play session.twee --backend auto
twee play session.twee --backend kitty
twee play session.twee --backend iterm2
twee play session.twee --backend sixel
```

`auto` is the default, preferring Kitty, then iTerm2, then Sixel. An explicit
backend either initializes it or fails with a precise diagnostic. Auto mode
reports the protocols attempted and why each was unavailable.

Define a small backend seam, conceptually:

```go
type graphicsBackend interface {
    Name() string
    Probe(context.Context, terminalIO) (support, error)
    NewSink(terminalIO, terminalGeometry) (frameSink, error)
}

type frameSink interface {
    Emit(Frame) error
    Close() error
}
```

`Frame` carries the rendered RGBA image, cell placement, toast, and status.
The shared playback lifecycle continues to own raw mode, alternate screen,
cursor visibility, signal restoration, footer sanitization, and absolute
footer positioning. Do not make every backend reimplement terminal cleanup.

Probe actual protocol replies, rather than inferring support from `$TERM`:

- retain Kitty's graphics query and device-attributes verification;
- use iTerm2 feature reporting or `OSC 1337;Capabilities` and require `FILE`;
- accept Sixel's `Sx` capability or primary device-attributes parameter `4`.

Initially support direct terminal connections only. Treat tmux/screen
passthrough as a separately scoped follow-up: it changes escaping, capability
detection, and redraw behavior.

Acceptance: all existing playback-loop tests run unchanged through fake
Kitty, iTerm2, and Sixel sinks; query parsers handle partial, noisy, oversized,
and timed-out replies; and explicit/auto selection diagnostics are covered by
golden tests.

## Phase 4: iTerm2 backend

Implement an iTerm2 sink that reuses PNG encoding and writes the inline-image
`OSC 1337;File=...;inline=1` sequence with cell width and height plus disabled
aspect-ratio preservation. Preserve placement and cleanup through the shared
playback lifecycle.

Before declaring the backend supported, run a real-terminal spike in iTerm2:
repeatedly place frames at the same alternate-screen location and verify they
replace rather than scroll, stack, or flicker unacceptably. The protocol does
not provide Kitty's documented placement identifiers. If the spike cannot meet
the contract, leave iTerm2 explicit/experimental and advance Sixel instead.

Acceptance: protocol byte-stream golden tests, manual replacement/flicker
matrix on supported iTerm2 versions, resize/exit cleanup checks, and a clear
unsupported-capability error.

## Phase 5: Sixel backend

Implement a deterministic internal Sixel encoder:

- share or extract the GIF palette/quantization utility instead of creating a
  divergent color path;
- quantize to at most 256 colors;
- emit raster attributes, palette definitions, sixel rows, and repeat
  compression;
- use an opaque background so each new frame overwrites old pixels; and
- save and restore any Sixel modes changed by the sink.

Require reliable cell-to-pixel geometry. In auto mode, skip Sixel with the
reason when geometry cannot be determined rather than producing a misaligned
replay.

Acceptance: deterministic encoder fixtures, palette bounds, valid row/repeat
encoding, overwrite behavior, geometry failure diagnostics, and manual tests
in at least two Sixel-capable terminals.

## Cross-cutting tests, benchmarks, and rollout

- Extend corrupt-bundle fuzzing to HTML export and backend selection. Expected
  outcome is a clean error, no panic, and no partial committed destination.
- Retain the established exporter benchmark corpus. Existing GIF/MP4/WebM
  performance must not materially regress; measure HTML time and peak memory
  independently of total frame count.
- Test output cleanup with pre-existing destinations, write failures, and
  cancellation at every sink stage.
- Keep byte-level protocol and viewer fixture tests deterministic. Put manual
  terminal and browser checks in a versioned matrix so release verification is
  repeatable.
- Release in this order: CI documentation, HTML export, backend abstraction,
  iTerm2 only if its redraw spike passes, then Sixel. Feature-gate or mark
  experimental any backend whose manual matrix is incomplete.

The implementation PRs should stay independently reviewable: docs, HTML
sink/CLI, backend seam/probing, iTerm2, and Sixel. Each PR must run the Nix
development-shell Go test suite and targeted fuzz/benchmark checks before
merge.
