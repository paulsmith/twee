# Twee - the multitool for terminal automation.

> Drive, inspect, test, record, and replay terminal applications from the shell.

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

$ twee click --x 12 --y 4
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

Relative `--out` paths (on `screenshot` and `trace start`), `--trace`
paths (on `start` and `run --trace-out`), and `diff`'s `--against` are
all resolved against the CLI invocation's own working directory, not
the daemon's — a session can outlive several `cd`s between the client
processes talking to it. Responses echo the resolved absolute path.

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
curl -LO https://github.com/paulsmith/twee/releases/download/v0.2.0/twee_0.2.0_darwin_arm64.tar.gz
tar -xzf twee_0.2.0_darwin_arm64.tar.gz
./twee version
```

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

`ls`'s `data` is always an array (each entry shaped like `twee status`'s
response, plus `name`): `[]` when no sessions are running, never `null`.

Session resolution order: per-command `--name`, global `--name`,
`$TWEE_SESSION`, then `default` — so `export TWEE_SESSION=mysess` pins
a whole script to one session. While running, each session holds a
`<name>.sock` and `<name>.lock` under `$XDG_STATE_HOME/twee` on Linux
and `~/Library/Application Support/twee` on macOS, with `0700` on the
directory and `0600` on the socket. Once a session has ended, a
`<name>.exited` tombstone file can take their place — see below.

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

`start --name <name>` colliding with an already-live daemon of that name
fails with `ALREADY_RUNNING` by default. `start --force` instead stops
the live session first (default grace) and proceeds with the new one,
adding `"replaced":true` to the response when it actually stopped
something. A stale leftover (a dead daemon's socket/lock left behind by
e.g. `kill -9`) is recovered automatically either way — with or without
`--force` — so `--force` only changes behavior for a genuinely live
collision, and doesn't add `"replaced"` for a stale one.

Sessions end one of two ways. `twee stop` SIGTERMs the child, waits a
grace period (250ms by default; override with `--grace <dur>`),
escalates to SIGKILL, and removes the socket and lock file. `--grace 0`
means SIGKILL immediately, skipping the SIGTERM wait entirely; a
negative grace is `INVALID_ARGUMENT`. Or the child exits on its own: the
daemon finalizes any active trace or recording, answers in-flight `wait
exit` calls — which block until artifacts are durable and report
`{"trace_path": ...}` — then removes its socket and lock file and exits.
A `wait exit` on a session that is already gone succeeds with
`{"exit_code":null,"daemon_already_gone":true}`.

`twee stop --all` stops every live session and cleans up every stale one
in one call, instead of naming one: `{"ok":true,"data":[{...}, ...]}`,
each element shaped like a single stop's data plus `"name"` — `[]` when
no sessions exist at all, still exit 0. It's mutually exclusive with
`--name` (local or global) — combining them is a usage error.

If a daemon is killed abruptly (e.g. `kill -9`) instead of exiting
cleanly, its socket and lock file can be left behind. `twee stop --name
<name>` on such a session detects the stale socket, removes both files,
and reports `{"name":..., "stopped":false, "stale_cleaned":true}`
(exit 0) instead of failing; a name with no socket file at all is still
`NOT_FOUND`. `twee ls` lists stale sessions too, alongside live ones, as
`{"name":..., "running":false, "stale":true}`, instead of silently
omitting them (a tombstone left by a session that ended cleanly is not
listed either way — see `twee status` below).

Either way a session ends — the child exiting on its own or an explicit
`twee stop` — the daemon writes a `<name>.exited` tombstone recording
the outcome (exit code or terminating signal, and which of the two
happened) before removing its socket and lock file. It's written
shortly after teardown starts, sharing the same ~100ms-plus window a
still-connected client gets to finish up (see `wait exit` above); a
`twee status` immediately after `twee stop` can transiently see
`NOT_FOUND` in that window before the tombstone lands. A fresh `twee
start` under the same name removes any old tombstone for it, so it
never gets confused with the new session's own eventual exit info.

`twee status` on a session with no reachable daemon consults the
tombstone before giving up: if one exists, it succeeds instead of
failing with `NOT_FOUND`, `"running":false`, and `"signal"` present only
when a signal (not a normal exit) ended it:

```
$ twee stop --name build
{"ok":true,"data":{"name":"build","stopped":true}}
$ twee status --name build
{"ok":true,"data":{"name":"build","running":false,"stopped":true,"exit_code":null,"signal":"SIGTERM","stopped_at":"2026-01-01T12:00:00Z","command":["make","-j8"]}}
```

A name with neither a reachable daemon nor a tombstone is still
`NOT_FOUND`, as before this existed. `twee ls` is unaffected either way:
it only ever lists live and stale sessions (see above), never a bare
tombstone.

For a one-shot run with no daemon to manage, see `twee run` below
(`run` manages its own ephemeral daemon and takes no `--name`).

## Command reference

Run `twee help` for the top-level list and `twee help <verb>` for
per-command usage (subverbs too: `twee help wait text`). The top-level
command list is:

| Command | Purpose |
|---|---|
| `cell` | Show one cell at x,y. |
| `click` | Click a viewport cell. |
| `wrap` | Wrap a terminal command with optional recording. |
| `completion` | Print shell completion setup (currently a placeholder). |
| `cursor` | Show cursor state. |
| `diff` | Compare the viewport to a saved text snapshot. |
| `do` | Run an op script against a running session. |
| `drag` | Drag between viewport cells. |
| `export` | Export a `.twee` trace bundle to GIF, self-contained HTML, MP4, or WebM. |
| `find` | Find text in the viewport. |
| `help` | Print top-level or per-command help. |
| `hover` | Move the mouse to a viewport cell. |
| `inspect` | Validate and summarize a `.twee` bundle, including network capture metadata. |
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
| `scroll` | Send vertical wheel input. |
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
`TIMEOUT` if the deadline fires, or `SESSION_ENDED` if the session
ends first (child exits, or `twee stop`) — see the Error codes table.
`wait exit` is unaffected: the session ending is its success path, not
a failure. `wait stable` also accepts `--quiet <dur>` — how long the
screen must hold still (default 100ms); unlike the other waits, a
session that ends while `wait stable` is waiting still reports success
(a dead screen is trivially "stable"), never `SESSION_ENDED`.

`wait text --regex` matches against the whole viewport joined by
newlines, compiled in multi-line mode: `^` and `$` anchor at each
line's start/end, not just the start/end of the whole viewport, so
`--pattern '^bravo'` matches a line reading `bravo` anywhere on screen.
`wait no-text` has no `--regex` option. `find --regex` matches per line
already, so its `^`/`$` anchor per line with no special handling
needed.

`find` reports `x` and `w` in terminal cells, not UTF-8 bytes or Unicode
code points. This remains true for narrow non-ASCII text, double-width
characters, and combining graphemes, so a result can be used directly:

```sh
match="$(twee find --pattern Submit)"
twee click \
  --x "$(jq -r '.data[0].x' <<<"$match")" \
  --y "$(jq -r '.data[0].y' <<<"$match")"
