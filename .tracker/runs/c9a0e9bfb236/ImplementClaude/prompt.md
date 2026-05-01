You are implementing a spec in an isolated git worktree. Read the spec
from .ai/decisions/spec.md in the main repo (parent directory).

RULES:
- Implement EXACTLY what the spec says. Nothing more, nothing less.
- Follow all constraints listed in the spec.
- Run tests after implementation to verify.
- Do NOT create documentation files unless the spec requires them.
- Do NOT modify files outside the scope listed in the spec.

When done, commit your work on this branch with a conventional commit message.

---
# Context from Prior Pipeline Stages

## Human Response
approve

## Previous Node Output
All passing. Let me provide a summary of everything that was implemented:

---

## Implementation Summary

### New files created

| File | Purpose |
|---|---|
| `internal/trace/trace.go` | Core `Trace` type — accumulates events + PNG screenshots in memory, writes a `.twee` zip bundle on `Close()` containing `manifest.json`, `events.jsonl`, and `screenshots/*.png` |
| `internal/trace/trace_test.go` | 3 tests: `TestTraceRoundTrip` (full write→read verification of zip contents), `TestTraceIdempotentClose`, `TestTraceConcurrentWrites` (10 goroutines, `-race` safe) |
| `internal/daemon/handlers_trace.go` | RPC handlers for `trace_start`/`trace_stop` — captures screenshots via `render.Render` at start and stop |
| `cmd/twee/cmd_trace.go` | CLI verb: `twee trace start [-out path.twee] [-name name]` / `twee trace stop [-name name]` |

### Existing files modified

| File | Change |
|---|---|
| `internal/ptyrunner/runner.go` | Added `Pid() int` method |
| `internal/engine/config.go` | Added `TracePath string` field |
| `internal/engine/term.go` | Added `tr *trace.Trace` field, `EnableTrace`/`DisableTrace`/`TracePath`/`TraceAddScreenshot` methods, `updateOutputHookLocked` fan-out helper. Updated `Start`/`Close`/`EnableRecording`/`DisableRecording` to use the unified hook. |
| `internal/engine/input.go` | `Type`, `Key`, `Paste`, `Resize` now write to both `rec` and `tr` when active |
| `internal/engine/query.go` | `Diagnostic()` now includes trace path |
| `internal/rpc/ops.go` | Added `OpTraceStart`, `OpTraceStop` constants |
| `internal/rpc/types.go` | Added `TraceStartArgs` struct |
| `tuitest/options.go` | Added `Trace(path string)` option, `tracePath` config field, wired into `toEngine()` |
| `tuitest/term.go` | Logs trace path on test failure |

### Key design decision: Import cycle avoidance

`internal/trace` has **zero** imports of `internal/engine` or `internal/render`. It accepts raw PNG bytes via `AddScreenshotPNG([]byte)`. The rendering (`engine.Snapshot` → PNG) happens in the daemon handlers (`handlers_trace.go`) which can safely import both `engine` and `render`. This cleanly breaks the `engine → trace → render → engine` cycle that was hit during implementation.