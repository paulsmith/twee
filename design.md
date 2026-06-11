# Design: Go TUI Test Harness Using `libghostty-vt`

## Status

Draft for implementation.

## Context

`libghostty-vt` is the virtual terminal core extracted from Ghostty. It parses terminal control sequences and maintains terminal state: screen cells, cursor, styles, wrapping, scrollback, and related VT behavior. A Go binding exists (`go-libghostty`) using CGO, but the Go API should be treated as unstable and hidden behind our own interface.

This design targets deterministic TUI tests for Go projects and other terminal applications. The goal is to assert against structured terminal state, not screenshots, OCR, `tmux capture-pane`, or ad hoc ANSI parsing.

## Critique of the previous design

The previous version had the right core idea but was too optimistic in a few places:

1. It treated terminal input encoding as a small detail. It is not. Keys depend on terminal mode, modifiers, application cursor mode, bracketed paste, mouse reporting, and sometimes TERM/terminfo behavior.
2. It suggested `TERM=xterm-256color` as a reasonable default. That may work for many apps, but it hides whether we are emulating Ghostty, xterm, or a reduced test terminal. We need an explicit compatibility profile.
3. It under-specified the event loop. A good harness must continuously drain the PTY, feed the terminal model, track quiescence, and avoid deadlocks.
4. It exposed style assertions too early. Visible text and cell layout matter first; styles should be a second-order feature.
5. It treated snapshots as straightforward. Full cell snapshots are useful but brittle. Normalized text snapshots should be the default; full cell snapshots should be opt-in.
6. It omitted recording/replay from v0. That was a mistake. Recording byte streams is cheap and invaluable for debugging.
7. It did not clearly separate public API, backend API, and test-runner lifecycle.
8. It did not address golden terminal byte-stream tests for the harness itself.
9. It implied mouse support in v0. That should be deferred unless there is a concrete app that needs it.
10. It did not discuss terminal-size determinism, locale, Unicode width, or process cleanup deeply enough.

## Goals

Build a Go library that can:

- Spawn a command under a PTY.
- Feed PTY output into `libghostty-vt`.
- Maintain a structured terminal screen model.
- Inject common keyboard input.
- Wait for stable observable states.
- Assert on visible text, cursor position, and selected cells.
- Record terminal sessions for debugging and replay.
- Work well in `go test`.

## Non-goals for v0

- Driving the Ghostty GUI.
- Screenshot comparison.
- OCR.
- `tmux` integration.
- Pixel-perfect rendering.
- Full mouse protocol support.
- Full terminfo emulation.
- Cross-platform support beyond Unix-like systems with PTYs.
- Guaranteeing identical behavior to every real terminal.
- Public exposure of `libghostty` or `go-libghostty` types.

## Architecture

```text
Go test
  |
  v
Public harness API
  |
  +-- Runner: process + PTY lifecycle
  +-- Pump: PTY output drain + terminal model feed
  +-- Input: semantic key/paste/resize API
  +-- Model: internal libghostty-vt wrapper
  +-- Query: text, line, region, cell, cursor
  +-- Assert/wait: polling over model snapshots
  +-- Recorder: raw byte stream + input events + resize events
  |
  v
Application under test
```

## Key design principle

The PTY byte stream is the source of truth. The terminal model is a derived state.

The harness should be able to save and replay:

```text
initial size
environment
output bytes
input events
resize events
process exit
timing metadata
```

This makes flakes diagnosable and lets us test the harness independently of live applications.

## Package shape

Suggested module layout:

```text
/tuitest
  public API

/internal/ptyrunner
  exec, PTY setup, process lifecycle

/internal/vt
  libghostty-vt wrapper

/internal/input
  key encoding and terminal mode handling

/internal/pump
  read loop, model feed, synchronization

/internal/snapshot
  text and cell snapshot serialization

/internal/trace
  .twee session recording and replay artifacts
```

## Public API sketch

```go
func TestCreateProject(t *testing.T) {
    term := tuitest.Run(t, "./myapp",
        tuitest.Size(100, 30),
        tuitest.Env("NO_COLOR", ""),
    )

    term.WaitForText("Projects")

    term.Key(tuitest.CtrlN)
    term.Type("pushup")
    term.Key(tuitest.Enter)

    term.WaitForText("Saved")
    term.ExpectText("pushup")
    term.ExpectNoText("panic")
}
```

Prefer `Run(t, ...)` so the library can register cleanup with `t.Cleanup`.

Also support lower-level construction:

```go
term, err := tuitest.Start(ctx, tuitest.Command("./myapp"))
if err != nil {
    return err
}
defer term.Close()
```

## Runner

The runner owns:

- `exec.CommandContext`.
- PTY allocation.
- Initial terminal size.
- Environment.
- Working directory.
- Signal forwarding where appropriate.
- Process termination.
- Cleanup on test failure.
- Exit status capture.

Use an existing Go PTY package initially, such as `creack/pty`, unless there is a compelling reason to write our own.

Recommended default environment:

```text
TERM=xterm-256color
COLORTERM=truecolor
LANG=C.UTF-8 or en_US.UTF-8
LC_ALL unset unless caller overrides
CI=1 optional, not default
```

