# Remaining agent-automation work

## Status

This plan continues the automation-safety work identified during the Twee
agent-skill review.

| Phase | Scope | Status |
|---|---|---|
| 1 | Private permissions for sensitive artifacts | Landed on `main` |
| 2 | Truthful terminal modes and mode-aware input encoding | Landed on `main` |
| 3 | Consistent client-relative paths in operation scripts | Landed on `main` |
| 4 | Ownership-safe session lifecycle operations | Landed on `main` |
| 5 | Correct `diff` and `cursor` help and response-contract tests | Landed on `main` |
| 6 | Machine-discoverable and consistent output contracts | Not started |
| 7 | Race-resistant find-and-act operations | Not started |

Phases 1–5 are the baseline for the remaining work. Phase 5 is jj change
`tlsoqvvy`; rebasing during integration changed its commit ID, so use the
stable change ID when inspecting its history.

## Phase 6: Machine-discoverable output contracts

### Problem

Twee has several valid output styles, but a caller must know which style each
command uses:

- Most daemon commands return one JSON envelope.
- `run --emit results` and `do --emit results` return NDJSON.
- `inspect --format text`, `help`, `version`, and `completion` return text.
- `export` is silent on success and prints runtime failures as text on standard
  error.
- Usage failures print text on standard error and exit with status 2.
- `play` and `wrap` can take over an interactive terminal.

This makes generic agent tooling fragile. It cannot determine how to parse a
command without maintaining its own command-specific table.

### Goal

Make every command's output behavior discoverable. Give every non-interactive
command an opt-in structured success and error contract without removing useful
human-oriented defaults.

### Proposed design

#### 1. Add command metadata to the CLI registry

Replace the separate dispatch, help, and summary registrations with, or back
them with, one command descriptor. The descriptor should identify:

- command path, summary, and usage text;
- whether the command is interactive;
- success output kind: `json`, `ndjson`, `text`, `silent`, or `interactive`;
- supported explicit output formats;
- whether errors can be emitted as structured JSON;
- documented exit-status meanings;
- whether the command writes a separate artifact and the field that reports
  its resolved path.

Expose these descriptors through:

```sh
twee help --format json
twee help diff --format json
```

The exact schema should have a version field from its first release. Command
tests should compare the registered descriptors with the dispatch table so a
new command cannot silently omit metadata.

Example shape:

```json
{
  "schema_version": 1,
  "commands": [
    {
      "path": ["diff"],
      "interactive": false,
      "success_output": "json",
      "formats": ["json"],
      "structured_errors": true,
      "exit_status": {
        "0": "comparison completed; inspect data.equal",
        "1": "operational failure",
        "2": "usage failure"
      }
    }
  ]
}
```

Metadata describes observable behavior. It is not a second parser schema and
should not duplicate every option definition in this phase.

#### 2. Provide one structured mode for non-interactive commands

Add an early, root-level machine-output selection that is available even when
argument parsing fails. Settle the spelling during implementation; a global
`--format json` is preferred if it can coexist cleanly with command-specific
formats such as `inspect --format text`. Otherwise use an unambiguous global
name such as `--machine`.

In structured mode:

- success is one JSON envelope unless the discovered output kind is NDJSON;
- runtime and usage failures are JSON envelopes on standard output;
- diagnostic logs remain on standard error;
- usage failures retain exit status 2;
- operational failures retain a nonzero status other than 2;
- interactive commands reject structured mode with a structured usage error.

Keep current default behavior where it is useful for people. Structured mode
is the stable automation surface.

#### 3. Bring `export` into the structured contract

In structured mode, `export` should return the resolved output path and format:

```json
{
  "ok": true,
  "data": {
    "path": "/absolute/path/replay.html",
    "format": "html"
  }
}
```

Runtime failures should use the same error envelope as other non-interactive
commands. The existing silent-success and text-error behavior may remain the
default.

#### 4. Centralize emission and exit handling

Move success, runtime-error, and usage-error emission behind one output-policy
object. Avoid command-local combinations of `fmt.Fprintln` and `os.Exit` for
non-interactive commands. This is required so root parsing failures, `export`,
and daemon-backed commands follow the same structured policy.

Do not redirect logs into JSON output. Standard output must contain only the
declared machine payload.

### Implementation slices

1. Introduce command descriptors and JSON help, with registry completeness
   tests. Do not change command output yet.
2. Add the root-level structured-mode parser and shared usage/runtime error
   emission.
3. Convert `export` to shared emission and return its resolved path.
4. Cover command families with black-box stdout, stderr, and exit-status
   contract tests.
5. Update `README.md`, command help, and the agent-facing Twee skill.

### Acceptance criteria

- An agent can discover whether every command returns JSON, NDJSON, text, no
  success output, or interactive terminal output.
- Every non-interactive command has an opt-in structured error form, including
  root and command usage errors.
- `export` reports its resolved output path in structured mode.
- JSON and NDJSON modes never mix diagnostics into standard output.
- Tests assert standard output, standard error, and exit status together for
  each output family.
- JSON help is versioned and registry tests fail when command metadata is
  missing.
- Human-readable defaults and interactive behavior are documented.

### Explicit non-goals

- A complete machine-readable argument or operation-script schema. That is a
  separate script-discoverability project.
- Changing daemon RPC response envelopes.
- Making interactive playback representable as JSON.
- Adding payload-only extraction such as `twee text --output plain`; it can be
  designed later using the same output-policy layer.