```

### Mouse input

Mouse coordinates are required, zero-based cells in the current visible
viewport. They are not pixels or scrollback coordinates:

```sh
twee click --x 12 --y 4
twee click --x 12 --y 4 --button right --modifier ctrl
twee hover --x 20 --y 8
twee scroll --x 20 --y 8 --direction down --ticks 3
twee drag --from-x 4 --from-y 2 --to-x 30 --to-y 12
```

Click and drag default to the left button; scroll defaults to one tick and
accepts at most 100 ticks. `--modifier` is repeatable and accepts `shift`,
`alt`, and `ctrl`; unknown or duplicate modifiers are errors. The child TUI
must enable a compatible mouse tracking mode: click accepts modes 9, 1000,
1002, or 1003; scroll accepts 1000, 1002, or 1003; drag accepts 1002 or 1003;
and hover requires 1003. Disabled or incompatible tracking fails without
writing a partial gesture.

`twee mode` always reports the aggregate `mouse` boolean and explicit raw
DECSET booleans: `mouse_tracking_x10`, `mouse_tracking_normal`,
`mouse_tracking_button`, `mouse_tracking_any`, `mouse_format_utf8`,
`mouse_format_sgr`, `mouse_format_urxvt`, and
`mouse_format_sgr_pixels`. Each raw field is present even when false.

The pinned VT API cannot always prove the effective scalar mode from those
independently retained raw bits. `mouse_tracking` (`none`, `x10`, `normal`,
`button`, or `any`) and `mouse_format` (`x10`, `utf8`, `sgr`, `urxvt`, or
`sgr_pixels`) are therefore included only when the backend can prove them;
automation must tolerate either field being omitted. For gesture preflight,
twee observes the configured encoder's effective tracking behavior entirely in
memory and validates the complete encoded report batch before writing it. This
supports applications that retain several tracking or format bits, without
publishing an inferred scalar mode. Any raw SGR-Pixels (1016) bit still causes
conservative rejection, even if another raw format bit is set, until twee has
real terminal pixel geometry.

The public Go harness mirrors the same gestures:

```go
term.Click(12, 4)
term.Click(12, 4,
    tuitest.WithButton(tuitest.RightButton),
    tuitest.WithMouseModifiers(tuitest.CtrlModifier),
)
term.Hover(20, 8)
term.Scroll(20, 8, tuitest.ScrollDown, 3)
term.Drag(4, 2, 30, 12)
```

All four methods return an error. Supplying an option to a gesture that
cannot use it, such as `WithButton` on `Hover`, is also an error.

### Flag syntax

Long options only; `-n`-style short flags are usage errors. `start`,
`run`, and `wrap` take `--` before the child command; `type` and
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
print plain text; `play` and `wrap` are interactive.)

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
| `NOT_FOUND` | Session unreachable: no daemon socket for that name (any verb), `details.name` names it and `message` leads with it. Also `trace stop` with no active trace (no `details` in that case — see below). |
| `ALREADY_RUNNING` | `start` collided with an existing daemon of that name (pass `--force` to stop it and proceed instead of failing). Also `trace start` while a trace is already active (`details.path` names the active trace); stop it first. |
| `CHILD_EXITED` | `start` observed the child exit during startup (within ~100ms); `details` carries `child_argv`, `exit_code`, `socket_created`, and `trace_path` when `--trace` was given. |
| `INVALID_ARGUMENT` | Bad op argument: malformed duration/regex, out-of-range coords, unknown op, unknown or missing arg key, malformed script. Also a negative `stop --grace`. |
| `FAILED_PRECONDITION` | The requested operation is valid but the current terminal state cannot perform it, such as disabled/incompatible mouse tracking, legacy-format coordinate limits, SGR-Pixels without pixel geometry, or a VT backend without mouse encoding. |
| `IO` | Socket / PTY / file error. |
| `INTERNAL` | Bug in twee (e.g. render or marshal failure). |
| `SESSION_ENDED` | `wait text`/`wait no-text`/`wait cursor` was still pending when the session ended (child exited, or `twee stop`) instead of its deadline firing. `wait exit` never uses this — the session ending is its success path. `wait stable` doesn't either, by design: a dead screen is trivially "stable" (see the waits section). |

Text queries that find nothing (`find`, etc.) return `ok:true` with
empty results, not `NOT_FOUND`.

`error.details` is shaped per failure. Wait failures carry `cause` — a
short root cause, `"pump: timeout"` for `TIMEOUT` or `"pump: closed"`
for `SESSION_ENDED` — and `last_screen` (the visible viewport text).
The full diagnostic dump — the child's command, terminal size, cursor,
recent input events, and the last ~1KB of PTY output, escaped — lives
only in `message`, not `details.cause`. `CHILD_EXITED` from `start`
carries the fields listed above. A `NOT_FOUND` from a failed dial — no
daemon socket for the resolved name, on any daemon-targeting verb —
carries `details.name` and a `message` leading with that name rather
than the socket path it never typed; other sources of `NOT_FOUND` (e.g.
`trace stop` with no active trace) carry no `details`, same as every
other code.

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
`"regex"`), even though the CLI flag is `--pattern`.

Args are decoded strictly: an unknown key — a misnamed `"pattern"`
where the wire name is `"text"`, say — fails the op with
`INVALID_ARGUMENT` naming the bad key and the op's accepted keys,
instead of being silently ignored (a stray key used to leave the
matching field at its zero value; for `wait_text` that meant waiting on
the empty string and succeeding instantly). `wait_text`, `wait_no_text`,
and `find` also reject an empty/missing `"text"` in literal (non-regex)
mode with `INVALID_ARGUMENT: "text or regex required"`, for the same
reason.

Mouse operations use the same vocabulary as the CLI and are available in
both `run` and `do` scripts:

```json
[
  {"op":"click","args":{"x":12,"y":4}},
  {"op":"hover","args":{"x":20,"y":8}},
  {"op":"scroll","args":{"x":20,"y":8,"direction":"down","ticks":3}},
  {"op":"drag","args":{"from_x":4,"from_y":2,"to_x":30,"to_y":12}}
]
```

Every coordinate is required even when its value is zero. Button defaults to
`"left"` and scroll ticks default to `1`.

Pass `--script -` (or omit `--script`) to read from stdin. With
`--emit results`, each op's response is streamed as NDJSON (each line
carries an `id`, the op index) instead of the summary envelope. Use
`--trace-out session.twee` to record the whole single-shot run as a
replayable trace bundle.

On Linux, `--network-capture` adds the managed program's raw IPv4 packets to a
whole-session trace. See [Network capture](#network-capture) for host
requirements, port publication, capture limits, and a complete example.

### CI replay artifacts

For a scripted CI scenario, record with `--trace-out`, export the resulting
bundle to GIF, and attach that GIF even when the scenario fails. The
[CI replay artifact recipe](docs/ci-artifacts.md) preserves the original
failure status, adds an artifact link to the job summary, and explains why raw
`.twee` bundles should be treated as sensitive.

## Scripts against a running session

`twee do` executes the same op-script format against an already-running
named session instead of spinning up its own ephemeral daemon — useful
for agents batching many ops into one process launch instead of paying
a spawn per op:

```
$ twee start --name agent -- ./myapp
$ twee do --name agent --script ops.json
{"ok":true,"data":{"ops":5}}
```

Session resolution works exactly like every other daemon verb
(per-command `--name`, global `--name`, `$TWEE_SESSION`, then
`default`); a missing session fails with `NOT_FOUND`, same as `status`
or `key` would. `--script -` (or omitting `--script`) reads from stdin,
so a script can be piped in with a heredoc:

```
$ twee do --name agent <<'EOF'
[
  {"op": "wait_text", "args": {"text": "Choose an option", "timeout": "5s"}},
  {"op": "key", "args": {"key": "Down"}}
]
EOF
{"ok":true,"data":{"ops":2}}
```

`--emit results` streams NDJSON per op just like `run`. `do` has no
`--trace-out` — use `twee trace start`/`trace stop` (see Traces below)
to record a named session instead — and no `--` child argv, since
there's no child to spawn. Ops like `stop` or `wait_exit` aren't
special-cased: they do whatever they normally do, including ending the
session.

## Wrap

`twee wrap` runs a command interactively in a parent-owned PTY. Script and
`.twee` trace recording are independent and optional: with no output flags it
only wraps the terminal until you start a recorder with a control chord.

```
$ twee wrap --script-out ops.json --trace-out session.twee -- ./myapp
$ twee play session.twee
```

Press `Ctrl+] s` to start or finalize JSON script capture and `Ctrl+] t` to
start or finalize trace capture. Each recorder is one-shot in a wrap session:
after finalization it cannot resume. `Ctrl+] q` finalizes active recorders and
terminates the child. Wrap also accepts `--cols`/`--rows` (default: your terminal
size, falling back to 80x24), `--dir`, repeatable `--env KEY=VALUE`,
and `--no-waits` to skip the automatically inserted `wait_stable` sync
ops.

On Linux, `wrap` accepts the same managed-network options as `start` and
`run`. `--network-capture` requires an immediate `--trace-out`; repeatable
`--publish-tcp LISTEN=GUEST_PORT` options expose guest servers to host clients.
Because that trace covers the complete child lifetime, `Ctrl+] t` cannot stop
it early. Exit the child normally or press `Ctrl+] q` to finalize the terminal
trace and PCAP together.

```sh
twee wrap --trace-out web-session.twee --network-capture \
  --publish-tcp 127.0.0.1:8080=3000 -- \
  ./dev-server --host 0.0.0.0 --port 3000
