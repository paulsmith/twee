TURN 1
TOOL CALL: read
INPUT:
{"path": ".ai/decisions/spec.md"}
TOOL RESULT: read
OUTPUT:
# Spec: Trace Recording (`.twee` zip bundle)

## Objective

Add a Playwright-inspired trace recording feature that captures session metadata, input events, PTY output, and screenshots into a `.twee` zip bundle, controllable via `twee trace start`/`twee trace stop`, the daemon RPC, and the `tuitest` Go API.

## Scope

### New files to create

| File | Purpose |
|---|---|
| `internal/trace/trace.go` | `Trace` type: accumulates events in memory, writes zip on `Close` |
| `internal/trace/trace_test.go` | Unit tests for the `Trace` type (round-trip write→read, manifest schema) |
| `internal/daemon/handlers_trace.go` | RPC handlers for `trace_start` / `trace_stop` |
| `cmd/twee/cmd_trace.go` | CLI verb `twee trace start` / `twee trace stop` |

### Existing files to modify

| File | Change |
|---|---|
| `internal/rpc/ops.go` | Add `OpTraceStart`, `OpTraceStop` constants |
| `internal/rpc/types.go` | Add `TraceStartArgs`, `TraceStopArgs` structs |
| `internal/engine/term.go` | Add `trace *trace.Trace` field, `EnableTrace`/`DisableTrace` methods, snapshot helper, wire output hook |
| `internal/ptyrunner/runner.go` | Add `Pid() int` method |
| `tuitest/options.go` | Add `Trace(path string)` option, `tracePath` config field, wire into `toEngine()` |
| `tuitest/term.go` | Wire `tracePath` into `engine.Config.TracePath`, log on failure |
| `internal/engine/config.go` | Add `TracePath string` field to `Config` |

### Files NOT touched

- `internal/recording/recording.go` — existing JSONL recording is untouched
- `internal/recording/replay.go` — no changes
- `internal/render/render.go` — used as-is, no modifications
- `Makefile` — no changes needed (pure Go + existing CGO deps)

## Requirements

### R1: Zip bundle format

The `.twee` file is a standard zip archive containing:

```
manifest.json
events.jsonl
screenshots/0000.png
screenshots/0001.png
...
```

### R2: `manifest.json` schema

```json
{
  "version": 1,
  "command": ["/usr/bin/vim", "foo.txt"],
  "env": {"TERM": "xterm-256color", ...},
  "cols": 80,
  "rows": 24,
  "pid": 12345,
  "host": {
    "os": "darwin",
    "arch": "arm64",
    "hostname": "macbook.local"
  },
  "started_at": "2025-05-01T10:00:00Z",
  "stopped_at": "2025-05-01T10:05:00Z",
  "screenshots": ["screenshots/0000.png", "screenshots/0001.png"]
}
```

All fields are populated at trace creation except `stopped_at` and the final screenshot list, which are filled at `Close`.

### R3: `events.jsonl` format

Same event schema as the existing `recording.Event` type — one JSON object per line, fields: `t_ms`, `type` (`"output"`, `"input"`, `"resize"`), `bytes_b64`, `kind`, `key`, `cols`, `rows`. The events JSONL is accumulated in an in-memory `bytes.Buffer` during the trace and flushed to the zip at `Close`.

### R4: Screenshots

- One screenshot is captured at trace start by the daemon handler (`handleTraceStart`) after calling `EnableTrace`.
- One screenshot is captured at trace stop by the daemon handler (`handleTraceStop`) before calling `DisableTrace`.
- Screenshots are PNG files rendered via `internal/render.Render` + `render.EncodePNG`, stored in `screenshots/NNNN.png` (zero-padded 4 digits).
- The `Trace` type exposes `AddScreenshotPNG(pngData []byte)` which accepts pre-encoded PNG bytes. The `engine.Term` type exposes `TraceAddScreenshot(pngData []byte)` for the same purpose.
- Rendering is done at the call site (daemon handlers, tuitest) to avoid an import cycle between `engine` and `render`. Screenshots are stored as `[]byte` in memory until zip flush.

### R5: `internal/trace.Trace` type

```go
type Manifest struct {
    Version     int               `json:"version"`
    Command     []string          `json:"command"`
    Env         map[string]string `json:"env,omitempty"`
    Cols        int               `json:"cols"`
    Rows        int               `json:"rows"`
    Pid         int               `json:"pid"`
    Host        HostInfo          `json:"host"`
    StartedAt   time.Time         `json:"started_at"`
    StoppedAt   time.Time         `json:"stopped_at"`
    Screenshots []string          `json:"screenshots"`
}

type HostInfo struct {
    OS       string `json:"os"`
    Arch     string `json:"arch"`
    Hostname string `json:"hostname"`
}

func New(path string, m Manifest) (*Trace, error)
func (tr *Trace) WriteOutput(b []byte, ts time.Time)
func (tr *Trace) WriteInput(kind, key string, b []byte)
func (tr *Trace) WriteResize(cols, rows int)
func (tr *Trace) AddScreenshotPNG(pngData []byte)
func (tr *Trace) Close() error
```

