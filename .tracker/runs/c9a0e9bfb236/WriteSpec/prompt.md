# Context Summary (fidelity: summary:high)

## node.Start.episode_summary
1. bash args={"command":"pwd && ls -la && git status 2>/dev/null || echo \"Not a git repo\""} outcome=success summary=/Users/paul/projects/research/twee total 76 drwxr-xr-x 26 paul staff 832 May 1 10:15 . drwxr-xr-x 8 paul staff 256 Apr 29 06:20 .. drwxr-xr-x 6 paul staff 192 M…
2. bash args={"command":"git log --oneline -5 2>/dev/null || echo \"No git history\""} outcome=success summary=a121c5a Don't track .envrc d0aea6b Add demo video link to README 65f10c0 Initial commit
3. bash args={"command":"cat README.md"} outcome=success summary=# twee — drive TUIs from the shell `twee` is a command-line tool for spawning a terminal UI under a PTY and driving it from outside: type, press keys, query the…
4. bash args={"command":"cat AGENTS.md && echo \"---\" && cat CLAUDE.md"} outcome=success summary=Use `go doc` and `gopls` liberally when writing, reading, understanding, and debugging Go code. ## Pushing a new bookmark with jj `jj git push --remote <r> -b <…
5. bash args={"command":"ls internal/ && cat go.mod"} outcome=success summary=daemon engine input ptyrunner pump recording render rpc snapshot vt module github.com/paulsmith/research/twee go 1.26.0 require ( github.com/creack/pty v1.1.24 …
6. bash args={"command":"cat design.md | head -100"} outcome=success summary=# Design: Go TUI Test Harness Using `libghostty-vt` ## Status Draft for implementation. ## Context `libghostty-vt` is the virtual terminal core extracted from G…
7. bash args={"command":"ls internal/daemon/ internal/engine/ internal/rpc/ 2>/dev/null"} outcome=success summary=internal/daemon/: dispatch.go handlers_diff.go handlers_input.go handlers_lifecycle.go handlers_query.go handlers_record.go handlers_screenshot.go handlers_wait…
8. bash args={"command":"ls tuitest/ && ls cmd/"} outcome=success summary=diagnostic_test.go expect.go input.go menu_e2e_test.go options.go runner_smoke_test.go snapshot.go snapshot_assert.go term.go testdata wait.go wait_test.go libg…
9. bash args={"command":"cat plan.md | hea…

## node.SetupWorkspace.outcome
success

## node.Start.last_cost
0.0338

## node.Explore.response.Explore
Now I have a complete and thorough picture of the entire codebase. Here is the structured exploration summary:

---

## Exploration Summary

### Relevant Files

| Path | Role |
|---|---|
| `internal/recording/recording.go` | **Core target.** `Recorder` + `Header` + `Event` types; writes JSONL (version 1). This is what we're replacing/extending with the zip trace format. |
| `internal/recording/replay.go` | Replay: reads JSONL, feeds output/resize events into a `vt.Model`. Will need updating to read from zip. |
| `internal/recording/recording_test.go` | Only test for recording; round-trips JSONL via `ReplayInto`. |
| `internal/engine/term.go` | `Term` owns the `*recording.Recorder`; calls `EnableRecording`/`DisableRecording`; hooks output via `pump.SetOutputHook`; writes input events inline. Central wiring point. |
| `internal/engine/input.go` | `Type`, `Key`, `Paste`, `Resize` — each appends to the recorder via `rec.WriteInput` / `rec.WriteResize`. |
| `internal/engine/config.go` | `Config.RecordPath string`; `Config.Env map[string]string`; `BuildEnv()`. Source of env/cmd metadata. |
| `internal/pump/pump.go` | `SetOutputHook(fn func([]byte, time.Time))` — the tap point for raw PTY output bytes. |
| `internal/ptyrunner/runner.go` | Owns `exec.Cmd`; `cmd.Process.Pid` accessible; `ExitCode()`, `ExitedCh()`. Source of PID. |
| `internal/daemon/handlers_record.go` | RPC handlers for `record_start`/`record_stop`; creates the JSONL path, calls `t.EnableRecording`. **Needs new zip-aware args.** |
| `cmd/twee/cmd_record.go` | CLI `twee record start/stop`; `-out` flag for path. Passes `RecordStartArgs` to daemon. |
| `internal/rpc/types.go` | `RecordStartArgs{Out string}` — needs new fields (or replacement) for zip output path. |
| `internal/rpc/ops.go` | Op name constants `OpRecordStart`, `OpRecordStop`. |
| `tuitest/term.go` | `Run()` auto-enables recording to `t.TempDir()/session.jsonl` unless `TUITEST_RECORD=0`. Logs path on test failure. |
| `tuitest/options.go` | `Record(path string)` option; `config.recordPath`. Will need a `RecordTrace(path string)` option for zip format. |
| `internal/render/render.go` | `Render(snap, opts) (*image.RGBA, error)` + `EncodePNG` — used by screenshot handler; we'll call this to write per-event screenshots into the zip. |
| `internal/engine/query.go` | `Snapshot()`, `VisibleText()`, `Lines()` — for capturing VT state snapshots into the trace. |
| `internal/engine/types.go` | `Snapshot`, `Cell`, `Line`, `Color` types — JSON-serializable for the zip manifest. |
| `cmd/twee/daemonize.go` | Daemon startup: has `os.Getpid()`, inherits `cmd`, `cols`, `rows`, `envOverrides`. Source of session metadata. |

---

### Architecture Constraints

1. **JSONL is the current wire format for recordings.** The `Recorder` streams events line-by-line to a single `.jsonl` file. The zip format will be a new parallel path — we should keep JSONL for the `tuitest` auto-record (lightweight replay/diagnostics) and introduce the zip trace as the "rich" format behind a separate flag/option.

2. **`Recorder` is instantiated at `engine.Start` time or via `EnableRecording`.** All recording state lives on `*engine.Term` under `cfgMu`. The zip recorder must satisfy the same interface contracts: constructor writes metadata immediately, methods are called from multiple goroutines, `Close` finalizes.

3. **Output hook (`pump.SetOutputHook`) is the only tap point for raw bytes.** It is called *outside* the pump's mutex (deliberate — "so a slow recorder cannot stall snapshots or waiters"). The zip recorder's output writer must be fast enough or buffer aggressively; writing to an in-memory `bytes.Buffer` then flushing to the zip at `Close` is the safe pattern.

4. **`engine.Term` uses `cfgMu` to protect `t.rec` and `t.cfg.RecordPath`.** Any new trace recorder field must be similarly guarded.

5. **`EnableRecording` / `DisableRecording` are the public engine API.** The daemon calls them; the tuitest harness sets `RecordPath` in `Config` at start. The zip format needs parallel `EnableTrace` / `DisableTrace` or a unified `EnableRecording` that accepts a mode flag — the cleanest approach is a **separate recorder type** that implements the same internal write calls.

6. **RPC types are a closed set in `rpc/types.go`.** New fields go in `RecordStartArgs`; we should add an `OutZip string` or `Format string` field. The daemon handler is in `handlers_record.go`.

7. **All ops return one JSON object and exit.** The zip path returned from `record stop` will be the `.tweerecord` (or `.zip`) file path, matching the existing `{"path": ...}` shape.

8. **`archive/zip` is stdlib** — no new dependencies needed.

9. **CGO is required** (libghostty-vt); rendering screenshots for the trace will pull in `internal/render`, which requires the CGO VT model. This is fine since we're always inside an engine context.

10. **`runtime.GOOS` / `runtime.GOARCH`** are available for host metadata. `os.Hostname()` is available. PID of the child is `runner.cmd.Process.Pid` — not yet publicly exposed via a method; we need to add a `Pid()` accessor to `Runner` (or pass it through `engine.Term`).

---

### Test Conventions

- **Unit tests**: `package foo` (same package). Files: `*_test.go` in the package dir. Examples: `internal/recording/recording_test.go`, `internal/pump/pump_test.go`, `internal/vt/term_test.go`.
- **Integration/server tests**: `internal/daemon/server_test.go` — spawns a real `/bin/sh`, starts a real Unix socket server, dials it with `dialAndCall`.
- **CLI e2e tests**: `cmd/twee/cli_e2e_test.go` — builds the binary with `buildBinary(t)`, runs it as a subprocess, parses JSON output. Uses `TWEE_STATE_DIR=t.TempDir()` for isolation.
- **Smoke tests**: `tuitest/runner_smoke_test.go` — uses the public `tuitest` API.
- **Build tag for flakes**: `//go:build flake` gates expensive loop tests.
- **Test helpers**: `mustOK(t, bin, env, args...)` checks `{"ok": true}`. `runCLI` returns `map[string]any`.
- **Fixtures**: `fixtures/menu/main.go` compiled to `bin/menu` by `make build`.
- **Build**: `make test` → `PKG_CONFIG_PATH=... go test ./...`. New tests follow the same pattern.

---

### Risks

1. **ZIP write-at-close vs streaming**: `archive/zip` requires writing the central directory at the end. If the process crashes mid-session the zip is corrupt. Mitigation: buffer each artifact in memory and flush to zip only at `Close`/stop. For very long sessions (many MB of output), provide a configurable max-size guard or segment.

2. **Screenshot rendering at every input event is expensive.** The Playwright trace model takes screenshots at each action. We need to make this opt-in or rate-limited. Safest: capture a screenshot only at `record stop` (or at each explicit `RecordFrame` call), not on every keystroke.

3. **PID exposure.** `ptyrunner.Runner` doesn't currently expose `Pid()`. We need to add `func (r *Runner) Pid() int` — trivial but a file change. Similarly `engine.Term` needs to forward it.

4. **Zip path collision with existing JSONL path.** `handlers_record.go` currently auto-generates a `.jsonl` path if none given. We need to not accidentally mix the two formats. Cleanest: separate `OpTraceStart` / `OpTraceStop` ops (new constants) or add a `Format string` field to `RecordStartArgs`.

5. **`internal/render` import in `internal/recording`**: currently `recording` doesn't import `render`. Adding trace screenshots creates a dependency. This is fine architecturally (both are internal) but the zip trace writer must live in a new subpackage (e.g. `internal/trace`) rather than polluting the lean JSONL `recording` package.

6. **Backward compatibility of `ReplayInto`**: existing JSONL replay must continue to work for `tuitest` auto-recordings. The new zip format adds a separate replay path; both coexist.

---

### Build System

```
make build          # builds libghostty-vt via cmake, then go build ./...
make test           # go test ./... with correct PKG_CONFIG_PATH + DYLD_LIBRARY_PATH
make twee           # just the CLI binary → bin/twee
```

New packages follow the same pattern — no Makefile changes needed for pure-Go packages. The `internal/trace` package (new) will be pure Go using `archive/zip`, `encoding/json`, `image/png`, and `internal/render` (CGO-backed), so it builds only when the libghostty stamp exists — same as today.

---

### Design Decision for the Implementation

Given all of the above, here is the clean plan before writing a single line of code:

**New package: `internal/trace`**  
`Trace` type (parallel to `Recorder`) that accumulates artifacts in memory and writes a `.tweerecord` zip at `Close`. The zip contains:

```
manifest.json          # session metadata (cmd, args, env, host, pid, cols, rows, started_at)
events.jsonl           # same event stream as today (output, input, resize, exit) with timestamps  
screenshots/
  0000.png             # screenshot at trace-start (initial screen)
  <n>.png              # one per explicit RecordFrame call (or at stop)
```

**`engine.Term` gains**: `EnableTrace(path string) error` / `DisableTrace() error` / `TraceRecordFrame()` — parallel to `EnableRecording`/`DisableRecording`.

**New RPC ops**: `OpTraceStart` / `OpTraceStop` (or reuse `OpRecordStart`/`OpRecordStop` with a `format` discriminator). New CLI: `twee trace start [-out foo.tweerecord]` / `twee trace stop`.

**`tuitest`**: new `Trace(path string)` option; `RecordFrame()` method on `*Term`.

## node.Explore.last_turns
22

## node.Start.last_turns
6

## last_cost
0.3025

## response.Explore
Now I have a complete and thorough picture of the entire codebase. Here is the structured exploration summary:

---

## Exploration Summary

### Relevant Files

| Path | Role |
|---|---|
| `internal/recording/recording.go` | **Core target.** `Recorder` + `Header` + `Event` types; writes JSONL (version 1). This is what we're replacing/extending with the zip trace format. |
| `internal/recording/replay.go` | Replay: reads JSONL, feeds output/resize events into a `vt.Model`. Will need updating to read from zip. |
| `internal/recording/recording_test.go` | Only test for recording; round-trips JSONL via `ReplayInto`. |
| `internal/engine/term.go` | `Term` owns the `*recording.Recorder`; calls `EnableRecording`/`DisableRecording`; hooks output via `pump.SetOutputHook`; writes input events inline. Central wiring point. |
| `internal/engine/input.go` | `Type`, `Key`, `Paste`, `Resize` — each appends to the recorder via `rec.WriteInput` / `rec.WriteResize`. |
| `internal/engine/config.go` | `Config.RecordPath string`; `Config.Env map[string]string`; `BuildEnv()`. Source of env/cmd metadata. |
| `internal/pump/pump.go` | `SetOutputHook(fn func([]byte, time.Time))` — the tap point for raw PTY output bytes. |
| `internal/ptyrunner/runner.go` | Owns `exec.Cmd`; `cmd.Process.Pid` accessible; `ExitCode()`, `ExitedCh()`. Source of PID. |
| `internal/daemon/handlers_record.go` | RPC handlers for `record_start`/`record_stop`; creates the JSONL path, calls `t.EnableRecording`. **Needs new zip-aware args.** |
| `cmd/twee/cmd_record.go` | CLI `twee record start/stop`; `-out` flag for path. Passes `RecordStartArgs` to daemon. |
| `internal/rpc/types.go` | `RecordStartArgs{Out string}` — needs new fields (or replacement) for zip output path. |
| `internal/rpc/ops.go` | Op name constants `OpRecordStart`, `OpRecordStop`. |
| `tuitest/term.go` | `Run()` auto-enables recording to `t.TempDir()/session.jsonl` unless `TUITEST_RECORD=0`. Logs path on test failure. |
| `tuitest/options.go` | `Record(path string)` option; `config.recordPath`. Will need a `RecordTrace(path string)` option for zip format. |
| `internal/render/render.go` | `Render(snap, opts) (*image.RGBA, error)` + `EncodePNG` — used by screenshot handler; we'll call this to write per-event screenshots into the zip. |
| `internal/engine/query.go` | `Snapshot()`, `VisibleText()`, `Lines()` — for capturing VT state snapshots into the trace. |
| `internal/engine/types.go` | `Snapshot`, `Cell`, `Line`, `Color` types — JSON-serializable for the zip manifest. |
| `cmd/twee/daemonize.go` | Daemon startup: has `os.Getpid()`, inherits `cmd`, `cols`, `rows`, `envOverrides`. Source of session metadata. |

---

### Architecture Constraints

1. **JSONL is the current wire format for recordings.** The `Recorder` streams events line-by-line to a single `.jsonl` file. The zip format will be a new parallel path — we should keep JSONL for the `tuitest` auto-record (lightweight replay/diagnostics) and introduce the zip trace as the "rich" format behind a separate flag/option.

2. **`Recorder` is instantiated at `engine.Start` time or via `EnableRecording`.** All recording state lives on `*engine.Term` under `cfgMu`. The zip recorder must satisfy the same interface contracts: constructor writes metadata immediately, methods are called from multiple goroutines, `Close` finalizes.

3. **Output hook (`pump.SetOutputHook`) is the only tap point for raw bytes.** It is called *outside* the pump's mutex (deliberate — "so a slow recorder cannot stall snapshots or waiters"). The zip recorder's output writer must be fast enough or buffer aggressively; writing to an in-memory `bytes.Buffer` then flushing to the zip at `Close` is the safe pattern.

4. **`engine.Term` uses `cfgMu` to protect `t.rec` and `t.cfg.RecordPath`.** Any new trace recorder field must be similarly guarded.

5. **`EnableRecording` / `DisableRecording` are the public engine API.** The daemon calls them; the tuitest harness sets `RecordPath` in `Config` at start. The zip format needs parallel `EnableTrace` / `DisableTrace` or a unified `EnableRecording` that accepts a mode flag — the cleanest approach is a **separate recorder type** that implements the same internal write calls.

6. **RPC types are a closed set in `rpc/types.go`.** New fields go in `RecordStartArgs`; we should add an `OutZip string` or `Format string` field. The daemon handler is in `handlers_record.go`.

7. **All ops return one JSON object and exit.** The zip path returned from `record stop` will be the `.tweerecord` (or `.zip`) file path, matching the existing `{"path": ...}` shape.

8. **`archive/zip` is stdlib** — no new dependencies needed.

9. **CGO is required** (libghostty-vt); rendering screenshots for the trace will pull in `internal/render`, which requires the CGO VT model. This is fine since we're always inside an engine context.

10. **`runtime.GOOS` / `runtime.GOARCH`** are available for host metadata. `os.Hostname()` is available. PID of the child is `runner.cmd.Process.Pid` — not yet publicly exposed via a method; we need to add a `Pid()` accessor to `Runner` (or pass it through `engine.Term`).

---

### Test Conventions

- **Unit tests**: `package foo` (same package). Files: `*_test.go` in the package dir. Examples: `internal/recording/recording_test.go`, `internal/pump/pump_test.go`, `internal/vt/term_test.go`.
- **Integration/server tests**: `internal/daemon/server_test.go` — spawns a real `/bin/sh`, starts a real Unix socket server, dials it with `dialAndCall`.
- **CLI e2e tests**: `cmd/twee/cli_e2e_test.go` — builds the binary with `buildBinary(t)`, runs it as a subprocess, parses JSON output. Uses `TWEE_STATE_DIR=t.TempDir()` for isolation.
- **Smoke tests**: `tuitest/runner_smoke_test.go` — uses the public `tuitest` API.
- **Build tag for flakes**: `//go:build flake` gates expensive loop tests.
- **Test helpers**: `mustOK(t, bin, env, args...)` checks `{"ok": true}`. `runCLI` returns `map[string]any`.
- **Fixtures**: `fixtures/menu/main.go` compiled to `bin/menu` by `make build`.
- **Build**: `make test` → `PKG_CONFIG_PATH=... go test ./...`. New tests follow the same pattern.

---

### Risks

1. **ZIP write-at-close vs streaming**: `archive/zip` requires writing the central directory at the end. If the process crashes mid-session the zip is corrupt. Mitigation: buffer each artifact in memory and flush to zip only at `Close`/stop. For very long sessions (many MB of output), provide a configurable max-size guard or segment.

2. **Screenshot rendering at every input event is expensive.** The Playwright trace model takes screenshots at each action. We need to make this opt-in or rate-limited. Safest: capture a screenshot only at `record stop` (or at each explicit `RecordFrame` call), not on every keystroke.

3. **PID exposure.** `ptyrunner.Runner` doesn't currently expose `Pid()`. We need to add `func (r *Runner) Pid() int` — trivial but a file change. Similarly `engine.Term` needs to forward it.

4. **Zip path collision with existing JSONL path.** `handlers_record.go` currently auto-generates a `.jsonl` path if none given. We need to not accidentally mix the two formats. Cleanest: separate `OpTraceStart` / `OpTraceStop` ops (new constants) or add a `Format string` field to `RecordStartArgs`.

5. **`internal/render` import in `internal/recording`**: currently `recording` doesn't import `render`. Adding trace screenshots creates a dependency. This is fine architecturally (both are internal) but the zip trace writer must live in a new subpackage (e.g. `internal/trace`) rather than polluting the lean JSONL `recording` package.

6. **Backward compatibility of `ReplayInto`**: existing JSONL replay must continue to work for `tuitest` auto-recordings. The new zip format adds a separate replay path; both coexist.

---

### Build System

```
make build          # builds libghostty-vt via cmake, then go build ./...
make test           # go test ./... with correct PKG_CONFIG_PATH + DYLD_LIBRARY_PATH
make twee           # just the CLI binary → bin/twee
```

New packages follow the same pattern — no Makefile changes needed for pure-Go packages. The `internal/trace` package (new) will be pure Go using `archive/zip`, `encoding/json`, `image/png`, and `internal/render` (CGO-backed), so it builds only when the libghostty stamp exists — same as today.

---

### Design Decision for the Implementation

Given all of the above, here is the clean plan before writing a single line of code:

**New package: `internal/trace`**  
`Trace` type (parallel to `Recorder`) that accumulates artifacts in memory and writes a `.tweerecord` zip at `Close`. The zip contains:

```
manifest.json          # session metadata (cmd, args, env, host, pid, cols, rows, started_at)
events.jsonl           # same event stream as today (output, input, resize, exit) with timestamps  
screenshots/
  0000.png             # screenshot at trace-start (initial screen)
  <n>.png              # one per explicit RecordFrame call (or at stop)
```

**`engine.Term` gains**: `EnableTrace(path string) error` / `DisableTrace() error` / `TraceRecordFrame()` — parallel to `EnableRecording`/`DisableRecording`.

**New RPC ops**: `OpTraceStart` / `OpTraceStop` (or reuse `OpRecordStart`/`OpRecordStop` with a `format` discriminator). New CLI: `twee trace start [-out foo.tweerecord]` / `twee trace stop`.

**`tuitest`**: new `Trace(path string)` option; `RecordFrame()` method on `*Term`.

## episode_summaries
["1. bash args={\"command\":\"pwd \u0026\u0026 ls -la \u0026\u0026 git status 2\u003e/dev/null || echo \\\"Not a git repo\\\"\"} outcome=success summary=/Users/paul/projects/research/twee total 76 drwxr-xr-x 26 paul staff 832 May 1 10:15 . drwxr-xr-x 8 paul staff 256 Apr 29 06:20 .. drwxr-xr-x 6 paul staff 192 M…\n2. bash args={\"command\":\"git log --oneline -5 2\u003e/dev/null || echo \\\"No git history\\\"\"} outcome=success summary=a121c5a Don't track .envrc d0aea6b Add demo video link to README 65f10c0 Initial commit\n3. bash args={\"command\":\"cat README.md\"} outcome=success summary=# twee — drive TUIs from the shell `twee` is a command-line tool for spawning a terminal UI under a PTY and driving it from outside: type, press keys, query the…\n4. bash args={\"command\":\"cat AGENTS.md \u0026\u0026 echo \\\"---\\\" \u0026\u0026 cat CLAUDE.md\"} outcome=success summary=Use `go doc` and `gopls` liberally when writing, reading, understanding, and debugging Go code. ## Pushing a new bookmark with jj `jj git push --remote \u003cr\u003e -b \u003c…\n5. bash args={\"command\":\"ls internal/ \u0026\u0026 cat go.mod\"} outcome=success summary=daemon engine input ptyrunner pump recording render rpc snapshot vt module github.com/paulsmith/research/twee go 1.26.0 require ( github.com/creack/pty v1.1.24 …\n6. bash args={\"command\":\"cat design.md | head -100\"} outcome=success summary=# Design: Go TUI Test Harness Using `libghostty-vt` ## Status Draft for implementation. ## Context `libghostty-vt` is the virtual terminal core extracted from G…\n7. bash args={\"command\":\"ls internal/daemon/ internal/engine/ internal/rpc/ 2\u003e/dev/null\"} outcome=success summary=internal/daemon/: dispatch.go handlers_diff.go handlers_input.go handlers_lifecycle.go handlers_query.go handlers_record.go handlers_screenshot.go handlers_wait…\n8. bash args={\"command\":\"ls tuitest/ \u0026\u0026 ls cmd/\"} outcome=success summary=diagnostic_test.go expect.go input.go menu_e2e_test.go options.go runner_smoke_test.go snapshot.go snapshot_assert.go term.go testdata wait.go wait_test.go libg…\n9. bash args={\"command\":\"cat plan.md | hea…","1. bash args={\"command\":\"cat internal/vt/types.go \u0026\u0026 cat internal/vt/visible.go\"} outcome=success summary=// Package vt is the internal terminal model. It is hidden behind a narrow // interface so the backend (libghostty-vt) can evolve without touching the // public…\n2. bash args={\"command\":\"cat internal/render/render.go | head -60 \u0026\u0026 cat internal/daemon/handlers_query.go | head -80\"} outcome=success summary=package render import ( \"image\" \"image/color\" \"image/draw\" \"image/png\" \"io\" \"golang.org/x/image/font\" \"golang.org/x/image/math/fixed\" \"github.com/paulsmith/rese…\n3. bash args={\"command\":\"cat internal/engine/query.go \u0026\u0026 cat internal/engine/types.go\"} outcome=success summary=package engine import ( \"fmt\" \"github.com/paulsmith/research/twee/internal/vt\" ) // Snapshot returns the current terminal state. func (t *Term) Snapshot() Snaps…\n4. bash args={\"command\":\"cat go.mod \u0026\u0026 cat Makefile 2\u003e/dev/null || ls -la\"} outcome=success summary=module github.com/paulsmith/research/twee go 1.26.0 require ( github.com/creack/pty v1.1.24 github.com/mitchellh/go-libghostty v0.0.0-20260428141358-29fdb3130d7…\n5. bash args={\"command\":\"cat cmd/twee/cmd_run.go\"} outcome=success summary=package main import ( \"context\" \"encoding/json\" \"flag\" \"fmt\" \"io\" \"net\" \"os\" \"path/filepath\" \"github.com/paulsmith/research/twee/internal/daemon\" \"github.com/pa…\n6. bash args={\"command\":\"cat tuitest/runner_smoke_test.go \u0026\u0026 cat tuitest/term.go\"} outcome=success summary=package tuitest import ( \"context\" \"strings\" \"testing\" \"time\" ) func TestRunHelloWorld(t *testing.T) { term := Run(t, \"/bin/sh\", Args(\"-c\", \"printf 'hello\\\\r\\\\n…\n7. bash args={\"command\":\"cat tuitest/options.go \u0026\u0026 cat tuitest/snapshot.go\"} outcome=success summary=package tuitest import ( \"time\" \"github.com/paulsmith/research/twee/internal/engine\" ) // Option configures a Term. type Option func(*config) type config struct…\n8. bash args={\"command\":\"cat internal/daemon/server.go \u0026\u0026 cat internal/daemon/server_test.go\"} outcome=success summary=/…"]

## node.Explore.episode_summary
1. bash args={"command":"cat internal/vt/types.go && cat internal/vt/visible.go"} outcome=success summary=// Package vt is the internal terminal model. It is hidden behind a narrow // interface so the backend (libghostty-vt) can evolve without touching the // public…
2. bash args={"command":"cat internal/render/render.go | head -60 && cat internal/daemon/handlers_query.go | head -80"} outcome=success summary=package render import ( "image" "image/color" "image/draw" "image/png" "io" "golang.org/x/image/font" "golang.org/x/image/math/fixed" "github.com/paulsmith/rese…
3. bash args={"command":"cat internal/engine/query.go && cat internal/engine/types.go"} outcome=success summary=package engine import ( "fmt" "github.com/paulsmith/research/twee/internal/vt" ) // Snapshot returns the current terminal state. func (t *Term) Snapshot() Snaps…
4. bash args={"command":"cat go.mod && cat Makefile 2>/dev/null || ls -la"} outcome=success summary=module github.com/paulsmith/research/twee go 1.26.0 require ( github.com/creack/pty v1.1.24 github.com/mitchellh/go-libghostty v0.0.0-20260428141358-29fdb3130d7…
5. bash args={"command":"cat cmd/twee/cmd_run.go"} outcome=success summary=package main import ( "context" "encoding/json" "flag" "fmt" "io" "net" "os" "path/filepath" "github.com/paulsmith/research/twee/internal/daemon" "github.com/pa…
6. bash args={"command":"cat tuitest/runner_smoke_test.go && cat tuitest/term.go"} outcome=success summary=package tuitest import ( "context" "strings" "testing" "time" ) func TestRunHelloWorld(t *testing.T) { term := Run(t, "/bin/sh", Args("-c", "printf 'hello\\r\\n…
7. bash args={"command":"cat tuitest/options.go && cat tuitest/snapshot.go"} outcome=success summary=package tuitest import ( "time" "github.com/paulsmith/research/twee/internal/engine" ) // Option configures a Term. type Option func(*config) type config struct…
8. bash args={"command":"cat internal/daemon/server.go && cat internal/daemon/server_test.go"} outcome=success summary=/…

## response.AskUser
let's add a recording feature. It's something you invoke at any time during a session with a start and stop boundary. The precedent I have in mind is the playwright trace also Vibium, which is what I'm thinking about while I'm building twee, has a native recording format also based on playwright traces. In that case, it captures screenshots of a browser instance under test along with DOM state, network traffic, etc. So let's make a TUI-native recording file (.zip bundle of artifacts with a known manifest). Off the top of my head: path to executable, args, env vars, host details (os, arch), pid, any events (keystrokes)

## graph.llm_model
claude-sonnet-4-6

## node.AskUser.outcome
success

## human_response
let's add a recording feature. It's something you invoke at any time during a session with a start and stop boundary. The precedent I have in mind is the playwright trace also Vibium, which is what I'm thinking about while I'm building twee, has a native recording format also based on playwright traces. In that case, it captures screenshots of a browser instance under test along with DOM state, network traffic, etc. So let's make a TUI-native recording file (.zip bundle of artifacts with a known manifest). Off the top of my head: path to executable, args, env vars, host details (os, arch), pid, any events (keystrokes)

## last_response
Now I have a complete and thorough picture of the entire codebase. Here is the structured exploration summary:

---

## Exploration Summary

### Relevant Files

| Path | Role |
|---|---|
| `internal/recording/recording.go` | **Core target.** `Recorder` + `Header` + `Event` types; writes JSONL (version 1). This is what we're replacing/extending with the zip trace format. |
| `internal/recording/replay.go` | Replay: reads JSONL, feeds output/resize events into a `vt.Model`. Will need updating to read from zip. |
| `internal/recording/recording_test.go` | Only test for recording; round-trips JSONL via `ReplayInto`. |
| `internal/engine/term.go` | `Term` owns the `*recording.Recorder`; calls `EnableRecording`/`DisableRecording`; hooks output via `pump.SetOutputHook`; writes input events inline. Central wiring point. |
| `internal/engine/input.go` | `Type`, `Key`, `Paste`, `Resize` — each appends to the recorder via `rec.WriteInput` / `rec.WriteResize`. |
| `internal/engine/config.go` | `Config.RecordPath string`; `Config.Env map[string]string`; `BuildEnv()`. Source of env/cmd metadata. |
| `internal/pump/pump.go` | `SetOutputHook(fn func([]byte, time.Time))` — the tap point for raw PTY output bytes. |
| `internal/ptyrunner/runner.go` | Owns `exec.Cmd`; `cmd.Process.Pid` accessible; `ExitCode()`, `ExitedCh()`. Source of PID. |
| `internal/daemon/handlers_record.go` | RPC handlers for `record_start`/`record_stop`; creates the JSONL path, calls `t.EnableRecording`. **Needs new zip-aware args.** |
| `cmd/twee/cmd_record.go` | CLI `twee record start/stop`; `-out` flag for path. Passes `RecordStartArgs` to daemon. |
| `internal/rpc/types.go` | `RecordStartArgs{Out string}` — needs new fields (or replacement) for zip output path. |
| `internal/rpc/ops.go` | Op name constants `OpRecordStart`, `OpRecordStop`. |
| `tuitest/term.go` | `Run()` auto-enables recording to `t.TempDir()/session.jsonl` unless `TUITEST_RECORD=0`. Logs path on test failure. |
| `tuitest/options.go` | `Record(path string)` option; `config.recordPath`. Will need a `RecordTrace(path string)` option for zip format. |
| `internal/render/render.go` | `Render(snap, opts) (*image.RGBA, error)` + `EncodePNG` — used by screenshot handler; we'll call this to write per-event screenshots into the zip. |
| `internal/engine/query.go` | `Snapshot()`, `VisibleText()`, `Lines()` — for capturing VT state snapshots into the trace. |
| `internal/engine/types.go` | `Snapshot`, `Cell`, `Line`, `Color` types — JSON-serializable for the zip manifest. |
| `cmd/twee/daemonize.go` | Daemon startup: has `os.Getpid()`, inherits `cmd`, `cols`, `rows`, `envOverrides`. Source of session metadata. |

---

### Architecture Constraints

1. **JSONL is the current wire format for recordings.** The `Recorder` streams events line-by-line to a single `.jsonl` file. The zip format will be a new parallel path — we should keep JSONL for the `tuitest` auto-record (lightweight replay/diagnostics) and introduce the zip trace as the "rich" format behind a separate flag/option.

2. **`Recorder` is instantiated at `engine.Start` time or via `EnableRecording`.** All recording state lives on `*engine.Term` under `cfgMu`. The zip recorder must satisfy the same interface contracts: constructor writes metadata immediately, methods are called from multiple goroutines, `Close` finalizes.

3. **Output hook (`pump.SetOutputHook`) is the only tap point for raw bytes.** It is called *outside* the pump's mutex (deliberate — "so a slow recorder cannot stall snapshots or waiters"). The zip recorder's output writer must be fast enough or buffer aggressively; writing to an in-memory `bytes.Buffer` then flushing to the zip at `Close` is the safe pattern.

4. **`engine.Term` uses `cfgMu` to protect `t.rec` and `t.cfg.RecordPath`.** Any new trace recorder field must be similarly guarded.

5. **`EnableRecording` / `DisableRecording` are the public engine API.** The daemon calls them; the tuitest harness sets `RecordPath` in `Config` at start. The zip format needs parallel `EnableTrace` / `DisableTrace` or a unified `EnableRecording` that accepts a mode flag — the cleanest approach is a **separate recorder type** that implements the same internal write calls.

6. **RPC types are a closed set in `rpc/types.go`.** New fields go in `RecordStartArgs`; we should add an `OutZip string` or `Format string` field. The daemon handler is in `handlers_record.go`.

7. **All ops return one JSON object and exit.** The zip path returned from `record stop` will be the `.tweerecord` (or `.zip`) file path, matching the existing `{"path": ...}` shape.

8. **`archive/zip` is stdlib** — no new dependencies needed.

9. **CGO is required** (libghostty-vt); rendering screenshots for the trace will pull in `internal/render`, which requires the CGO VT model. This is fine since we're always inside an engine context.

10. **`runtime.GOOS` / `runtime.GOARCH`** are available for host metadata. `os.Hostname()` is available. PID of the child is `runner.cmd.Process.Pid` — not yet publicly exposed via a method; we need to add a `Pid()` accessor to `Runner` (or pass it through `engine.Term`).

---

### Test Conventions

- **Unit tests**: `package foo` (same package). Files: `*_test.go` in the package dir. Examples: `internal/recording/recording_test.go`, `internal/pump/pump_test.go`, `internal/vt/term_test.go`.
- **Integration/server tests**: `internal/daemon/server_test.go` — spawns a real `/bin/sh`, starts a real Unix socket server, dials it with `dialAndCall`.
- **CLI e2e tests**: `cmd/twee/cli_e2e_test.go` — builds the binary with `buildBinary(t)`, runs it as a subprocess, parses JSON output. Uses `TWEE_STATE_DIR=t.TempDir()` for isolation.
- **Smoke tests**: `tuitest/runner_smoke_test.go` — uses the public `tuitest` API.
- **Build tag for flakes**: `//go:build flake` gates expensive loop tests.
- **Test helpers**: `mustOK(t, bin, env, args...)` checks `{"ok": true}`. `runCLI` returns `map[string]any`.
- **Fixtures**: `fixtures/menu/main.go` compiled to `bin/menu` by `make build`.
- **Build**: `make test` → `PKG_CONFIG_PATH=... go test ./...`. New tests follow the same pattern.

---

### Risks

1. **ZIP write-at-close vs streaming**: `archive/zip` requires writing the central directory at the end. If the process crashes mid-session the zip is corrupt. Mitigation: buffer each artifact in memory and flush to zip only at `Close`/stop. For very long sessions (many MB of output), provide a configurable max-size guard or segment.

2. **Screenshot rendering at every input event is expensive.** The Playwright trace model takes screenshots at each action. We need to make this opt-in or rate-limited. Safest: capture a screenshot only at `record stop` (or at each explicit `RecordFrame` call), not on every keystroke.

3. **PID exposure.** `ptyrunner.Runner` doesn't currently expose `Pid()`. We need to add `func (r *Runner) Pid() int` — trivial but a file change. Similarly `engine.Term` needs to forward it.

4. **Zip path collision with existing JSONL path.** `handlers_record.go` currently auto-generates a `.jsonl` path if none given. We need to not accidentally mix the two formats. Cleanest: separate `OpTraceStart` / `OpTraceStop` ops (new constants) or add a `Format string` field to `RecordStartArgs`.

5. **`internal/render` import in `internal/recording`**: currently `recording` doesn't import `render`. Adding trace screenshots creates a dependency. This is fine architecturally (both are internal) but the zip trace writer must live in a new subpackage (e.g. `internal/trace`) rather than polluting the lean JSONL `recording` package.

6. **Backward compatibility of `ReplayInto`**: existing JSONL replay must continue to work for `tuitest` auto-recordings. The new zip format adds a separate replay path; both coexist.

---

### Build System

```
make build          # builds libghostty-vt via cmake, then go build ./...
make test           # go test ./... with correct PKG_CONFIG_PATH + DYLD_LIBRARY_PATH
make twee           # just the CLI binary → bin/twee
```

New packages follow the same pattern — no Makefile changes needed for pure-Go packages. The `internal/trace` package (new) will be pure Go using `archive/zip`, `encoding/json`, `image/png`, and `internal/render` (CGO-backed), so it builds only when the libghostty stamp exists — same as today.

---

### Design Decision for the Implementation

Given all of the above, here is the clean plan before writing a single line of code:

**New package: `internal/trace`**  
`Trace` type (parallel to `Recorder`) that accumulates artifacts in memory and writes a `.tweerecord` zip at `Close`. The zip contains:

```
manifest.json          # session metadata (cmd, args, env, host, pid, cols, rows, started_at)
events.jsonl           # same event stream as today (output, input, resize, exit) with timestamps  
screenshots/
  0000.png             # screenshot at trace-start (initial screen)
  <n>.png              # one per explicit RecordFrame call (or at stop)
```

**`engine.Term` gains**: `EnableTrace(path string) error` / `DisableTrace() error` / `TraceRecordFrame()` — parallel to `EnableRecording`/`DisableRecording`.

**New RPC ops**: `OpTraceStart` / `OpTraceStop` (or reuse `OpRecordStart`/`OpRecordStop` with a `format` discriminator). New CLI: `twee trace start [-out foo.tweerecord]` / `twee trace stop`.

**`tuitest`**: new `Trace(path string)` option; `RecordFrame()` method on `*Term`.

## node.Start.last_response
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

## node.SetupWorkspace.tool_stdout
workspace-ready

## node.Start.response.Start
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

## node.Explore.last_cost
0.3025

## graph.default_fidelity
summary:high

## graph.goal
Ask the user what to build, explore and spec it, fan out to 3 isolated implementations via git worktrees, cross-critique, select the best, apply it, and clean up everything else.

## node.AskUser.human_response
let's add a recording feature. It's something you invoke at any time during a session with a start and stop boundary. The precedent I have in mind is the playwright trace also Vibium, which is what I'm thinking about while I'm building twee, has a native recording format also based on playwright traces. In that case, it captures screenshots of a browser instance under test along with DOM state, network traffic, etc. So let's make a TUI-native recording file (.zip bundle of artifacts with a known manifest). Off the top of my head: path to executable, args, env vars, host details (os, arch), pid, any events (keystrokes)

## last_turns
22

## node.Explore.last_response
Now I have a complete and thorough picture of the entire codebase. Here is the structured exploration summary:

---

## Exploration Summary

### Relevant Files

| Path | Role |
|---|---|
| `internal/recording/recording.go` | **Core target.** `Recorder` + `Header` + `Event` types; writes JSONL (version 1). This is what we're replacing/extending with the zip trace format. |
| `internal/recording/replay.go` | Replay: reads JSONL, feeds output/resize events into a `vt.Model`. Will need updating to read from zip. |
| `internal/recording/recording_test.go` | Only test for recording; round-trips JSONL via `ReplayInto`. |
| `internal/engine/term.go` | `Term` owns the `*recording.Recorder`; calls `EnableRecording`/`DisableRecording`; hooks output via `pump.SetOutputHook`; writes input events inline. Central wiring point. |
| `internal/engine/input.go` | `Type`, `Key`, `Paste`, `Resize` — each appends to the recorder via `rec.WriteInput` / `rec.WriteResize`. |
| `internal/engine/config.go` | `Config.RecordPath string`; `Config.Env map[string]string`; `BuildEnv()`. Source of env/cmd metadata. |
| `internal/pump/pump.go` | `SetOutputHook(fn func([]byte, time.Time))` — the tap point for raw PTY output bytes. |
| `internal/ptyrunner/runner.go` | Owns `exec.Cmd`; `cmd.Process.Pid` accessible; `ExitCode()`, `ExitedCh()`. Source of PID. |
| `internal/daemon/handlers_record.go` | RPC handlers for `record_start`/`record_stop`; creates the JSONL path, calls `t.EnableRecording`. **Needs new zip-aware args.** |
| `cmd/twee/cmd_record.go` | CLI `twee record start/stop`; `-out` flag for path. Passes `RecordStartArgs` to daemon. |
| `internal/rpc/types.go` | `RecordStartArgs{Out string}` — needs new fields (or replacement) for zip output path. |
| `internal/rpc/ops.go` | Op name constants `OpRecordStart`, `OpRecordStop`. |
| `tuitest/term.go` | `Run()` auto-enables recording to `t.TempDir()/session.jsonl` unless `TUITEST_RECORD=0`. Logs path on test failure. |
| `tuitest/options.go` | `Record(path string)` option; `config.recordPath`. Will need a `RecordTrace(path string)` option for zip format. |
| `internal/render/render.go` | `Render(snap, opts) (*image.RGBA, error)` + `EncodePNG` — used by screenshot handler; we'll call this to write per-event screenshots into the zip. |
| `internal/engine/query.go` | `Snapshot()`, `VisibleText()`, `Lines()` — for capturing VT state snapshots into the trace. |
| `internal/engine/types.go` | `Snapshot`, `Cell`, `Line`, `Color` types — JSON-serializable for the zip manifest. |
| `cmd/twee/daemonize.go` | Daemon startup: has `os.Getpid()`, inherits `cmd`, `cols`, `rows`, `envOverrides`. Source of session metadata. |

---

### Architecture Constraints

1. **JSONL is the current wire format for recordings.** The `Recorder` streams events line-by-line to a single `.jsonl` file. The zip format will be a new parallel path — we should keep JSONL for the `tuitest` auto-record (lightweight replay/diagnostics) and introduce the zip trace as the "rich" format behind a separate flag/option.

2. **`Recorder` is instantiated at `engine.Start` time or via `EnableRecording`.** All recording state lives on `*engine.Term` under `cfgMu`. The zip recorder must satisfy the same interface contracts: constructor writes metadata immediately, methods are called from multiple goroutines, `Close` finalizes.

3. **Output hook (`pump.SetOutputHook`) is the only tap point for raw bytes.** It is called *outside* the pump's mutex (deliberate — "so a slow recorder cannot stall snapshots or waiters"). The zip recorder's output writer must be fast enough or buffer aggressively; writing to an in-memory `bytes.Buffer` then flushing to the zip at `Close` is the safe pattern.

4. **`engine.Term` uses `cfgMu` to protect `t.rec` and `t.cfg.RecordPath`.** Any new trace recorder field must be similarly guarded.

5. **`EnableRecording` / `DisableRecording` are the public engine API.** The daemon calls them; the tuitest harness sets `RecordPath` in `Config` at start. The zip format needs parallel `EnableTrace` / `DisableTrace` or a unified `EnableRecording` that accepts a mode flag — the cleanest approach is a **separate recorder type** that implements the same internal write calls.

6. **RPC types are a closed set in `rpc/types.go`.** New fields go in `RecordStartArgs`; we should add an `OutZip string` or `Format string` field. The daemon handler is in `handlers_record.go`.

7. **All ops return one JSON object and exit.** The zip path returned from `record stop` will be the `.tweerecord` (or `.zip`) file path, matching the existing `{"path": ...}` shape.

8. **`archive/zip` is stdlib** — no new dependencies needed.

9. **CGO is required** (libghostty-vt); rendering screenshots for the trace will pull in `internal/render`, which requires the CGO VT model. This is fine since we're always inside an engine context.

10. **`runtime.GOOS` / `runtime.GOARCH`** are available for host metadata. `os.Hostname()` is available. PID of the child is `runner.cmd.Process.Pid` — not yet publicly exposed via a method; we need to add a `Pid()` accessor to `Runner` (or pass it through `engine.Term`).

---

### Test Conventions

- **Unit tests**: `package foo` (same package). Files: `*_test.go` in the package dir. Examples: `internal/recording/recording_test.go`, `internal/pump/pump_test.go`, `internal/vt/term_test.go`.
- **Integration/server tests**: `internal/daemon/server_test.go` — spawns a real `/bin/sh`, starts a real Unix socket server, dials it with `dialAndCall`.
- **CLI e2e tests**: `cmd/twee/cli_e2e_test.go` — builds the binary with `buildBinary(t)`, runs it as a subprocess, parses JSON output. Uses `TWEE_STATE_DIR=t.TempDir()` for isolation.
- **Smoke tests**: `tuitest/runner_smoke_test.go` — uses the public `tuitest` API.
- **Build tag for flakes**: `//go:build flake` gates expensive loop tests.
- **Test helpers**: `mustOK(t, bin, env, args...)` checks `{"ok": true}`. `runCLI` returns `map[string]any`.
- **Fixtures**: `fixtures/menu/main.go` compiled to `bin/menu` by `make build`.
- **Build**: `make test` → `PKG_CONFIG_PATH=... go test ./...`. New tests follow the same pattern.

---

### Risks

1. **ZIP write-at-close vs streaming**: `archive/zip` requires writing the central directory at the end. If the process crashes mid-session the zip is corrupt. Mitigation: buffer each artifact in memory and flush to zip only at `Close`/stop. For very long sessions (many MB of output), provide a configurable max-size guard or segment.

2. **Screenshot rendering at every input event is expensive.** The Playwright trace model takes screenshots at each action. We need to make this opt-in or rate-limited. Safest: capture a screenshot only at `record stop` (or at each explicit `RecordFrame` call), not on every keystroke.

3. **PID exposure.** `ptyrunner.Runner` doesn't currently expose `Pid()`. We need to add `func (r *Runner) Pid() int` — trivial but a file change. Similarly `engine.Term` needs to forward it.

4. **Zip path collision with existing JSONL path.** `handlers_record.go` currently auto-generates a `.jsonl` path if none given. We need to not accidentally mix the two formats. Cleanest: separate `OpTraceStart` / `OpTraceStop` ops (new constants) or add a `Format string` field to `RecordStartArgs`.

5. **`internal/render` import in `internal/recording`**: currently `recording` doesn't import `render`. Adding trace screenshots creates a dependency. This is fine architecturally (both are internal) but the zip trace writer must live in a new subpackage (e.g. `internal/trace`) rather than polluting the lean JSONL `recording` package.

6. **Backward compatibility of `ReplayInto`**: existing JSONL replay must continue to work for `tuitest` auto-recordings. The new zip format adds a separate replay path; both coexist.

---

### Build System

```
make build          # builds libghostty-vt via cmake, then go build ./...
make test           # go test ./... with correct PKG_CONFIG_PATH + DYLD_LIBRARY_PATH
make twee           # just the CLI binary → bin/twee
```

New packages follow the same pattern — no Makefile changes needed for pure-Go packages. The `internal/trace` package (new) will be pure Go using `archive/zip`, `encoding/json`, `image/png`, and `internal/render` (CGO-backed), so it builds only when the libghostty stamp exists — same as today.

---

### Design Decision for the Implementation

Given all of the above, here is the clean plan before writing a single line of code:

**New package: `internal/trace`**  
`Trace` type (parallel to `Recorder`) that accumulates artifacts in memory and writes a `.tweerecord` zip at `Close`. The zip contains:

```
manifest.json          # session metadata (cmd, args, env, host, pid, cols, rows, started_at)
events.jsonl           # same event stream as today (output, input, resize, exit) with timestamps  
screenshots/
  0000.png             # screenshot at trace-start (initial screen)
  <n>.png              # one per explicit RecordFrame call (or at stop)
```

**`engine.Term` gains**: `EnableTrace(path string) error` / `DisableTrace() error` / `TraceRecordFrame()` — parallel to `EnableRecording`/`DisableRecording`.

**New RPC ops**: `OpTraceStart` / `OpTraceStop` (or reuse `OpRecordStart`/`OpRecordStop` with a `format` discriminator). New CLI: `twee trace start [-out foo.tweerecord]` / `twee trace stop`.

**`tuitest`**: new `Trace(path string)` option; `RecordFrame()` method on `*Term`.

## graph.llm_provider
anthropic

## node.Start.episode_summaries
["1. bash args={\"command\":\"pwd \u0026\u0026 ls -la \u0026\u0026 git status 2\u003e/dev/null || echo \\\"Not a git repo\\\"\"} outcome=success summary=/Users/paul/projects/research/twee total 76 drwxr-xr-x 26 paul staff 832 May 1 10:15 . drwxr-xr-x 8 paul staff 256 Apr 29 06:20 .. drwxr-xr-x 6 paul staff 192 M…\n2. bash args={\"command\":\"git log --oneline -5 2\u003e/dev/null || echo \\\"No git history\\\"\"} outcome=success summary=a121c5a Don't track .envrc d0aea6b Add demo video link to README 65f10c0 Initial commit\n3. bash args={\"command\":\"cat README.md\"} outcome=success summary=# twee — drive TUIs from the shell `twee` is a command-line tool for spawning a terminal UI under a PTY and driving it from outside: type, press keys, query the…\n4. bash args={\"command\":\"cat AGENTS.md \u0026\u0026 echo \\\"---\\\" \u0026\u0026 cat CLAUDE.md\"} outcome=success summary=Use `go doc` and `gopls` liberally when writing, reading, understanding, and debugging Go code. ## Pushing a new bookmark with jj `jj git push --remote \u003cr\u003e -b \u003c…\n5. bash args={\"command\":\"ls internal/ \u0026\u0026 cat go.mod\"} outcome=success summary=daemon engine input ptyrunner pump recording render rpc snapshot vt module github.com/paulsmith/research/twee go 1.26.0 require ( github.com/creack/pty v1.1.24 …\n6. bash args={\"command\":\"cat design.md | head -100\"} outcome=success summary=# Design: Go TUI Test Harness Using `libghostty-vt` ## Status Draft for implementation. ## Context `libghostty-vt` is the virtual terminal core extracted from G…\n7. bash args={\"command\":\"ls internal/daemon/ internal/engine/ internal/rpc/ 2\u003e/dev/null\"} outcome=success summary=internal/daemon/: dispatch.go handlers_diff.go handlers_input.go handlers_lifecycle.go handlers_query.go handlers_record.go handlers_screenshot.go handlers_wait…\n8. bash args={\"command\":\"ls tuitest/ \u0026\u0026 ls cmd/\"} outcome=success summary=diagnostic_test.go expect.go input.go menu_e2e_test.go options.go runner_smoke_test.go snapshot.go snapshot_assert.go term.go testdata wait.go wait_test.go libg…\n9. bash args={\"command\":\"cat plan.md | hea…"]

## tool_stdout
workspace-ready

## node.AskUser.response.AskUser
let's add a recording feature. It's something you invoke at any time during a session with a start and stop boundary. The precedent I have in mind is the playwright trace also Vibium, which is what I'm thinking about while I'm building twee, has a native recording format also based on playwright traces. In that case, it captures screenshots of a browser instance under test along with DOM state, network traffic, etc. So let's make a TUI-native recording file (.zip bundle of artifacts with a known manifest). Off the top of my head: path to executable, args, env vars, host details (os, arch), pid, any events (keystrokes)

## graph.default_max_retry
2

## node.Start.outcome
success

## response.Start
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

## episode_summary
1. bash args={"command":"cat internal/vt/types.go && cat internal/vt/visible.go"} outcome=success summary=// Package vt is the internal terminal model. It is hidden behind a narrow // interface so the backend (libghostty-vt) can evolve without touching the // public…
2. bash args={"command":"cat internal/render/render.go | head -60 && cat internal/daemon/handlers_query.go | head -80"} outcome=success summary=package render import ( "image" "image/color" "image/draw" "image/png" "io" "golang.org/x/image/font" "golang.org/x/image/math/fixed" "github.com/paulsmith/rese…
3. bash args={"command":"cat internal/engine/query.go && cat internal/engine/types.go"} outcome=success summary=package engine import ( "fmt" "github.com/paulsmith/research/twee/internal/vt" ) // Snapshot returns the current terminal state. func (t *Term) Snapshot() Snaps…
4. bash args={"command":"cat go.mod && cat Makefile 2>/dev/null || ls -la"} outcome=success summary=module github.com/paulsmith/research/twee go 1.26.0 require ( github.com/creack/pty v1.1.24 github.com/mitchellh/go-libghostty v0.0.0-20260428141358-29fdb3130d7…
5. bash args={"command":"cat cmd/twee/cmd_run.go"} outcome=success summary=package main import ( "context" "encoding/json" "flag" "fmt" "io" "net" "os" "path/filepath" "github.com/paulsmith/research/twee/internal/daemon" "github.com/pa…
6. bash args={"command":"cat tuitest/runner_smoke_test.go && cat tuitest/term.go"} outcome=success summary=package tuitest import ( "context" "strings" "testing" "time" ) func TestRunHelloWorld(t *testing.T) { term := Run(t, "/bin/sh", Args("-c", "printf 'hello\\r\\n…
7. bash args={"command":"cat tuitest/options.go && cat tuitest/snapshot.go"} outcome=success summary=package tuitest import ( "time" "github.com/paulsmith/research/twee/internal/engine" ) // Option configures a Term. type Option func(*config) type config struct…
8. bash args={"command":"cat internal/daemon/server.go && cat internal/daemon/server_test.go"} outcome=success summary=/…

## node.Explore.outcome
success

## node.Explore.episode_summaries
["1. bash args={\"command\":\"pwd \u0026\u0026 ls -la \u0026\u0026 git status 2\u003e/dev/null || echo \\\"Not a git repo\\\"\"} outcome=success summary=/Users/paul/projects/research/twee total 76 drwxr-xr-x 26 paul staff 832 May 1 10:15 . drwxr-xr-x 8 paul staff 256 Apr 29 06:20 .. drwxr-xr-x 6 paul staff 192 M…\n2. bash args={\"command\":\"git log --oneline -5 2\u003e/dev/null || echo \\\"No git history\\\"\"} outcome=success summary=a121c5a Don't track .envrc d0aea6b Add demo video link to README 65f10c0 Initial commit\n3. bash args={\"command\":\"cat README.md\"} outcome=success summary=# twee — drive TUIs from the shell `twee` is a command-line tool for spawning a terminal UI under a PTY and driving it from outside: type, press keys, query the…\n4. bash args={\"command\":\"cat AGENTS.md \u0026\u0026 echo \\\"---\\\" \u0026\u0026 cat CLAUDE.md\"} outcome=success summary=Use `go doc` and `gopls` liberally when writing, reading, understanding, and debugging Go code. ## Pushing a new bookmark with jj `jj git push --remote \u003cr\u003e -b \u003c…\n5. bash args={\"command\":\"ls internal/ \u0026\u0026 cat go.mod\"} outcome=success summary=daemon engine input ptyrunner pump recording render rpc snapshot vt module github.com/paulsmith/research/twee go 1.26.0 require ( github.com/creack/pty v1.1.24 …\n6. bash args={\"command\":\"cat design.md | head -100\"} outcome=success summary=# Design: Go TUI Test Harness Using `libghostty-vt` ## Status Draft for implementation. ## Context `libghostty-vt` is the virtual terminal core extracted from G…\n7. bash args={\"command\":\"ls internal/daemon/ internal/engine/ internal/rpc/ 2\u003e/dev/null\"} outcome=success summary=internal/daemon/: dispatch.go handlers_diff.go handlers_input.go handlers_lifecycle.go handlers_query.go handlers_record.go handlers_screenshot.go handlers_wait…\n8. bash args={\"command\":\"ls tuitest/ \u0026\u0026 ls cmd/\"} outcome=success summary=diagnostic_test.go expect.go input.go menu_e2e_test.go options.go runner_smoke_test.go snapshot.go snapshot_assert.go term.go testdata wait.go wait_test.go libg…\n9. bash args={\"command\":\"cat plan.md | hea…","1. bash args={\"command\":\"cat internal/vt/types.go \u0026\u0026 cat internal/vt/visible.go\"} outcome=success summary=// Package vt is the internal terminal model. It is hidden behind a narrow // interface so the backend (libghostty-vt) can evolve without touching the // public…\n2. bash args={\"command\":\"cat internal/render/render.go | head -60 \u0026\u0026 cat internal/daemon/handlers_query.go | head -80\"} outcome=success summary=package render import ( \"image\" \"image/color\" \"image/draw\" \"image/png\" \"io\" \"golang.org/x/image/font\" \"golang.org/x/image/math/fixed\" \"github.com/paulsmith/rese…\n3. bash args={\"command\":\"cat internal/engine/query.go \u0026\u0026 cat internal/engine/types.go\"} outcome=success summary=package engine import ( \"fmt\" \"github.com/paulsmith/research/twee/internal/vt\" ) // Snapshot returns the current terminal state. func (t *Term) Snapshot() Snaps…\n4. bash args={\"command\":\"cat go.mod \u0026\u0026 cat Makefile 2\u003e/dev/null || ls -la\"} outcome=success summary=module github.com/paulsmith/research/twee go 1.26.0 require ( github.com/creack/pty v1.1.24 github.com/mitchellh/go-libghostty v0.0.0-20260428141358-29fdb3130d7…\n5. bash args={\"command\":\"cat cmd/twee/cmd_run.go\"} outcome=success summary=package main import ( \"context\" \"encoding/json\" \"flag\" \"fmt\" \"io\" \"net\" \"os\" \"path/filepath\" \"github.com/paulsmith/research/twee/internal/daemon\" \"github.com/pa…\n6. bash args={\"command\":\"cat tuitest/runner_smoke_test.go \u0026\u0026 cat tuitest/term.go\"} outcome=success summary=package tuitest import ( \"context\" \"strings\" \"testing\" \"time\" ) func TestRunHelloWorld(t *testing.T) { term := Run(t, \"/bin/sh\", Args(\"-c\", \"printf 'hello\\\\r\\\\n…\n7. bash args={\"command\":\"cat tuitest/options.go \u0026\u0026 cat tuitest/snapshot.go\"} outcome=success summary=package tuitest import ( \"time\" \"github.com/paulsmith/research/twee/internal/engine\" ) // Option configures a Term. type Option func(*config) type config struct…\n8. bash args={\"command\":\"cat internal/daemon/server.go \u0026\u0026 cat internal/daemon/server_test.go\"} outcome=success summary=/…"]

---

Based on the exploration of the codebase, write a precise implementation
specification for the user's request: let's add a recording feature. It's something you invoke at any time during a session with a start and stop boundary. The precedent I have in mind is the playwright trace also Vibium, which is what I'm thinking about while I'm building twee, has a native recording format also based on playwright traces. In that case, it captures screenshots of a browser instance under test along with DOM state, network traffic, etc. So let's make a TUI-native recording file (.zip bundle of artifacts with a known manifest). Off the top of my head: path to executable, args, env vars, host details (os, arch), pid, any events (keystrokes)

The spec must be concrete enough that three independent agents can implement
it without further clarification and produce comparable results.

Structure:
## Objective
One sentence.

## Scope
Exactly which files to create/modify. Nothing else should be touched.

## Requirements
Numbered list. Each requirement is testable.

## Non-requirements
Explicitly list what is OUT of scope to prevent over-engineering.

## Success criteria
How to verify the implementation is correct and complete.

## Constraints
Architecture patterns, naming conventions, test patterns to follow.

Write the spec to .ai/decisions/spec.md