```

Hotkey artifacts use the invocation directory: `twee-script-YYYYMMDD-HHMMSS.json`
and `twee-trace-YYYYMMDD-HHMMSS.twee`, with a numeric suffix on collision.
A script started after prior session activity is marked partial in the status bar.
That activity can be child output, user input, or a resize. The one-row
status bar remains visible above full-screen applications and includes active
recorder spinners and the available control chords; use `--no-status` when
terminal fidelity is more important than status feedback.

On an interactive capable terminal, wrap uses a wrapper-owned compositor so
the status row is not part of child output or recordings. This presentation
does not preserve graphics, OSC integrations, or uncommon terminal protocols;
`--no-status` is the raw PTY passthrough escape hatch. On non-terminal output
or `TERM=dumb`, wrap automatically uses that raw mode and does not emit status
control sequences.

The compositor mirrors only modes that change host-generated input: application
cursor/keypad, bracketed paste, focus events, and the supported mouse reporting
modes. It also reflects the child cursor shape. It intentionally does not
forward arbitrary display modes, palettes, OSC, alternate-screen, or margin
settings to the physical host terminal.

SGR-pixel mouse reporting cannot be mapped safely to the child viewport while
the status row is visible, so use `--no-status` when an application needs it.

For xterm-compatible terminals, wrap saves and restores the affected private
input modes and cursor visibility around its alternate screen. The alternate
screen also restores the prior cursor shape after the child view closes. On an
unrecognized terminal type, wrap uses a safe reset of only those input modes;
use `--no-status` when preserving unknown terminal state is more important.

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

Each successful high-level mouse gesture is one structured input event, with
the complete encoded report batch in `bytes_b64` and gesture semantics under
`mouse`. Valid zero coordinates remain explicit:

```json
{"t_ms":1250,"type":"input","kind":"mouse","bytes_b64":"...","mouse":{"gesture":"click","x":0,"y":4,"button":"left","modifiers":[]}}
```

Hover and scroll use `x`/`y`; scroll additionally records `direction` and
`ticks`. Drag uses `from_x`, `from_y`, `to_x`, and `to_y`. Button and the
explicit modifiers array are recorded where applicable. Failed gestures
produce no trace event.

Playback treats these bytes as an annotation rather than feeding them back
into the VT model. `twee play` and export's `--input-overlay` render stable
gesture toasts such as:

```text
[01.250s] → click left @(0,4)
[02.100s] → scroll down x3 @(20,8)
[03.000s] → drag left (4,2)->(30,12)
```

If `--out` is omitted, `trace start` chooses a temporary `.twee` path
and returns it in the JSON response.

`trace start` on a session that already has an active trace fails with
`ALREADY_RUNNING` (`error.details.path` names the active trace) instead
of silently finalizing the first trace and starting a new one; stop the
active trace first.

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

### Network capture

`--network-capture` runs the managed program and its descendants in netwrap's
private IPv4 network. The resulting whole-session `.twee` bundle contains a
classic PCAP stream at `streams/network.pcap`, in addition to terminal events.
It is a raw packet capture, not a HAR file: analysis tools reconstruct requests
and responses from the packets when the application protocol permits it.

Network capture is Linux-only. The host must permit unprivileged user
namespaces and let the current user open `/dev/net/tun`. Setup fails closed; it
does not silently run the program on the host network.

Use repeatable `--publish-tcp LISTEN=GUEST_PORT` options when a host client
must reach a managed server. `LISTEN` requires a literal IPv4 address and a
numeric port from 1 through 65535; `GUEST_PORT` is the managed server's numeric
port in the same range. For example, `127.0.0.1:8080=3000` publishes guest port
3000 on host port 8080. Bind the managed server to `0.0.0.0`, not loopback. A
host listener on `127.0.0.1` accepts only local clients. A listener on
`0.0.0.0` can expose the development server to other machines, subject to host
routing and firewall rules.

This example starts a named development server, sends a client request, stops
the session, waits for durable artifacts, and extracts and inspects the PCAP:

```sh
trace_path="$PWD/web-session.twee"

