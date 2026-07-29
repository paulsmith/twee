# Proposal: find improvements (column semantics, region-around-match, style filters)

## Status

Proposal. `twee` is pre-release experimental software, so this proposal assumes
the CLI, daemon protocol, and JSON output may change without a compatibility
bridge — including `find`'s response shape, below.

Grounded against `internal/daemon/handlers_query.go`'s `handleFind`, its
supporting `vt.VisibleLines`, and `rpc.FindMatch`/`CellData`/`ColorData`. All
of the following have already landed on this branch and are assumed as
baseline: `cell`/`region` return snake_case cells with full style attributes
(`bold`, `dim`, `italic`, `underline`, `inverse`, `strikethrough`, plus
`fg`/`bg` as `{kind, index|r,g,b}`); numeric coordinate args on `cell`/
`region` are `--x`/`--y`/`--w`/`--h` flags; daemon-side arg decoding is
strict about unknown keys.

## Context

Stress testing turned up three `find`-shaped desire paths:

1. Consumers doing anything with `x`/`w` beyond printing them (drawing a box,
   feeding them to `region`) need to know what unit they're in. Wide CJK and
   emoji cells occupy two display columns for one glyph — does `find` count
   in columns, characters, or bytes?
2. Testers wanted the rectangle around a match without doing coordinate math
   by hand: "give me the cells around the 2nd match of `ERROR`."
3. Cells now carry full style attributes; testers wanted to search *by* those
   attributes — find the bold line, find the red text — not just by
   character content.

## Goals

- State, precisely, what `find`'s `x`/`w` mean today, verified against
  `handleFind`'s source, not assumed from the field name.
- Let a match's surrounding cells be fetched in the same call, in the same
  wire shape `region` already returns.
- Let matches be filtered by cell style and color, with a stated,
  unambiguous color-matching rule.

## Non-goals

- Changing how lines are matched (still per-line, still no cross-line
  substring matches — see baseline). A pattern split across a wrapped line is
  a `wait`-side diagnostics problem, addressed in the companion
  wait-extensions proposal, not a `find` change.
- General fuzzy/approximate text matching. Section 3's color matching is
  approximate by necessity (color names name regions of color space, not
  points); text matching stays exact-substring or `--regex`, unchanged.

## Baseline: what `find` does today

