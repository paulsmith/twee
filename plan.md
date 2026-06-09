# Implementation Plan: twee (Go TUI Test Harness)

> **Note:** the VT-backend portion of this plan is superseded by
> [`docs/plan-libghostty.md`](docs/plan-libghostty.md), which swaps the
> hand-rolled pure-Go emulator for `libghostty-vt`.

This plan operationalizes `design.md`. It also resolves or sequences the
critical issues flagged in the design review so they don't block work
mid-milestone.

Module path: `github.com/paulsmith/twee`. Public package: `tuitest`.
The directory `twee/` is the repo location; `tuitest` is the import name.

## Up-front decisions

These are committed now so milestones don't stall on them.

- **Default terminal profile**: `xterm-256color`. No DECCKM (application
  cursor mode) awareness in v0 — cursor keys always emit the CSI form
  (`ESC [ A` etc.). Document the limitation; revisit in v1 if a fixture
  needs it.
- **Default timeouts**: `WaitFor*` = 5s. `WaitForStableScreen` = 100ms
  quiescence window with 5s overall cap. Both overridable globally
  (`tuitest.SetDefaultTimeout`) and per call (`WithTimeout(d)`).
- **`Expect*` vs `WaitFor*`**: `Expect*` is `WaitFor*` with the default
  timeout, calling `t.Fatalf` on timeout. There is no synchronous
  snapshot assertion in the public API. Tests that want sync semantics
  call `Snapshot()` and inspect the returned struct directly.
- **`Type` / `Key` concurrency**: synchronous write to the PTY master,
  no waiting for any model effect. Ordering guarantee: bytes written by
  `Type` are flushed before the call returns, so a subsequent
  `WaitForText` will observe their effects once the pump processes them.
- **`WaitForText` matching**: against the visible viewport only, not
  scrollback. Match is a substring search against each line's collapsed
  visible text (continuation cells removed, trailing spaces stripped).
  Cross-line matching is not supported in v0; users who need it call
  `WaitForTextRegex` or assert on `VisibleText()` directly.
- **stderr**: merged into the PTY by default (matches how a real
  terminal works). No separate capture in v0.
- **Scrollback in `VisibleText()`**: viewport only. Scrollback access is
  a separate method, `Scrollback() []string`, gated by an option that
  enables retention.
- **PTY library**: `github.com/creack/pty`.

## Milestone 0: preflight (½ day)

Goal: a repo skeleton that builds and runs a trivial test in CI.

- Create `twee/` Go module: `go mod init github.com/paulsmith/twee`.
- Add directory layout from design doc: `tuitest/` (public),
  `internal/{vt,ptyrunner,pump,input,snapshot,recording}/`.
- Add `go-libghostty` as a dependency. Vendor it. Pin the C
  `libghostty-vt` build to a known commit; commit a build script under
  `scripts/build-libghostty.sh`.
- CI: a GitHub Actions workflow that installs the C toolchain, builds
  libghostty-vt, runs `go test ./...`. Linux only.
- Smoke test: a single passing test in `tuitest/` that imports nothing
  from libghostty yet.

Exit criterion: `go test ./...` green in CI.

## Milestone 1: VT model wrapper (2-3 days)

Goal: feed bytes to a hidden libghostty model and read structured state
back out, with no PTY involved.

Files:

- `internal/vt/model.go` — `Model` interface, libghostty-backed impl.
- `internal/vt/snapshot.go` — `Snapshot`, `Line`, `Cell`, `Cursor`,
  `Size`, `Color` types, all Go-native.
- `internal/vt/visible.go` — `VisibleText(snap)` extraction.
- `internal/vt/model_test.go` — golden byte-stream tests.

Concrete tasks:

1. Define the `Model` interface from the design doc (`Feed`, `Resize`,
   `Snapshot`).
2. Implement it against `go-libghostty`. Each `Snapshot()` call returns
   a fully copied, immutable Go struct — no shared memory with the C
   side. Snapshots are cheap because tests don't run a hot loop.
3. Implement `VisibleText`: walk lines, skip continuation cells, strip
   trailing spaces per line, join with `\n`. No scrollback.
