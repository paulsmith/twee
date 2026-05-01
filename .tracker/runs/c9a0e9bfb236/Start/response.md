TURN 1
TOOL CALL: bash
INPUT:
{"command": "pwd && ls -la && git status 2>/dev/null || echo \"Not a git repo\""}
TOOL RESULT: bash
OUTPUT:
/Users/paul/projects/research/twee
total 76
drwxr-xr-x 26 paul staff   832 May  1 10:15 .
drwxr-xr-x  8 paul staff   256 Apr 29 06:20 ..
drwxr-xr-x  6 paul staff   192 May  1 10:14 .direnv
-rw-r--r--  1 paul staff   144 May  1 10:12 .envrc
drwxr-xr-x 11 paul staff   352 May  1 10:15 .git
-rw-r--r--  1 paul staff    37 May  1 10:14 .gitignore
drwxr-xr-x  5 paul staff   160 Apr 29 10:27 .jj
drwxr-xr-x  3 paul staff    96 May  1 10:15 .tracker
-rw-r--r--  1 paul staff   860 Apr 29 09:05 AGENTS.md
-rw-r--r--  1 paul staff    11 Apr 28 20:36 CLAUDE.md
-rw-r--r--  1 paul staff   451 Apr 28 20:41 CMakeLists.txt
-rw-r--r--  1 paul staff  1199 Apr 28 22:30 Makefile
-rw-r--r--  1 paul staff  8067 Apr 29 10:32 README.md
drwxr-xr-x  4 paul staff   128 Apr 29 10:07 bin
drwxr-xr-x  8 paul staff   256 Apr 28 20:46 build
drwxr-xr-x  4 paul staff   128 Apr 29 06:46 cmd
-rw-r--r--  1 paul staff 15205 Apr 28 20:34 design.md
drwxr-xr-x  4 paul staff   128 Apr 28 21:58 docs
drwxr-xr-x  3 paul staff    96 Apr 28 20:34 fixtures
-rw-r--r--  1 paul staff  3018 Apr 28 20:42 flake.lock
-rw-r--r--  1 paul staff  1872 Apr 28 21:04 flake.nix
-rw-r--r--  1 paul staff   313 Apr 28 22:40 go.mod
-rw-r--r--  1 paul staff  1026 Apr 28 22:40 go.sum
drwxr-xr-x 12 paul staff   384 Apr 28 22:40 internal
-rw-r--r--  1 paul staff 11957 Apr 28 20:50 plan.md
drwxr-xr-x 14 paul staff   448 Apr 28 22:27 tuitest
Not currently on any branch.
Untracked files:
  (use "git add <file>..." to include in what will be committed)
	.tracker/

nothing added to commit but untracked files present (use "git add" to track)

TOOL CALL: bash
INPUT:
{"command": "git log --oneline -5 2>/dev/null || echo \"No git history\""}
TOOL RESULT: bash
OUTPUT:
a121c5a Don't track .envrc
d0aea6b Add demo video link to README
65f10c0 Initial commit

TURN 2
TOOL CALL: bash
INPUT:
{"command": "cat README.md"}
TOOL RESULT: bash
OUTPUT:
# twee — drive TUIs from the shell

`twee` is a command-line tool for spawning a terminal UI under a PTY
and driving it from outside: type, press keys, query the screen, wait
for text, take screenshots. Every command prints one JSON object and
exits, so it composes well from `bash`, scripts, and AI agents.

