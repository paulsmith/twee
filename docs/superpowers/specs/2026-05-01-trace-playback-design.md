# Design: `twee play` — trace bundle playback

## Status

Draft for implementation.

## Context

`twee` already records sessions as `.twee` zip bundles (`manifest.json` and `events.jsonl`) via the `internal/trace` package. The event stream can reconstruct terminal state, but there is no way for a human to *watch* a recorded session — to see the TUI animate, see what was typed, scrub the timeline, and inspect cause and effect.

`twee play` fills that gap. Primary audience: a human debugging a flake or reviewing a session in their own terminal, the way `asciinema play` works for asciicast files.

## Goals

- Play a `.twee` bundle as an animation in the user's terminal at recorded speed.
- Surface input events (keys, paste, resize) as a transient on-screen toast so the viewer correlates input with screen change.
- Provide minimal interactive controls: pause, step, fast-forward, restart, quit.
- Reuse the existing `vt.Model` and `internal/render` rasterizer; emit frames via the Kitty graphics protocol.
- Single binary, no daemon, no network server, no extra process.

## Non-goals (v0)

- Sixel, iTerm2 inline images, or any non-Kitty graphics protocol.
- Backward seek (`r` restarts from t=0; that is the only way to "rewind").
- Looping playback.
- Headless export to GIF/MP4/SVG. (Future feature, separate verb.)
- Streaming a *live* in-progress trace.
- Querying state at a timestamp as text/JSON (separate feature for agent consumers).
- Mouse input.
- Editing or trimming a bundle.

## CLI

```
twee play <bundle.twee> [--speed N] [--step] [--max-idle 2s]
```

| Flag | Default | Purpose |
|---|---|---|
| `--speed N` | `1.0` | Wall-clock multiplier on event timing. `0.5` = half speed, `4` = 4×. |
| `--step` | off | Start paused at t=0; advance one event at a time with `.`. |
| `--max-idle <dur>` | `2s` | Cap the gap between events. `0` disables. Long natural pauses get compressed. |

`twee play` does **not** emit the JSON envelope used by other verbs — it owns the screen for its lifetime. Errors go to stderr as plain text. Exit codes: `0` on clean exit, `1` on preflight or read failure. Add `-v` to print a one-line summary to stderr after exit.

## Interactive controls

Active during playback (raw mode, single keystrokes):

| Key | Action |
|---|---|
| `space` | Toggle pause / resume |
| `.` | Step forward one event (also engages pause) |
| `>` | Jump forward 1s of trace time |
| `r` | Restart from t=0 |
| `q` (or Ctrl+C) | Quit |

At end-of-trace the final frame is held; `r` restarts, `q` exits. There is no backward seek; users who want to rewind press `r`.

## Architecture

```
                     +-----------------+
                     | bundle.twee     |  zip on disk
                     +--------+--------+
                              |
                              v
              +---------------+---------------+
              | bundle reader (internal/play) |  parse manifest, decode
              | -> Manifest, []Event          |  events.jsonl into memory
              +---------------+---------------+
                              |
                              v
                +-------------+-------------+
                | preflight                 |  TTY check, Kitty query+200ms
                | (terminal capabilities)   |  read, size check vs.
                |                           |  max(rows,cols) in trace
                +-------------+-------------+
                              |
                              v
   +--------------+    cmd channel    +-------------+
   | input loop   |------------------>| play loop   |
   | raw stdin    |                   | (single     |
   | parses keys  |<------------------| goroutine)  |
   +--------------+    quit signal    +------+------+
                                             |
                                             v
                                  +----------+----------+
                                  | vt.Model            |  feed output bytes;
                                  | (libghostty wrapper)|  Snapshot() -> grid
                                  +----------+----------+
                                             |
                                             v
                                  +----------+----------+
                                  | render.Render       |  snapshot -> RGBA
                                  | (existing package)  |
                                  +----------+----------+
                                             |
                                             v
                                  +----------+----------+
                                  | kitty graphics      |  RGBA -> base64 PNG
                                  | encoder             |  -> APC sequence
                                  |                     |  -> stdout
                                  +---------------------+
```

The play loop owns all mutable state — model, cursor, clock, toast — in one goroutine. Only the keyboard reader runs concurrently, and it only sends `command` values over a channel. This avoids any locking around the model.

## Playback loop

A single goroutine drives a 33ms ticker (≈30 fps). The per-tick function is pure — it takes "now" as a parameter so tests don't need real time.

```go
type loop struct {
    events     []Event
    cursor     int               // index of next unprocessed event
    model      vt.Model
    snapHash   []byte            // hash of last emitted snapshot
    playT      time.Duration     // logical trace time
    wallPrev   time.Time         // wall clock at last tick
    speed      float64
    maxIdle    time.Duration
    paused     bool
    atEnd      bool
    toast      toast             // input event text + expiresAt
    cmds       <-chan command
    sink       frameSink
    cols, rows int               // current model dimensions
    initCols   int               // for restart
    initRows   int
}

func (l *loop) tick(now time.Time) (done bool) { ... }
```

**Per tick:**