twee start --name web --trace "$trace_path" --network-capture \
  --publish-tcp 127.0.0.1:8080=3000 -- \
  ./dev-server --host 0.0.0.0 --port 3000

twee wait text --name web --pattern "listening" --timeout 2m
curl --fail http://127.0.0.1:8080/health
twee stop --name web --grace 2s

# stop returns only after the known trace path is durable.
twee inspect --format text "$trace_path"
unzip -p "$trace_path" streams/network.pcap >network.pcap
tcpdump -nn -r network.pcap
```

For one-shot automation, use the same options with `run`:

```sh
twee run --trace-out web-session.twee --network-capture \
  --publish-tcp 127.0.0.1:8080=3000 --script ops.json -- ./dev-server
```

For an interactive foreground session, use them with `wrap`; send host
requests from another terminal through the published port:

```sh
twee wrap --trace-out web-session.twee --network-capture \
  --publish-tcp 127.0.0.1:8080=3000 -- ./dev-server
```

Capture begins before the managed command starts and ends only when the
session exits or is stopped. Because network capture and its trace cover the
whole session, `twee trace stop` cannot stop such a trace early, `twee trace
start` cannot add network capture to an existing session, and `Ctrl+] t`
cannot stop a network-enabled `wrap` trace. Wait for exit, stop the named
session, or use `Ctrl+] q` in `wrap` to finalize the bundle.

The PCAP limit is 64 MiB, including its header. When the next complete packet
would exceed that limit, packet recording stops, netwrap prints one warning to
the managed terminal, and the program continues. Captures do not rotate.

Capture covers IPv4 packets crossing netwrap's TUN boundary. It does not cover
guest loopback traffic, IPv6, UNIX sockets or other local IPC, traffic through
inherited standard-stream descriptors, or traffic after the session ends.
TLS and other encrypted protocols remain encrypted. Netwrap proxies sockets,
so guest addresses, timing, and network behavior can differ from the host.

PCAP data can contain passwords, tokens, cookies, request bodies, DNS names,
and other secrets. `.twee` files and extracted PCAPs are sensitive artifacts;
restrict their access and retention.

[`scripts/example-vim.sh`](scripts/example-vim.sh) is a complete worked
example: it pins a session via `TWEE_SESSION`, records the whole run
with `start --trace`, drives a vim edit, and leaves a replayable bundle
behind at `wait exit`.

[`scripts/example-herdr-mouse.sh`](scripts/example-herdr-mouse.sh) records a
fixed-size Herdr session with an isolated first-run configuration. It clicks
settings and theme controls, opens a pane's right-click menu, chooses
`Split right`, focuses both panes, validates the resulting bundle, and leaves
the trace at the path passed as its first argument.

### Inspect bundles

`twee inspect` validates and summarizes a `.twee` file directly — no daemon
or terminal — for debugging and CI. JSON is the default; use `--format text`
for a human-readable report.

```
$ twee inspect /tmp/run.twee
{"ok":true,"data":{"path":"/tmp/run.twee","version":1,"command":["./myapp"],"duration":"5s","duration_ms":5000,"event_span_ms":5000,"started_at":"2026-01-01T12:00:00Z","stopped_at":"2026-01-01T12:00:05Z","terminal":{"cols":80,"rows":24,"max_cols":100,"max_rows":30},"events":{"total":13,"by_type":{"exit":1,"input":2,"output":9,"resize":1},"input_by_kind":{"key":2}},"exit":{"recorded":true,"code":0},"network_capture":{"present":false,"truncated":false}}}
```

Before returning the summary, `inspect` checks zip integrity, the manifest and
supported version, every `events.jsonl` record, and timestamp ordering. For
network traces it also fully reads the PCAP, verifies its CRC and framing, and
checks its declared size, packet count, format, link type, limit, and status.
An invalid bundle reports `ok:false` with code `INVALID_ARGUMENT` and every
problem found (not just the first) in `error.details.issues`:

```
$ twee inspect /tmp/broken.twee
{"ok":false,"error":{"code":"INVALID_ARGUMENT","message":"inspect: 2 issue(s) found","details":{"issues":["events.jsonl line 4: unknown event type \"teleport\"","events.jsonl line 7: timestamp 120 before previous 500"]}}}
```

A missing or unreadable file fails with code `IO` instead.

## Playback

`twee play` replays a `.twee` bundle in your terminal as an animated
session. It uses the recorded event timing, compressing idle gaps
longer than 2s by default, supports accelerated playback, and shows a
footer for the latest input or resize event so you can correlate user
actions with screen changes. Recorded mouse gestures also receive a brief
visual annotation on the replayed terminal by default; this includes click,
hover, scroll, and drag feedback.

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
| `--backend auto\|kitty\|iterm2\|sixel` | Graphics backend (default `auto`; iTerm2 and Sixel are experimental). |
| `--speed N` | Playback speed multiplier (default `1.0`). `0.5` is half-speed, `4` is 4x. |
| `--step` | Start paused and advance with `.`. |
| `--max-idle <dur>` | Cap long idle gaps between events (default `2s`; `0` disables compression). |
| `--no-mouse-annotations` | Hide transient visual feedback for recorded mouse gestures. |
| `--verbose` | Print a one-line summary to stderr after exit. |

Playback owns the terminal for its lifetime: it switches to the alt screen,
enters raw mode, and writes frames with a terminal graphics protocol. `stdout`
must be a TTY, and the terminal must be large enough for the maximum recorded
trace size plus two footer rows.

`--backend auto` probes protocol capabilities and prefers Kitty, then iTerm2,
then Sixel. An explicit backend fails with a backend-specific diagnostic when
the terminal does not advertise the required capability. Sixel also requires
reliable terminal pixel geometry; auto detection skips Sixel when that geometry
is unavailable.

Graphics playback currently requires a direct terminal connection. tmux and
screen passthrough are not supported. The iTerm2 and Sixel backends are
experimental until their redraw, resize, flicker, and cleanup behavior has
passed the [real-terminal verification matrix](docs/playback-export-verification.md).

## Export

`twee export` renders a `.twee` bundle to a replay artifact without opening a
terminal UI. The output format is inferred from the `-o` extension. Animated
GIF and self-contained HTML are encoded in pure Go; MP4 and WebM require an
`ffmpeg` binary on `PATH` (or an explicit `--ffmpeg` path). HTML output works
offline from a local file and includes playback, frame-step, speed, and
timeline controls.

```
$ twee export /tmp/myapp.twee -o /tmp/myapp.gif
$ twee export /tmp/myapp.twee -o /tmp/myapp.html
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
| `-o <path>` | Output path. Extension must be `.gif`, `.html`, `.mp4`, or `.webm`. |
| `--speed N` | Playback speed multiplier (default `1.0`). |
| `--max-idle <dur>` | Cap long idle gaps between events (default `0`, faithful timing). |
| `--font-size <pt>` | Render font size in points (default `14`). |
| `--fps-cap N` | Limit snapshot/render work to at most this many frames per second of video time (default `30`). |
| `--ffmpeg <path>` | ffmpeg binary for MP4/WebM output (default: find `ffmpeg` on `PATH`). |
| `--crop <x,y,w,h>` | Render only this cell rectangle of the screen. `w,h` must be `> 0`, `x,y` must be `>= 0`. |
| `--input-overlay` | Append a footer strip below the frames showing the most recent input or resize event. |
| `--quality low\|medium\|high` | ffmpeg encoder preset for MP4/WebM (default `medium`). Usage error for `.gif` and `.html` output. |

