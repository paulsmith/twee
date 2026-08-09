# Product feedback for twee

## Context

This feedback comes from using `twee` to diagnose terminal-rendering differences between two
implementations of the same application: a C binary and its Go port. The task was to run identical
mock-provider conversations in both binaries, inspect tool-call rendering, change the Go code, and
verify visual consistency.

The useful workflow was:

1. Start each binary in an 80×24 terminal with the same environment and working directory.
2. Wait for the prompt, type the same input, and press Enter.
3. Wait for a known final response and for the viewport to settle.
4. Read the settled viewport with `twee text` and compare the two results.
5. Repeat with fixtures covering verbose calls, collapsed calls, output gutters, and read ranges.

`twee` found a real issue quickly: the Go application rendered only `[bash]`, while the C application
rendered `[bash] echo marker42 > out.txt`. It also verified that exploration commands and reads were
collapsed identically after the fix. This is a strong use case for twee: direct shell pipes do not
exercise TTY-only rendering, carriage returns, deferred newlines, terminal width, or transient
repaints.

## Priority 1: First-class differential runs

Comparing two implementations is currently possible, but requires manually duplicating every
`twee start`, `wait`, `type`, `key`, and inspection command.

Add a differential command that drives the same operation script against two child commands and
compares their terminal state.

Example shape:

```sh
twee diff-run \
  --cols 80 --rows 24 \
  --script tool-call.json \
  --left ./c-hax \
  --right ./go-hax
```

It should support:

- Separate labels, commands, environments, and working directories for each side.
- Comparison of settled viewport text, cells, cursor state, terminal modes, and optionally each
  recorded frame.
- Text normalization rules for expected nondeterminism, such as elapsed-time rows or temporary
  paths.
- A nonzero exit status on mismatch.
- A compact textual diff by default and an optional trace bundle containing both sides.

Acceptance criteria:

- One script drives both programs with identical input.
- A mismatch identifies the first differing row and frame.
- The command can ignore a caller-provided regular expression such as `^[0-9]+s$`.
- The output is suitable for CI without additional `jq` or shell orchestration.

## Priority 1: Make run scripts discoverable and diagnosable

My first attempt used `twee run --script` and failed with:

```text
{"ok":false,"error":{"code":"INVALID_ARGUMENT","message":"EOF"}}
```

The top-level help says scripts contain RPC bodies, but does not show a complete script or link each
CLI verb to its wire operation and field names. I guessed fields such as `text`, `key`, and
`duration_ms`; the resulting error did not identify the operation or missing field.

Improve this in three ways:

1. Add a complete copy-paste example to `twee help run`.
2. Add `twee script schema` or `twee run --print-schema` with every operation and field.
3. Validate the whole script before starting the child and report JSON-path-aware errors.

Desired error:

```text
script[0] (wait_text): missing required field "pattern"
```

Acceptance criteria:

- A user can translate the documented `start`/`wait text`/`type`/`key` workflow into a run script
  without external documentation.
- Invalid JSON and invalid operations are distinguished.
- Errors include the operation index, operation name, bad field, and expected type or allowed
  values.

## Priority 2: Record assertions and snapshots as named artifacts

For rendering work, a final viewport is useful, but transient behavior matters too. Tool output can
replace a spinner row, overprint a gutter glyph, or briefly flash an incorrect status. A settled
`twee text` result cannot prove those intermediate frames were correct.

Allow scripts to name checkpoints and assertions:

```json
{"op":"snapshot","name":"tool-running"}
{"op":"assert_text","name":"tool-finished","golden":"goldens/tool-finished.txt"}
```

A trace should retain these names and `twee inspect` should navigate directly to them. Failed
assertions should include the nearest before/after frames.

Acceptance criteria:

- Checkpoints are visible by name in trace inspection and exported HTML.
- Golden text assertions produce a unified row diff.
- Cell assertions can detect style, width, and cursor differences that plain text omits.
- CI can update or verify goldens explicitly; verification must never rewrite them implicitly.