Do not default `NO_COLOR`, because it changes app behavior. Instead, expose a test option for disabling color when desired.

Important: document that tests are for a declared terminal compatibility profile. The profile should start as `xterm-256color` because most TUIs understand it. A later `ghostty` profile can be added once we are confident about input and terminfo behavior.

## Pump / event loop

The harness needs a continuously running read loop:

```text
PTY read -> append to trace recording -> feed VT model -> notify waiters
```

Requirements:

- Never wait for the process before draining the PTY.
- Avoid deadlocks from full PTY buffers.
- Serialize access to the VT model.
- Allow waiters to observe fresh snapshots.
- Surface read errors and process exit.
- Preserve raw output bytes for diagnostics.
- Support a `Drain()` or `WaitForStableScreen()` operation.

Quiescence should be defined as “no model-changing output for duration D,” not “process is idle.”

## VT backend

Hide `go-libghostty` behind a narrow interface:

```go
type Model interface {
    Feed([]byte) error
    Resize(cols, rows int) error
    Snapshot() Snapshot
}
```

The public API should only expose Go-native structs:

```go
type Snapshot struct {
    Size   Size
    Cursor Cursor
    Lines  []Line
}

type Line struct {
    Cells []Cell
}

type Cell struct {
    Text      string // grapheme or display cell text
    Width     int
    Fg        Color
    Bg        Color
    Bold      bool
    Dim       bool
    Underline bool
    Inverse   bool
}
```

Do not let callers hold mutable references to internal terminal state. Snapshots should be immutable copies or immutable views with a clear lifetime.

## Unicode and cells

This needs care.

A terminal cell is not always a Unicode scalar value. The model must handle:

- Wide characters.
- Combining marks.
- Emoji sequences.
- Zero-width joiners.
- Continuation cells.
- Invalid UTF-8.
- Ambiguous-width characters under the active locale/profile.

For v0, expose both:

```go
snapshot.VisibleText()
snapshot.Cells()
```

`VisibleText()` should be the preferred assertion surface. Cell-level tests should be used sparingly.

## Input

Input is not merely “write bytes.”

Support this in v0:

```go
term.Type("literal text")
term.Key(Enter)
term.Key(Escape)
term.Key(Tab)
term.Key(Backspace)
term.Key(Delete)
term.Key(Up)
term.Key(Down)
term.Key(Left)
term.Key(Right)
term.Key(Home)
term.Key(End)
term.Key(PageUp)
term.Key(PageDown)
term.Key(Ctrl('C'))
term.Key(Ctrl('D'))
term.Paste("multi\nline")
term.Resize(120, 40)
```

Defer:

- Mouse input.
- Complex modifier combinations.
- Kitty keyboard protocol.
- Application-specific keyboard protocols.
- Rich paste safety checks.

Input encoding should be profile-aware. For example, cursor keys may differ when application cursor mode is active. If `libghostty-vt` exposes the active mode state, use it. Otherwise, keep v0 conservative and document behavior.

`Paste` should support two modes:

```go
term.Paste(text)        // bracketed paste if app enabled it; fallback otherwise
term.Type(text)         // literal key-like text entry
```

## Query API

Start with plain observable queries:

```go
term.VisibleText() string
term.Lines() []string
term.Cursor() Cursor
term.Cell(x, y int) Cell
term.Region(x, y, w, h int) Region
term.Find(substr string) []Match
term.FindRegex(re *regexp.Regexp) []Match
```

Avoid higher-level widget semantics in v0. We do not know whether the app uses tables, menus, lists, panes, or arbitrary drawing.

## Waits and assertions

All waits must have timeouts.

Default timeout should be short enough for tests, configurable globally and per call.

```go
term.WaitForText("Saved")
term.WaitForNoText("Loading")
term.WaitForStableScreen(100 * time.Millisecond)
term.WaitUntil(func(s Snapshot) bool { ... })

term.ExpectText("Saved")
term.ExpectNoText("panic")
term.ExpectCursorAt(0, 3)
term.ExpectCell(0, 0, CellMatcher{Bold: true})
```

Implementation detail: waits should subscribe to model-change notifications instead of polling with fixed sleeps where possible. Polling can still be used as a fallback.

On failure, print:

- Command.
- Exit status if available.
- Last visible screen.
- Recent raw output bytes, escaped.
- Recent input events.
- Snapshot diff if relevant.
- Path to `.twee` trace bundle if enabled.

## Snapshots

Use two snapshot tiers.

### Tier 1: normalized text snapshots

Default.

- Visible text only.
- Strip trailing spaces by default.
- Optionally preserve blank lines.
- Stable across minor style changes.

```go
term.ExpectTextSnapshot("project-list")
```

### Tier 2: cell snapshots

Opt-in.

- Include cell text, width, selected styles, and colors.
- Useful for apps where alignment/color/style is behavior.
- More brittle.

```go
term.ExpectCellSnapshot("project-list")
```

Snapshot format should be deterministic JSON plus a human-readable `.txt` rendering.

Do not use screenshots in v0.

## Recording and replay

