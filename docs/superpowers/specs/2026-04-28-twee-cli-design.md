# Design: `twee` CLI — Vibium-style harness for TUIs

## Status

Draft for implementation. Brainstormed with Paul on 2026-04-28.

## Vision

`twee` becomes a CLI tool that lets AI agents (primary user) and humans
(secondary) drive terminal UIs the way [Vibium](https://www.npmjs.com/package/vibium)
drives browsers: spawn a TUI under a PTY in a long-running daemon, then
issue subcommands (`type`, `wait`, `screenshot`, `text`, `find`) against
it. Agents call `twee` directly via `bash`. There is no MCP server; the
JSON-by-default CLI is the entire surface.

The existing Go test API (`tuitest`) stays first-class and keeps
working. Both `tuitest` (Go test library) and `cmd/twee` (CLI + daemon)
consume a shared `internal/engine` package built from the existing
internal components.

## Non-goals

- MCP integration. Out of v0; agents call the CLI directly.
- Multiple sessions per daemon. One TUI per daemon; run multiple
  daemons for parallel sessions.
- Mouse input (click/hover/drag). Deferred per the existing v0 design.
- Browser-specific Vibium ops: `frames`, `cookies`, `storage`, `pdf`,
  `download`, `upload`, `dialog`, `back`/`forward`/`reload`, `url`,
  `attr`/`html`/`value`, `check`/`uncheck`/`select`, `a11y-tree`,
  `map`, `eval`, `geolocation`, `media`, `pipe`.
- Pixel-comparison golden tests for screenshots. Visual correctness is
  hand-verified at v0.

## Architecture

```
tuitest/                      — public Go test API (shape unchanged)
cmd/twee/                     — CLI binary entry point
internal/engine/              — shared engine: Term, lifecycle, queries
                                (composes ptyrunner + pump + vt + input)
internal/daemon/              — socket server, RPC handlers
internal/rpc/                 — request/response types, JSON codec
internal/render/              — cell-grid → PNG renderer (new)
internal/ptyrunner/           — unchanged
internal/pump/                — unchanged
internal/vt/                  — unchanged (libghostty-vt wrapper)
internal/input/               — unchanged
internal/recording/           — unchanged
internal/snapshot/            — unchanged
```

`tuitest.Run` and `tuitest.Start` become thin wrappers around
`engine.New`. `cmd/twee start` instantiates an `engine.Term`, hands it
to a `daemon.Server`, which serves RPCs over a Unix socket.
`cmd/twee <verb>` opens the socket, sends one request, prints the JSON
response, exits.

### Invariants

- The engine is the single owner of a `Term`. The daemon serializes
  RPCs onto it; multiple concurrent requests are fine because the
  engine's existing mutex + cond mechanism remains the synchronization
  primitive.
- The CLI binary and daemon are the same binary. `start` re-execs
  itself with a hidden `__daemon` flag to enter daemon mode. Single-shot
  `run` runs the daemon in-process.
- `libghostty-vt` is not visible to `cmd/twee` — only `internal/engine`
  and `internal/vt` see it.

## Session model

One TUI per daemon. Multiple daemons supported via named sessions.
Single-shot mode for batch use.

- **Default name** `default` → socket at `$XDG_STATE_HOME/twee/default.sock`
  (Linux), `$HOME/Library/Application Support/twee/default.sock`
  (macOS), or `$TMPDIR/twee-$USER/default.sock` as fallback.
- **`--name foo`** → same dir, `foo.sock`.
- **Single-shot ephemeral**: `os.MkdirTemp` → `<tmp>/twee.sock`,
  removed on exit.
- Permission `0700` on the dir, `0600` on the socket.

Each named daemon is guarded by a `<dir>/<name>.lock` file held with
BSD `flock(2)` (via `syscall.Flock`, available on Linux and macOS).
The lock fd is inherited across re-exec (child uses `ExtraFiles` on
the `*exec.Cmd`); `flock` advisory locks are preserved across
`execve(2)` on both platforms. Stale locks (process dead) are
reclaimed on startup by attempting a non-blocking `flock` and, if it
succeeds, overwriting the lock-file contents with the new PID.

## Command surface

### Lifecycle

- `twee start <cmd> [args...] [--name foo] [--cols 80] [--rows 24] [--env K=V] [--dir path]`
  Spawn TUI under a PTY in a new daemon. Forks into background. Prints
  `{name, socket, pid}`.
- `twee stop [--name foo]` SIGTERM → 250ms → SIGKILL the child, shut
  down daemon, remove socket and lock.
- `twee ls` List running daemons. Each entry uses the same shape as
  `twee status`. Entries whose socket cannot be reached are omitted.
- `twee status [--name foo]` Returns `{name, socket, pid, cmd: [string], size: Size, started_at: RFC3339, running: bool, exit_code: int|null}`. `running` is `true` while the child is alive; once it exits, `running` becomes `false` and `exit_code` is populated.
- `twee run <cmd> [args...] --script ops.json` Single-shot: ephemeral
  daemon, run script, exit.

### Input

- `twee type <text>` Write literal text to the PTY.
- `twee key <name>` Send one named key (`Enter`, `Down`, `Ctrl+C`, ...).
- `twee keys <seq...>` Convenience: chain multiple keys.
- `twee paste <text>` Bracketed paste.
- `twee signal <name>` Send `SIGWINCH`/`SIGINT`/`SIGTERM`/etc. to child.

### Queries

- `twee text` Visible viewport as a single string.
- `twee lines` Viewport as `string[]`.
- `twee cell <x> <y>` Single cell with style.
- `twee region <x> <y> <w> <h>` Rectangle of cells.
- `twee cursor` `{x, y, visible, shape}`.
- `twee find <substr> [--regex]` Matches: `[{x, y, w, h, line, text}]`.
- `twee size` `{cols, rows}`.
- `twee title` `{title}` (OSC 0/2).
- `twee mode` Active VT modes: `{decckm, bracketed_paste, alt_screen, mouse, ...}`.
- `twee scrollback` Scrollback lines (only if retention enabled at start).
- `twee snapshot` Full `Snapshot` (Size + Cursor + Lines + Cells).

### State changes

- `twee resize <cols> <rows>` TIOCSWINSZ + SIGWINCH + model resize.
- `twee screenshot [--out path.png]` Render to PNG.
- `twee record start|stop [--out path.jsonl]` Toggle recording on the
  running session.
- `twee diff --against path` Text diff between the current visible
  viewport and a previously saved text-snapshot file (the format
  written by `internal/snapshot/text.go`). `data` is
  `{equal: bool, unified: string, current: string, expected: string}`
  where `unified` is a unified-diff (3 lines of context) string. The
  process exits 0 even when the screens differ — callers branch on
  `data.equal`. Use case: an agent inspecting drift between expected
  and actual screen state without writing a file.

### Waits

- `twee wait text <substr> [--timeout 5s] [--regex]`
- `twee wait no-text <substr> [--timeout 5s]`
- `twee wait stable [--quiet 100ms] [--timeout 5s]`
- `twee wait cursor <x> <y> [--timeout 5s]`
- `twee wait exit [--timeout 30s]` `{exit_code}`.

### Misc

- `twee sleep <duration>`, `twee version`, `twee help`,
  `twee completion <shell>`.

## Single-shot mode & script format

Ephemeral daemon, runs commands, exits.

```
twee run ./myapp --script ops.json
twee run ./myapp < ops.json    # stdin
```

Script is a JSON array of RPC request bodies (without the `id` field —
the runner injects monotonic ids). Each entry is the *exact* wire
shape of a daemon request — `op` uses the RPC names from the right
column of the mapping table below (e.g. `wait_text`), not the CLI
verb form (e.g. `wait text`). The script is a literal transcript of
RPC requests, so there is no second grammar.

```json
[
  {"op": "wait_text", "args": {"text": "Choose an option", "timeout": "5s"}},
  {"op": "key", "args": {"key": "Down"}},
  {"op": "wait_text", "args": {"text": "> second"}},
  {"op": "key", "args": {"key": "Enter"}},
  {"op": "wait_text", "args": {"text": "selected: second"}},
  {"op": "screenshot", "args": {"out": "out.png"}}
]
```

Default: silent on success, structured error on failure. With
`--emit results`, each op's response is streamed to stdout as one JSON
object per line (NDJSON). With `--text`, errors render as a
human-readable diagnostic block.

### CLI verb → RPC op mapping

The CLI's positional verb syntax maps mechanically to the wire format.
Multi-word verbs join with `_`; flags become keys under `args`;
positional arguments map by op-specific name (documented per op via
`twee help <verb>`).

| CLI invocation | Wire `{op, args}` |
|---|---|
| `twee type "hello"` | `{"op": "type", "args": {"text": "hello"}}` |
| `twee key Enter` | `{"op": "key", "args": {"key": "Enter"}}` |
| `twee paste "x\ny"` | `{"op": "paste", "args": {"text": "x\ny"}}` |
| `twee signal SIGWINCH` | `{"op": "signal", "args": {"name": "SIGWINCH"}}` |
| `twee text` | `{"op": "text", "args": {}}` |
| `twee lines` | `{"op": "lines", "args": {}}` |
| `twee cell 3 5` | `{"op": "cell", "args": {"x": 3, "y": 5}}` |
| `twee region 0 0 80 24` | `{"op": "region", "args": {"x": 0, "y": 0, "w": 80, "h": 24}}` |
| `twee cursor` | `{"op": "cursor", "args": {}}` |
| `twee find "Saved" --regex` | `{"op": "find", "args": {"text": "Saved", "regex": true}}` |
| `twee size` | `{"op": "size", "args": {}}` |
| `twee title` | `{"op": "title", "args": {}}` |
| `twee mode` | `{"op": "mode", "args": {}}` |
| `twee scrollback` | `{"op": "scrollback", "args": {}}` |
| `twee snapshot` | `{"op": "snapshot", "args": {}}` |
| `twee resize 100 30` | `{"op": "resize", "args": {"cols": 100, "rows": 30}}` |
| `twee screenshot --out f.png` | `{"op": "screenshot", "args": {"out": "f.png"}}` |
| `twee record start --out r.jsonl` | `{"op": "record_start", "args": {"out": "r.jsonl"}}` |
| `twee record stop` | `{"op": "record_stop", "args": {}}` |
| `twee diff --against snap.txt` | `{"op": "diff", "args": {"against": "snap.txt"}}` |
| `twee wait text "Saved" --timeout 5s` | `{"op": "wait_text", "args": {"text": "Saved", "timeout": "5s"}}` |
| `twee wait no-text "Loading"` | `{"op": "wait_no_text", "args": {"text": "Loading"}}` |
| `twee wait stable --quiet 100ms` | `{"op": "wait_stable", "args": {"quiet": "100ms"}}` |
| `twee wait cursor 0 3` | `{"op": "wait_cursor", "args": {"x": 0, "y": 3}}` |
| `twee wait exit` | `{"op": "wait_exit", "args": {}}` |
| `twee sleep 200ms` | `{"op": "sleep", "args": {"duration": "200ms"}}` |

Lifecycle verbs (`start`, `stop`, `ls`, `status`, `run`) are
client-side concerns: `start` re-execs into daemon mode, `ls` scans
the state dir, `run` runs an ephemeral daemon. They do not appear in
this mapping.

`keys` is also client-side: `twee keys Down Down Enter` desugars into
three sequential `{"op": "key"}` requests over the same connection.

## JSON output schema

### Envelope

Every command, success or failure, prints exactly one JSON value to
stdout and exits 0 (success) or non-zero (failure). Logs go to stderr,
not stdout.

```json
{"ok": true, "data": {...}}
{"ok": false, "error": {"code": "TIMEOUT", "message": "...", "details": {...}}}
```

`data` is shaped per command (`null` for void ops like `type`).

### Error codes (closed set)

- `TIMEOUT` — wait expired.
- `NOT_FOUND` — daemon not running for that name; or text not found in
  a non-wait query.
- `ALREADY_RUNNING` — `start` collided with an existing daemon of the
  same name.
- `CHILD_EXITED` — operation requires a live child; child has exited.
- `INVALID_ARGUMENT` — bad flag, malformed script, out-of-range coords.
- `IO` — socket/PTY/file error.
- `INTERNAL` — bug; includes a stack hint.

`details` is a structured rendering of the same diagnostic block
`tuitest` produces today on test failure. Stable keys:

- `last_screen: string` — visible viewport at failure time, newline-joined.
- `recent_bytes_b64: string` — last ~4KB of PTY output, base64.
- `cursor: Cursor`
- `size: Size`
- `cmd: [string]` — the command line of the child.
- `exit_code: int|null` — populated if the child has exited.
- `cause: string` — original Go error string, for `IO`/`INTERNAL`.

Both code paths (CLI errors and `tuitest.Fatalf`) produce identical
JSON; the human-readable rendering is built from this struct.

### Reused types

- `Cell {text, width, fg, bg, bold, dim, underline, inverse}`
- `Color` — string: `"default"`, `"red"` (named SGR), `"p123"`
  (256-palette), `"#rrggbb"` (truecolor). Same normalization the
  existing cell snapshots use.
- `Cursor {x, y, visible, shape}`
- `Size {cols, rows}`
- `Match {x, y, w, h, line, text}`
- `Snapshot {size, cursor, lines: [{cells: [Cell]}]}`

### `--text` mode

Flips output to human-readable: `text` prints the screen, `find`
prints `x y\ttext` per line, `cell` prints a single styled rendering.
Errors print the same diagnostic block `tuitest` produces. Exit codes
unchanged.

## Daemon RPC

### Transport

Unix domain socket. One connection per CLI invocation, opened, used
for one request, closed. No multiplexing.

### Wire format

Length-prefixed JSON. Each message is `<u32 big-endian length><JSON bytes>`.
One request, one response. Stdlib only (`encoding/json`,
`encoding/binary`, `net`).

```json
// request
{"id": "uuid-or-monotonic", "op": "wait_text", "args": {"text": "Saved", "timeout": "5s"}}

// response
{"id": "...", "ok": true, "data": {...}}
{"id": "...", "ok": false, "error": {"code": "TIMEOUT", "message": "...", "details": {...}}}
```

The CLI's stdout JSON is the response body verbatim — no translation.

### Concurrency

The daemon accepts connections in a goroutine per conn. Ops dispatch
onto the engine's existing mutex — `Wait*` ops block on `cond.Wait`
exactly as today; `Type/Key/Resize` take the mutex briefly. Multiple
concurrent waiters are supported. The daemon adds no ordering
guarantees beyond the engine's existing semantics: a `type` op
arriving while two `wait_text` ops are blocked behaves exactly as it
would in the Go test API — the bytes are written to the PTY, the pump
processes the resulting output, and waiters re-evaluate their
predicates. Op processing within a single connection is sequential
(one request, one response).

### Lifecycle

- `twee start ./myapp [--name foo]`:
  1. Acquire `<dir>/<name>.lock` via `flock`. If locked, error
     `ALREADY_RUNNING`.
  2. Re-exec `cmd/twee` with a hidden `__daemon` flag and the lock fd
     inherited; parent waits on a pipe for "ready".
  3. Child: opens socket, writes `{name, socket, pid}` to the ready
     pipe, closes stdio, runs the engine + RPC loop. The daemon exits
     only on (a) explicit `stop` op, (b) a fatal signal (SIGTERM/INT),
     or (c) the child TUI process exiting — at which point the daemon
     finishes its read drain, then tears down. There is no inactivity
     timer.
  4. Parent: prints `{name, socket, pid}` and exits.
- `twee stop [--name foo]`: connect, send `stop`, daemon SIGTERMs the
  child (250ms grace → SIGKILL), closes socket, removes socket and
  lock, exits.
- Stale locks (process dead) are reclaimed on startup.

### Signals

- `twee signal SIGWINCH`/`SIGINT`/etc. forwards to the child via
  `os.Process.Signal`.
- The daemon handles SIGTERM/SIGINT itself by running the same
  teardown as `twee stop`.

### Errors at the boundary

Any syscall/IO error inside the engine becomes an `IO`-coded error on
the wire with the original error string in `details.cause`. `INTERNAL`
is reserved for "this is a bug" cases (panics caught by a recover in
the RPC handler).

## Screenshot rendering

`internal/render/` turns a cell-grid `Snapshot` into a PNG. Software
rasterization, pure Go, no GPU.

1. **Font.** One monospace TTF bundled via `embed`. Default candidate:
   JetBrains Mono or IBM Plex Mono (both SIL OFL, free to bundle).
   Single regular weight at v0.
2. **Glyph rasterizer.** `golang.org/x/image/font` +
   `golang.org/x/image/font/opentype`. Both are Go subrepos under the
   `golang.org/x` umbrella, maintained by the Go team. Approved as a
   third-party dep for this purpose.
3. **Compositor.** For each cell `(x, y)`: fill bg rect at
   `(x*cw, y*ch, cw, ch)`; rasterize glyph at `(x*cw, y*ch + baseline)`
   in fg; bold synthesized as a 1px offset double-draw; underline as a
   1px line at the bottom of the cell.
4. **Output.** `image/png` (stdlib) to file path or stdout.

The render path consumes only a `Snapshot`; it has no dependency on
libghostty-vt or the daemon.

### v0 scope cuts

- One bundled font; no `--font` flag.
- Bold via synthetic stroke; no real bold face.
- No emoji or color-glyph rendering. Wide cells get the glyph at the
  leftmost cell; continuation cells render as space.
- No DPI scaling; fixed cell metrics.
- Cursor not drawn (`--cursor` flag is a v1 add).
- No title bar / chrome — just the cell grid.

## Recording integration

`twee record start|stop` toggles the existing `internal/recording`
JSONL writer on the live session. `twee diff --against path` does text
diff via `internal/snapshot`. The format is unchanged from
`plan.md`/`docs/plan-libghostty.md`.

## Testing strategy

Three layers, each cheaper to debug than the one above.

### 1. Engine tests

Existing `tuitest/`, `internal/vt`, `internal/pump`, `internal/input`
tests keep working unchanged once `internal/engine` is extracted. The
M0 refactor is mostly moving private helpers into `internal/engine` and
re-exporting from `tuitest/`. `go test -race ./...` must pass at every
commit.

### 2. Daemon RPC tests (`internal/daemon`)

No CLI shell-out, no fork. Test creates a `daemon.Server` in-process,
listens on an `os.MkdirTemp` socket, dials it as a client, sends framed
JSON requests, asserts responses. Uses `fixtures/menu` as the spawned
TUI. Covers:

- Request/response framing.
- All op handlers (one happy-path test per op; one error-path test per
  error code).
- Concurrent waiters (two `wait_text` ops on the same daemon, one
  resolves, the other times out).
- Stop semantics: child reaped, socket removed, lock released.
- Stale-lock recovery on second `start`.

### 3. CLI integration tests (`cmd/twee`)

End-to-end against the menu fixture. Tests `go build` the `twee` binary
into `$TMPDIR` once per package and shell out. Covers:

- `start` / `stop` round-trip with the default name.
- Single-shot `run` with the README demo as a literal script.
- Parallel sessions: `start --name a` + `start --name b`.
- `screenshot` writes a non-empty PNG with the right dimensions.
- `--text` vs JSON output for one query op.
- Recording: `record start` → drive → `record stop` → replay JSONL into
  a fresh `vt.Model`, assert final screen matches.

### Flake guard

200-iteration loop running the menu single-shot script under `-race`.
Must be 0% flaky before declaring done.

## Milestones

Each is independently shippable and testable.

### M0 — engine extraction (½ day)

Move PTY/pump/input/term composition out of `tuitest/` into
`internal/engine/`. `tuitest/term.go` becomes a thin wrapper:
`tuitest.Run` calls `engine.New` and registers `t.Cleanup`. No new
behavior. Exit: `go test -race ./...` green.

### M1 — `cmd/twee` skeleton + RPC types (1 day)

Add `cmd/twee/main.go` with subcommand routing (decide cobra vs stdlib
`flag.NewFlagSet` here; lean stdlib unless UX demands otherwise). Add
`internal/rpc` with request/response types and length-prefixed JSON
codec. Add `version`, `help`. No daemon yet. Exit: `twee version`
prints; `twee help` lists planned subcommands.

### M2 — daemon + lifecycle (2 days)

`internal/daemon` server. `twee start` (re-exec to background),
`twee stop`, `twee ls`, `twee status`. Lock file + socket discovery.
No ops besides lifecycle. Exit:
`twee start ./fixtures/menu/menu && twee status && twee stop` works;
`twee ls` shows running daemons. PTY semantics tested on Linux and
macOS.

### M3 — input + queries + waits (2 days)

Wire all ops listed under "Command surface" except `screenshot`,
`record`, `diff`. JSON envelope, error codes, `--text` mode. Daemon
RPC tests for each op. Exit: an agent can drive the menu fixture
end-to-end via `twee` calls.

### M4 — single-shot `run` + script format (1 day)

`twee run ./myapp --script ops.json`. NDJSON `--emit results`. Stdin
script. Cleanup on error. Exit: README's demo runs as a single-shot
script.

### M5 — recording integration (½ day)

`twee record start|stop` toggles the existing `internal/recording`
writer on the live session. `twee diff --against path`. Exit:
record→replay round-trip from the CLI passes.

### M6 — screenshot rendering (2-3 days)

`internal/render`: bundle font, glyph compositor, PNG encode.
`twee screenshot --out path.png`. Exit: rendering the menu fixture
produces a visually correct PNG (eyeballed); pixel header/size tests
pass.

### M7 — flake harness + docs (1 day)

200-iteration single-shot loop under `-race`. Update README with CLI
quickstart and an agent-facing usage section. Document deferred items
(mouse, MCP, multi-session-per-daemon). Exit: README ready, flake
harness 0%.

**Total estimate:** ~10-11 working days.

## Risk register (additions)

On top of the original `plan.md` risks:

| Risk | Likelihood | Mitigation |
|---|---|---|
| Fork-to-background portability differs Linux vs macOS | Medium | Re-exec with inherited fds is the established Go pattern; integration-test on both early in M2. |
| Font bundling bloats binary | Low | One TTF is 100-300KB; tolerable. Add `--font` flag in v1 if needed. |
| `golang.org/x/image` API churn | Low | Pin the version. The font/opentype packages are stable. |
| `cobra` vs stdlib `flag` decision delays M1 | Low | Decide by end of M1 day 1. Default to stdlib. |
| Cell-grid → PNG visual correctness across platforms | Medium | No pixel goldens in v0. Hand-verify on the menu fixture and document known limitations (no emoji, no real bold). |

## Open questions

1. Cobra or stdlib `flag` for subcommand routing? Default stdlib unless
   M1 finds friction.
2. Bundle JetBrains Mono or IBM Plex Mono? Either works; pick by file
   size in M6.
3. Should `twee ls` discover daemons by scanning the state dir or by
   pinging each socket? Default: scan + ping in parallel, drop any
   unreachable.
