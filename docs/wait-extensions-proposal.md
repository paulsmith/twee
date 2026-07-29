# Proposal: wait extensions (region exclusion, chaining, partial-match diagnostics)

## Status

Proposal. `twee` is pre-release experimental software, so this proposal assumes
the CLI, daemon protocol, and JSON output may change without a compatibility
bridge.

Grounded against the current `wait` implementation: `internal/pump/pump.go`
(`Wait`/`WaitStable`), `internal/engine/wait.go`, and
`internal/daemon/handlers_wait.go`. All of the following have already landed
on this branch and are assumed as baseline: `wait text --regex` compiles with
`(?m)`; a `SESSION_ENDED` code distinguishes teardown from timeout for
`text`/`no-text`/`cursor` (never `stable` or `exit`); timeout/session-ended
failures carry a full diagnostic dump in `message`, a short `details.cause`,
and `details.last_screen`; `twee do` runs op scripts against a named session.

## Context

A stress-testing exercise driving real TUIs surfaced three recurring desire
paths around `wait`:

1. `wait stable` is useless against anything with a live clock, spinner, or
   progress ticker — it correctly times out, because *something* is always
   redrawing, but that "correctly" is exactly the problem: the tool can never
   report stability for a whole class of otherwise-idle apps.
2. Agents chain waits: `wait A` → send a key → `wait B` → send a key → ...
   Each step is a process spawn plus a socket round trip, and testers asked
   for a way to fuse a wait and its follow-up action into one call.
3. When a `wait text` pattern never appears and times out, the failure gives
   you the whole visible screen (`last_screen`) but no help pinpointing why —
   in particular, no signal that the text *almost* appeared (wrapped across a
   line boundary, a stray trailing character, a case mismatch).

## Goals

- Make `wait stable` usable against apps that redraw a small, known-irrelevant
  region continuously.
- Give an honest answer on whether wait chaining deserves new CLI/wire
  surface given that `twee do` already exists.
- Make a timed-out `wait text`/`wait no-text` cheaper to debug without
  changing what "found" means.

## Non-goals

- Region exclusion for `wait text`/`wait no-text`/`find` matching (masking a
  rectangle out of *search*, as opposed to out of *stability*) — a different
  problem, not requested by the stress tests. See "Alternatives considered."
- Changing `wait stable`'s no-`--exclude` behavior or performance at all.
- A general fuzzy-matching mode for `wait text` itself. Section 3 is
  diagnostics only: it never changes whether a wait succeeds, only what the
  failure says.

## 1. Region exclusion for `wait stable`

### Two different stability definitions

`pump.WaitStable` (pump.go:245) is **timestamp-based**, not content-based. It
never inspects the screen. It tracks one fact — `lastFeed`, the time of the
most recent `Feed` call — and declares stability once `quietFor` has elapsed
since then. This is why a 50ms ticker defeats it: every tick is a `Feed`,
every `Feed` resets `lastFeed`, so `now.Sub(lastFeed) >= quietFor` never
holds, regardless of whether the tick changes anything a human would call
"the screen."

Region exclusion changes the question from "did any bytes arrive?" to "did
anything *outside these rectangles* change?" That question can't be answered
from a timestamp — it requires rendering the screen and comparing it to the
last rendering at every wakeup, for as long as the wait runs. This is a
strictly more expensive and more invasive definition of "stable," and the
proposal says so plainly rather than presenting `--exclude` as a free
refinement of the existing check: default `wait stable` stays a single cheap
timestamp compare, correct for "is this app doing anything at all"; opting
into `--exclude` buys "is this app doing anything *I care about*" at the cost
of rendering and diffing the masked screen on every output-triggered wakeup,
proportional to screen size and to how chatty the excluded region's redraws
are — a spinner that redraws the *entire* screen 60 times a second pays that
cost 60 times a second no matter how tightly `--exclude` is drawn.

### Design: `WaitStableMasked`, opt-in

Add a new pump primitive alongside `WaitStable`, engaged only when the caller
supplies at least one exclusion rectangle:

```go
// WaitStableMasked behaves like WaitStable, but "stable" means mask's
// projection of the snapshot hasn't changed for quietFor, rather than
// "no Feed call happened" for quietFor. mask is caller-supplied; pump
// stays ignorant of what a "rectangle" or "cell" is, seeing only
// vt.Snapshot in and a comparable string out — the same boundary
// WaitForText's pred closures already cross.
func (p *Pump) WaitStableMasked(ctx context.Context, quietFor, timeout time.Duration, mask func(vt.Snapshot) string) error
```