4. Golden tests for: plain ASCII, CR/LF handling, cursor movement
   (`CUP`, `CUU`, `CUD`, `CUF`, `CUB`), erase (`ED`, `EL`), line wrap
   at right margin, SGR styles (bold, underline, fg/bg color),
   alternate screen enter/exit, and a wide-character + combining-mark
   string. Each test is a `[]byte` literal → `Feed` → assertion on
   `VisibleText`/`Snapshot`.

Exit criterion: ~15 golden byte-stream tests passing. No PTY code yet.

## Milestone 2: PTY runner + pump (3-4 days)

Goal: spawn a process under a PTY, drain its output into the model
continuously, and clean up reliably.

Files:

- `internal/ptyrunner/runner.go` — `Runner` type wrapping `exec.Cmd` +
  `pty.Open`.
- `internal/pump/pump.go` — read loop and synchronization primitives.
- `tuitest/term.go` — `Term` struct, `Run`, `Start`, `Close`.
- `tuitest/options.go` — `Option`, `Size`, `Env`, `Dir`, `Command`.

Synchronization (this is the bit the design doc left vague):

- One goroutine, the **pump**, owns all reads from the PTY master.
- The pump holds a `sync.Mutex` named `mu` that guards the VT model.
- After each `Feed`, the pump increments a uint64 `gen` counter and
  calls `cond.Broadcast()` on a `*sync.Cond` whose `L` is `&mu`.
- Waiters acquire `mu`, read the current snapshot, evaluate their
  predicate, and either return or `cond.Wait()` until the next
  broadcast or their timeout fires.
- A separate goroutine watches the timeout via `time.AfterFunc`; on
  fire it acquires `mu` and broadcasts so the waiter wakes and returns
  a timeout error.

Process exit and final drain:

- The pump never blocks on `cmd.Wait`. A second goroutine waits for the
  process; when it exits, that goroutine signals an `exited` channel.
- The pump keeps reading the PTY master until it returns an error
  (Linux: `EIO` after slave closes; macOS: `n=0` / `io.EOF`). Only then
  does the pump close the model and broadcast a final wakeup.
- `Close()` sequence: cancel context → SIGTERM → 250ms grace → SIGKILL
  → wait for pump goroutine → close PTY master.

Tasks:

1. `Runner.Start` — open PTY, set initial winsize via `TIOCSWINSZ`,
   apply env (defaults: `TERM=xterm-256color`, `COLORTERM=truecolor`,
   `LANG=C.UTF-8`), `cmd.Start()`.
2. `Pump.Loop` — read into a 32KB buffer, append to recording (no-op
   for now), feed model under `mu`, broadcast.
3. `Term` — orchestrates Runner + Pump + Model. `Run(t, ...)` registers
   `t.Cleanup(term.Close)`.
4. Integration test: spawn `printf 'hello\n'` (or a tiny Go fixture that
   does the same), `WaitForText("hello")` (stub impl ok), assert exit
   status 0.
5. Integration test: spawn a fixture that writes 10MB and exits;
   confirm no deadlock and final screen is correct.
6. Integration test: spawn `sleep 60`, call `Close()` immediately,
   confirm process is reaped within 1s.

Exit criterion: three integration tests pass. `go test -race` clean.

## Milestone 3: input, waits, and assertions (3-4 days)

Goal: drive a TUI fixture from end to end.

Files:

- `internal/input/keys.go` — `Key` enum, encoder.
- `tuitest/input.go` — `Type`, `Key`, `Paste`, `Resize`.
- `tuitest/wait.go` — `WaitForText`, `WaitForNoText`,
  `WaitForStableScreen`, `WaitUntil`, `WaitForTextRegex`.
- `tuitest/expect.go` — `Expect*` wrappers that call `t.Fatalf`.
- `fixtures/menu/main.go` — small TUI fixture for the demo test.

Tasks:

1. Key encoder for the v0 set: literal text, Enter, Escape, Tab,
   Backspace, Delete, arrows (CSI form), Home, End, PageUp/Down,
   Ctrl+letter. No DECCKM, no Kitty protocol.
2. `Paste`: emit `ESC [ 200 ~` … `ESC [ 201 ~` brackets. The
   "fallback if app didn't enable bracketed paste" path noted in the
   design doc is **deferred** — v0 always emits brackets and apps that
   don't enable bracketed paste mode will see the brackets as literal
   text. Document.
3. `Resize`: update PTY winsize via `TIOCSWINSZ`, model resize, emit
   `SIGWINCH` to the child.