1. Drain pending commands non-blockingly:
   - `pause`: toggle `paused`; reset `wallPrev`.
   - `step`: set `paused=true`; if events remain, advance `playT` to `events[cursor].t_ms`, dispatch the one event, increment `cursor`.
   - `fwd1s`: `playT += 1s` (events ≤ new `playT` get dispatched in step 3).
   - `restart`: `model = freshModel(initCols, initRows); cursor = 0; playT = 0; toast = {}; snapHash = nil; atEnd = false`.
   - `quit`: return `done=true`.
2. If not paused and not at end: `dt = (now - wallPrev) * speed; wallPrev = now`. If `cursor < len(events)` and `events[cursor].t_ms - playT.ms() > maxIdle.ms()`, snap `playT = events[cursor].t_ms - maxIdle` (idle cap). Then `playT += dt`.
3. While `cursor < len(events) && events[cursor].t_ms <= playT.ms()`:
   - `output`: `model.Feed(decode(ev.bytes_b64))`.
   - `resize`: `model.Resize(ev.cols, ev.rows); cols, rows = ev.cols, ev.rows`.
   - `input`: `toast = {text: formatInput(ev), expiresAt: now + 500ms}`.
   - `exit`: ignored in v0.
   - Increment `cursor`.
4. If `cursor == len(events)`, set `atEnd = true`. Don't advance `playT` further.
5. Compute snapshot hash. Emit a frame if `hash != snapHash` *or* the toast just expired/changed (so the footer can clear).

**`emitFrame()`:** rasterize via `render.Render(model.Snapshot(), opts)`; encode as PNG; send Kitty APC sequence with a stable image ID so subsequent frames replace rather than stack; move cursor to row `rows+1`, write the toast line with `\x1b[2K` + content; row `rows+2` for the status line; cursor home. Update `snapHash`.

## Footer

Two rows below the rendered image grid:

- **Row 1 (toast)** — most recent input event, transient (cleared 500ms after firing). Format: `[02.314s] → Enter`, `[02.871s] → type "hello"`, `[03.105s] → resize 100x40`.
- **Row 2 (status)** — permanent. Format: `playing 1.0× • 42/189 events • space=pause .=step >=+1s r=restart q=quit`. `playing` becomes `paused` / `step` / `at end` as appropriate.

## Preflight

Run before entering alt-screen, in this order. Any failure prints to stderr and exits 1 *without* touching the screen.

1. **stdout is a TTY.** `isatty(stdout)` — playback to a pipe is meaningless.
2. **Bundle is openable and well-formed.** Open the zip; require `manifest.json` and `events.jsonl`; check `manifest.version == 1`.
3. **Scan events for max dimensions.** Walk events for resize records to find max(cols), max(rows) across the trace.
4. **Terminal big enough.** Query terminal size; require `cols >= maxCols` and `rows >= maxRows + 2` (for footer). Otherwise: `twee play: terminal is 80x24; trace needs at least 120x40`.
5. **Kitty graphics support.** Set TTY raw with a 200ms read deadline. Write the Kitty query: `\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\`. Read until terminator. Accept `\x1b_Gi=31;OK\x1b\\`. On timeout or non-OK reply: `twee play: kitty graphics protocol not detected`. Restore cooked mode before exit.

After preflight: enter alt-screen + hide cursor with `defer restoreTerm()`. The deferred restore also fires on panic.

## Package layout

```
internal/play/
  bundle.go          // open .twee zip, decode manifest + events.jsonl
  bundle_test.go
  preflight.go       // TTY check, kitty query+read, size validation
  preflight_test.go
  loop.go            // playback state machine (pure, clock injected)
  loop_test.go
  kitty.go           // Kitty graphics APC encoder (RGBA -> bytes)
  kitty_test.go
  input.go           // raw-mode keystroke -> command channel
  toast.go           // input event + status formatters
  play.go            // orchestrator: wires bundle + preflight + loop + io

cmd/twee/
  cmd_play.go        // verb registration; flag parsing; calls play.Run(...)