The loop reuses `WaitStable`'s structure (one timer, `cond.Wait` driven by
the same broadcasts), replacing `lastFeed` with a digest and a change time:

```go
snap := p.model.Snapshot()
d := mask(snap)
if !initialized || d != lastDigest {
    lastDigest, lastChange, initialized = d, now, true
}
stable := p.gotAnyFeed && now.Sub(lastChange) >= quietFor
if stable || p.closed {
    return nil // dead screen is trivially stable, same rule as WaitStable
}
// ...same deadline/wakeIn/timer.Reset(wakeIn)/cond.Wait() as WaitStable
```

`mask` renders the snapshot to text via the same helpers `VisibleText` uses,
then blanks the excluded rectangles — so masking lives at the engine layer
(which knows about cells and rectangles), not in `pump`. Digest comparison is
plain string equality, not a hash: a viewport is at most a few KB, so a
`memcmp`-equivalent compare every wakeup is cheaper than hashing and sidesteps
any collision discussion entirely.

**`--exclude` absent → zero change.** `handleWaitStable` keeps calling the
existing `WaitForStableScreen` (unmodified `pump.WaitStable`) when no
exclusion rectangles are given; `WaitStableMasked` is a strictly additive path
taken only on opt-in.

### CLI and wire sketch

Repeatable `--exclude x,y,w,h`, parsed the same way `export --crop` already
parses its `x,y,w,h` cell rectangle (`cmd/twee/cmd_export.go`,
`parseCropFlag`): `w,h` must be `> 0`, `x,y` must be `>= 0`, both
`INVALID_ARGUMENT` before the op runs. A rectangle extending past the current
screen is clamped to the visible extent rather than rejected (matches
`region`'s tolerant clipping) — a resize between picking coordinates and
issuing the wait shouldn't turn a defensive exclude into a hard failure.

```sh
$ twee wait stable --exclude 70,0,10,1 --exclude 0,23,80,1 --quiet 200ms
{"ok":true,"data":null}
```

```json
{"op":"wait_stable","args":{"quiet":"200ms","timeout":"5s","exclude":[{"x":70,"y":0,"w":10,"h":1},{"x":0,"y":23,"w":80,"h":1}]}}
```

`rpc.WaitStableArgs` gains one field; a shared `Rect` shape is introduced
(also useful for the companion find-improvements proposal):

```go
type WaitStableArgs struct {
    Quiet   string `json:"quiet,omitempty"`
    Timeout string `json:"timeout,omitempty"`
    Exclude []Rect `json:"exclude,omitempty"`
}

type Rect struct{ X, Y, W, H int }
```

### Alternatives considered

- **Dirty-rectangle tracking inside the VT model**, so the daemon re-renders
  only changed cells instead of the whole screen per wakeup. Cheaper at
  scale, but `libghostty-vt` doesn't expose per-cell change tracking today;
  left as future work that depends on upstream capability twee doesn't use.
- **Excluding regions from `wait text`/`find` matching too**, for symmetry.
  Rejected here: ignore-for-stability and ignore-for-search are different
  problems, and conflating them under one flag would blur what `--exclude`
  means per command. A matching-side need would get its own flag.
- **Comparing structured cell grids instead of rendered text.** Functionally
  equivalent; text reuses `VisibleText`'s existing path instead of adding a
  second cell-diffing code path.

## 2. Wait chaining: recommend against a dedicated syntax

The stress tests' complaint — a `wait`/`key`/`wait`/`key` sequence pays a
process spawn and a socket round trip per step — is real, but `twee do`
(merged on this branch) already solves it, generically:

```sh
$ twee do --name agent <<'EOF'
[
  {"op": "wait_text", "args": {"text": "Choose an option", "timeout": "5s"}},
  {"op": "key", "args": {"key": "Down"}},
  {"op": "wait_text", "args": {"text": "> second"}},
  {"op": "key", "args": {"key": "Enter"}}
]
EOF
{"ok":true,"data":{"ops":4}}
```

One process, one socket connection, an arbitrary-length sequence of arbitrary
ops, `--emit results` for per-step NDJSON. A `--then` flag on `wait text`
would reinvent a worse subset of this: it only helps a two-step
`wait`-then-*one-thing* shape (real sequences are longer, and chaining flags
don't compose past one hop without inventing `--then-then` or similar); every
chained op needs its own flag family (`--then-key`, `--then-pattern`,
`--then-regex`, ...) duplicating `do`'s JSON args as a second, parallel
encoding that can drift out of sync with the wire names strict arg
validation now enforces; and it gives up `do`'s per-op error attribution and
`--emit results` streaming for a "convenience" that's really `do` with worse
syntax.

**Recommendation: do not add wait chaining.** `do` already covers the
motivating cost more generally than any flag-based chaining syntax could. If
the remaining friction is ergonomics rather than cost — writing a JSON array
feels heavy for an ad hoc two-step sequence — the actionable follow-up is
letting `do --script` accept an inline JSON literal alongside a path/stdin, a
small `do`-scoped change, not part of this proposal.

## 3. Partial-match diagnostics on `wait text`/`wait no-text` timeout

`wait text --pattern "Choose an option"` times out and the failure carries
`last_screen` — the whole viewport — but nothing pointing at *why* the
pattern didn't match. Motivating case: the terminal wrapped "Choose an
option" across a line boundary ("Choose an optio" + newline + "n"), so no
single line ever contains it as a substring, and nothing today distinguishes
that from the text simply never rendering.

### Design: compute it once, at failure time, never in the wait loop

This must not touch `pump.Wait`'s hot path — `pred` runs on every
`Feed`-triggered broadcast, and anything added there multiplies by every byte
the child emits. The near-match computation instead runs exactly once,
inside `waitErr` (`internal/daemon/handlers_wait.go:167`), against the same
final snapshot already used for `last_screen`: one extra linear pass over a
screen already being rendered for the diagnostic, not new per-wakeup cost.

Two cheap, targeted checks, both `O(lines × len(pattern))`:

1. **Line-wrap check**: for each adjacent line pair `(i, i+1)`, test whether
   some prefix of the pattern matches the end of line `i` and the remaining
   suffix matches the start of line `i+1` — directly diagnosing the
   motivating case.
2. **Best-line substring overlap**: for each line, find the longest
   contiguous run shared with the pattern (longest-common-substring, not
   full edit distance), and report only the single best-scoring line — a
   per-line dump would bloat the response and bury the one useful candidate.

Only emit `near_match` when a candidate clears a floor (e.g. covers at least
half the pattern's length), so "nothing like this appeared" stays visibly
different from "something close appeared":

```json
{
  "ok": false,
  "error": {
    "code": "TIMEOUT",
    "message": "WaitForText(\"Choose an option\"): pump: timeout\n...",
    "details": {
      "cause": "pump: timeout",
      "last_screen": "...",
      "near_match": {
        "wrapped": true,
        "line": 12, "text": "Choose an optio",
        "continued_line": 13, "continued_text": "n"
      }
    }
  }
}
```

Non-wrapped case (case/typo drift): `"near_match": {"wrapped": false, "line":
4, "text": "Choose an Option"}`.

### Cost/complexity vs. value, and scope

Full fuzzy matching (Levenshtein distance, tunable similarity thresholds)
would catch more cases — typos, extra whitespace — but adds a
tunable-parameter surface for a debugging aid, not a matching mode. The two
checks above handle the concretely-reported failure (line wrap) and the next
most likely one (near-identical text), with no threshold to tune beyond the
"covers half the pattern" floor, and cost nothing while a wait is actually
progressing. **Recommendation: ship the two checks above; leave general
fuzzy matching as future work** — this round of stress testing surfaced only
the wrap case, so there's no evidence yet the added complexity would pay for
itself.

Scope this to `wait text` and `wait no-text` only; `wait cursor` and `wait
stable` have no "pattern" to fuzzy-match against, so their `waitErr` calls
are unaffected.

## Implementation order

1. `WaitStableMasked` in `pump.go`, unit-tested against a fake reader feeding
   a ticking region plus a genuinely-stable region, with and without
   `--exclude` covering the ticking part.
2. Engine-layer mask builder (render + blank rectangles) and
   `Term.WaitForStableScreenMasked`, mirroring `WaitForStableScreen`.
3. `rpc.Rect`, `WaitStableArgs.Exclude`, `handleWaitStable` branching on
   whether `Exclude` is empty.
4. CLI: repeatable `--exclude`, reusing/extracting `parseCropFlag`'s
   `x,y,w,h` parser as a shared helper.
5. `near_match` diagnostics: line-wrap check, best-line substring check,
   wired into `waitErr` for `wait text`/`wait no-text` only.
6. README: document `--exclude`, retire the "Region-exclusion is a future
   feature" line in Limitations, and document `near_match`.

Steps 1–4 (region exclusion) and step 5 (diagnostics) are independent and can
land in either order or in parallel; wait chaining (section 2) has no
implementation — the recommendation is not to build it.