4. `WaitForText`: matching rules as per the up-front decision.
   Re-evaluate predicate on every `cond.Broadcast`. On timeout return a
   structured error including last visible screen.
5. `WaitForStableScreen(quietFor, opts...)`: track time of last `Feed`;
   wait until `now - lastFeed >= quietFor` or overall timeout. **Known
   limitation**: hangs on apps with always-running spinners. Document
   `IgnoreRegion` as a v1 escape hatch.
6. `Expect*` wrappers: each calls the corresponding `WaitFor*` with the
   default timeout; on error calls `t.Fatalf` with the failure
   diagnostic block (command, exit status, last screen, recent bytes).
7. End-to-end: write `fixtures/menu` (a Bubble Tea program with three
   menu items). Test navigates Down + Enter, asserts on result line.

Exit criterion: the demo test from `design.md` ("Recommended first
target") passes deterministically.

## Milestone 4: snapshots and recording (2-3 days)

Files:

- `internal/snapshot/text.go` — Tier 1 text snapshots.
- `internal/snapshot/cell.go` — Tier 2 cell snapshots.
- `internal/recording/recorder.go` — write recordings.
- `internal/recording/replay.go` — replay output+resize into a Model.
- `tuitest/snapshot.go` — `ExpectTextSnapshot`, `ExpectCellSnapshot`.

Tasks:

1. Text snapshot writer/comparator. Path:
   `testdata/snapshots/<test>/<name>.txt`. Update on `-update` flag
   (standard Go pattern).
2. Recording: pump appends each output chunk with a monotonic ms
   timestamp; input layer appends key/paste/resize events; runner
   appends exit. Format = JSON Lines, one event per line, with a
   header line. (Switching from the design doc's single JSON document
   — JSONL streams better for huge sessions.)
3. Recording is **off by default**, enabled via `tuitest.Record(path)`
   option or `TUITEST_RECORD=1` env var. When a test fails, the
   harness writes the recording to `t.TempDir()` and prints the path.
4. Replay: read JSONL, feed output bytes into a fresh `Model`,
   apply resizes. Used by harness self-tests.
5. Cell snapshot: include text, width, bold, underline, inverse, and a
   **normalized** color (named SGR → name; 256-palette → `p<n>`;
   truecolor → `#rrggbb`). Default: include text + bold + underline
   only; colors require an opt-in flag on the snapshot call. This
   addresses the cell-snapshot flakiness flagged in review.

Exit criterion: a snapshot test for the menu fixture; a replay test
that round-trips a recording through the model and reproduces the
final screen byte-for-byte.

## Milestone 5: real TUI validation + docs (2-3 days)

Tasks:

1. Fixtures: `fixtures/altscreen` (enter/exit alt screen),
   `fixtures/spinner` (always-redrawing), `fixtures/resize`
   (re-flowing on SIGWINCH).
2. Tests for each, using `WaitForText` rather than
   `WaitForStableScreen` for the spinner case (validates the up-front
   policy).
3. Bubble Tea smoke test: take a small upstream Bubble Tea example,
   drive it, assert.
4. Flake harness: a test that runs the menu fixture 200 times in a
   loop with `-race`. Must be 0% flaky.
5. README: install, quickstart, the failure-artifact format, the v0
   limitations list (no DECCKM, no mouse, no Kitty, no Windows, no
   region-exclusion in `WaitForStableScreen`).

Exit criterion: README ready; flake harness green; the demo test runs
in <1s on a developer laptop.

## Risk register

| Risk | Likelihood | Mitigation |
|---|---|---|
| libghostty-vt C API churns mid-build | Medium | Pin commit, vendor, isolate behind `internal/vt`. |
| `creack/pty` final-drain behavior differs Linux vs macOS | High | Treat both `EIO` and `EOF`/`n=0` as terminal; integration-test on both. |
| Bubble Tea fixture relies on DECCKM cursor keys | Medium | Catch in M5; if blocking, accelerate DECCKM support into v0. |
| Snapshot churn from libghostty version bumps | Medium | Cell snapshots opt-in; text snapshots skip styles. |
| CGO build flakes in CI | Medium | Cache the compiled `libghostty-vt` artifact across runs. |

## Total estimate

12-17 working days for a single engineer to reach the M5 exit
criterion.
