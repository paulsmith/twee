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

Download a release tarball from
[GitHub Releases](https://github.com/paulsmith/twee/releases), unpack
it, and put `twee` on your `PATH`.

```sh
curl -LO https://github.com/paulsmith/twee/releases/download/v0.1.0/twee_0.1.0_darwin_arm64.tar.gz
tar -xzf twee_0.1.0_darwin_arm64.tar.gz
./twee version
```

To build locally with Nix:

```sh
nix build
./result/bin/twee version
```

For contributor builds, use the development shell:

```sh
nix develop                         # enter the dev shell
make twee                           # builds libghostty-vt + ./bin/twee
./bin/twee version
```

`twee` uses `libghostty-vt` (a CGO library built from the
[Ghostty](https://ghostty.org) source tree) for VT parsing. A vanilla
`go build` outside the Nix build or dev shell will fail because the cgo
package needs `libghostty-vt` on the pkg-config path.

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
available) for per-command usage. The top-level command list is:

| Command | Purpose |
|---|---|
| `cell` | Show one cell at x,y. |
| `codegen` | Interactively author a run script. |
| `completion` | Print shell completion setup. |
| `cursor` | Show cursor state. |
| `diff` | Compare the viewport to a saved text snapshot. |
| `find` | Find text in the viewport. |
| `help` | Print top-level or per-command help. |
| `key` | Send one named key. |
| `keys` | Send multiple named keys. |
| `lines` | Show visible viewport lines. |
| `ls` | List running daemons. |
| `mode` | Show active terminal modes. |
| `paste` | Send bracketed paste text. |
| `play` | Play a `.twee` trace bundle. |
| `record` | Start or stop JSONL recording. |
| `region` | Show cells in a rectangular region. |
| `resize` | Resize the terminal. |
| `run` | Run a one-shot ephemeral session. |
| `screenshot` | Render the current screen to PNG. |
| `scrollback` | Show retained scrollback. |
| `signal` | Send a signal to the child process. |
| `size` | Show terminal dimensions. |
| `sleep` | Sleep client-side. |
| `snapshot` | Show the full terminal snapshot. |
| `start` | Spawn a TUI in a daemon. |
| `status` | Show daemon status. |
| `stop` | Stop the running daemon. |
| `text` | Show visible viewport text. |
| `title` | Show the window title. |
| `trace` | Start or stop `.twee` trace recording. |
| `type` | Write literal text to the PTY. |
| `version` | Print the twee version. |
| `wait` | Wait for terminal state or process exit. |

Wait subcommands:

| Command | Purpose |
|---|---|
| `wait cursor` | Wait for the cursor to reach a position. |
| `wait exit` | Wait for the child process to exit. |
| `wait no-text` | Wait for text to disappear. |
| `wait stable` | Wait for the screen to stop changing. |
| `wait text` | Wait for text or a regex to appear. |

All waits accept `--timeout <duration>` (default 5s, except `wait
exit` which defaults to 30s). Failure exits non-zero with code
`TIMEOUT`.

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

The script is a JSON array of RPC request bodies. Each `op` uses the
daemon RPC wire name, for example `wait_text` rather than `wait text`.
Pass `--script -` (or omit `--script`) to read from stdin. With
`--emit results`, each op's response is streamed as NDJSON instead of
the summary envelope. Use `--trace-out session.twee` to record the whole
single-shot run as a replayable trace bundle.

## Codegen

`twee codegen` runs a command interactively and writes the actions you
take as a replayable JSON operations script. It can also record `.twee`
trace bundles for `twee play`.

```
$ twee codegen ./myapp --out ops.json --trace-out session.twee
$ twee play session.twee
```

Without `--trace-out`, press `Ctrl+] t` during codegen to start and
stop a hotkey trace. Hotkey traces are written next to `--out`: for
`--out ops.json`, the first path is
`ops-trace-YYYYMMDD-HHMMSS.twee`; if that exists, codegen uses
`-02`, `-03`, and so on. Codegen prints the selected path to stderr
when hotkey tracing starts and when it stops.

## Traces

`twee trace` records a running daemon to a `.twee` zip bundle for
debugging and replay tooling. A trace contains `manifest.json`,
`events.jsonl`, and `screenshots/*.png`. Events include PTY output,
input, and terminal resizes; screenshots capture the initial and final
viewports.

```
$ twee start ./myapp
$ twee trace start --out /tmp/myapp.twee
$ twee wait text "Choose an option"
$ twee key Down
$ twee trace stop
{"ok":true,"data":{"path":"/tmp/myapp.twee"}}
```

If `--out` is omitted, `trace start` chooses a temporary `.twee` path
and returns it in the JSON response.

## Playback

`twee play` replays a `.twee` bundle in your terminal as an animated
session. It uses the recorded event timing by default, supports
accelerated playback, and shows a footer for the latest input or resize
event so you can correlate user actions with screen changes.

```
$ twee play /tmp/myapp.twee --speed 2
```

Interactive controls during playback:

| Key | Action |
|---|---|
| `space` | Pause / resume |
| `.` | Step one event and remain paused |
| `>` | Jump forward 1s of trace time |
| `r` | Restart from the beginning |
| `q` | Quit |

Flags:

| Flag | Purpose |
|---|---|
| `--speed N` | Playback speed multiplier. `0.5` is half-speed, `4` is 4x. |
| `--step` | Start paused and advance with `.`. |
| `--max-idle <dur>` | Cap long idle gaps between events. `0` disables compression. |
| `-v` | Print a one-line summary to stderr after exit. |

Playback owns the terminal for its lifetime: it switches to the alt
screen, enters raw mode, and writes frames with the Kitty graphics
protocol. `stdout` must be a TTY, and the terminal must be large enough
for the maximum recorded trace size plus two footer rows. Today that
means Kitty-compatible playback only; there is no Sixel or iTerm2 image
backend yet.

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