```

The split keeps the loop a pure function. Anything I/O is behind one of three injectable interfaces:

```go
type clock interface { Now() time.Time }
type cmdSource interface { Recv() (command, bool) }
type frameSink interface { Emit(img *image.RGBA, toast, status string) error }
```

In production the clock is wall-clock, `cmdSource` reads raw stdin, `frameSink` writes Kitty APC + footer escapes to stdout. Tests substitute fakes for all three.

## Errors & failure modes

| Condition | Handling |
|---|---|
| Bundle path missing / not a zip | `twee play: open <path>: <err>` to stderr, exit 1 |
| Manifest version unknown | `twee play: unsupported bundle version N` |
| `events.jsonl` truncated / malformed | report line number, exit 1 |
| stdout not a TTY | `twee play: refusing to play to a non-tty` |
| Kitty query times out | `twee play: kitty graphics protocol not detected` |
| Terminal smaller than max trace size | `twee play: terminal is 80x24; trace needs at least 120x40` |
| Read error from stdin mid-playback | exit cleanly via deferred restoreTerm |
| Panic mid-playback | deferred `restoreTerm()` runs (cooked mode + main screen + show cursor) before re-panicking |

## Testing strategy

Five layers; the bulk of the confidence sits in layer 2 (loop unit tests with a fake clock).

### 1. Bundle reader unit tests

No terminal involved. Feed handcrafted `.twee` zips and assert manifest + events parse correctly. Negative cases:

- Missing `manifest.json`.
- Missing or truncated `events.jsonl`.
- Malformed JSON on a specific line (line number must surface in error).
- `manifest.version` other than 1.
- Empty `events` array (legal — playback should hold the initial blank frame and exit on `q`).

### 2. Loop unit tests with fake clock + fake sink

Most coverage here. Construct a `loop` directly with:

- A synthetic `[]Event` slice (e.g., output `"A"` at 0ms, output `"B"` at 100ms, input `Enter` at 150ms, output `"C"` at 200ms, resize 100×40 at 250ms).
- A `fakeSink` that records every `Emit` call.
- A `command` channel the test pushes commands onto.
- Direct calls to `loop.tick(now)` with controlled `now` values.

Assertions cover:

- Frames are emitted only when snapshot hash changes (no spurious emissions).
- Toast text matches the most recent input event and clears at the right tick.
- `pause` freezes `playT` advancement.
- `step` advances exactly one event regardless of timing.
- `fwd1s` skips the configured duration.
- `restart` resets the model, cursor, and toast atomically.
- `quit` returns `done=true` from `tick`.
- `--max-idle` snaps `playT` past long gaps.
- `resize` events change `cols`/`rows` and (separately) the next emitted frame reflects the new dimensions.
- End-of-trace sets `atEnd` and stops advancing `playT` but still emits frames in response to commands (e.g., `r` restart).

Because the loop is pure, these tests are deterministic with no goroutines, no sleeps, no flake.

### 3. Kitty encoder golden tests

Feed a tiny known RGBA image (e.g., 4×2 red/blue checkerboard) to the encoder; compare the resulting byte sequence to a golden file under `testdata/`. Include:

- Single-chunk image (small enough to fit in one APC sequence).
- Multi-chunk image (verifies `m=1`/`m=0` continuation byte handling and base64 chunking).
- Image-replace path (subsequent emission with same image ID).

### 4. Preflight unit tests

Inject a fake PTY pair for stdin/stdout in tests; the test side writes responses, the preflight side reads with a deadline. Cover:

- OK reply within deadline → success.
- Garbled reply → fail with diagnostic.
- Timeout (test side never writes) → fail with diagnostic.
- Stdout not a TTY → fail before any escape sequence is written.
- Terminal smaller than max trace dimensions → fail with the size-mismatch message.

### 5. End-to-end smoke test

Mirror the existing `cli_trace_e2e_test.go` pattern:

- Run real `vim` under `twee start`, record a trace, stop.
- Invoke `twee play <bundle.twee> --speed 100`.
- Skip the test if the test environment doesn't advertise Kitty support (check `$TERM` or `$KITTY_WINDOW_ID`).
- Assert the binary exits 0 within a wall-clock budget.

The point of the E2E is to catch panics, alt-screen restore bugs, and integration mistakes — not pixel verification (covered by `internal/render` tests) or terminal handshake (covered by golden bytes).

A second integration test, gated by env var (`TWEE_PLAY_FAKE_KITTY=1`), runs the orchestrator against a `*os.File`-backed fake stdout and asserts the encoder produced *some* APC output for each frame change. This gives a real-binary test that runs in CI without a Kitty terminal.

### Determinism notes

- The production loop wires `time.Tick(33ms)` to `loop.tick`, but tests never use a real ticker. Tests call `tick(now)` directly with synthetic timestamps.
- The keyboard reader is its own goroutine in production; tests inject commands via the channel directly and never start the reader.
- The renderer is deterministic (existing tests prove this), so the same snapshot bytes always produce the same image bytes.

### What we don't test

- Pixel-perfect rendering — already covered by `internal/render` tests.
- Real Kitty terminal handshake — no Kitty in CI; covered by golden bytes plus manual smoke.
- Scrollback — playback shows the viewport only.

## Open questions

1. Should `--speed 0` mean "step mode" (synonym for `--step`), or is it an error? Lean toward error.
2. Does `>` (jump 1s forward) feed all intermediate events into the model, or only output events up to the new time? Current design: dispatch every event ≤ new time, including any input events (which means their toasts may flash by faster than 500ms; acceptable).
3. Should `r` (restart) preserve `paused` state? Current design: restart resumes playing regardless of prior pause state. (Easy to reverse if it's annoying in practice.)

## Future work (out of v0 scope)

- Headless export: `twee play <bundle> --to out.gif` / `--to out.mp4` / `--to out.svg`.
- Sixel and iTerm2 backends.
- Backward seek / true scrubber bar (would require keyframe caching of model state).
- Live mode: tail an in-progress trace as it's being recorded.
- Web playback: `twee play --serve <bundle>` opens a local HTTP server with an HTML5 viewer (xterm.js).
- A query verb for agents: `twee trace at <bundle> <duration>` returns the rendered viewport at that time as text/JSON.