**Demo:** [Watch the 2-minute walkthrough on YouTube](https://www.youtube.com/watch?v=5TVU-ACDD1A).

```
$ twee start ./myapp
{"ok":true,"data":{"name":"default","socket":"...","pid":12345}}

$ twee wait text "Choose an option"
{"ok":true,"data":null}

$ twee key Down
{"ok":true,"data":null}

$ twee text
{"ok":true,"data":{"text":"...visible viewport..."}}

$ twee screenshot --out /tmp/myapp.png
{"ok":true,"data":{"out":"/tmp/myapp.png","width":640,"height":480}}

$ twee stop
{"ok":true,"data":{"name":"default","stopped":true}}
```

## Why

Agents and scripts can already drive *web* UIs — Playwright, Vibium,
and friends do exactly this. `twee` is the same idea aimed at terminal
UIs: a long-running daemon owns the PTY and a VT model of the screen,
and the CLI is a thin client that pokes it. JSON-by-default makes it
trivially scriptable; an agent doesn't need an MCP server, just
permission to run `twee` via `bash`.

## Install

`twee` uses `libghostty-vt` (a CGO library built from the
[Ghostty](https://ghostty.org) source tree) for VT parsing. The
provided Nix flake pulls Zig, CMake, Go, and pkg-config into a dev
shell; CMake's `FetchContent` fetches and builds `libghostty-vt` on
first use.

```
nix develop                         # enter the dev shell
make twee                           # builds libghostty-vt + ./bin/twee
./bin/twee version
```

A vanilla `go build` outside the dev shell will fail because the cgo
package needs `libghostty-vt` on the pkg-config path. Toolchain
requirements: CGO, Zig 0.15.2, and (on macOS) Apple Developer Tools —
libghostty-vt's zig build assembles an xcframework on darwin and needs
`xcodebuild`.

## Model

One TUI per daemon. Multiple daemons run in parallel via `--name`.
The default name is `default`; sockets live under `$XDG_STATE_HOME/twee`
on Linux and `~/Library/Application Support/twee` on macOS, with
`0700` on the directory and `0600` on the socket.

`twee start` forks a daemon in the background, prints `{name, socket,
pid}`, and exits. Subsequent commands (`type`, `key`, `wait`, `text`,
…) connect to that daemon's socket, send one request, print the
response, and exit. `twee stop` SIGTERMs the child, waits 250ms,
escalates to SIGKILL, and removes the socket.

For a one-shot run with no daemon to manage, see `twee run` below.

## Command reference

Run `twee help` for the top-level list and `twee help <verb>` (when
available) for per-verb usage.

### Lifecycle

| Verb | Purpose |
|---|---|
| `start <cmd> [args...]` | Spawn TUI in a new daemon. Flags: `--name`, `--cols`, `--rows`, `--dir`, `--env K=V` (repeatable). |
| `stop` | Stop the daemon. |
| `ls` | List running daemons. |
| `status` | `{name, socket, pid, cmd, size, started_at, running, exit_code}`. |
| `run <cmd> [args...] --script ops.json` | Single-shot ephemeral session. |

### Input

| Verb | Wire op | Notes |
|---|---|---|
| `type <text>` | `type` | Literal text to the PTY. |
| `key <name>` | `key` | One named key (`Enter`, `Down`, `Ctrl+C`, …). |
| `keys <name...>` | desugars to N `key` calls | Convenience. |
| `paste <text>` | `paste` | Bracketed paste (DEC mode 2004). |
| `signal <name>` | `signal` | `SIGWINCH`, `SIGINT`, `SIGTERM`, … to the child. |

### Queries

| Verb | Wire op | Returns |
|---|---|---|
| `text` | `text` | Visible viewport as one string. |
| `lines` | `lines` | Viewport as `string[]`. |
| `cell <x> <y>` | `cell` | One cell with style. |
| `region <x> <y> <w> <h>` | `region` | Rectangle of cells. |
| `cursor` | `cursor` | `{x, y, visible, shape}`. |
| `find <text> [--regex]` | `find` | `[{x, y, w, h, line, text}]`. |
| `size` | `size` | `{cols, rows}`. |
| `title` | `title` | OSC 0/2 title. |
| `mode` | `mode` | Active VT modes (`alt_screen`, …). |
| `scrollback` | `scrollback` | Scrollback lines. |
| `snapshot` | `snapshot` | Full `{size, cursor, lines, cells}`. |

### State changes

| Verb | Wire op | Notes |
|---|---|---|
| `resize <cols> <rows>` | `resize` | TIOCSWINSZ + SIGWINCH + model resize. |
| `screenshot [--out path.png]` | `screenshot` | PNG to disk; without `--out`, response includes `png_base64`. |
| `record start [--out path.jsonl]` / `record stop` | `record_start` / `record_stop` | Toggle JSONL recording. |
| `diff --against path` | `diff` | Compare current viewport to a saved text snapshot. Always exits 0; branch on `data.equal`. |

### Waits

All waits accept `--timeout <duration>` (default 5s, except `wait
exit` which defaults to 30s). Failure exits non-zero with code
`TIMEOUT`.

| Verb | Wire op |
|---|---|
| `wait text <substr> [--regex]` | `wait_text` |
| `wait no-text <substr>` | `wait_no_text` |
| `wait stable [--quiet 100ms]` | `wait_stable` |
| `wait cursor <x> <y>` | `wait_cursor` |
| `wait exit` | `wait_exit` |

### Misc

`sleep <duration>` (client-side), `version`, `help`,
`completion <bash|zsh|fish>`.

## JSON envelope

Every invocation prints exactly one JSON value to stdout and exits 0
on success or non-zero on failure. Logs go to stderr, not stdout.

```json
{"ok": true, "data": {...}}
{"ok": false, "error": {"code": "TIMEOUT", "message": "...", "details": {...}}}
```

`data` is shaped per command (`null` for void ops like `type`).

### Error codes

| Code | Meaning |
|---|---|
| `TIMEOUT` | Wait expired. |
| `NOT_FOUND` | No daemon for that name; or text not found in a non-wait query. |
| `ALREADY_RUNNING` | `start` collided with an existing daemon of that name. |
| `CHILD_EXITED` | Operation requires a live child; child has exited. |
| `INVALID_ARGUMENT` | Bad flag, malformed script, out-of-range coords. |
| `IO` | Socket / PTY / file error. |
| `INTERNAL` | Bug; includes a stack hint. |

`error.details` includes a structured diagnostic block: `last_screen`,
`recent_bytes_b64` (last ~4KB of PTY output), `cursor`, `size`, the
child's `cmd`, `exit_code` if it has exited, and a `cause` string for
`IO` / `INTERNAL`.

## Single-shot scripts

`twee run` spins up an ephemeral daemon, executes a JSON script of
ops, then exits — useful in CI or ad-hoc one-liners.

```
$ cat ops.json
[
  {"op": "wait_text", "args": {"text": "Choose an option", "timeout": "5s"}},
  {"op": "key", "args": {"key": "Down"}},
  {"op": "wait_text", "args": {"text": "> second"}},
  {"op": "key", "args": {"key": "Enter"}},
  {"op": "screenshot", "args": {"out": "out.png"}}
]

$ twee run ./myapp --script ops.json
{"ok":true,"data":{"ops":5}}
```

The script is a JSON array of RPC request bodies — `op` uses the wire
name from the tables above (e.g. `wait_text`, not `wait text`). Pass
`--script -` (or omit `--script`) to read from stdin. With
`--emit results`, each op's response is streamed as NDJSON instead of
the summary envelope.

## Parallel sessions

```
$ twee start --name a ./app-a
$ twee start --name b ./app-b
$ twee ls
$ twee text --name a
$ twee stop --name a
$ twee stop --name b
```

Every verb that talks to a daemon accepts `--name`.

## Limitations

- No mouse input (`click`/`hover`/`drag`). Keys, paste, type, signal
  only.
- One TUI per daemon. Use multiple daemons via `--name` for parallel
  sessions.
- `wait stable` will hang on apps with always-running spinners. Use
  `wait text` instead. Region-exclusion is a future feature.
- No Kitty keyboard protocol, no DECCKM-aware cursor keys, no
  scrollback retention by default.
- Title and mode reporting beyond `alt_screen` return defaults until
  the underlying VT layer exposes more state.
- Screenshots use synthetic bold and render emoji cells as the
  leftmost glyph plus space.
- macOS-tested. Linux should work but isn't exercised in this POC.

## Spec

The wire format and full op set are specified in
[`docs/superpowers/specs/2026-04-28-twee-cli-design.md`](docs/superpowers/specs/2026-04-28-twee-cli-design.md).

TOOL CALL: bash
INPUT:
{"command": "cat AGENTS.md && echo \"---\" && cat CLAUDE.md"}
TOOL RESULT: bash
OUTPUT:
Use `go doc` and `gopls` liberally when writing, reading, understanding, and
debugging Go code.

## Pushing a new bookmark with jj

`jj git push --remote <r> -b <bookmark> --allow-new` still works but prints a
deprecation warning. The replacement is to mark the bookmark as tracked on
the remote *before* pushing, e.g.:

```
jj bookmark track <bookmark>@<remote>   # if the remote ref already exists
# or, for a brand-new bookmark, configure auto-tracking once:
jj config set --repo 'git.auto-local-bookmark' true
# (or set remotes.<remote>.auto-track-bookmarks per the jj docs)
```

If a previous PR merged and GitHub auto-deleted the remote branch, a fetch
will surface the deletion as a *bookmark conflict* on the local side
(`+ <new>` vs `- <old>`). Resolve with `jj bookmark set <name> -r @` before
pushing — otherwise the push fails with "stale info".
---
@AGENTS.md

TURN 3
TOOL CALL: bash
INPUT:
{"command": "ls internal/ && cat go.mod"}
TOOL RESULT: bash
OUTPUT:
daemon
engine
input
ptyrunner
pump
recording
render
rpc
snapshot
vt
module github.com/paulsmith/research/twee

go 1.26.0

require (
	github.com/creack/pty v1.1.24
	github.com/mitchellh/go-libghostty v0.0.0-20260428141358-29fdb3130d7c
	golang.org/x/image v0.39.0
	golang.org/x/term v0.42.0
)

require (
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
)

TOOL CALL: bash
INPUT:
{"command": "cat design.md | head -100"}
TOOL RESULT: bash
OUTPUT:
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

TURN 4
TOOL CALL: bash
INPUT:
{"command": "ls internal/daemon/ internal/engine/ internal/rpc/ 2>/dev/null"}
TOOL RESULT: bash
OUTPUT:
internal/daemon/:
dispatch.go
handlers_diff.go
handlers_input.go
handlers_lifecycle.go
handlers_query.go
handlers_record.go
handlers_screenshot.go
handlers_wait.go
server.go
server_test.go

internal/engine/:
config.go
input.go
query.go
term.go
types.go
wait.go

internal/rpc/:
codec.go
codec_test.go
errors.go
ops.go
types.go

TOOL CALL: bash
INPUT:
{"command": "ls tuitest/ && ls cmd/"}
TOOL RESULT: bash
OUTPUT:
diagnostic_test.go
expect.go
input.go
menu_e2e_test.go
options.go
runner_smoke_test.go
snapshot.go
snapshot_assert.go
term.go
testdata
wait.go
wait_test.go
libghostty-smoke
twee

TURN 5
TOOL CALL: bash
INPUT:
{"command": "cat plan.md | head -80"}
TOOL RESULT: bash
OUTPUT:
# Implementation Plan: twee (Go TUI Test Harness)

> **Note:** the VT-backend portion of this plan is superseded by
> [`docs/plan-libghostty.md`](docs/plan-libghostty.md), which swaps the
> hand-rolled pure-Go emulator for `libghostty-vt`.

This plan operationalizes `design.md`. It also resolves or sequences the
critical issues flagged in the design review so they don't block work
mid-milestone.

Module path: `github.com/paulsmith/research/twee`. Public package: `tuitest`.
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

- Create `twee/` Go module: `go mod init github.com/paulsmith/research/twee`.
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

TOOL CALL: bash
INPUT:
{"command": "ls internal/daemon/ && wc -l internal/daemon/*.go internal/engine/*.go internal/rpc/*.go tuitest/*.go cmd/**/*.go 2>/dev/null | tail -5"}
TOOL RESULT: bash
OUTPUT:
dispatch.go
handlers_diff.go
handlers_input.go
handlers_lifecycle.go
handlers_query.go
handlers_record.go
handlers_screenshot.go
handlers_wait.go
server.go
server_test.go
   133 cmd/twee/main.go
    63 cmd/twee/main_test.go
   100 cmd/twee/paths.go
    49 cmd/twee/paths_test.go
  4287 total

TURN 6
TEXT:
Excellent — I now have a thorough understanding of the project. Here's what I see:

---

## Pipeline Initialized ✅

**Project:** `twee` — a CLI tool for spawning terminal UIs under a PTY and driving them from outside (typing, key presses, waiting for text, screenshots, etc.). Every command prints one JSON object and exits.

**Tech stack:** Go + CGO (`libghostty-vt` via `go-libghostty`), Unix PTY, JSON-over-Unix-socket wire protocol.

**Current state:** A working implementation with:
- `tuitest/` — public Go test harness API
- `internal/daemon/` — daemon server + handlers for all ops
- `internal/engine/` — VT model, input, query, wait logic
- `internal/rpc/` — wire codec, op types, error codes
- `cmd/twee/` — the CLI itself

---

## What would you like to build?

Given this is a TUI-driving tool, here are some directions I can see being valuable — but tell me what *you* have in mind:

1. **Mouse input support** — `click <x> <y>`, `hover`, `drag` (currently listed as a known limitation)
2. **`twee run` scripting improvements** — streaming results, parallel op execution, conditional branching
3. **New wait/query primitives** — e.g. `wait regex`, `wait cell-style`, region-aware stable wait with exclusion zones
4. **Recording & replay** — full session playback for debugging flaky tests
5. **Linux CI / cross-platform hardening** — the README says "macOS-tested, Linux should work but isn't exercised"
6. **Something entirely different** — a new feature, a refactor, a bugfix you have in mind

**What would you like to build?**

Session ba7f5aad completed in 22s
Turns: 6 | Tool calls: 10 (bash: 10)
Tokens: 2578 (in: 1520, out: 1058) | Cost: $0.03
Longest turn: 11s
