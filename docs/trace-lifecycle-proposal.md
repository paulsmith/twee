# Proposal: deterministic artifact finalization at session teardown

## Status

Proposal. `twee` is pre-release experimental software, so this proposal assumes
the CLI, daemon protocol, and JSON output may change without a compatibility
bridge.

Companion to [go-arg-cli-proposal.md](go-arg-cli-proposal.md), which is
implemented. This proposal addresses the findings that refactor did not cover,
recorded in external field notes from automating a vim edit/save/quit session
("twee gotchas", four documents plus a README).

## Findings triage

Each field-note finding was re-verified empirically against HEAD (`6525704`,
which differs from the notes' reference `4be5ea5` only by a Makefile change).
The results differ from the notes in important ways:

| Field-note finding | Claimed status | Verified behavior at HEAD | Remaining gap |
|---|---|---|---|
| Flags after the child command leak into child argv | Resolved | Confirmed resolved (`start [opts] -- cmd`) | None |
| Trailing `-name` swallowed as payload | Resolved | Confirmed resolved (`TWEE_SESSION`, global `--name`, `--`) | None |
| `wait text` positional ordering | Resolved, one sharp edge | Confirmed; `--pattern "-- X --"` space form fails with `missing value for --pattern` (exit 2), equals form works | Unhelpful error message |
| `start` reports `ok:true` for a child that dies instantly | Open, not re-verified | **Already fixed.** `bash -c 'exit 3'` returns `CHILD_EXITED` with `exit_code:3`, `socket_created:true`, exit 1; a non-executable returns an IO error at start time. The quick-exit contract from the go-arg proposal is implemented (`runDaemonChildReal`, 100ms observation window) | Stale `.lock` file left behind on failed start |
| Trace silently discarded if child exits before `trace stop` | Open, "re-verified still lost" | **Not lost.** The bundle is written via `Term.Close` → `trace.Close` during daemon teardown (`daemonize.go` exit path). Verified: valid zip with populated `stopped_at` | Three real gaps, below |

### Why the notes observed a "lost" trace

Finalization is asynchronous with respect to `wait exit`. The teardown
sequence after child exit is:

1. The exit goroutine in `runDaemonChildReal` sees `Term.ExitedCh()` close,
   sleeps 100ms (grace window — `wait exit` and `status` still answer), then
   stops the server and closes the listener.
2. `Server.Serve` drains in-flight connections and returns.
3. `Term.Close` runs: closes the PTY runner, waits for the output pump to
   drain, then finalizes the trace (zip + atomic rename to the `--out` path)
   and the recording.
4. The socket file is removed and the daemon exits.

`wait exit` responds in step 1; the bundle appears in step 3. Measured on
this machine: `wait exit; ls out.twee` reports *no such file*, and the bundle
exists ~100–200ms later. The notes' recipe (`twee wait exit` then `ls -l
probe.twee`) lands squarely in that window, observes nothing, and reasonably
concludes the recording was discarded. Nothing ever reports otherwise.

### The real gaps

1. **No synchronization point.** The natural "I'm done" call, `wait exit`,
   returns before artifacts are durable. Scripts that check or consume the
   bundle immediately afterward race with teardown and intermittently lose.
2. **The auto-finalized bundle is incomplete.** `handleTraceStop` captures a
   final screenshot before closing the trace; the teardown path does not, so
   child-exit bundles carry only `screenshots/0000.png` while `twee help
   trace` promises initial *and* final viewport screenshots.
3. **Nothing is reported.** No response ever carries the finalized path on
   the auto path. `trace stop` issued after teardown fails with a raw dial
   error (`code:IO`, "no such file or directory") that neither says the trace
   was already saved nor where.
4. **Stale `.lock` files.** Both natural child exit and failed starts leave
   `<name>.lock` in the state dir. Restarting the same name works (flock on
   the existing file), but the litter accumulates and `twee stop` on such a
   session fails on the dial without cleaning up.

## Goal

Make this contract hold, and make it observable:

> When `wait exit` returns, every artifact the session promised — trace
> bundle, recording — is durable on disk, complete, and the response says
> where it is.

Secondary goals: failed paths stay loud (per the field notes' meta-ask, "fail
loudly, not silently"), teardown leaves no litter, and the one remaining
parse-time trap guides the user to the fix.

## Design

### Split `Term.Close` into drain and finalize seams

`engine.Term` gains two idempotent methods that factor the body of `Close`:

- `DrainOutput()` — close the PTY runner, wait for the output pump to drain.
  After it returns, `Snapshot()` reflects the final terminal state, including
  any output emitted between the last client call and child exit.
- `FinalizeArtifacts() error` — close the active trace and recording (the
  current `Close` body after the pump wait), record the finalized trace path,
  and close a `done` channel exposed as `ArtifactsDone() <-chan struct{}`.
  A `FinalizedTracePath() string` getter returns the bundle path once written
  (empty before finalization or when no trace was active). This replaces
  reliance on `TracePath()` after close, which currently returns a stale
  config value.

`Term.Close` becomes `DrainOutput` + `FinalizeArtifacts` + existing error
bookkeeping, preserving current semantics for `twee run`, `codegen`, and
`tuitest`, which manage their own lifecycles.

### Reorder daemon teardown: finalize while the socket still answers

The exit goroutine in `runDaemonChildReal` becomes:

1. `<-te.ExitedCh()`
2. `te.DrainOutput()`
3. Capture the final screenshot (the existing `renderScreenshot` helper) and
   add it to the active trace, if any — same as `handleTraceStop`.
4. `te.FinalizeArtifacts()` — bundle is now durable; `ArtifactsDone` closes.
5. Grace sleep, `srv.Stop()`, listener close — unchanged.

Finalization must complete *before* `srv.Stop()`: the server is fully
answerable there, so blocked `wait exit` handlers can respond with the path.
(Placing work after `srv.Stop()` is already concurrent with listener
teardown.) Closing the runner in step 2 is safe — the child has exited — and
read-only handlers (`status`, `text`, `snapshot`) keep working from in-memory
terminal state during the grace window.

This also fixes gap 2: child-exit bundles get the final post-exit screenshot,
which the field notes point out is *more* faithful than the staged `:wq`
workaround frame.

### `wait exit` blocks on finalization and reports the path

`handleWaitExit`, after `WaitForExit` returns the exit code, additionally
waits on `ArtifactsDone()` bounded by the request's remaining timeout, and
`rpc.WaitExitData` gains:

```go
TracePath string `json:"trace_path,omitempty"`
```

The bounded wait means a pathological finalization failure degrades to the
current behavior (exit code, no path) rather than failing the wait. For
sessions with no trace, the gate adds only the finalization cost of closing
the recording, i.e. effectively nothing.

This supersedes the field notes' ask for a `wait exit --trace-out` flag: no
flag is needed when `wait exit` synchronizes implicitly.

### `trace stop` after auto-finalization answers usefully

During the grace window, a `trace stop` that arrives after auto-finalization
currently re-reads the stale path and silently drops its screenshot into a
closed trace. Instead, `handleTraceStop` checks `FinalizedTracePath()` first
and returns:

```json
{"ok":true,"data":{"path":"...","already_finalized":true}}
```

After the daemon is gone entirely, the client cannot know the path, but it
can stop being cryptic. The `trace stop` dial-failure error gets a hint
appended:

> session not found — if the child already exited, an active trace was
> finalized automatically to its `--out` path (see `twee help trace`)

While here, map dial failures uniformly: `trace stop` currently surfaces
`code:IO` where `status` surfaces `NOT_FOUND` for the same condition.

### `start --trace PATH`

Record the whole session from spawn to teardown, finalized automatically:

```sh
twee start --name s --trace run.twee -- vim file
```

Implementation: a `--trace` daemon option on `start` (propagated like
`--dir`/`--env` via the daemon env handoff). `runDaemonChildReal` enables the
trace immediately after `engine.Start` succeeds — before the readiness
window — so the bundle covers the child's first output byte. `codegen
--trace-out` already proves this full-session shape; `start --trace` covers
it without `codegen`'s event loop.

If the child exits during the 100ms quick-exit observation window with
`--trace` active, `Term.Close` on that path already finalizes the bundle;
add `trace_path` to the `CHILD_EXITED` error details so even a failed start
points at its recording.

### Remove lock files on daemon exit

The daemon removes its `.lock` alongside its socket on every exit path:
natural teardown, quick-exit, and startup failure. Because a concurrent
`start` may have already opened the old inode, `daemonize` guards the flock:
after acquiring it, `fstat` the held fd and `stat` the path; on inode
mismatch or unlink, close and retry (bounded). `twee stop`'s dial-failure
path opportunistically removes a stale lock when it can acquire the flock
itself (proving no live owner).

### Guide past the dash-leading-value trap

Keep the go-arg grammar decision from the CLI proposal — greedy value
consumption stays rejected (`--env` must consume exactly one value and must
never steal child argv). What changes is the failure: when a parse fails
with `missing value for --X` and the offending next token begins with `--`,
the `parseArg` wrapper (and `requireSeparateValues` for `--env`) appends:

> values beginning with '-' need the equals form: `--X=VALUE`

So the observed trap becomes:

```console
$ twee wait text --pattern "-- INSERT --" --timeout 3s
twee: wait text: missing value for --pattern (the next token "-- INSERT --"
begins with '-'; use --pattern="-- INSERT --")
```

### Document the lifecycle

`twee help trace` states: an active trace is finalized automatically when
the child exits; `wait exit` blocks until the bundle is durable and reports
`trace_path`; bundles always contain initial and final screenshots. README
gains the `start --trace` one-liner as the recommended way to record a full
session.

## Out of scope

- Greedy flag-value consumption — rejected with rationale in
  [go-arg-cli-proposal.md](go-arg-cli-proposal.md) ("Repeated `--env`",
  "Text Search Grammar").
- Persisting session metadata past daemon death (`ls` showing exited
  sessions, exit codes on disk). The recording file already persists the exit
  code when recording is enabled; a general session journal is a separate
  design.
- Changing the 100ms grace window or quick-exit observation window.

## Implementation sequence

1. Engine seams: `DrainOutput`, `FinalizeArtifacts`, `ArtifactsDone`,
   `FinalizedTracePath`; re-express `Term.Close` over them. Unit tests for
   idempotence and for `Close` equivalence.
2. Teardown reorder in `runDaemonChildReal`: drain → final screenshot →
   finalize → grace → stop. E2E test: bundle exists and contains both
   screenshots at the moment `wait exit` returns.
3. `wait exit` gating + `trace_path` in `WaitExitData`;
   `already_finalized` answer for late `trace stop`; uniform `NOT_FOUND` for
   dial failures; dial-failure hint text.
4. `start --trace`, including `trace_path` in quick-exit details.
5. Lock removal on all daemon exit paths + flock inode guard + `stop`
   stale-lock cleanup.
6. Parser error hints in `parseArg` / `requireSeparateValues`.
7. Help text and README updates.
8. `go test ./...` plus the acceptance list below.

Steps 1–3 deliver the contract and can land alone; 4–7 are independent.

## Acceptance tests

- `trace start --out X` on a session whose child exits on its own: at the
  instant `wait exit` returns, `X` exists, is a valid zip with
  `manifest.json` (populated `stopped_at`), `events.jsonl`, and two
  screenshots; the `wait exit` response carries `trace_path:X`. Run the
  existence check in the same shell pipeline as `wait exit` to pin the race.
- Same with no `--out`: `trace_path` reports the generated temp path.
- `wait exit` on a session with no trace: unchanged response shape, no
  `trace_path` key.
- `trace stop` during the grace window after child exit returns
  `already_finalized:true` and the path; after daemon death it returns the
  hint text.
- `start --trace run.twee -- vim file` followed by quit: bundle covers
  startup output; `wait exit` reports the path. With `-- bash -c 'exit 3'`:
  `CHILD_EXITED` details include `trace_path` and the bundle exists.
- After natural exit, quick-exit, startup failure, and `twee stop`, the state
  dir contains neither `<name>.sock` nor `<name>.lock`; the same name can be
  started immediately afterward; concurrent `start` races on the same name
  yield exactly one daemon.
- `twee wait text --pattern "-- INSERT --" --timeout 1s` fails with the
  equals-form hint; `--pattern="-- INSERT --"` still parses;
  `--env -- vim` and bare trailing `--env` keep their `missing value`
  errors (now with hint where the next token is dash-leading).
- Recording parity: `record start` followed by child exit produces a durable
  recording (with `WriteExit`) by the time `wait exit` returns.