## Priority 2: Better terminal-state comparison controls

`twee text` was ideal for comparing settled content, but it intentionally loses styling and cell
metadata. `snapshot` preserves more information but is too verbose for routine review.

Add comparison and display levels:

- `text`: visible Unicode text only.
- `layout`: text plus row boundaries, trailing spaces, wraps, and cursor position.
- `style`: layout plus normalized SGR roles.
- `cells`: exact cell-by-cell state.

Also provide a human-readable snapshot format. For example, annotate only cells whose style differs
instead of returning the entire cell matrix as JSON.

Acceptance criteria:

- Users can distinguish a textual match from a layout or style match.
- Deferred autowrap and explicit newline differences are visible in `layout` mode.
- Exact JSON remains available for tools, while default output remains readable by humans.

## Priority 2: Isolated temporary working directories

The comparison fixture ran `echo marker42 > out.txt`. Because both children used the repository as
their working directory, this created a file that had to be removed manually. Rendering tests often
need realistic commands that write files, so isolation should be easy rather than left to every
script.

Add a temporary-directory mode:

```sh
twee start --temp-dir --copy scripts/mock -- ./hax
```

For differential runs, support independent copies seeded from the same source tree. Expose the
created path as a script variable and remove it after the run unless `--keep-dir` is set.

Acceptance criteria:

- File-writing fixtures cannot modify the source checkout by default when temp mode is requested.
- Both sides receive equivalent initial files but do not share mutations.
- Failed runs report and optionally retain their temporary directories for debugging.

## Priority 3: Easier environment reuse

Starting the two programs required repeating several environment values:

```text
HAX_PROVIDER=mock
HAX_MOCK_SCRIPT=...
HAX_NO_SESSION=1
HAX_NOTIFY=off
HAX_MARKDOWN=0
```

Support `--env-file`, reusable profiles, or a common environment block in run scripts. Differential
runs should have a common environment plus per-side overrides.

Environment values displayed in logs and traces must redact likely secrets by default, with an
explicit allowlist for values needed in debugging.

## Priority 3: Structured extraction of command results

The daemon verbs return JSON, which is good for automation, but routine shell use required either
reading JSON manually or relying on the human-readable nested `data.text` value.

Add a consistent `--output` option:

```sh
twee text --output plain
twee snapshot --output json
twee diff --output unified
```

`plain` should print only the requested payload and preserve the exit status contract. This would
make command substitution and ordinary shell diffs straightforward while keeping JSON as the API
format.

## Priority 3: Session lifecycle ergonomics

Named daemons worked reliably, but comparison work left several sessions that needed explicit
cleanup. Add scoped groups or automatic cleanup:

```sh
twee group start render-check ...
twee group stop render-check
```

A shell-friendly alternative is `twee start --cleanup-on-parent-exit`. `twee ls` should show age,
child command, working directory, and whether tracing is active so stale sessions are easy to
identify.

## Documentation: publish task-oriented recipes

The command reference is useful once the user knows the model. Add short recipes for common jobs:

- Compare two TUI implementations.
- Capture and test a transient spinner or progress view.
- Create a deterministic golden at a fixed terminal size.
- Debug a failed one-shot script.
- Run safely against a fixture that writes files.
- Export the relevant frames from a trace for a bug report.

Each recipe should include a complete script and explain when to use `text`, `snapshot`, a trace,
or a screenshot.

## Overall assessment

`twee` earned its keep in this session. Shell commands remained better for source inspection,
building, and tests, while twee supplied evidence about the actual terminal behavior. The largest
opportunity is to turn the successful but manually orchestrated comparison workflow into a
first-class, deterministic differential-testing feature. The next most important improvement is
script discoverability: a one-shot run should be easier to author than a sequence of daemon verbs,
not harder to debug.