Recording should be in v0.

A recording enables:

- Replaying a terminal session from a `.twee` bundle.
- Debugging flakes.
- Creating harness golden tests.
- Comparing behavior across backend versions.

Suggested recording format: a `.twee` zip bundle with a manifest, JSONL event
stream, and sidecar resources.

Manifest:

```json
{
  "version": 1,
  "command": ["./myapp"],
  "env": {"TERM": "xterm-256color"},
  "cols": 100,
  "rows": 30
}
```

Events live in `events.jsonl`:

```jsonl
{"t_ms":0,"type":"output","bytes_b64":"..."}
{"t_ms":42,"type":"input","kind":"key","key":"Enter"}
{"t_ms":77,"type":"resize","cols":120,"rows":40}
{"t_ms":100,"type":"exit","code":0}
```

The replay path should feed only output and resize events into the model. Input events are diagnostic metadata unless replaying against a live process.

## Testing the harness

The harness itself needs tests independent of real TUIs.

Test layers:

1. Byte-stream golden tests:
   - Feed known ANSI/VT byte sequences.
   - Assert resulting visible text, cursor, and cells.

2. PTY integration tests:
   - Run small fixture programs that write predictable sequences.

3. Real TUI smoke tests:
   - Bubble Tea-style app.
   - Alternate-screen app.
   - Menu app using arrow keys.
   - App that resizes/reflows text.

4. Flake tests:
   - Delayed output.
   - Rapid redraws.
   - Large output bursts.
   - Process exits while output remains unread.

## Failure modes to design for

- Process writes enough output to fill PTY buffer.
- App never exits.
- App exits before expected screen appears.
- App switches to alternate screen.
- App relies on terminfo not matching our `TERM`.
- App disables echo or uses raw mode.
- Unicode width mismatch.
- Spinner/clock causes screen never to stabilize.
- Test sends input before app is ready.
- Resize produces transient inconsistent screens.
- CGO/libghostty version mismatch.
- Panic in test leaves child process alive.

## Versioning and dependency policy

Because `go-libghostty` and/or the underlying C API may change, pin versions tightly.

Recommendations:

- Hide the binding behind `/internal/vt`.
- Add a backend compatibility test suite.
- Vendor or pin dependency versions for reproducible CI.
- Consider a pure-Go fallback later only if needed.
- Keep public API independent of backend-specific concepts.

## v0 implementation plan

### Milestone 1: read-only model

- Create VT backend wrapper.
- Feed static byte slices.
- Extract visible text.
- Add golden tests for ANSI movement, clearing, wrapping, and styles.

### Milestone 2: PTY runner

- Spawn process under PTY.
- Continuous output pump.
- Fixed terminal size.
- Cleanup and timeouts.
- Basic failure diagnostics.

### Milestone 3: input

- Type text.
- Basic key enum.
- Resize.
- Wait for text.
- First end-to-end tests against fixture programs.

### Milestone 4: snapshots and recordings

- Text snapshots.
- `.twee` session recording.
- Replay recorded output into VT backend.
- Failure artifact output.

### Milestone 5: real TUI validation

- Test a small full-screen TUI.
- Test alternate screen.
- Test redraw-heavy app.
- Document unsupported cases.

## Minimal public API for v0

```go
type Term struct {}

func Run(t testing.TB, args ...string) *Term
func Start(ctx context.Context, opts ...Option) (*Term, error)

func (t *Term) Close() error
func (t *Term) Type(s string) error
func (t *Term) Paste(s string) error
func (t *Term) Key(k Key) error
func (t *Term) Resize(cols, rows int) error

func (t *Term) Snapshot() Snapshot
func (t *Term) VisibleText() string
func (t *Term) Lines() []string
func (t *Term) Cursor() Cursor

func (t *Term) WaitForText(s string, opts ...WaitOption) error
func (t *Term) WaitForNoText(s string, opts ...WaitOption) error
func (t *Term) WaitForStableScreen(d time.Duration, opts ...WaitOption) error
func (t *Term) WaitUntil(fn func(Snapshot) bool, opts ...WaitOption) error

func (t *Term) ExpectText(s string)
func (t *Term) ExpectNoText(s string)
func (t *Term) ExpectTextSnapshot(name string)
```

## Open questions

1. Should the default terminal profile be `xterm-256color`, `ghostty`, or configurable with no default?
2. Can the input encoder query `libghostty-vt` for modes such as application cursor keys and bracketed paste?
3. How much scrollback should the model retain by default?
4. Should failure artifacts be written automatically under `testdata/` or only under a temporary directory?
5. Should this start as a library only, or also include a CLI replay/debugger?

## Recommended first target

Start as a library only.

First successful demo:

```go
func TestMenuNavigation(t *testing.T) {
    term := tuitest.Run(t, "./fixtures/menu", tuitest.Size(80, 24))

    term.WaitForText("Choose an option")
    term.Key(tuitest.Down)
    term.Key(tuitest.Enter)

    term.WaitForText("selected: second")
    term.ExpectNoText("panic")
}
```

Success criterion: the test is readable, deterministic, and produces a useful failure artifact without screenshots or tmux.
