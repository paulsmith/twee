# twee — drive TUIs from the shell

`twee` is a command-line tool for spawning a terminal UI under a PTY
and driving it from outside: type, press keys, query the screen, wait
for text, take screenshots. Every command prints one JSON object and
exits, so it composes well from `bash`, scripts, and AI agents.

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