- `New` records `StartedAt = time.Now()` and stores the manifest. It does **not** write anything to disk yet.
- `WriteOutput`, `WriteInput`, `WriteResize` append to the internal `events` buffer (same JSON encoding as `recording.Event`). These are goroutine-safe (protected by a mutex).
- `AddScreenshotPNG` stores pre-encoded PNG bytes in an internal `[][]byte` slice. Callers are responsible for rendering.
- `Close` sets `StoppedAt`, assembles the zip (manifest.json, events.jsonl, screenshots/*), writes to the path, and returns any error. `Close` is idempotent — second call returns the same error.

### R6: `ptyrunner.Runner.Pid()` method

```go
func (r *Runner) Pid() int {
    if r.cmd.Process != nil {
        return r.cmd.Process.Pid
    }
    return 0
}
```

### R7: `engine.Term` integration

- Add `TracePath string` to `engine.Config`.
- Add `tr *trace.Trace` field to `engine.Term` (guarded by `cfgMu`, same as `rec`).
- Add `EnableTrace(path string) error`:
  - Creates a `trace.New(path, ...)` with manifest populated from `t.cfg` and `t.runner.Pid()`. Host info is populated by `trace.DefaultHostInfo()` inside `trace.New`.
  - Wires the output hook: if both `t.rec` and `t.tr` are non-nil, the hook writes to both. If only one, it writes to that one. (The pump only supports one hook, so the hook function must fan out via `updateOutputHookLocked`.)
  - Records `t.cfg.TracePath = path`.
  - Does NOT capture screenshots (to avoid import cycle with `render`). Screenshot capture is done by the daemon handler or tuitest caller.
- Add `DisableTrace() error`:
  - Calls `tr.Close()`.
  - Sets `t.tr = nil`, `t.cfg.TracePath = ""`.
  - Resets output hook via `updateOutputHookLocked`.
- Add `TraceAddScreenshot(pngData []byte)`:
  - Passes pre-encoded PNG to the active trace (no-op if no trace active).
- In `engine.Start`: if `cfg.TracePath != ""`, call `EnableTrace` after the pump is running.
- In `engine.Close`: if `t.tr != nil`, close the trace before closing recorder.
- In `Type`, `Key`, `Paste`, `Resize` (in `input.go`): after recording to `rec`, also write to `tr` if non-nil (same lock-then-unlock-then-write pattern).

### R8: RPC layer

In `internal/rpc/ops.go`:
```go
OpTraceStart = "trace_start"
OpTraceStop  = "trace_stop"
```

In `internal/rpc/types.go`:
```go
type TraceStartArgs struct {
    Out string `json:"out,omitempty"`
}
```

### R9: Daemon handlers (`internal/daemon/handlers_trace.go`)

- `handleTraceStart`: parse `TraceStartArgs`, default path to `os.MkdirTemp` + `session-<nanos>.twee`, call `t.EnableTrace(path)`, return `{"out": path}`.
- `handleTraceStop`: get `t.TracePath()`, call `t.DisableTrace()`, return `{"path": path}`.
- Register via `init()` + `optionalRegistrations` (same pattern as `handlers_record.go`).

### R10: CLI verb (`cmd/twee/cmd_trace.go`)

```
twee trace start [-out <path.twee>] [-name <name>]
twee trace stop [-name <name>]
```

Follows the exact pattern of `cmd/twee/cmd_record.go`. Register via `init()`.

### R11: `tuitest` integration

- `tuitest/options.go`: add `Trace(path string) Option` that sets `config.tracePath`.
- `tuitest/options.go`: `toEngine()` copies `c.tracePath` to `engine.Config.TracePath`.
- `tuitest/term.go`: in `Run()`, if `tracePath` is set, it's already wired through config; on `t.Failed()`, log the trace path.

### R12: Unit tests (`internal/trace/trace_test.go`)

1. **TestTraceRoundTrip**: Create a `Trace`, write some output events, some input events, a resize, add a screenshot (using a synthetic `engine.Snapshot` with a small grid), `Close`, then open the zip and verify:
   - `manifest.json` parses to a valid `Manifest` with version 1, correct command, non-zero pid, non-empty host fields, `StartedAt` before `StoppedAt`.
   - `events.jsonl` has the expected number of lines, each parseable as `recording.Event`.
   - `screenshots/0000.png` exists and is a valid PNG.
2. **TestTraceIdempotentClose**: Call `Close` twice, second returns nil error.
3. **TestTraceConcurrentWrites**: Launch 10 goroutines writing output concurrently, then `Close` — no races (run with `-race`).

## Non-requirements

- **Replay/viewer for `.twee` files**: Out of scope. We only define the write path.
- **Automatic periodic screenshots**: Screenshots are only at start, stop, and explicit `AddScreenshot` calls. No timer-based capture.
- **Modifying the existing JSONL recording format**: The `internal/recording` package is untouched. Both formats coexist independently.
- **CLI for inspecting `.twee` files** (e.g., `twee trace show`): Out of scope.
- **Streaming zip writes**: The zip is assembled entirely at `Close`. No incremental disk writes during the trace.
- **Size limits or rotation**: No max-size guards in this iteration.
- **Exit event in trace**: The trace captures events between start/stop boundaries only. It does not record process exit (the existing JSONL recorder handles that).
- **Recording screenshots for every input event**: Too expensive. Explicit opt-in only.

## Success criteria

1. `go build ./...` succeeds with no new warnings.
2. `go vet ./...` passes.
3. `go test ./internal/trace/` passes, including under `-race`.
4. `go test ./internal/engine/` passes (existing tests still work).
5. `go test ./tuitest/` passes (existing tests still work).
6. The `internal/trace` package produces a valid zip that can be opened with `archive/zip.OpenReader` and contains `manifest.json`, `events.jsonl`, and `screenshots/*.png` — verified by `TestTraceRoundTrip`.
7. `go doc ./internal/trace` shows the public API: `Trace`, `Manifest`, `HostInfo`, `New`, `WriteOutput`, `WriteInput`, `WriteResize`, `AddScreenshot`, `Close`.
8. `go doc ./internal/engine Term.EnableTrace` shows the method.
9. The new RPC ops `trace_start`/`trace_stop` are registered in the dispatcher (verified by `go build` — if handler registration panics on missing op, it would fail at init time).
10. `cmd/twee/cmd_trace.go` compiles and `twee help trace` prints usage.

## Constraints

1. **Package placement**: The new trace writer lives in `internal/trace`, not in `internal/recording`. The two packages are independent.
2. **No new dependencies**: Use only `archive/zip`, `encoding/json`, `image/png` from stdlib, plus existing internal packages.
3. **Goroutine safety**: `Trace.WriteOutput`, `WriteInput`, `WriteResize`, `AddScreenshotPNG` must be safe for concurrent use. Use a single `sync.Mutex` (same pattern as `recording.Recorder`).
4. **Handler registration**: Use the `init()` + `optionalRegistrations` pattern (same as `handlers_record.go`, `handlers_input.go`, etc.).
5. **CLI verb pattern**: `cmd_trace.go` follows the exact structure of `cmd_record.go` — `register("trace", runTrace)`, subverbs `start`/`stop`, `flag.NewFlagSet`, `callAndEmit`.
6. **Error codes**: Reuse `rpc.CodeIO` and `rpc.CodeInvalidArgument` (no new error codes).
7. **File extension**: `.twee` for trace bundles.
8. **JSON field naming**: `snake_case` everywhere, matching existing conventions.
9. **Test file naming**: `*_test.go` in the same package (not `_test` suffix package).
10. **Import cycle avoidance**: `internal/trace` does NOT import `internal/engine` or `internal/render`. It accepts raw PNG bytes via `AddScreenshotPNG`. Rendering is done by the daemon handlers (`handlers_trace.go`) which import both `engine` and `render`. This breaks the `engine → trace → render → engine` cycle.

TOOL CALL: read
INPUT:
{"path": ".ai/candidates/claude.diff"}
TOOL RESULT: read
TOOL CALL: read
INPUT:
{"path": ".ai/candidates/claude.test"}
TOOL RESULT: read
OUTPUT:
?   	github.com/paulsmith/research/twee/cmd/libghostty-smoke	[no test files]
ok  	github.com/paulsmith/research/twee/cmd/twee	(cached)
?   	github.com/paulsmith/research/twee/fixtures/menu	[no test files]
ok  	github.com/paulsmith/research/twee/internal/daemon	(cached)
?   	github.com/paulsmith/research/twee/internal/engine	[no test files]
?   	github.com/paulsmith/research/twee/internal/input	[no test files]
?   	github.com/paulsmith/research/twee/internal/ptyrunner	[no test files]
ok  	github.com/paulsmith/research/twee/internal/pump	(cached)
ok  	github.com/paulsmith/research/twee/internal/recording	(cached)
ok  	github.com/paulsmith/research/twee/internal/render	(cached)
ok  	github.com/paulsmith/research/twee/internal/rpc	(cached)
?   	github.com/paulsmith/research/twee/internal/snapshot	[no test files]
ok  	github.com/paulsmith/research/twee/internal/trace	(cached)
ok  	github.com/paulsmith/research/twee/internal/vt	(cached)
ok  	github.com/paulsmith/research/twee/tuitest	0.546s

TOOL CALL: read
INPUT:
{"path": ".ai/candidates/codex.diff"}
TOOL RESULT: read
TOOL CALL: read
INPUT:
{"path": ".ai/candidates/codex.test"}
TOOL RESULT: read
OUTPUT:
?   	github.com/paulsmith/research/twee/cmd/libghostty-smoke	[no test files]
ok  	github.com/paulsmith/research/twee/cmd/twee	4.002s
?   	github.com/paulsmith/research/twee/fixtures/menu	[no test files]
ok  	github.com/paulsmith/research/twee/internal/daemon	0.032s
?   	github.com/paulsmith/research/twee/internal/engine	[no test files]
?   	github.com/paulsmith/research/twee/internal/input	[no test files]
?   	github.com/paulsmith/research/twee/internal/ptyrunner	[no test files]
ok  	github.com/paulsmith/research/twee/internal/pump	0.010s
ok  	github.com/paulsmith/research/twee/internal/recording	0.012s
ok  	github.com/paulsmith/research/twee/internal/render	0.012s
ok  	github.com/paulsmith/research/twee/internal/rpc	0.019s
?   	github.com/paulsmith/research/twee/internal/snapshot	[no test files]
ok  	github.com/paulsmith/research/twee/internal/vt	0.013s
ok  	github.com/paulsmith/research/twee/tuitest	0.716s

TOOL CALL: read
INPUT:
{"path": ".ai/candidates/gemini.diff"}
TOOL RESULT: read
TOOL CALL: read
INPUT:
{"path": ".ai/candidates/gemini.test"}
TOOL RESULT: read
OUTPUT:
?   	github.com/paulsmith/research/twee/cmd/libghostty-smoke	[no test files]
ok  	github.com/paulsmith/research/twee/cmd/twee	4.019s
?   	github.com/paulsmith/research/twee/fixtures/menu	[no test files]
ok  	github.com/paulsmith/research/twee/internal/daemon	0.032s
?   	github.com/paulsmith/research/twee/internal/engine	[no test files]
?   	github.com/paulsmith/research/twee/internal/input	[no test files]
?   	github.com/paulsmith/research/twee/internal/ptyrunner	[no test files]
ok  	github.com/paulsmith/research/twee/internal/pump	0.012s
ok  	github.com/paulsmith/research/twee/internal/recording	0.015s
ok  	github.com/paulsmith/research/twee/internal/render	0.014s
ok  	github.com/paulsmith/research/twee/internal/rpc	0.019s
?   	github.com/paulsmith/research/twee/internal/snapshot	[no test files]
ok  	github.com/paulsmith/research/twee/internal/vt	0.014s
ok  	github.com/paulsmith/research/twee/tuitest	0.691s

TURN 2
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee && ls -la .ai/candidates/"}
TOOL RESULT: bash
OUTPUT:
total 12
drwxr-xr-x 8 paul staff  256 May  1 10:44 .
drwxr-xr-x 5 paul staff  160 May  1 10:15 ..
-rw-r--r-- 1 paul staff    0 May  1 10:44 claude.diff
-rw-r--r-- 1 paul staff 1000 May  1 10:44 claude.test
-rw-r--r-- 1 paul staff    0 May  1 10:44 codex.diff
-rw-r--r-- 1 paul staff  922 May  1 10:44 codex.test
-rw-r--r-- 1 paul staff    0 May  1 10:44 gemini.diff
-rw-r--r-- 1 paul staff  922 May  1 10:44 gemini.test

TURN 3
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee && git branch -a | grep impl"}
TOOL RESULT: bash
OUTPUT:
+ impl/claude
+ impl/codex
+ impl/gemini

TURN 4
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee && git log --oneline main..impl/claude | head -5"}
TOOL RESULT: bash
OUTPUT:
a1c2856 feat: add trace recording (.twee zip bundle)

TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee && git log --oneline main..impl/codex | head -5"}
TOOL RESULT: bash
OUTPUT:
(no output)
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee && git log --oneline main..impl/gemini | head -5"}
TOOL RESULT: bash
OUTPUT:
(no output)
TURN 5
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee && git diff main..impl/codex --stat"}
TOOL RESULT: bash
OUTPUT:
(no output)
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee && git diff main..impl/gemini --stat"}
TOOL RESULT: bash
OUTPUT:
(no output)
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee && git diff main..impl/claude --stat"}
TOOL RESULT: bash
OUTPUT:
 cmd/twee/cmd_trace.go             |  47 +++++++++
 internal/daemon/handlers_trace.go |  68 ++++++++++++
 internal/engine/config.go         |   1 +
 internal/engine/input.go          |  16 +++
 internal/engine/query.go          |   3 +
 internal/engine/term.go           | 102 +++++++++++++++++-
 internal/ptyrunner/runner.go      |   8 ++
 internal/rpc/ops.go               |   2 +
 internal/rpc/types.go             |   4 +
 internal/trace/trace.go           | 215 ++++++++++++++++++++++++++++++++++++++
 internal/trace/trace_test.go      | 199 +++++++++++++++++++++++++++++++++++
 tuitest/options.go                |   8 ++
 tuitest/term.go                   |   9 +-
 13 files changed, 677 insertions(+), 5 deletions(-)

TURN 6
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee && git diff main..impl/claude"}
TOOL RESULT: bash
OUTPUT:
diff --git a/cmd/twee/cmd_trace.go b/cmd/twee/cmd_trace.go
new file mode 100644
index 0000000..a382066
--- /dev/null
+++ b/cmd/twee/cmd_trace.go
@@ -0,0 +1,47 @@
+package main
+
+import (
+	"flag"
+
+	"github.com/paulsmith/research/twee/internal/rpc"
+)
+
+func init() {
+	register("trace", runTrace)
+
+	registerUsage("trace", `twee trace start [-out <path.twee>] [-name <name>]
+twee trace stop [-name <name>]
+Start/stop a trace recording on the running session.
+The trace is a .twee zip bundle containing a manifest, events, and screenshots.`)
+	registerUsage("trace start", `twee trace start [-out <path.twee>] [-name <name>]
+Start a trace recording on the running session.`)
+	registerUsage("trace stop", `twee trace stop [-name <name>]
+Stop a trace recording and write the .twee bundle.`)
+}
+
+func runTrace(args []string) {
+	if len(args) == 0 {
+		fatalUsage("trace: missing subverb (start|stop)")
+	}
+	sub := args[0]
+	rest := args[1:]
+	switch sub {
+	case "start":
+		fs := flag.NewFlagSet("trace start", flag.ExitOnError)
+		name := fs.String("name", "default", "session name")
+		out := fs.String("out", "", "output path (.twee)")
+		if err := fs.Parse(rest); err != nil {
+			fatalUsage("trace start: %v", err)
+		}
+		callAndEmit(*name, rpc.OpTraceStart, rpc.TraceStartArgs{Out: *out})
+	case "stop":
+		fs := flag.NewFlagSet("trace stop", flag.ExitOnError)
+		name := fs.String("name", "default", "session name")
+		if err := fs.Parse(rest); err != nil {
+			fatalUsage("trace stop: %v", err)
+		}
+		callAndEmit(*name, rpc.OpTraceStop, nil)
+	default:
+		fatalUsage("trace: unknown subverb %q", sub)
+	}
+}
diff --git a/internal/daemon/handlers_trace.go b/internal/daemon/handlers_trace.go
new file mode 100644
index 0000000..3ff9a2f
--- /dev/null
+++ b/internal/daemon/handlers_trace.go
@@ -0,0 +1,68 @@
+package daemon
+
+import (
+	"bytes"
+	"encoding/json"
+	"fmt"
+	"os"
+	"path/filepath"
+	"time"
+
+	"github.com/paulsmith/research/twee/internal/engine"
+	"github.com/paulsmith/research/twee/internal/render"
+	"github.com/paulsmith/research/twee/internal/rpc"
+)
+
+func init() {
+	optionalRegistrations = append(optionalRegistrations, func(d *Dispatcher) {
+		d.Register(rpc.OpTraceStart, handleTraceStart)
+		d.Register(rpc.OpTraceStop, handleTraceStop)
+	})
+}
+
+func handleTraceStart(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
+	var a rpc.TraceStartArgs
+	if err := json.Unmarshal(raw, &a); err != nil && len(raw) > 0 {
+		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
+	}
+	if a.Out == "" {
+		dir, err := os.MkdirTemp("", "twee-trace-")
+		if err != nil {
+			return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
+		}
+		a.Out = filepath.Join(dir, fmt.Sprintf("session-%d.twee", time.Now().UnixNano()))
+	}
+	if err := t.EnableTrace(a.Out); err != nil {
+		return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
+	}
+	// Capture initial screenshot.
+	if png, err := renderScreenshot(t); err == nil {
+		t.TraceAddScreenshot(png)
+	}
+	return map[string]string{"out": a.Out}, nil
+}
+
+func handleTraceStop(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
+	path := t.TracePath()
+	// Capture final screenshot before closing the trace.
+	if png, err := renderScreenshot(t); err == nil {
+		t.TraceAddScreenshot(png)
+	}
+	if err := t.DisableTrace(); err != nil {
+		return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
+	}
+	return map[string]string{"path": path}, nil
+}
+
+func renderScreenshot(t *engine.Term) ([]byte, error) {
+	snap := t.Snapshot()
+	img, err := render.Render(snap, render.Default())
+	if err != nil {
+		return nil, err
+	}
+	var buf bytes.Buffer
+	if err := render.EncodePNG(&buf, img); err != nil {
+		return nil, err
+	}
+	return buf.Bytes(), nil
+}
diff --git a/internal/engine/config.go b/internal/engine/config.go
index ac2a246..979d6f9 100644
--- a/internal/engine/config.go
+++ b/internal/engine/config.go
@@ -19,6 +19,7 @@ type Config struct {
 	StableQuietWindow time.Duration
 
 	RecordPath string
+	TracePath  string
 }
 
 // applyDefaults fills in zero fields with sensible values.
diff --git a/internal/engine/input.go b/internal/engine/input.go
index 3cc0b30..896c03a 100644
--- a/internal/engine/input.go
+++ b/internal/engine/input.go
@@ -15,10 +15,14 @@ func (t *Term) Type(s string) error {
 	t.recordInput(fmt.Sprintf("Type %q", s))
 	t.cfgMu.Lock()
 	rec := t.rec
+	tr := t.tr
 	t.cfgMu.Unlock()
 	if rec != nil {
 		rec.WriteInput("type", "", []byte(s))
 	}
+	if tr != nil {
+		tr.WriteInput("type", "", []byte(s))
+	}
 	return nil
 }
 
@@ -34,10 +38,14 @@ func (t *Term) Key(k input.Key) error {
 	t.recordInput("Key " + input.Name(k))
 	t.cfgMu.Lock()
 	rec := t.rec
+	tr := t.tr
 	t.cfgMu.Unlock()
 	if rec != nil {
 		rec.WriteInput("key", input.Name(k), b)
 	}
+	if tr != nil {
+		tr.WriteInput("key", input.Name(k), b)
+	}
 	return nil
 }
 
@@ -50,10 +58,14 @@ func (t *Term) Paste(text string) error {
 	t.recordInput(fmt.Sprintf("Paste %q", text))
 	t.cfgMu.Lock()
 	rec := t.rec
+	tr := t.tr
 	t.cfgMu.Unlock()
 	if rec != nil {
 		rec.WriteInput("paste", "", b)
 	}
+	if tr != nil {
+		tr.WriteInput("paste", "", b)
+	}
 	return nil
 }
 
@@ -69,10 +81,14 @@ func (t *Term) Resize(cols, rows int) error {
 	t.recordInput(fmt.Sprintf("Resize %dx%d", cols, rows))
 	t.cfgMu.Lock()
 	rec := t.rec
+	tr := t.tr
 	t.cfgMu.Unlock()
 	if rec != nil {
 		rec.WriteResize(cols, rows)
 	}
+	if tr != nil {
+		tr.WriteResize(cols, rows)
+	}
 	return nil
 }
 
diff --git a/internal/engine/query.go b/internal/engine/query.go
index aa062e3..ca0bceb 100644
--- a/internal/engine/query.go
+++ b/internal/engine/query.go
@@ -84,6 +84,9 @@ func (t *Term) Diagnostic() string {
 	if t.cfg.RecordPath != "" {
 		sb.printf("recording: %s\n", t.cfg.RecordPath)
 	}
+	if t.cfg.TracePath != "" {
+		sb.printf("trace: %s\n", t.cfg.TracePath)
+	}
 	return sb.String()
 }
 
diff --git a/internal/engine/term.go b/internal/engine/term.go
index 2f19897..11415be 100644
--- a/internal/engine/term.go
+++ b/internal/engine/term.go
@@ -10,6 +10,7 @@ import (
 	"github.com/paulsmith/research/twee/internal/ptyrunner"
 	"github.com/paulsmith/research/twee/internal/pump"
 	"github.com/paulsmith/research/twee/internal/recording"
+	"github.com/paulsmith/research/twee/internal/trace"
 	"github.com/paulsmith/research/twee/internal/vt"
 )
 
@@ -21,6 +22,7 @@ type Term struct {
 	runner *ptyrunner.Runner
 	pump   *pump.Pump
 	rec    *recording.Recorder
+	tr     *trace.Trace
 
 	closeOnce sync.Once
 	closeErr  error
@@ -73,7 +75,6 @@ func Start(ctx context.Context, cfg Config) (*Term, error) {
 			_ = runner.Close()
 			return nil, fmt.Errorf("recording: %w", err)
 		}
-		p.SetOutputHook(func(b []byte, ts time.Time) { rec.WriteOutput(b, ts) })
 	}
 
 	t := &Term{
@@ -84,10 +85,21 @@ func Start(ctx context.Context, cfg Config) (*Term, error) {
 		pumpDone:  make(chan struct{}),
 		startedAt: time.Now(),
 	}
+	// Set up output hook for the initial recorder (if any). The hook
+	// will be updated by EnableTrace if a trace is also started.
+	if rec != nil {
+		t.updateOutputHookLocked() // safe: no other goroutines access t yet
+	}
 	go func() {
 		_ = p.Run()
 		close(t.pumpDone)
 	}()
+	if cfg.TracePath != "" {
+		if err := t.EnableTrace(cfg.TracePath); err != nil {
+			_ = t.Close()
+			return nil, fmt.Errorf("trace: %w", err)
+		}
+	}
 	return t, nil
 }
 
@@ -97,6 +109,10 @@ func (t *Term) Close() error {
 		t.closeErr = t.runner.Close()
 		<-t.pumpDone
 		t.cfgMu.Lock()
+		if t.tr != nil {
+			_ = t.tr.Close()
+			t.tr = nil
+		}
 		if t.rec != nil {
 			t.rec.WriteExit(t.runner.ExitCode())
 			_ = t.rec.Close()
@@ -147,7 +163,7 @@ func (t *Term) EnableRecording(path string) error {
 	}
 	t.rec = rec
 	t.cfg.RecordPath = path
-	t.pump.SetOutputHook(func(b []byte, ts time.Time) { rec.WriteOutput(b, ts) })
+	t.updateOutputHookLocked()
 	return nil
 }
 
@@ -162,10 +178,90 @@ func (t *Term) DisableRecording() error {
 	err := t.rec.Close()
 	t.rec = nil
 	t.cfg.RecordPath = ""
-	t.pump.SetOutputHook(nil)
+	t.updateOutputHookLocked()
 	return err
 }
 
+// TracePath returns the trace path (or "" if not tracing).
+func (t *Term) TracePath() string {
+	t.cfgMu.Lock()
+	defer t.cfgMu.Unlock()
+	return t.cfg.TracePath
+}
+
+// EnableTrace starts a trace recording to path.
+func (t *Term) EnableTrace(path string) error {
+	t.cfgMu.Lock()
+	defer t.cfgMu.Unlock()
+	if t.tr != nil {
+		_ = t.tr.Close()
+		t.tr = nil
+	}
+	tr, err := trace.New(path, trace.Manifest{
+		Command: t.cfg.Cmd,
+		Env:     t.cfg.Env,
+		Cols:    t.cfg.Cols,
+		Rows:    t.cfg.Rows,
+		Pid:     t.runner.Pid(),
+	})
+	if err != nil {
+		return err
+	}
+	t.tr = tr
+	t.cfg.TracePath = path
+	t.updateOutputHookLocked()
+	return nil
+}
+
+// DisableTrace stops tracing and writes the zip bundle.
+func (t *Term) DisableTrace() error {
+	t.cfgMu.Lock()
+	defer t.cfgMu.Unlock()
+	if t.tr == nil {
+		return nil
+	}
+	err := t.tr.Close()
+	t.tr = nil
+	t.cfg.TracePath = ""
+	t.updateOutputHookLocked()
+	return err
+}
+
+// TraceAddScreenshot adds a pre-encoded PNG screenshot to the active trace.
+// Returns nil if no trace is active.
+func (t *Term) TraceAddScreenshot(pngData []byte) {
+	t.cfgMu.Lock()
+	tr := t.tr
+	t.cfgMu.Unlock()
+	if tr != nil {
+		tr.AddScreenshotPNG(pngData)
+	}
+}
+
+// updateOutputHookLocked sets the pump's output hook to fan out to
+// whichever recorders are active. Must be called with cfgMu held.
+func (t *Term) updateOutputHookLocked() {
+	rec := t.rec
+	tr := t.tr
+	switch {
+	case rec != nil && tr != nil:
+		t.pump.SetOutputHook(func(b []byte, ts time.Time) {
+			rec.WriteOutput(b, ts)
+			tr.WriteOutput(b, ts)
+		})
+	case rec != nil:
+		t.pump.SetOutputHook(func(b []byte, ts time.Time) {
+			rec.WriteOutput(b, ts)
+		})
+	case tr != nil:
+		t.pump.SetOutputHook(func(b []byte, ts time.Time) {
+			tr.WriteOutput(b, ts)
+		})
+	default:
+		t.pump.SetOutputHook(nil)
+	}
+}
+
 // recordInput appends a description to the bounded ring buffer.
 func (t *Term) recordInput(desc string) {
 	t.inputsMu.Lock()
diff --git a/internal/ptyrunner/runner.go b/internal/ptyrunner/runner.go
index fbe2d46..6235f6a 100644
--- a/internal/ptyrunner/runner.go
+++ b/internal/ptyrunner/runner.go
@@ -81,6 +81,14 @@ func (r *Runner) wait() {
 	close(r.exited)
 }
 
+// Pid returns the child process ID, or 0 if the process has not started.
+func (r *Runner) Pid() int {
+	if r.cmd.Process != nil {
+		return r.cmd.Process.Pid
+	}
+	return 0
+}
+
 // Master returns the PTY master fd. Reads on it produce app output;
 // writes deliver input to the app.
 func (r *Runner) Master() io.ReadWriter { return r.master }
diff --git a/internal/rpc/ops.go b/internal/rpc/ops.go
index 28aa292..ded8302 100644
--- a/internal/rpc/ops.go
+++ b/internal/rpc/ops.go
@@ -27,6 +27,8 @@ const (
 	OpScreenshot  = "screenshot"
 	OpRecordStart = "record_start"
 	OpRecordStop  = "record_stop"
+	OpTraceStart  = "trace_start"
+	OpTraceStop   = "trace_stop"
 	OpDiff        = "diff"
 
 	// Waits
diff --git a/internal/rpc/types.go b/internal/rpc/types.go
index 29abfa7..07cc7b1 100644
--- a/internal/rpc/types.go
+++ b/internal/rpc/types.go
@@ -95,6 +95,10 @@ type RecordStartArgs struct {
 	Out string `json:"out,omitempty"`
 }
 
+type TraceStartArgs struct {
+	Out string `json:"out,omitempty"`
+}
+
 type DiffArgs struct {
 	Against string `json:"against"`
 }
diff --git a/internal/trace/trace.go b/internal/trace/trace.go
new file mode 100644
index 0000000..8c9dbe3
--- /dev/null
+++ b/internal/trace/trace.go
@@ -0,0 +1,215 @@
+// Package trace writes a .twee trace bundle — a zip archive containing
+// a manifest, JSONL event stream, and PNG screenshots.
+package trace
+
+import (
+	"archive/zip"
+	"bytes"
+	"encoding/base64"
+	"encoding/json"
+	"fmt"
+	"os"
+	"runtime"
+	"sync"
+	"time"
+)
+
+// Manifest is the top-level metadata written to manifest.json inside
+// the zip bundle.
+type Manifest struct {
+	Version     int               `json:"version"`
+	Command     []string          `json:"command"`
+	Env         map[string]string `json:"env,omitempty"`
+	Cols        int               `json:"cols"`
+	Rows        int               `json:"rows"`
+	Pid         int               `json:"pid"`
+	Host        HostInfo          `json:"host"`
+	StartedAt   time.Time         `json:"started_at"`
+	StoppedAt   time.Time         `json:"stopped_at"`
+	Screenshots []string          `json:"screenshots"`
+}
+
+// HostInfo captures details about the machine that recorded the trace.
+type HostInfo struct {
+	OS       string `json:"os"`
+	Arch     string `json:"arch"`
+	Hostname string `json:"hostname"`
+}
+
+// DefaultHostInfo returns HostInfo populated from the current machine.
+func DefaultHostInfo() HostInfo {
+	h, _ := os.Hostname()
+	return HostInfo{
+		OS:       runtime.GOOS,
+		Arch:     runtime.GOARCH,
+		Hostname: h,
+	}
+}
+
+// event mirrors recording.Event so that the trace package does not
+// import internal/recording.
+type event struct {
+	TMS   int64  `json:"t_ms"`
+	Type  string `json:"type"`
+	Bytes string `json:"bytes_b64,omitempty"`
+	Kind  string `json:"kind,omitempty"`
+	Key   string `json:"key,omitempty"`
+	Cols  int    `json:"cols,omitempty"`
+	Rows  int    `json:"rows,omitempty"`
+}
+
+// Trace accumulates session artifacts in memory and writes a .twee zip
+// bundle when Close is called.
+type Trace struct {
+	mu   sync.Mutex
+	path string
+	man  Manifest
+
+	events      bytes.Buffer
+	evEnc       *json.Encoder
+	screenshots [][]byte // PNG-encoded
+
+	start  time.Time
+	closed bool
+	err    error
+}
+
+// New creates a Trace that will be written to path on Close.
+// The manifest's StartedAt is set to time.Now(); Version is forced to 1.
+func New(path string, m Manifest) (*Trace, error) {
+	now := time.Now()
+	m.Version = 1
+	m.StartedAt = now
+	m.Host = DefaultHostInfo()
+	tr := &Trace{
+		path:  path,
+		man:   m,
+		start: now,
+	}
+	tr.evEnc = json.NewEncoder(&tr.events)
+	return tr, nil
+}
+
+func (tr *Trace) ms(ts time.Time) int64 {
+	if ts.IsZero() {
+		ts = time.Now()
+	}
+	return ts.Sub(tr.start).Milliseconds()
+}
+
+// WriteOutput records raw PTY output bytes.
+func (tr *Trace) WriteOutput(b []byte, ts time.Time) {
+	tr.mu.Lock()
+	defer tr.mu.Unlock()
+	_ = tr.evEnc.Encode(event{
+		TMS:   tr.ms(ts),
+		Type:  "output",
+		Bytes: base64.StdEncoding.EncodeToString(b),
+	})
+}
+
+// WriteInput records an input event (type, key, paste).
+func (tr *Trace) WriteInput(kind, key string, b []byte) {
+	tr.mu.Lock()
+	defer tr.mu.Unlock()
+	_ = tr.evEnc.Encode(event{
+		TMS:   tr.ms(time.Now()),
+		Type:  "input",
+		Kind:  kind,
+		Key:   key,
+		Bytes: base64.StdEncoding.EncodeToString(b),
+	})
+}
+
+// WriteResize records a terminal resize.
+func (tr *Trace) WriteResize(cols, rows int) {
+	tr.mu.Lock()
+	defer tr.mu.Unlock()
+	_ = tr.evEnc.Encode(event{
+		TMS:  tr.ms(time.Now()),
+		Type: "resize",
+		Cols: cols,
+		Rows: rows,
+	})
+}
+
+// AddScreenshotPNG stores a pre-encoded PNG screenshot. The caller is
+// responsible for rendering the snapshot to PNG before calling this.
+func (tr *Trace) AddScreenshotPNG(pngData []byte) {
+	tr.mu.Lock()
+	defer tr.mu.Unlock()
+	tr.screenshots = append(tr.screenshots, append([]byte(nil), pngData...))
+}
+
+// Close finalises the trace, writing the zip bundle to disk. It is
+// idempotent — the second and subsequent calls return the error (if
+// any) from the first call.
+func (tr *Trace) Close() error {
+	tr.mu.Lock()
+	defer tr.mu.Unlock()
+	if tr.closed {
+		return tr.err
+	}
+	tr.closed = true
+	tr.err = tr.writeLocked()
+	return tr.err
+}
+
+func (tr *Trace) writeLocked() error {
+	tr.man.StoppedAt = time.Now()
+
+	// Build screenshot manifest paths.
+	tr.man.Screenshots = make([]string, len(tr.screenshots))
+	for i := range tr.screenshots {
+		tr.man.Screenshots[i] = fmt.Sprintf("screenshots/%04d.png", i)
+	}
+
+	f, err := os.Create(tr.path)
+	if err != nil {
+		return err
+	}
+	zw := zip.NewWriter(f)
+
+	// manifest.json
+	mw, err := zw.Create("manifest.json")
+	if err != nil {
+		_ = f.Close()
+		return err
+	}
+	enc := json.NewEncoder(mw)
+	enc.SetIndent("", "  ")
+	if err := enc.Encode(tr.man); err != nil {
+		_ = f.Close()
+		return err
+	}
+
+	// events.jsonl
+	ew, err := zw.Create("events.jsonl")
+	if err != nil {
+		_ = f.Close()
+		return err
+	}
+	if _, err := ew.Write(tr.events.Bytes()); err != nil {
+		_ = f.Close()
+		return err
+	}
+
+	// screenshots
+	for i, png := range tr.screenshots {
+		sw, err := zw.Create(fmt.Sprintf("screenshots/%04d.png", i))
+		if err != nil {
+			_ = f.Close()
+			return err
+		}
+		if _, err := sw.Write(png); err != nil {
+			_ = f.Close()
+			return err
+		}
+	}
+
+	if err := zw.Close(); err != nil {
+		_ = f.Close()
+		return err
+	}
+	return f.Close()
+}
diff --git a/internal/trace/trace_test.go b/internal/trace/trace_test.go
new file mode 100644
index 0000000..0ef55d9
--- /dev/null
+++ b/internal/trace/trace_test.go
@@ -0,0 +1,199 @@
+package trace
+
+import (
+	"archive/zip"
+	"bufio"
+	"bytes"
+	"encoding/json"
+	"image"
+	"image/color"
+	"image/png"
+	"path/filepath"
+	"sync"
+	"testing"
+	"time"
+)
+
+// makeTinyPNG creates a small valid PNG in memory for testing.
+func makeTinyPNG(t *testing.T) []byte {
+	t.Helper()
+	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
+	for y := 0; y < 4; y++ {
+		for x := 0; x < 4; x++ {
+			img.Set(x, y, color.RGBA{200, 200, 200, 255})
+		}
+	}
+	var buf bytes.Buffer
+	if err := png.Encode(&buf, img); err != nil {
+		t.Fatal(err)
+	}
+	return buf.Bytes()
+}
+
+func TestTraceRoundTrip(t *testing.T) {
+	dir := t.TempDir()
+	path := filepath.Join(dir, "session.twee")
+
+	tr, err := New(path, Manifest{
+		Command: []string{"/bin/sh", "-c", "echo hello"},
+		Env:     map[string]string{"TERM": "xterm-256color"},
+		Cols:    10,
+		Rows:    3,
+		Pid:     12345,
+	})
+	if err != nil {
+		t.Fatal(err)
+	}
+
+	// Write some events.
+	tr.WriteOutput([]byte("hello\r\n"), time.Now())
+	tr.WriteInput("type", "", []byte("h"))
+	tr.WriteInput("key", "Enter", []byte("\r"))
+	tr.WriteResize(20, 5)
+	tr.WriteOutput([]byte("world"), time.Now())
+
+	// Add a screenshot.
+	tr.AddScreenshotPNG(makeTinyPNG(t))
+
+	if err := tr.Close(); err != nil {
+		t.Fatal(err)
+	}
+
+	// Open the zip and verify contents.
+	zr, err := zip.OpenReader(path)
+	if err != nil {
+		t.Fatal(err)
+	}
+	defer zr.Close()
+
+	// Check manifest.json
+	mf, err := zr.Open("manifest.json")
+	if err != nil {
+		t.Fatal("manifest.json not found:", err)
+	}
+	var man Manifest
+	if err := json.NewDecoder(mf).Decode(&man); err != nil {
+		t.Fatal("decode manifest:", err)
+	}
+	mf.Close()
+
+	if man.Version != 1 {
+		t.Errorf("version = %d, want 1", man.Version)
+	}
+	if len(man.Command) != 3 || man.Command[0] != "/bin/sh" {
+		t.Errorf("command = %v", man.Command)
+	}
+	if man.Pid != 12345 {
+		t.Errorf("pid = %d, want 12345", man.Pid)
+	}
+	if man.Cols != 10 || man.Rows != 3 {
+		t.Errorf("size = %dx%d, want 10x3", man.Cols, man.Rows)
+	}
+	if man.Host.OS == "" || man.Host.Arch == "" {
+		t.Errorf("host info empty: %+v", man.Host)
+	}
+	if man.StartedAt.IsZero() || man.StoppedAt.IsZero() {
+		t.Errorf("timestamps: started=%v stopped=%v", man.StartedAt, man.StoppedAt)
+	}
+	if !man.StoppedAt.After(man.StartedAt) && !man.StoppedAt.Equal(man.StartedAt) {
+		t.Errorf("stopped_at (%v) should be >= started_at (%v)", man.StoppedAt, man.StartedAt)
+	}
+	if len(man.Screenshots) != 1 {
+		t.Fatalf("screenshots = %v, want 1 entry", man.Screenshots)
+	}
+	if man.Screenshots[0] != "screenshots/0000.png" {
+		t.Errorf("screenshot[0] = %q", man.Screenshots[0])
+	}
+
+	// Check events.jsonl
+	ef, err := zr.Open("events.jsonl")
+	if err != nil {
+		t.Fatal("events.jsonl not found:", err)
+	}
+	sc := bufio.NewScanner(ef)
+	nEvents := 0
+	for sc.Scan() {
+		line := sc.Bytes()
+		if len(bytes.TrimSpace(line)) == 0 {
+			continue
+		}
+		var ev event
+		if err := json.Unmarshal(line, &ev); err != nil {
+			t.Fatalf("event line %d: %v\nraw: %s", nEvents, err, line)
+		}
+		nEvents++
+	}
+	ef.Close()
+	if nEvents != 5 { // 2 output + 2 input + 1 resize
+		t.Errorf("events count = %d, want 5", nEvents)
+	}
+
+	// Check screenshot is a valid PNG.
+	sf, err := zr.Open("screenshots/0000.png")
+	if err != nil {
+		t.Fatal("screenshot not found:", err)
+	}
+	if _, err := png.Decode(sf); err != nil {
+		t.Fatal("screenshot is not valid PNG:", err)
+	}
+	sf.Close()
+}
+
+func TestTraceIdempotentClose(t *testing.T) {
+	dir := t.TempDir()
+	path := filepath.Join(dir, "session.twee")
+
+	tr, err := New(path, Manifest{
+		Command: []string{"echo"},
+		Cols:    10,
+		Rows:    3,
+	})
+	if err != nil {
+		t.Fatal(err)
+	}
+
+	if err := tr.Close(); err != nil {
+		t.Fatal("first close:", err)
+	}
+	if err := tr.Close(); err != nil {
+		t.Fatal("second close should succeed:", err)
+	}
+}
+
+func TestTraceConcurrentWrites(t *testing.T) {
+	dir := t.TempDir()
+	path := filepath.Join(dir, "session.twee")
+
+	tr, err := New(path, Manifest{
+		Command: []string{"echo"},
+		Cols:    10,
+		Rows:    3,
+	})
+	if err != nil {
+		t.Fatal(err)
+	}
+
+	var wg sync.WaitGroup
+	for i := 0; i < 10; i++ {
+		wg.Add(1)
+		go func(n int) {
+			defer wg.Done()
+			for j := 0; j < 50; j++ {
+				tr.WriteOutput([]byte("data"), time.Now())
+				tr.WriteInput("type", "", []byte("x"))
+			}
+		}(i)
+	}
+	wg.Wait()
+
+	if err := tr.Close(); err != nil {
+		t.Fatal("close:", err)
+	}
+
+	// Verify the zip is well-formed.
+	zr, err := zip.OpenReader(path)
+	if err != nil {
+		t.Fatal(err)
+	}
+	zr.Close()
+}
diff --git a/tuitest/options.go b/tuitest/options.go
index ed18fce..4d3235c 100644
--- a/tuitest/options.go
+++ b/tuitest/options.go
@@ -20,6 +20,7 @@ type config struct {
 	stableQuietWindow time.Duration
 
 	recordPath string
+	tracePath  string
 }
 
 func newConfig() *config {
@@ -42,6 +43,7 @@ func (c *config) toEngine() engine.Config {
 		DefaultTimeout:    c.defaultTimeout,
 		StableQuietWindow: c.stableQuietWindow,
 		RecordPath:        c.recordPath,
+		TracePath:         c.tracePath,
 	}
 }
 
@@ -79,3 +81,9 @@ func DefaultTimeout(d time.Duration) Option {
 func Record(path string) Option {
 	return func(c *config) { c.recordPath = path }
 }
+
+// Trace enables trace recording to the given path. The trace is a .twee
+// zip bundle containing a manifest, JSONL event stream, and screenshots.
+func Trace(path string) Option {
+	return func(c *config) { c.tracePath = path }
+}
diff --git a/tuitest/term.go b/tuitest/term.go
index 9fbe62a..c3b110d 100644
--- a/tuitest/term.go
+++ b/tuitest/term.go
@@ -48,8 +48,13 @@ func Run(t testing.TB, command string, opts ...Option) *Term {
 	te := &Term{Term: eng, t: t}
 	t.Cleanup(func() {
 		_ = te.Close()
-		if t.Failed() && te.RecordPath() != "" {
-			t.Logf("tuitest recording: %s", te.RecordPath())
+		if t.Failed() {
+			if te.RecordPath() != "" {
+				t.Logf("tuitest recording: %s", te.RecordPath())
+			}
+			if te.TracePath() != "" {
+				t.Logf("tuitest trace: %s", te.TracePath())
+			}
 		}
 	})
 	return te

TURN 7
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee && cat .ai/candidates/claude.test"}
TOOL RESULT: bash
OUTPUT:
?   	github.com/paulsmith/research/twee/cmd/libghostty-smoke	[no test files]
ok  	github.com/paulsmith/research/twee/cmd/twee	(cached)
?   	github.com/paulsmith/research/twee/fixtures/menu	[no test files]
ok  	github.com/paulsmith/research/twee/internal/daemon	(cached)
?   	github.com/paulsmith/research/twee/internal/engine	[no test files]
?   	github.com/paulsmith/research/twee/internal/input	[no test files]
?   	github.com/paulsmith/research/twee/internal/ptyrunner	[no test files]
ok  	github.com/paulsmith/research/twee/internal/pump	(cached)
ok  	github.com/paulsmith/research/twee/internal/recording	(cached)
ok  	github.com/paulsmith/research/twee/internal/render	(cached)
ok  	github.com/paulsmith/research/twee/internal/rpc	(cached)
?   	github.com/paulsmith/research/twee/internal/snapshot	[no test files]
ok  	github.com/paulsmith/research/twee/internal/trace	(cached)
ok  	github.com/paulsmith/research/twee/internal/vt	(cached)
ok  	github.com/paulsmith/research/twee/tuitest	0.546s

TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee && cat .ai/candidates/codex.test"}
TOOL RESULT: bash
OUTPUT:
?   	github.com/paulsmith/research/twee/cmd/libghostty-smoke	[no test files]
ok  	github.com/paulsmith/research/twee/cmd/twee	4.002s
?   	github.com/paulsmith/research/twee/fixtures/menu	[no test files]
ok  	github.com/paulsmith/research/twee/internal/daemon	0.032s
?   	github.com/paulsmith/research/twee/internal/engine	[no test files]
?   	github.com/paulsmith/research/twee/internal/input	[no test files]
?   	github.com/paulsmith/research/twee/internal/ptyrunner	[no test files]
ok  	github.com/paulsmith/research/twee/internal/pump	0.010s
ok  	github.com/paulsmith/research/twee/internal/recording	0.012s
ok  	github.com/paulsmith/research/twee/internal/render	0.012s
ok  	github.com/paulsmith/research/twee/internal/rpc	0.019s
?   	github.com/paulsmith/research/twee/internal/snapshot	[no test files]
ok  	github.com/paulsmith/research/twee/internal/vt	0.013s
ok  	github.com/paulsmith/research/twee/tuitest	0.716s

TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee && cat .ai/candidates/gemini.test"}
TOOL RESULT: bash
OUTPUT:
?   	github.com/paulsmith/research/twee/cmd/libghostty-smoke	[no test files]
ok  	github.com/paulsmith/research/twee/cmd/twee	4.019s
?   	github.com/paulsmith/research/twee/fixtures/menu	[no test files]
ok  	github.com/paulsmith/research/twee/internal/daemon	0.032s
?   	github.com/paulsmith/research/twee/internal/engine	[no test files]
?   	github.com/paulsmith/research/twee/internal/input	[no test files]
?   	github.com/paulsmith/research/twee/internal/ptyrunner	[no test files]
ok  	github.com/paulsmith/research/twee/internal/pump	0.012s
ok  	github.com/paulsmith/research/twee/internal/recording	0.015s
ok  	github.com/paulsmith/research/twee/internal/render	0.014s
ok  	github.com/paulsmith/research/twee/internal/rpc	0.019s
?   	github.com/paulsmith/research/twee/internal/snapshot	[no test files]
ok  	github.com/paulsmith/research/twee/internal/vt	0.014s
ok  	github.com/paulsmith/research/twee/tuitest	0.691s

TURN 8
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee && git show impl/codex:internal/trace/trace.go 2>&1 | head -5"}
TOOL RESULT: bash
OUTPUT:
fatal: path 'internal/trace/trace.go' exists on disk, but not in 'impl/codex'

TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee && git show impl/gemini:internal/trace/trace.go 2>&1 | head -5"}
TOOL RESULT: bash
OUTPUT:
fatal: path 'internal/trace/trace.go' exists on disk, but not in 'impl/gemini'

TURN 9
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee && git stash 2>/dev/null; git checkout impl/claude 2>&1 && go test -v -race ./internal/trace/ 2>&1"}
TOOL RESULT: bash
OUTPUT:
fatal: 'impl/claude' is already used by worktree at '/Users/paul/projects/research/twee/.ai/worktrees/claude'

exit code: 128
TURN 10
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee/.ai/worktrees/claude && go test -v -race ./internal/trace/ 2>&1"}
TOOL RESULT: bash
OUTPUT:
=== RUN   TestTraceRoundTrip
--- PASS: TestTraceRoundTrip (0.00s)
=== RUN   TestTraceIdempotentClose
--- PASS: TestTraceIdempotentClose (0.00s)
=== RUN   TestTraceConcurrentWrites
--- PASS: TestTraceConcurrentWrites (0.01s)
PASS
ok  	github.com/paulsmith/research/twee/internal/trace	(cached)

TURN 11
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee/.ai/worktrees/claude && go vet ./... 2>&1"}
TOOL RESULT: bash
OUTPUT:
(no output)
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee/.ai/worktrees/claude && go build ./... 2>&1"}
TOOL RESULT: bash
OUTPUT:
(no output)
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee/.ai/worktrees/claude && go doc ./internal/trace 2>&1"}
TOOL RESULT: bash
OUTPUT:
package trace // import "github.com/paulsmith/research/twee/internal/trace"

Package trace writes a .twee trace bundle — a zip archive containing a manifest,
JSONL event stream, and PNG screenshots.

type HostInfo struct{ ... }
    func DefaultHostInfo() HostInfo
type Manifest struct{ ... }
type Trace struct{ ... }
    func New(path string, m Manifest) (*Trace, error)

TURN 12
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee/.ai/worktrees/claude && go doc ./internal/engine Term.EnableTrace 2>&1"}
TOOL RESULT: bash
OUTPUT:
package engine // import "github.com/paulsmith/research/twee/internal/engine"

func (t *Term) EnableTrace(path string) error
    EnableTrace starts a trace recording to path.


TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee/.ai/worktrees/claude && ./bin/twee help trace 2>&1 || go run ./cmd/twee help trace 2>&1"}
TOOL RESULT: bash
OUTPUT:
sh: line 1: ./bin/twee: No such file or directory
twee trace start [-out <path.twee>] [-name <name>]
twee trace stop [-name <name>]
Start/stop a trace recording on the running session.
The trace is a .twee zip bundle containing a manifest, events, and screenshots.

TURN 13
TOOL CALL: bash
INPUT:
{"command": "cd /Users/paul/projects/research/twee/.ai/worktrees/claude && git diff main --name-only"}
TOOL RESULT: bash
OUTPUT:
cmd/twee/cmd_trace.go
internal/daemon/handlers_trace.go
internal/engine/config.go
internal/engine/input.go
internal/engine/query.go
internal/engine/term.go
internal/ptyrunner/runner.go
internal/rpc/ops.go
internal/rpc/types.go
internal/trace/trace.go
internal/trace/trace_test.go
tuitest/options.go
tuitest/term.go

TURN 14
TOOL CALL: bash
INPUT:
{"command": "echo \"=== Spec: New files ===\"\necho \"internal/trace/trace.go\"\necho \"internal/trace/trace_test.go\"\necho \"internal/daemon/handlers_trace.go\"\necho \"cmd/twee/cmd_trace.go\"\necho \"\"\necho \"=== Spec: Modified files ===\"\necho \"internal/rpc/ops.go\"\necho \"internal/rpc/types.go\"\necho \"internal/engine/term.go\"\necho \"internal/ptyrunner/runner.go\"\necho \"tuitest/options.go\"\necho \"tuitest/term.go\"\necho \"internal/engine/config.go\"\necho \"\"\necho \"=== Spec says NOT mentioned but Claude modified ===\"\necho \"internal/engine/input.go\"\necho \"internal/engine/query.go\""}
TOOL RESULT: bash
OUTPUT:
=== Spec: New files ===
internal/trace/trace.go
internal/trace/trace_test.go
internal/daemon/handlers_trace.go
cmd/twee/cmd_trace.go

=== Spec: Modified files ===
internal/rpc/ops.go
internal/rpc/types.go
internal/engine/term.go
internal/ptyrunner/runner.go
tuitest/options.go
tuitest/term.go
internal/engine/config.go

=== Spec says NOT mentioned but Claude modified ===
internal/engine/input.go
internal/engine/query.go

TURN 15
TOOL CALL: grep_search
INPUT:
{"pattern": "input\\.go|query\\.go", "path": ".ai/decisions/spec.md"}
TOOL RESULT: grep_search
OUTPUT:
.ai/decisions/spec.md:149:- In `Type`, `Key`, `Paste`, `Resize` (in `input.go`): after recording to `rec`, also write to `tr` if non-nil (same lock-then-unlock-then-write pattern).
.ai/decisions/spec.md:225:4. **Handler registration**: Use the `init()` + `optionalRegistrations` pattern (same as `handlers_record.go`, `handlers_input.go`, etc.).
TURN 16
TOOL CALL: grep_search
INPUT:
{"path": ".ai/decisions/spec.md", "pattern": "Diagnostic"}
TOOL RESULT: grep_search
OUTPUT:
no matches for pattern "Diagnostic"
TURN 17
TOOL CALL: grep_search
INPUT:
{"path": ".ai/decisions/spec.md", "pattern": "query"}
TOOL RESULT: grep_search
OUTPUT:
no matches for pattern "query"
TURN 18
TOOL CALL: write
INPUT:
{"path": ".ai/decisions/critique.md", "content": "# Implementation Critique: Trace Recording (`.twee` zip bundle)\n\n## Executive Summary\n\n**Only Claude produced a working implementation.** Codex and Gemini branches are\nidentical to `main` — zero diff, zero new code. Their \"test\" outputs simply show\nthe baseline test suite passing against an unmodified codebase. They are\ndisqualified.\n\n---\n\n## Per-Implementation Assessment\n\n### Claude (impl/claude)\n\n**Verdict: Complete, spec-faithful implementation. Ranked 1st (by default and on merit).**\n\n#### Files touched (13 total)\n\n| File | Status | Spec-required? |\n|---|---|---|\n| `internal/trace/trace.go` | New | ✅ Yes |\n| `internal/trace/trace_test.go` | New | ✅ Yes |\n| `internal/daemon/handlers_trace.go` | New | ✅ Yes |\n| `cmd/twee/cmd_trace.go` | New | ✅ Yes |\n| `internal/rpc/ops.go` | Modified | ✅ Yes |\n| `internal/rpc/types.go` | Modified | ✅ Yes |\n| `internal/engine/term.go` | Modified | ✅ Yes |\n| `internal/engine/config.go` | Modified | ✅ Yes |\n| `internal/engine/input.go` | Modified | ✅ Yes (R7) |\n| `internal/ptyrunner/runner.go` | Modified | ✅ Yes (R6) |\n| `tuitest/options.go` | Modified | ✅ Yes (R11) |\n| `tuitest/term.go` | Modified | ✅ Yes (R11) |\n| `internal/engine/query.go` | Modified | ⚠️ Not explicitly required |\n\n#### Spec Fidelity (R1–R12)\n\n| Requirement | Status | Notes |\n|---|---|---|\n| R1: Zip bundle format | ✅ | manifest.json, events.jsonl, screenshots/NNNN.png |\n| R2: manifest.json schema | ✅ | All fields present, `version`, `command`, `env`, `cols`, `rows`, `pid`, `host`, `started_at`, `stopped_at`, `screenshots` |\n| R3: events.jsonl format | ✅ | Uses private `event` struct mirroring `recording.Event`; same fields (`t_ms`, `type`, `bytes_b64`, `kind`, `key`, `cols`, `rows`) |\n| R4: Screenshots | ✅ | Start/stop screenshots in daemon handlers; `AddScreenshotPNG` accepts pre-encoded PNG; zero-padded 4-digit naming |\n| R5: `Trace` type API | ✅ | `New`, `WriteOutput`, `WriteInput`, `WriteResize`, `AddScreenshotPNG`, `Close` — all present with correct signatures |\n| R6: `Pid()` method | ✅ | Exact implementation as specified |\n| R7: `engine.Term` integration | ✅ | `EnableTrace`/`DisableTrace`/`TracePath`/`TraceAddScreenshot`, fan-out via `updateOutputHookLocked`, input.go wiring |\n| R8: RPC constants + types | ✅ | `OpTraceStart`, `OpTraceStop`, `TraceStartArgs` |\n| R9: Daemon handlers | ✅ | `handleTraceStart`/`handleTraceStop` with `init()` + `optionalRegistrations`; screenshot capture at start/stop |\n| R10: CLI verb | ✅ | `twee trace start [-out] [-name]` / `twee trace stop [-name]`; follows `cmd_record.go` pattern |\n| R11: `tuitest` integration | ✅ | `Trace(path)` option, `toEngine()` wiring, failure logging |\n| R12: Unit tests | ✅ | `TestTraceRoundTrip`, `TestTraceIdempotentClose`, `TestTraceConcurrentWrites` — all 3 pass with `-race` |\n\n#### Strengths\n\n1. **Exact spec adherence.** Every requirement R1–R12 is implemented as specified.\n   No missing features.\n2. **Clean architecture.** The `trace` package is fully decoupled from `engine`\n   and `render` (no import cycle). PNG bytes are passed in via `AddScreenshotPNG`.\n3. **Fan-out hook (`updateOutputHookLocked`).** Correctly handles the case where\n   both `rec` and `tr` are active simultaneously — the pump only supports one\n   hook, so the function multiplexes.\n4. **Idempotent `Close`.** Second call returns cached error, as specified.\n5. **Goroutine safety.** Mutex on all write paths. Race detector passes.\n6. **Defensive PNG copy in `AddScreenshotPNG`.** Uses `append([]byte(nil), pngData...)`\n   to avoid aliasing the caller's buffer.\n7. **Test quality.** Round-trip test verifies manifest fields, event count, and\n   PNG validity. Concurrent test exercises race detector.\n8. **Minimal diff.** 677 additions, 5 deletions across 13 files. No unnecessary\n   refactoring.\n\n#### Weaknesses / Minor Deviations\n\n1. **`query.go` change not in spec.** Adding trace path to `Diagnostic()` output\n   (3 lines) is not explicitly required. However, this is consistent with the\n   existing `RecordPath` pattern and is defensible as keeping diagnostic parity.\n   Very minor scope creep.\n2. **`DefaultHostInfo()` is exported.** The spec says \"Host info is populated by\n   `trace.DefaultHostInfo()` inside `trace.New`\" — which is what happens. The\n   function being exported is fine for testability but slightly beyond minimal.\n3. **No `TraceStopArgs` type.** The spec mentions `TraceStopArgs` in R8 but the\n   implementation passes `nil` for stop args (the handler ignores the raw\n   message). This is a trivial omission with no functional impact since the\n   stop operation takes no arguments.\n\n#### Success Criteria Verification\n\n| Criterion | Result |\n|---|---|\n| 1. `go build ./...` | ✅ Pass |\n| 2. `go vet ./...` | ✅ Pass |\n| 3. `go test ./internal/trace/` with `-race` | ✅ 3/3 pass |\n| 4. `go test ./internal/engine/` | ✅ Pass (no test files) |\n| 5. `go test ./tuitest/` | ✅ Pass |\n| 6. Valid zip with manifest, events, screenshots | ✅ Verified by TestTraceRoundTrip |\n| 7. `go doc ./internal/trace` shows public API | ✅ Trace, Manifest, HostInfo, New |\n| 8. `go doc Term.EnableTrace` | ✅ Shows method |\n| 9. RPC ops registered | ✅ (builds without init-time panic) |\n| 10. `twee help trace` prints usage | ✅ Verified |\n\n---\n\n### Codex (impl/codex)\n\n**Verdict: DISQUALIFIED — No implementation.**\n\nThe `impl/codex` branch is identical to `main`. Zero diff. The test output file\nshows the baseline test suite passing, with notably **no `internal/trace` package\nlisted** (confirming the package was never created).\n\nNo new files. No modified files. No implementation of any spec requirement.\n\n---\n\n### Gemini (impl/gemini)\n\n**Verdict: DISQUALIFIED — No implementation.**\n\nThe `impl/gemini` branch is identical to `main`. Zero diff. Same situation as\nCodex — baseline tests passing against an unmodified codebase.\n\nNo new files. No modified files. No implementation of any spec requirement.\n\n---\n\n## Head-to-Head Comparison\n\n| Criterion | Claude | Codex | Gemini |\n|---|---|---|---|\n| Implementation exists | ✅ Yes (677 lines) | ❌ No (0 lines) | ❌ No (0 lines) |\n| New files created | 4/4 | 0/4 | 0/4 |\n| Existing files modified | 9/7 required (+2 extra) | 0/7 | 0/7 |\n| R1: Zip bundle format | ✅ | ❌ | ❌ |\n| R2: Manifest schema | ✅ | ❌ | ❌ |\n| R3: Events JSONL | ✅ | ❌ | ❌ |\n| R4: Screenshots | ✅ | ❌ | ❌ |\n| R5: Trace type API | ✅ | ❌ | ❌ |\n| R6: Pid() method | ✅ | ❌ | ❌ |\n| R7: Engine integration | ✅ | ❌ | ❌ |\n| R8: RPC layer | ✅ | ❌ | ❌ |\n| R9: Daemon handlers | ✅ | ❌ | ❌ |\n| R10: CLI verb | ✅ | ❌ | ❌ |\n| R11: tuitest integration | ✅ | ❌ | ❌ |\n| R12: Unit tests | ✅ 3/3 pass | ❌ | ❌ |\n| Tests pass | ✅ All pass | ✅ (baseline only) | ✅ (baseline only) |\n| `go build` / `go vet` | ✅ | ✅ (baseline) | ✅ (baseline) |\n| Scope creep | Minor (query.go) | N/A | N/A |\n| Spec deviations | Missing `TraceStopArgs` type (trivial) | N/A | N/A |\n\n---\n\n## Final Ranking\n\n| Rank | Candidate | Justification |\n|---|---|---|\n| **1st** | **Claude** | Complete, faithful implementation of all 12 spec requirements. All tests pass including `-race`. Clean, minimal diff. Two extremely minor deviations (3-line query.go addition, missing no-op `TraceStopArgs` type). |\n| **DQ** | **Codex** | No implementation. Branch identical to main. |\n| **DQ** | **Gemini** | No implementation. Branch identical to main. |\n\n### Recommendation\n\n**Select Claude's implementation.** It is the only candidate that produced working\ncode. The implementation is high-quality, spec-faithful, and passes all success\ncriteria. The two minor deviations are inconsequential.\n"}
TOOL RESULT: write
OUTPUT:
wrote 7728 bytes to .ai/decisions/critique.md
TURN 19
TEXT:
The critique has been written to `.ai/decisions/critique.md`. Here's the summary:

## Key Findings

**Only Claude produced a working implementation.** Codex and Gemini are disqualified — their branches are identical to `main` with zero diff (no code was ever written).

### Claude's Implementation
- **Complete**: All 12 spec requirements (R1–R12) implemented
- **13 files touched**: 4 new, 9 modified (spec required 4 new + 7 modified; 2 extra modifications for `input.go` which was implied by R7, and `query.go` which is 3 lines of minor scope creep)
- **All tests pass** including with `-race` flag: `TestTraceRoundTrip`, `TestTraceIdempotentClose`, `TestTraceConcurrentWrites`
- **All success criteria verified**: `go build`, `go vet`, `go doc`, `twee help trace` all pass
- **Two trivial deviations**: 
  1. `query.go` Diagnostic() addition (not in spec, but follows existing pattern — 3 lines)
  2. Missing `TraceStopArgs` type (spec mentions it but stop takes no args, so `nil` is used)
- **Architecture is clean**: No import cycles, proper mutex protection, idempotent Close, defensive buffer copies

### Ranking
1. **Claude** — Winner (complete, faithful implementation)
2. **Codex** — Disqualified (no implementation)
3. **Gemini** — Disqualified (no implementation)

Session 83ff71ca completed in 2m1s
Turns: 19 | Tool calls: 34 (bash: 23, grep_search: 3, read: 7, write: 1)
Tokens: 9670 (in: 3741, out: 5929) | Cost: $0.36
Longest turn: 51s