`handleFind` runs on `t.Lines()`, i.e. `vt.VisibleLines(snapshot)` — the same
per-row strings `wait text`'s non-regex path and `Diagnostic()`'s screen dump
use. Each row string is built by walking that row's cells: skip a cell
entirely if `Width == 0` (a wide character's trailing/continuation cell),
write a space if `Text == ""` (a blank background cell), else write `Text` —
then trim trailing spaces. A wide character's continuation cell contributes
**zero bytes**, not a filler space, so the row string is shorter, in
cell-count terms, than the row's display width by one position per wide
character.

Both match branches then operate on that string with byte-offset Go APIs:

```go
i := strings.Index(ln[start:], a.Text)              // literal: byte offset
idx := re.FindAllStringIndex(ln, -1)                // regex: byte offset pairs
```

`FindMatch.X` is that byte offset; `FindMatch.W` is `len(a.Text)` (literal)
or `idx[1]-idx[0]` (regex) — both byte lengths. Neither is a rune count or a
display column. Worked example: a row rendering `中x` (`中` is one rune, one
cell, `Width: 2`, three UTF-8 bytes; `x` is one rune, one cell, one byte).
`VisibleLines` builds the row string `"中x"` — 4 bytes, 2 runes, 3 display
columns wide. Searching for `"x"`:

| Measure | Value |
|---|---|
| `FindMatch.X` (byte offset, what `handleFind` returns today) | 3 |
| Rune offset | 1 |
| Display column | 2 |

All three differ. They coincide only for pure-ASCII screens and patterns —
presumably why this has gone unnoticed. Worse, the direction of the error
depends on which side the wide character is on: a wide character earlier in
the row inflates `X` past its display column (as above); a wide character
*inside the search pattern itself* inflates `W` past the pattern's display
width (`len("中")` is 3, but it is 1 rune and 2 display columns).

This is also inconsistent with the rest of the query surface. `CellArgs.X`
and `RegionArgs.X` (`handleCell`, `handleRegion`) index directly into
`snap.Lines[y].Cells`, which *does* include `Width == 0` continuation cells
as real slice elements — so for those two ops, `X` is a true display-column
index. Same field name, same RPC surface, two different units: `X` means
"display column" everywhere except `FindMatch`, where it means "byte offset
into a wide-char-collapsed string." A consumer that finds a match and feeds
its `x` into `region --x` today is silently wrong on any non-ASCII screen.

## 1. Fix: `find`'s `x`/`w` become display columns

Make `FindMatch.X`/`.W` mean the same thing `CellArgs`/`RegionArgs` already
mean, so a match's rectangle is directly usable as a `region` query.

Implementation, anchored to the existing code: build, alongside each row's
search string, a parallel `byteToCol []int` of length `len(row)+1` mapping
each byte offset in that string to the display column of the cell that
produced it (plus one sentinel entry, the row's total display width, for a
match ending at the string's end). Since continuation cells contribute zero
bytes, this mapping is unambiguous — every emitted byte maps to exactly one
column, no interpolation needed. After a match is found at byte range
`[start, end)`, look up `X = byteToCol[start]`, `W = byteToCol[end] - X`. Cost
is `O(row length)` per row, built once per `find` call, not per match.

```json
{"op":"find","args":{"text":"中"}}
```

```json
{"ok":true,"data":{"matches":[{"x":0,"y":3,"w":2,"h":1,"line":3,"text":"中"}]}}
```

(Today this same call would report `{"x":0,"w":3,...}` — the UTF-8 byte
length of "中" — instead of `{"x":0,"w":2,...}`, its display width.)

## 2. Region around a match

Add `--context <n>` (default: absent, meaning no region attached) and
`--nth <k>` (default `1`, 1-indexed in the existing match order — top to
bottom, left to right, the order `handleFind` already produces). When
`--context` is given, the response gains a `region` field: the `--nth`
match's rectangle expanded by `n` cells on every side, clamped to the
screen (same tolerant clamping `region` already does for an out-of-bounds
rectangle), plus that rectangle's actual cells in `region`'s existing
`CellData` row-array shape. `--context 0` is legal and means "just this
match's own cells, no expansion" — deliberately covering the plain
"region-shaped result, no math" ask without a second `--region` boolean flag
duplicating `--context`'s job.

```sh
$ twee find --pattern ERROR --nth 2 --context 2
```

```json
{"op":"find","args":{"text":"ERROR","nth":2,"context":2}}
```

```json
{
  "ok": true,
  "data": {
    "matches": [
      {"x":4,"y":1,"w":5,"h":1,"line":1,"text":"ERROR"},
      {"x":10,"y":6,"w":5,"h":1,"line":6,"text":"ERROR"}
    ],
    "region": {
      "x": 8, "y": 4, "w": 9, "h": 5,
      "cells": [ [ {"text":"...", "width":1, "fg":{"kind":"default"}, "bg":{"kind":"default"}, "bold":false, "dim":false, "italic":false, "underline":false, "inverse":false, "strikethrough":false}, "..." ] ]
    }
  }
}
```

`matches` always lists every match, unfiltered by `--nth` — only the
attached `region` is scoped to one of them. If `--nth` exceeds the match
count, `region` is simply omitted (`ok:true`, matching the existing "found
nothing is not an error" convention for text queries) rather than failing
the call.

Composition note: because section 1 makes `FindMatch` coordinates
display-column-based, the plain (no `--context`) case already composes with
`region` by hand — `twee region --x <m.x> --y <m.y> --w <m.w> --h <m.h>`
reproduces exactly the matched cells. `--context` just saves that round trip
and the expand-and-clamp arithmetic: implemented by reusing `handleRegion`'s
row-slicing loop against the expanded rectangle, factored into a shared
helper rather than copied.

## 3. Style- and color-aware find

`--style <attr>` (repeatable, one of `bold`, `dim`, `italic`, `underline`,
`inverse`, `strikethrough`) and `--fg <color>` / `--bg <color>` filter
`matches` down to those where **every** cell the match spans satisfies every
requested condition (AND across repeats, AND across the span — a match
where only some of its cells are bold does not qualify as `--style bold`;
a partially-styled match is exactly the kind of thing this filter exists to
distinguish from a fully-styled one).

### Color matching: the ambiguity, and the pick

`fg`/`bg` on the wire are `{"kind":"default"}`, `{"kind":"palette","index":N}`
(0–255), or `{"kind":"rgb","r":N,"g":N,"b":N}`. A color *filter* has to
decide what "red" means against all three shapes. The proposal resolves
`--fg`/`--bg`'s value by its own shape, each with a different, explicit
match rule:

| Value shape | Example | Matches | Rule |
|---|---|---|---|
| Bare integer | `--fg 196` | `kind:"palette"` only | Exact `index` equality. Never matches `rgb`/`default`. |
| Hex triple | `--fg '#ff0000'` | `kind:"rgb"` only | Exact `r`,`g`,`b` equality. Never matches `palette`/`default`. |
| 16-color name | `--fg red` | `palette` and `rgb` | Approximate — see below. |
| `default` | `--fg default` | `kind:"default"` only | Exact. |

Named colors (`black red green yellow blue magenta cyan white`, each with a
`bright-` variant, the classic 16-color set) are the only approximate case,
because a name denotes a region of color space, not a point:

- Against a `palette` cell: exact match if `index` is that name's canonical
  slot (0–7 dim, 8–15 bright — e.g. `red` is index 1 or 9). A palette index
  above 15 (the 256-color cube) **never** matches a name, on purpose —
  precise 256-color-by-name matching is out of scope; use the bare-integer
  form for those.
- Against an `rgb` cell: nearest-of-the-16-reference-colors by Euclidean
  RGB distance, matching if the nearest is the requested name.

This is stated as policy, not left implicit, because it's the one place a
`find` filter can silently do something the caller didn't expect: `--fg red`
matching an RGB cell that isn't very red, just redder than the other 15
buckets. Callers who need precision use the integer or hex forms, which are
exact by construction.

```sh
$ twee find --pattern ERROR --style bold --fg red
```

```json
{"op":"find","args":{"text":"ERROR","style":["bold"],"fg":"red"}}
```

```json
{"ok":true,"data":{"matches":[{"x":4,"y":1,"w":5,"h":1,"line":1,"text":"ERROR"}]}}
```

A match whose "ERROR" is only partly bold (e.g. bold on `ERR`, not `OR`, a
real thing a raw-mode app can do) is excluded, not returned with a caveat —
callers wanting "any cell in the span" instead of "every cell in the span"
can drop `--style`/`--fg` and inspect `region`'s per-cell attributes
themselves via section 2's `--context`.

## Alternatives considered

- **Rune count instead of display column for `X`/`W`.** Rejected: doesn't
  fix the actual downstream need (composing with `region`, which is
  column-indexed), and still diverges from display width for every wide
  character — trading one wrong unit for a different wrong unit.
- **`--region` as a separate boolean flag**, distinct from `--context`.
  Rejected as redundant: `--context 0` already means "just the match's own
  cells," so a second flag would just be another spelling of the same
  request.
- **Exact-only color matching (integer/hex, no names).** Friendlier CLI
  ergonomics lost: `--fg 1` vs `--fg red` is a real usability gap for
  interactive/ad hoc use, and the ambiguity is fully containable by scoping
  names to the 16-color set and documenting the nearest-neighbor rule, so
  there's no need to give up names entirely to avoid it.

## Back-compat notes

Pre-release, so breaking changes are acceptable, but two are worth flagging
explicitly:

- **`find`'s response shape changes from a bare array to an object**:
  `{"matches": [...], "region": {...}?}` instead of today's `[...]` directly
  under `data`. This is the only way to attach `region` without a shape that
  depends on which flags were passed (array when plain, object when
  `--context` given) — which would be worse. Any script doing
  `jq '.data[0].x'` becomes `jq '.data.matches[0].x'`. No shipped script or
  test in this repo depends on the old shape (`find` isn't used in
  `scripts/` or the README beyond the command-reference table).
- **`FindMatch.X`/`.W` change value on any non-ASCII screen** (section 1).
  Pure-ASCII matches are numerically unaffected (byte offset, rune count,
  and display column all coincide there), so this is a silent-until-tested
  break: existing ASCII-only tests keep passing; anything with wide
  characters gets different, now-correct, numbers.

## Recommendation and implementation order

Ship all three; they're independent but section 1 should land first since
sections 2 and 3 are more useful with correct coordinates underneath them.

1. Section 1: `byteToCol` mapping, `FindMatch.X`/`.W` fixed. Unit tests with
   CJK and emoji content, asserting `X`/`W` match `region`'s column indexing
   for the same cells.
2. Section 2: `--context`/`--nth`, shared region-cell-extraction helper
   between `handleRegion` and `handleFind`, response shape change to
   `{matches, region}`.
3. Section 3: `--style`/`--fg`/`--bg`, the 16-color name table and
   nearest-neighbor distance function (small, shared with anything else that
   ever wants to render a color by name — e.g. a future `screenshot`
   annotation feature, not part of this proposal).
4. README: replace the `find` reference entry's `{x, y, w, h, line, text}`
   description with the new `{matches, region?}` shape, state the
   display-column contract explicitly, and document `--context`/`--nth`/
   `--style`/`--fg`/`--bg`.