### Risks and decisions to resolve

- `inspect` already uses `--format`. Decide whether a root-level option can be
  distinguished reliably or whether `--machine` is clearer.
- NDJSON needs a structured terminal failure rule. Preserve the current
  per-operation stream and document whether a final summary record exists.
- JSON help becomes an automation API. Add a schema version and test it even
  though Twee is pre-release.

## Phase 7: Atomic find and click

### Problem

Agents currently call `find`, choose a match in client code, then call `click`.
This has two failure modes:

1. The caller can accidentally choose among zero or multiple matches.
2. The terminal can repaint between the query and the click.

The existing `find` command must remain available for exploration. Phase 7
adds a deliberate transactional action path.

### Recommended CLI

Extend `click` with a pattern form while retaining its coordinate form:

```sh
twee click --pattern Submit --require one
twee click --pattern 'Save .*' --regex --require one
twee click --pattern Item --select first
twee click --pattern Item --select 3
```

Rules:

- Coordinate flags and pattern flags are mutually exclusive.
- Pattern clicks require exactly one match by default. `--require one` may be
  accepted explicitly for readability.
- Ambiguous matches fail unless the caller supplies `--select first`,
  `--select last`, or a one-based match number.
- The default target is the center display cell of the selected match. For an
  even width, choose the left of the two center cells. Return that exact cell
  in the response.
- Existing button and modifier flags apply unchanged.

The RPC operation should be a distinct atomic operation, such as
`find_click`, rather than a client-side composition of `find` and `click`.
Operation scripts must be able to use it.

### Atomicity model

Matching, cardinality checking, target selection, viewport-bound validation,
mouse-mode validation, and mouse encoding must use one terminal-model snapshot
or a version-checked equivalent.

Do not implement the new handler by calling `handleFind` and then
`handleClick`: each takes or validates against live state separately. Factor
the existing search-line and match logic into a shared pure helper, then add an
engine/pump operation that performs selection and mouse preflight while the
terminal model is locked. Release the model lock before a potentially blocking
PTY write, following the existing input lock ordering.

The response should identify the state and decision used:

```json
{
  "ok": true,
  "data": {
    "match": {"x": 10, "y": 6, "w": 6, "h": 1, "line": 6, "text": "Submit"},
    "target": {"x": 12, "y": 6},
    "selection": "exactly_one"
  }
}
```

If the terminal model already has a useful monotonic generation, return it.
Otherwise, do not add a public generation only for decoration; the important
guarantee is that matching and validation used the same locked state.

### Errors

Zero and multiple matches must have distinct structured errors. Prefer stable,
specific codes such as `NOT_FOUND` and `AMBIGUOUS_MATCH`; if existing broad
codes are retained, include an unambiguous reason and cardinality in details.

Error details should include:

- pattern and whether it was a regular expression;
- observed match count;
- requested cardinality or selection policy;
- matches or a bounded sample of matches for ambiguity diagnostics.

An out-of-range numeric selection is a separate invalid-selection error, not a
zero-match result.

### Implementation slices

1. Extract and unit-test shared matching over one snapshot, including Unicode
   and wide-cell coordinates.
2. Add RPC argument, result, and error types with strict decoding.
3. Add the engine/pump atomic selection-and-encoding operation with documented
   lock ordering.
4. Register the daemon operation and add the `click --pattern` CLI form.
5. Add operation-script decoding and path-independent run/do tests.
6. Add help, JSON output metadata from phase 6, trace assertions, and end-to-end
   tests against a repainting fixture.

### Acceptance criteria

- Zero and multiple matches produce distinct structured errors when exactly
  one match is required.
- Ambiguous input is never resolved silently.
- Matching, selection, bounds checking, mouse-mode checking, and encoding use
  one locked or version-checked terminal state.
- The successful response reports the selected match and click coordinates.
- Literal, regular-expression, Unicode, wide-cell, modifier, and button cases
  are tested.
- A repaint stress test demonstrates that selection and validation cannot
  observe different screen generations.
- The semantic find-and-click action and actual encoded mouse bytes are
  represented correctly in traces.
- Direct CLI, `run`, and `do` forms have equivalent behavior.
- Coordinate-based `click` and exploratory `find` remain available.

### Relationship to the broader find proposal

[`find-improvements-proposal.md`](find-improvements-proposal.md) covers display
column semantics, regions around matches, and style-aware filtering. Phase 7
should reuse its shared matching primitives, but atomic click delivery does not
depend on region or style-filter features. Do not expand phase 7 to implement
the whole proposal unless that scope is approved separately.

## Cross-phase delivery checklist

After phases 6 and 7:

- Remove obsolete defensive workarounds from the agent-facing Twee skill.
- Add one unattended automation test that creates a token-owned session, runs
  an operation script with relative artifacts, inspects machine-readable
  metadata, performs an atomic pattern click, and cleans up unconditionally.
- Re-run Unix permission tests under umask `0022`.
- Run `gofmt`, `gopls check`, focused tests, `go test ./...`, `go test -race`
  for changed concurrency-sensitive packages, and `go vet ./...` in the Nix
  development shell.
- Independently review each phase before landing it.
- Update the README, command help, security notes, and skill examples together
  with behavior changes.

## Recommended order

1. Implement phase 6 metadata before changing output behavior.
2. Complete phase 6 structured output and documentation.
3. Implement phase 7 on top of the discoverable contract registry.
4. Update and simplify the agent-facing skill, then run the full unattended
   workflow test.
