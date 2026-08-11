# Twee — the multitool for terminal automation

> Drive, inspect, test, record, and replay terminal applications from the shell.

Twee runs a terminal user interface (TUI) in a pseudo-terminal (PTY). It lets another process control and inspect the application.

Daemon and RPC commands return structured JSON for shell scripts, continuous integration (CI), and AI agents. The Go package provides a test harness.

Twee can also record a session as a `.twee` trace. You can inspect, play, or export the trace later.

> [!WARNING]
> Twee is pre-release experimental software. Its CLI, Go API, JSON output, daemon protocol, and trace formats can change without notice.

**Demo:** [Watch the two-minute walkthrough on YouTube](https://www.youtube.com/watch?v=5TVU-ACDD1A).

## Why Twee

Normal pipes do not reproduce TTY-only rendering, cursor movement, terminal width, or transient repaints. Twee observes the application through a terminal model.

Use Twee to automate full-screen applications, compare terminal behavior, test interactive flows, or preserve a failed CI session for review.

## Quick start

Start an application in a background session. The `--trace` option records the complete session.

```sh
twee start --trace session.twee -- ./myapp
twee wait text --pattern "Choose an option"
twee key Down
twee type -- "hello, world"
twee click --x 12 --y 4
twee text
twee screenshot --out screen.png
twee stop
```

Each daemon command prints one JSON object. A successful response has this form:

```json
{"ok":true,"data":{"name":"default","socket":"...","pid":12345}}
```

Inspect or replay the completed trace:

```sh
twee inspect --format text session.twee
twee play session.twee
twee export session.twee -o session.gif --input-overlay
```

Twee supports four primary workflows:

| Workflow | Use it for | Entry point |
|---|---|---|
| Named session | Incremental shell or agent control | `twee start` |
| One-shot script | Repeatable automation and CI | `twee run` |
| Interactive wrapper | Manual operation with script or trace capture | `twee wrap` |
| Go test harness | TUI assertions in `go test` | `tuitest.Run` |

## Install

Download an archive from [GitHub Releases](https://github.com/paulsmith/twee/releases). Releases currently include Linux x86-64 and macOS arm64 binaries.

This example installs version `v0.2.0` for macOS arm64:

```sh
curl -LO https://github.com/paulsmith/twee/releases/download/v0.2.0/twee_0.2.0_darwin_arm64.tar.gz
tar -xzf twee_0.2.0_darwin_arm64.tar.gz
./twee version
```

Put the `twee` binary on your `PATH` after extraction.

### Build from source

Source builds use the Nix flake. The flake supplies Go, `pkg-config`, and the prebuilt `libghostty-vt` library.

A plain `go build` outside the development shell cannot find `libghostty-vt`.

Build and install the committed revision:

```sh
make install
```

Build the current working copy:

```sh
nix develop
make twee
./bin/twee version
```

Run the test suite inside the development shell:

```sh
nix develop -c go test ./...
```

`nix build` reads the committed tree. Use `make twee` inside the development shell to include uncommitted changes.

## Named sessions

`twee start` starts one TUI in a background daemon. Later commands connect to the daemon, perform one operation, and exit.

Use `--name` to run multiple sessions:

```sh
twee start --name editor -- vim notes.txt
twee start --name monitor -- htop
twee ls
twee text --name editor
twee stop --name editor
twee stop --name monitor
```

Twee resolves the session name in this order:

1. The command-specific `--name` value.
2. The global `--name` value.
3. The `TWEE_SESSION` environment variable.
4. The name `default`.

Set `TWEE_SESSION` once to target one session from a complete script:

```sh
export TWEE_SESSION=my-test
twee start -- ./myapp
twee wait text --pattern Ready
twee key Enter
twee stop
```

You can put the global option before a session-aware command:

```sh
twee --name my-test status
```

`start --force` replaces a live session with the same name. A normal `start` reports `ALREADY_RUNNING` for that collision.

`stop` sends `SIGTERM`, waits 250 milliseconds, and then sends `SIGKILL` if necessary. Use `--grace` to change the wait.

```sh
twee stop --grace 2s
twee stop --grace 0
twee stop --all
```

`stop --all` stops all live sessions and removes stale session state. It cannot be used with `--name`.

`status` reports a completed session when its exit record still exists. A new session with the same name removes the old record.

## One-shot scripts

`twee run` starts an ephemeral session, runs a JSON operation script, and removes the daemon. This workflow is useful in CI.

Create an operation script:

```json
[
  {"op":"wait_text","args":{"text":"Choose an option","timeout":"5s"}},
  {"op":"key","args":{"key":"Down"}},
  {"op":"wait_text","args":{"text":"> second"}},
  {"op":"key","args":{"key":"Enter"}},
  {"op":"screenshot","args":{"out":"out.png"}}
]
```

Run the script and record the complete session:

```sh
twee run --script ops.json --trace-out session.twee -- ./myapp
```

The script uses RPC wire names. For example, use `wait_text` and its `text` argument instead of `wait text` and `--pattern`.

Twee rejects unknown argument keys and missing required values. This strict decoding prevents a misspelled key from producing a false success.

Use `--script -` or omit `--script` to read the script from standard input. Use `--emit results` to stream operation results as NDJSON.

`twee do` runs the same script against an existing named session:

```sh
twee start --name agent -- ./myapp
twee do --name agent --script ops.json
```

Scripts from `twee wrap --script-out` use this format. You can replay them with either `run` or `do`.

## Interactive recording

`twee wrap` runs a command interactively in a foreground PTY. It can record a JSON script, a `.twee` trace, or both.

Start both recorders immediately:

```sh
twee wrap --script-out ops.json --trace-out session.twee -- ./myapp
```

Use these controls during the session:

| Control | Action |
|---|---|
| `Ctrl+] s` | Start or finalize JSON script capture. |
| `Ctrl+] t` | Start or finalize trace capture. |
| `Ctrl+] q` | Finalize active recorders and terminate the child. |

Each recorder is one-shot during one `wrap` session. A finalized recorder cannot start again in that session.

Without an output flag, a control creates a timestamped file in the invocation directory. Use `--no-waits` to omit generated `wait_stable` operations.

The default display reserves one row for recorder status. Use `--no-status` when the application needs exact terminal behavior.

The status display does not preserve graphics, Operating System Command (OSC) integrations, or uncommon terminal protocols. Non-terminal output uses raw passthrough automatically.

## Go test harness

The [`tuitest`](https://pkg.go.dev/github.com/paulsmith/twee/tuitest) package provides Playwright-style control for Go tests. It uses the same PTY and terminal model as the CLI.

```go
package myapp_test

import (
	"testing"

	"github.com/paulsmith/twee/tuitest"
)

func TestMenuNavigation(t *testing.T) {
	term := tuitest.Run(t, "./myapp", tuitest.Size(40, 10))

	term.ExpectText("Choose an option")
	if err := term.Key(tuitest.Down); err != nil {
		t.Fatal(err)
	}
	term.ExpectText("> second")
	if err := term.Key(tuitest.Enter); err != nil {
		t.Fatal(err)
	}
	term.ExpectText("selected: second")
}
```

`tuitest.Run` closes the child during test cleanup. It records a temporary trace by default and reports the path when the test fails.

Set `TUITEST_RECORD=0` to disable automatic traces. Use `tuitest.Trace` or `tuitest.NetworkCapture` for explicit trace artifacts.

Use `term.ExpectTextSnapshot(...)` to compare the visible text with a stored snapshot.

The package also supports keyboard input, paste, mouse gestures, screen queries, waits, terminal resize, and diagnostics.

## Command guide

Run `twee help` for the current command list. Run `twee help <command>` for flags and behavior.
Use JSON help to discover each command's output contract without maintaining a
separate command table:

```sh
twee help start
twee help wait text
twee help wrap
twee help --format json
twee help diff --format json
```

JSON help starts with `schema_version`. Each descriptor reports whether the
command is interactive, its normal success-output kind, supported formats,
structured-error support, exit statuses, and any separately written artifact.

The commands are grouped below by purpose:

| Group | Commands | Purpose |
|---|---|---|
| Sessions | `start`, `status`, `ls`, `stop` | Manage background TUI sessions. |
| Scripts | `run`, `do` | Run JSON operation scripts. |
| Interactive | `wrap` | Run a foreground command with optional recording. |
| Input | `type`, `key`, `keys`, `paste`, `signal` | Send text, keys, paste, or a signal. |
| Mouse | `click`, `hover`, `scroll`, `drag` | Send cell-based mouse gestures. |
| Screen | `text`, `lines`, `cell`, `region`, `snapshot`, `screenshot` | Read or render terminal content. |
| State | `cursor`, `size`, `title`, `mode`, `scrollback` | Read terminal state. |
| Assertions | `assert cell`, `assert region` | Check cell content and styles without `jq`. |
| Search | `find`, `diff` | Search the viewport or compare a text snapshot. |
| Control | `resize`, `sleep`, `wait` | Change size, delay locally, or wait for state. |
| Traces | `trace`, `inspect`, `play`, `export` | Record, validate, replay, or export `.twee` bundles. |
| Meta | `help`, `version`, `completion` | Show help, version, or completion output. |

`completion` is currently a placeholder. It does not generate a functional completion script.

### CLI syntax

Most commands accept long options only. `export` also accepts `-o` for its output path.

Put `--` before a child command or a literal payload:

```sh
twee start --cols 100 -- vim file
twee start -- vim file --cols 100
twee type -- "hello, world"
twee paste -- "pasted text"
```

The first `--cols` belongs to Twee. The second `--cols` belongs to Vim.

Use `type` for all printable characters. `key` accepts named keys such as `Enter`, `Down`, `Escape`, and `Ctrl+C`.

Use the equals form when an option value starts with `-`:

```sh
twee wait text --pattern="-- INSERT --"
```

### JSON output

Daemon commands print one JSON value to standard output. Logs go to standard error.

Place the root `--machine` option before a non-interactive command to request a
stable automation contract. Success is one JSON envelope. Runtime and usage
errors are JSON envelopes on standard output; diagnostics remain on standard
error. Usage errors still exit with status 2 and operational errors with status
1. Interactive `play` and `wrap` reject `--machine` with a structured usage
error.

```sh
twee --machine version
twee --machine export session.twee -o replay.html
```

The machine-mode export response includes the resolved absolute output path and
the selected artifact format. Export remains silent on success by default.

`run --emit results` and `do --emit results` stream multiple NDJSON values instead.
Each completed operation writes one response record. The first failing
operation writes its error response as the terminal record and the command
exits with status 1; there is no additional summary record. Failures before an
operation response exists write one error record. Standard error remains
reserved for diagnostics.

```json
{"ok":true,"data":{"text":"visible viewport"}}
{"ok":false,"error":{"code":"TIMEOUT","message":"...","details":{}}}
```

Void operations return `null` in `data`. Query commands return command-specific data.

`help`, `version`, and `completion` print plain text. `play` and `wrap` use the terminal interactively.

Without `--machine`, usage errors print plain text to standard error, print
nothing to standard output, and exit with status 2.

Common error codes are:

| Code | Meaning |
|---|---|
| `TIMEOUT` | A wait reached its deadline. |
| `NOT_FOUND` | The selected session or trace state does not exist. |
| `ALREADY_RUNNING` | A session or trace already exists. |
| `CHILD_EXITED` | The child exited during session startup. |
| `INVALID_ARGUMENT` | An operation argument is invalid. |
| `FAILED_PRECONDITION` | The current terminal state cannot perform the operation. |
| `ASSERTION_FAILED` | A live cell/region assertion or offline trace containment assertion did not match. |
| `IO` | A socket, PTY, or file operation failed. |
| `INTERNAL` | Twee encountered an internal error. |
| `SESSION_ENDED` | A pending wait ended because the session ended. |

Text queries that find no matches return a successful empty result.

### Paths

For daemon file operations, Twee resolves relative paths from the client invocation directory. It does not use the daemon working directory.

This rule applies to:

- `screenshot --out`
- `trace start --out`
- `start --trace`
- `run --trace-out`
- `diff --against`

Responses return the resolved absolute path when applicable.

Without `--out`, `screenshot` returns the PNG data in `data.png_base64`.

### Artifact permissions

Terminal artifacts can contain credentials, typed input, and unredacted screen
content. On Unix, Twee creates new screenshots, trace bundles, wrap scripts,
text snapshots, and replay exports with mode `0600` so only the owner can read
or modify them. Atomic replay export replacement preserves the existing
destination's permissions. Review an existing destination's permissions before
overwriting it, and review every artifact before sharing it. Machine-mode
artifact responses contain resolved absolute paths, which can disclose local
directory names if logs are shared.

## Wait for terminal state

Waits synchronize automation with the application. Prefer a state wait to a fixed delay.

| Command | Condition | Default timeout |
|---|---|---|
| `wait text` | Text or a regular expression appears. | 5 seconds |
| `wait no-text` | Text disappears. | 5 seconds |
| `wait stable` | The screen stops changing. | 5 seconds |
| `wait cell` | A physical cell matches text, width, color, and style predicates. | 5 seconds |
| `wait cursor` | The cursor reaches a cell. | 5 seconds |
| `wait exit` | The child exits and artifacts are durable. | 30 seconds |

Use `--timeout` to change a timeout. Use `wait stable --quiet` to change the stable period from its 100-millisecond default.

`wait text --regex` matches the complete viewport in multiline mode. The `^` and `$` anchors apply to each visible line.

Cell predicates match exact physical-cell state, including wide-character
continuation cells. Conditions are ANDed:

```sh
twee wait cell --x 0 --y 1 --fg palette:1 --bold --timeout 2s
twee assert cell --x 0 --y 1 --text X --bold=false
twee assert region --contains-style fg=palette:1
twee assert region --x 0 --y 0 --w 80 --h 1 --match all --bg default
```

Colors use `default`, `palette:N`, `#RRGGBB`, or `rgb:R,G,B`. Region assertions
default to the whole viewport and `--match any`; use `--match all` to require
every clipped cell to match. Assertion mismatches return `ASSERTION_FAILED`.

Failed waits and assertions include structured diagnostic details captured from
the evaluated terminal state: viewport text and dimensions, cursor, input and
mouse modes, recent bounded input/output/resize activity, and an active or
finalized trace path when available. Region assertion failures also report the
clipped region, matching/total cell counts, and the first mismatching cell.

`wait text`, `wait no-text`, `wait cell`, and `wait cursor` return `SESSION_ENDED` when the session ends first. This result differs from `TIMEOUT`.

`wait stable` returns success when the session ends because the screen cannot change again. An active spinner can prevent stability.

`wait exit` returns after Twee finalizes active traces. If the daemon is already gone, the command reports `daemon_already_gone` and succeeds.

## Screen and mouse coordinates

Screen and mouse commands use zero-based terminal cells. They do not use pixels, bytes, code points, or scrollback positions.

`find` reports `x` and `w` in terminal cells and remains useful for
exploration. For an action, prefer the atomic pattern form of `click`:

```sh
twee click --pattern Submit
twee click --pattern 'Save .*' --regex --require one
twee click --pattern Item --select first
twee click --pattern Item --select 3
```

Pattern clicks require exactly one match by default. Multiple matches return
`AMBIGUOUS_MATCH`; no matches return `NOT_FOUND`; and an invalid or out-of-range
number returns `INVALID_SELECTION`. `--select first`, `--select last`, and a
one-based number resolve ambiguity explicitly. The response reports the match,
the exact target cell, and the applied selection. Matching, selection, mouse
mode checks, and encoding all use one locked terminal state.

Operation scripts use the distinct `find_click` operation:

```json
{"op":"find_click","args":{"pattern":"Submit","require":"one"}}
```

Mouse commands require a compatible mouse mode from the child TUI. Twee rejects an incompatible gesture before it writes partial input.

```sh
twee click --x 12 --y 4 --button right --modifier ctrl
twee hover --x 20 --y 8
twee scroll --x 20 --y 8 --direction down --ticks 3
twee drag --from-x 4 --from-y 2 --to-x 30 --to-y 12
```

`--modifier` accepts `shift`, `alt`, and `ctrl`. The option is repeatable.

## Traces

A `.twee` trace is a ZIP bundle. It contains session metadata and timestamped terminal events.

Record a complete named session with `start --trace`:

```sh
twee start --trace session.twee -- ./myapp
twee key Enter
twee wait exit
```

Start and stop recording during an existing session:

```sh
twee start -- ./myapp
twee trace start --out session.twee
twee key Enter
twee trace stop
```

If you omit `--out`, `trace start` creates a temporary path. The JSON response contains that path.

Twee automatically finalizes an active trace when the child exits. `wait exit` and `stop` wait for the trace file to become durable.

Trace events include PTY output, input, terminal resize, and process exit. High-level mouse gestures include their encoded bytes and gesture metadata.

Search recorded raw PTY output directly without unpacking `events.jsonl`:

```sh
twee trace contains-output session.twee --text 'saved'
twee trace contains-output session.twee --hex 1b5b3f323030346c
twee trace contains-output session.twee --regex 'error [0-9]+'
```

Output events form one byte stream, so matches can cross event boundaries. A match reports the completion timestamp and zero-based inclusive bundle event range. No match exits with `ASSERTION_FAILED`. Use `--hex` for arbitrary bytes; `--text` and the recorded output must be valid UTF-8 for `--regex`, which uses Go/RE2 syntax.

Atomic pattern clicks also record the pattern, regular-expression flag,
selection decision, match, and target. Treat traces and screenshots as
sensitive: terminal text can contain credentials, and a pattern only identifies
rendered text—it does not establish that the application or target is trusted.

### Inspect traces

`twee inspect` validates a bundle, replays its output and resize events through the terminal model, and reports metadata plus final semantic state. It does not need a daemon or an interactive terminal.

```sh
twee inspect session.twee
twee inspect --format text session.twee
```

The JSON `replay` object contains the initial modes, a full styled final
snapshot, and every observed mode transition. Transitions identify the trace
timestamp, zero-based event index, and byte offset that completed the control
sequence, so enable/disable pairs remain visible even when they occur in one
output event. Text output prints the final viewport and a concise transition
summary. Mid-session traces describe state observable from the recorded replay;
modes enabled before tracing began are not reconstructible unless the trace
contains their enabling sequence.

Validation covers ZIP integrity, the manifest, the format version, every event, timestamp order, replay-safe dimensions, and declared network capture data.

An invalid bundle returns `INVALID_ARGUMENT`. The `error.details.issues` array contains all detected validation problems.

### Play traces

`twee play` replays terminal events with their recorded timing. It compresses idle gaps longer than two seconds by default.

```sh
twee play session.twee
twee play session.twee --speed 2
twee play session.twee --step
```

Playback controls are:

| Key | Action |
|---|---|
| `space` | Pause or resume. |
| `.` | Advance one event and remain paused. |
| `>` | Advance one second of trace time. |
| `r` | Restart from the beginning. |
| `q` | Quit. |

The automatic backend tries Kitty, iTerm2, and Sixel in that order. Playback rescales when the terminal window changes size.

Graphics playback requires a direct terminal connection. Twee does not support `tmux` or `screen` passthrough for playback.

### Export traces

`twee export` creates GIF, self-contained HTML, MP4, or WebM output. The output extension selects the format.

```sh
twee export session.twee -o session.gif
twee export session.twee -o session.html
twee export session.twee -o session.mp4 --speed 2 --quality high
```

GIF and HTML export use pure Go encoders. MP4 and WebM export require `ffmpeg` on `PATH` or through `--ffmpeg`.

HTML output works from a local file without a network connection. It includes playback, step, speed, and timeline controls.

Export keeps recorded timing by default. Use `--max-idle` to limit long idle gaps.

Use `--crop x,y,w,h` to select a cell rectangle. Use `--input-overlay` to show recent input and resize events.

Run `twee help export` for font, frame-rate, quality, crop, timing, and encoder options.

### CI replay artifacts

Record a one-shot scenario with `run --trace-out`. Export the trace after the scenario, even when the scenario fails.

The [CI replay artifact recipe](docs/ci-artifacts.md) preserves the failure status and attaches a reviewable GIF.

> [!CAUTION]
> Traces and replay files can contain secrets. Review every artifact before you upload it or share it.

A trace can contain typed input, terminal output, command arguments, and network packets. An input overlay can expose text that the application did not echo.

## Network capture

Network capture adds the managed program's raw IPv4 traffic to a complete `.twee` trace. The bundle stores a classic PCAP at `streams/network.pcap`.

Network capture is available on Linux only. The host must allow unprivileged user namespaces and access to `/dev/net/tun`.

Setup fails if Twee cannot create the private network. Twee does not silently use the host network as a fallback.

> [!CAUTION]
> A listener on `0.0.0.0` can expose the managed server to other machines. Select the listen address before you publish a port.

Use `--publish-tcp LISTEN=GUEST_PORT` when a host client must reach the managed server. The option is repeatable.

```sh
twee start --name web --trace web-session.twee --network-capture \
  --publish-tcp 127.0.0.1:8080=3000 -- \
  ./dev-server --host 0.0.0.0 --port 3000

twee wait text --name web --pattern listening --timeout 2m
curl --fail http://127.0.0.1:8080/health
twee stop --name web --grace 2s
twee inspect --format text web-session.twee
unzip -p web-session.twee streams/network.pcap >network.pcap
tcpdump -nn -r network.pcap
```

Bind the managed server to `0.0.0.0` inside its private network. The example exposes it only on the host loopback address.

The same capture options work with `run` and `wrap`. Each form requires its complete-session trace option.

```sh
twee run --trace-out web-session.twee --network-capture \
  --publish-tcp 127.0.0.1:8080=3000 --script ops.json -- ./dev-server

twee wrap --trace-out web-session.twee --network-capture \
  --publish-tcp 127.0.0.1:8080=3000 -- ./dev-server
```

Network recording starts before the managed command. It ends when the command exits or the session stops.

Each PCAP has a 64 MiB limit. At that limit, Twee stops packet recording and lets the managed program continue.

The capture includes IPv4 packets that cross the private network boundary. It excludes loopback traffic, IPv6, UNIX sockets, and inherited standard-stream traffic.

TLS and other encrypted protocols remain encrypted. The private network can change guest addresses, timing, and network behavior.

> [!CAUTION]
> PCAP data can contain passwords, tokens, cookies, request bodies, and Domain Name System (DNS) names. Restrict access and retention.

## Examples and additional documentation

- [`scripts/example-vim.sh`](scripts/example-vim.sh) drives Vim and records a complete trace.
- [`scripts/example-herdr-mouse.sh`](scripts/example-herdr-mouse.sh) demonstrates mouse automation.
- [`examples/recordings`](examples/recordings/README.md) contains replayable scenarios for terminal applications.
- [`docs/ci-artifacts.md`](docs/ci-artifacts.md) shows a GitHub Actions replay workflow.
- [`docs/extensible-trace-format.md`](docs/extensible-trace-format.md) describes trace format extension rules.
- [`docs/playback-export-verification.md`](docs/playback-export-verification.md) describes playback and export verification.

## Current limitations

- Each daemon owns one TUI.
- `wait stable` cannot complete while an application changes the screen continuously.
- `scrollback` returns an empty list because scrollback retention is not implemented.
- Twee does not support the Kitty keyboard protocol or DECCKM-aware cursor keys.
- Mouse input is synthetic. Twee does not capture the physical host mouse or support SGR-Pixels coordinates.
- Title and some non-mouse mode queries return defaults when the terminal model cannot expose that state.
- Screenshots use synthetic bold. An emoji cell renders as its leftmost glyph and a space.
- Graphics playback requires a direct terminal and does not support `tmux` or `screen` passthrough.