`--crop` takes cell coordinates, not pixels. A frame whose actual screen
is smaller than the crop rectangle (e.g. before a later resize grows it)
renders the intersection and blank-fills the rest rather than failing
mid-export:

```
$ twee export /tmp/myapp.twee -o /tmp/corner.gif --crop 0,0,40,10
```

`--input-overlay` adds a row below the frames formatted like `twee
play`'s footer (`[12.345s] -> Enter`, `[1.000s] -> resize 100x30`, ...).
A qualifying input or resize event always produces its own frame, even
when the screen itself didn't change — otherwise it could be skipped
entirely by the emit-on-screen-change rule:

```
$ twee export /tmp/myapp.twee -o /tmp/annotated.mp4 --input-overlay
```

`--quality` selects an ffmpeg CRF/preset for MP4 (libx264) and WebM
(libvpx-vp9, constant-quality mode) output: `low` trades quality for
encode speed, `high` the reverse, and `medium` (the default) reproduces
the encoder's own out-of-the-box settings — passing `--quality medium`
explicitly changes nothing. The pure-Go GIF and HTML encoders have no such
knob, so `--quality` with a `.gif` or `.html` `-o` path is a usage error rather
than a silently ignored flag:

```
$ twee export /tmp/myapp.twee -o /tmp/myapp.mp4 --quality high
$ twee export /tmp/myapp.twee -o /tmp/myapp.gif --quality high
twee: export: --quality is not supported for .gif output (the pure-Go encoder has no quality/CRF knob)
$ twee export /tmp/myapp.twee -o /tmp/myapp.html --quality high
twee: export: --quality is not supported for .html output (the pure-Go encoder has no quality/CRF knob)
```

## Limitations

- Mouse input is synthetic and application-directed only: twee does not
  capture a physical host mouse, scroll its own viewport, retain scrollback,
  send horizontal wheel input, expose separate stateful press/release RPCs,
  or support SGR-Pixels yet.
- One TUI per daemon (see Model).
- `wait stable` will hang on apps with always-running spinners. Use
  `wait text --pattern ...` instead. Region-exclusion is a future feature.
- No Kitty keyboard protocol, no DECCKM-aware cursor keys, and no
  scrollback retention (`scrollback` always returns an empty list).
- Title reporting and non-mouse modes beyond `alt_screen` return defaults
  until the underlying VT layer exposes more state.
- Screenshots use synthetic bold and render emoji cells as the
  leftmost glyph plus space.
- macOS-tested. Linux should work but isn't exercised yet.
