You are a senior engineer evaluating three independent implementations of the
same spec. Read:
- The spec: .ai/decisions/spec.md
- Claude's diff: .ai/candidates/claude.diff
- Claude's tests: .ai/candidates/claude.test
- Codex's diff: .ai/candidates/codex.diff
- Codex's tests: .ai/candidates/codex.test
- Gemini's diff: .ai/candidates/gemini.diff
- Gemini's tests: .ai/candidates/gemini.test

For EACH implementation, evaluate:

1. **Spec fidelity** (most important): Does it implement exactly what was
   specified? Nothing missing? Nothing extra? No scope creep? No unnecessary
   refactoring, documentation, or "improvements" beyond the spec?

2. **Test evidence**: Do the tests pass? Did the implementation add appropriate
   tests? Are there test failures that indicate bugs?

3. **Minimality**: Is the diff focused? Does it touch only the files the spec
   requires? Is the code clean without being over-engineered?

Write a structured comparison to .ai/decisions/critique.md with:
- Per-implementation assessment (strengths, weaknesses, spec deviations)
- Head-to-head comparison table
- Ranking: 1st, 2nd, 3rd with clear justification
- Any disqualifications (test failures, spec violations, scope creep)

---
# Context from Prior Pipeline Stages

## Human Response
approve

## Previous Node Output
The implementation has been committed to the `impl/claude` branch. Here's a summary of what was implemented:

## Summary

### All spec requirements fulfilled:

**New files created:**
- `internal/trace/trace.go` — `Trace` type with `New`, `WriteOutput`, `WriteInput`, `WriteResize`, `AddScreenshotPNG`, `Close` (idempotent); writes `.twee` zip bundle with `manifest.json`, `events.jsonl`, `screenshots/*.png`
- `internal/trace/trace_test.go` — 3 tests: `TestTraceRoundTrip`, `TestTraceIdempotentClose`, `TestTraceConcurrentWrites` (all pass with `-race`)
- `internal/daemon/handlers_trace.go` — `handleTraceStart`/`handleTraceStop` RPC handlers registered via `init()` + `optionalRegistrations`
- `cmd/twee/cmd_trace.go` — CLI `twee trace start [-out <path.twee>] [-name <name>]` / `twee trace stop [-name <name>]`

**Modified files:**
- `internal/ptyrunner/runner.go` — `Pid() int` method
- `internal/engine/config.go` — `TracePath string` field
- `internal/engine/term.go` — `tr *trace.Trace`, `EnableTrace`/`DisableTrace`/`TracePath`/`TraceAddScreenshot`, `updateOutputHookLocked` fan-out
- `internal/engine/input.go` — `Type`/`Key`/`Paste`/`Resize` write to `tr` when active
- `internal/engine/query.go` — `Diagnostic()` includes trace path
- `internal/rpc/ops.go` — `OpTraceStart`, `OpTraceStop`
- `internal/rpc/types.go` — `TraceStartArgs`
- `tuitest/options.go` — `Trace(path string) Option`, `tracePath` field, wired into `toEngine()`
- `tuitest/term.go` — logs trace path on test failure

**All success criteria met:** `go build ./...` ✓, `go vet ./...` ✓, all tests pass including `-race` ✓