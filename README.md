# twee — drive TUIs from the shell

`twee` is a command-line tool for spawning a terminal UI under a PTY
and driving it from outside: type, press keys, query the screen, wait
for text, take screenshots. Each daemon-driving command prints one JSON
object and exits, so it composes well from `bash`, scripts, and AI
agents.

`twee` is pre-release experimental software. There are no compatibility
guarantees for its CLI, Go API, JSON output, daemon protocol, or trace
formats yet; any of them may change without notice before a stable release.

**Demo:** [Watch the 2-minute walkthrough on YouTube](https://www.youtube.com/watch?v=5TVU-ACDD1A).

```
$ twee start -- ./myapp
{"ok":true,"data":{"name":"default","socket":"...","pid":12345}}

$ twee wait text --pattern "Choose an option"
{"ok":true,"data":null}

$ twee key Down
{"ok":true,"data":null}

$ twee type -- "hello, world"
{"ok":true,"data":null}

$ twee text
{"ok":true,"data":{"text":"...visible viewport..."}}

$ twee screenshot --out /tmp/myapp.png
{"ok":true,"data":{"out":"/tmp/myapp.png","width":640,"height":480}}

$ twee stop
{"ok":true,"data":{"name":"default","stopped":true}}
```

Omit `--out` on `screenshot` to receive the PNG inline as
`data.png_base64`.

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

This README tracks `main`, which has breaking CLI changes since v0.1.0
(`--` before child argv, `wait text --pattern`, mostly long options
only; `twee export` uses `-o`). For the v0.1.0 CLI, read the README at
that tag — or build from source.

### Building from source

All source builds go through the Nix flake. `twee` uses `libghostty-vt`
(a CGO library built from the [Ghostty](https://ghostty.org) source
tree) for VT parsing; the flake pins that source tree and the Zig
toolchain it needs, and builds the library as the `ghostty-vt` package.
A vanilla `go build` outside the dev shell fails because cgo needs
`libghostty-vt` on the pkg-config path.

To build and install:

```sh
make install                        # nix build + copy to ~/.local/bin
```

Note that `nix build` builds the *committed* tree — uncommitted changes
only show up in dev-shell builds like `make twee`.

For development, enter the dev shell (or `direnv allow` once) and use
ordinary Go commands; `libghostty-vt` comes prebuilt from the flake:

```sh
nix develop
go test ./...
make twee                           # ./bin/twee from the working copy
./bin/twee version
```

## Model

One TUI per daemon. Multiple daemons run in parallel via `--name`,
which works before or after the verb (`twee --name a status` ≡
`twee status --name a`):

```
$ twee start --name a -- ./app-a
$ twee start --name b -- ./app-b
$ twee ls
$ twee text --name a
$ twee stop --name a
$ twee stop --name b
```

Session resolution order: per-command `--name`, global `--name`,
`$TWEE_SESSION`, then `default` — so `export TWEE_SESSION=mysess` pins
a whole script to one session. While running, each session holds a
`<name>.sock` and `<name>.lock` under `$XDG_STATE_HOME/twee` on Linux
and `~/Library/Application Support/twee` on macOS, with `0700` on the
directory and `0600` on the socket.

`twee start` forks a daemon in the background, prints `{name, socket,
pid}`, and exits. The PTY starts at 80x24 (`--cols`/`--rows` override);
`--dir` sets the child's working directory and repeatable
`--env KEY=VALUE` overrides its environment. Subsequent commands
(`type`, `key`, `wait`, `text`, …) connect to that daemon's socket,
send one request, print the response, and exit. If the child dies
within the first ~100ms, `start` reports it instead of succeeding, and
leaves no socket or lock behind:

```
$ twee start -- /bin/sh -c 'exit 3'
{"ok":false,"error":{"code":"CHILD_EXITED","message":"child exited during startup","details":{"name":"default","child_argv":["/bin/sh","-c","exit 3"],"exit_code":3,"socket_created":true}}}
```

Sessions end one of two ways. `twee stop` SIGTERMs the child, waits
250ms, escalates to SIGKILL, and removes the socket and lock file. Or
the child exits on its own: the daemon finalizes any active trace or
recording, answers in-flight `wait exit` calls — which block until
artifacts are durable and report `{"trace_path": ...}` — then removes
its socket and lock file and exits. A `wait exit` on a session that is
already gone succeeds with `{"exit_code":null,"daemon_already_gone":true}`.

For a one-shot run with no daemon to manage, see `twee run` below
(`run` manages its own ephemeral daemon and takes no `--name`).

## Command reference

Run `twee help` for the top-level list and `twee help <verb>` for
per-command usage (subverbs too: `twee help wait text`). The top-level
command list is:

| Command | Purpose |
|---|---|
| `cell` | Show one cell at x,y. |
| `codegen` | Interactively author a run script. |
| `completion` | Print shell completion setup (currently a placeholder). |
| `cursor` | Show cursor state. |
| `diff` | Compare the viewport to a saved text snapshot. |
| `export` | Export a `.twee` trace bundle to GIF, MP4, or WebM. |
| `find` | Find text in the viewport. |
| `help` | Print top-level or per-command help. |
| `key` | Send one named key. |
| `keys` | Send multiple named keys. |
| `lines` | Show visible viewport lines. |
| `ls` | List running daemons. |
| `mode` | Show active terminal modes. |
| `paste` | Send bracketed paste text. |
| `play` | Play a `.twee` trace bundle. |
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
`TIMEOUT`. `wait stable` also accepts `--quiet <dur>` — how long the
screen must hold still (default 100ms).

### Flag syntax

Long options only; `-n`-style short flags are usage errors. `start`,
`run`, and `codegen` take `--` before the child command; `type` and
`paste` take `--` before literal text:

```
$ twee start --cols 100 -- vim file   # --cols is twee's
$ twee start -- vim file --cols 100   # vim's
$ twee type -- "hello, world"
```

`key` accepts only named keys (`Enter`, `Down`, `Ctrl+C`, …); for
letters, use `type`. Flag values that begin with `-` need the equals
form:

```
$ twee wait text --pattern "-- INSERT --"
twee: wait text: missing value for --pattern (the next token "-- INSERT --" begins with '-'; pass dash-leading values as --pattern=VALUE)
$ twee wait text --pattern="-- INSERT --"
```

## JSON envelope

Every daemon-targeting invocation prints exactly one JSON value to
stdout and exits 0 on success or non-zero on failure. Logs go to
stderr, not stdout. (Meta commands — `version`, `help`, `completion` —
print plain text; `play` and `codegen` are interactive.)

```json
{"ok": true, "data": {...}}
{"ok": false, "error": {"code": "TIMEOUT", "message": "...", "details": {...}}}
```

`data` is shaped per command (`null` for void ops like `type`).

Exception: usage errors (unknown verb, bad flag syntax, missing `--`)
print a plain `twee: ...` line to stderr and exit 2, with nothing on
stdout.

### Error codes

| Code | Meaning |
|---|---|
| `TIMEOUT` | Wait expired. |
| `NOT_FOUND` | Session unreachable: no daemon socket for that name (any verb). Also `trace stop` with no active trace. |
| `ALREADY_RUNNING` | `start` collided with an existing daemon of that name. |
| `CHILD_EXITED` | `start` observed the child exit during startup (within ~100ms); `details` carries `child_argv`, `exit_code`, `socket_created`, and `trace_path` when `--trace` was given. |
| `INVALID_ARGUMENT` | Bad op argument: malformed duration/regex, out-of-range coords, unknown op, malformed script. |
| `IO` | Socket / PTY / file error. |
| `INTERNAL` | Bug in twee (e.g. render or marshal failure). |

Text queries that find nothing (`find`, etc.) return `ok:true` with
empty results, not `NOT_FOUND`.

`error.details` is shaped per failure. Wait timeouts carry `cause` and
`last_screen` (the visible viewport text); the message itself embeds a
diagnostic dump — the child's command, terminal size, cursor, recent
input events, and the last ~1KB of PTY output, escaped. `CHILD_EXITED`
from `start` carries the fields listed above. Other codes carry no
`details`.

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

$ twee run --script ops.json -- ./myapp
{"ok":true,"data":{"ops":5}}
```

The script is a JSON array of RPC request bodies. Each `op` uses the
daemon RPC wire name, for example `wait_text` rather than `wait text`.
Arg names are wire names too and can differ from CLI flags:
`wait_text`/`wait_no_text`/`find` take `"text"` (plus optional
`"regex"`), even though the CLI flag is `--pattern`. Unknown arg keys
are silently ignored — a misnamed key waits on the empty string and
succeeds instantly.

Pass `--script -` (or omit `--script`) to read from stdin. With
`--emit results`, each op's response is streamed as NDJSON (each line
carries an `id`, the op index) instead of the summary envelope. Use
`--trace-out session.twee` to record the whole single-shot run as a
replayable trace bundle.

## Codegen

`twee codegen` runs a command interactively and writes the actions you
take as a replayable JSON operations script. It can also record `.twee`
trace bundles for `twee play`.

```
$ twee codegen --out ops.json --trace-out session.twee -- ./myapp
$ twee play session.twee
```

Press `Ctrl+] q` to stop recording, terminate the child, and write the
script. Codegen also accepts `--cols`/`--rows` (default: your terminal
size, falling back to 80x24), `--dir`, repeatable `--env KEY=VALUE`,
and `--no-waits` to skip the automatically inserted `wait_stable` sync
ops.

Without `--trace-out`, press `Ctrl+] t` during codegen to start and
stop a hotkey trace. Hotkey traces are written next to `--out`: for
`--out ops.json`, the first path is
`ops-trace-YYYYMMDD-HHMMSS.twee`; if that exists, codegen uses
`-02`, `-03`, and so on. Codegen prints the selected path to stderr
when hotkey tracing starts and when it stops.

## Traces

`twee trace` records a running daemon to a `.twee` zip bundle for
debugging and replay tooling. A trace contains `manifest.json`,
and `events.jsonl`. Events include PTY output, input, terminal resizes,
and process exit.

```
$ twee start -- ./myapp
$ twee trace start --out /tmp/myapp.twee
{"ok":true,"data":{"out":"/tmp/myapp.twee"}}
$ twee wait text --pattern "Choose an option"
$ twee key Down
$ twee trace stop
{"ok":true,"data":{"path":"/tmp/myapp.twee"}}
```

If `--out` is omitted, `trace start` chooses a temporary `.twee` path
and returns it in the JSON response.

A trace left active when the child exits is finalized automatically
before the daemon tears down, and `twee wait exit` blocks until the
bundle is durable, reporting it as `{"trace_path": ...}`. To record an
entire session from spawn to teardown in one step:

```
$ twee start --trace /tmp/run.twee -- ./myapp
{"ok":true,"data":{"name":"default","socket":"...","pid":12345,"trace":"/tmp/run.twee"}}
$ twee key Enter
$ twee wait exit
{"ok":true,"data":{"exit_code":0,"trace_path":"/tmp/run.twee"}}
```

[`scripts/example-vim.sh`](scripts/example-vim.sh) is a complete worked
example: it pins a session via `TWEE_SESSION`, records the whole run
with `start --trace`, drives a vim edit, and leaves a replayable bundle
behind at `wait exit`.

## Playback

`twee play` replays a `.twee` bundle in your terminal as an animated
session. It uses the recorded event timing, compressing idle gaps
longer than 2s by default, supports accelerated playback, and shows a
footer for the latest input or resize event so you can correlate user
actions with screen changes.

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

When playback reaches the end of the trace, the final frame dims and a
centered "End of playback" banner appears; press `r` to restart or `q`
to quit.

Flags:

| Flag | Purpose |
|---|---|
| `--speed N` | Playback speed multiplier (default `1.0`). `0.5` is half-speed, `4` is 4x. |
| `--step` | Start paused and advance with `.`. |
| `--max-idle <dur>` | Cap long idle gaps between events (default `2s`; `0` disables compression). |
| `--verbose` | Print a one-line summary to stderr after exit. |

Playback owns the terminal for its lifetime: it switches to the alt
screen, enters raw mode, and writes frames with the Kitty graphics
protocol. `stdout` must be a TTY, and the terminal must be large enough
for the maximum recorded trace size plus two footer rows. Today that
means Kitty-compatible playback only; there is no Sixel or iTerm2 image
backend yet.

## Export

`twee export` renders a `.twee` bundle to a video file without opening a
terminal UI. The output format is inferred from the `-o` extension:
animated GIF is encoded in pure Go, while MP4 and WebM require an
`ffmpeg` binary on `PATH` (or an explicit `--ffmpeg` path).

```
$ twee export /tmp/myapp.twee -o /tmp/myapp.gif
$ twee export /tmp/myapp.twee -o /tmp/myapp.mp4 --speed 2
```

Exports emit a frame only when the visible screen changes. The cursor is
not drawn, matching `twee play`, so cursor-only movement does not create
extra frames. Timing is faithful to the recording by default: unlike
`twee play`, `--max-idle` defaults to `0`, so long idle gaps are kept
unless you explicitly cap them.

Flags:

| Flag | Purpose |
|---|---|
| `-o <path>` | Output path. Extension must be `.gif`, `.mp4`, or `.webm`. |
| `--speed N` | Playback speed multiplier (default `1.0`). |
| `--max-idle <dur>` | Cap long idle gaps between events (default `0`, faithful timing). |
| `--font-size <pt>` | Render font size in points (default `14`). |
| `--fps-cap N` | Limit snapshot/render work to at most this many frames per second of video time (default `30`). |
| `--ffmpeg <path>` | ffmpeg binary for MP4/WebM output (default: find `ffmpeg` on `PATH`). |

## Limitations

- No mouse input (`click`/`hover`/`drag`). Keys, paste, type, signal
  only.
- One TUI per daemon (see Model).
- `wait stable` will hang on apps with always-running spinners. Use
  `wait text --pattern ...` instead. Region-exclusion is a future feature.
- No Kitty keyboard protocol, no DECCKM-aware cursor keys, and no
  scrollback retention (`scrollback` always returns an empty list).
- Title and mode reporting beyond `alt_screen` return defaults until
  the underlying VT layer exposes more state.
- Screenshots use synthetic bold and render emoji cells as the
  leftmost glyph plus space.
- macOS-tested. Linux should work but isn't exercised yet.
