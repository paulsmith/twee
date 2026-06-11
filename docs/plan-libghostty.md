# Implementation Plan: libghostty-vt Integration

This plan supersedes the VT-backend portion of `plan.md`. The original
plan called for `mitchellh/go-libghostty` from day one; the POC shipped
with a hand-rolled pure-Go emulator because the development environment
lacked the tooling to build libghostty-vt. This plan swaps the
hand-rolled emulator for the real binding and removes it.

The seam is already in place: `internal/vt/Model` (`Feed`, `Resize`,
`Snapshot`) is the only contact surface between the parser and the rest
of the harness. Everything outside `internal/vt/` is unaffected.

## Up-front decisions (resolved)

- **Hand-rolled emulator**: delete after the swap. No dual-backend.
- **`Color.Kind` distinction**: drop `ColorIndexed`. libghostty does not
  distinguish "named SGR 0–15" from "256-palette"; both go through one
  palette path. Affected test (`TestSGRBoldColor`) is updated.
- **Cell style attributes**: add `Italic` and `Strikethrough` while
  we're already touching `Cell`. libghostty exposes them for free.
- **CGO build infrastructure**: Nix flake + direnv, matching the
  project-wide convention. Pull libghostty-vt as a flake input from the
  ghostty source repo; expose `pkg-config` and `PKG_CONFIG_PATH` to the
  dev shell.
- **go-libghostty version pinning**: pin to a specific commit via
  `go.mod` pseudo-version. The binding's README explicitly disclaims
  API stability; isolate to `internal/vt/ghostty.go`.
- **Snapshot construction**: use the `RenderState` API, not per-cell
  `GridRef` calls. RenderState is purpose-built for repeated full-grid
  reads.

## Scope: what changes vs. what stays

| Layer | Change |
|---|---|
| `tuitest/*` (public API) | None |
| `internal/pump` | None — already serializes model access under `mu`, exactly what libghostty's single-goroutine constraint requires |
| `internal/ptyrunner` | None |
| `internal/trace` | None — trace writing stores raw event bytes; backend-agnostic |
| `internal/input/keys.go` | None for the swap. M4 (optional) adds DECCKM awareness |
| `internal/snapshot` | None |
| `internal/vt/types.go` | Drop `ColorIndexed`; add `Italic` + `Strikethrough` to `Cell` |
| `internal/vt/term.go` | Deleted in M3 |
| `internal/vt/model.go` | `New(c,r)` returns the libghostty-backed impl |
| `internal/vt/visible.go` | No expected change |
| `internal/vt/ghostty.go` | New: implements `Model` over `*libghostty.Terminal` |
| `internal/vt/term_test.go` | Regression oracle for the swap; expectations updated where libghostty is more correct |

## Milestone 0: build infrastructure (½–1 day)

Goal: `nix develop && go build ./...` works with `go-libghostty` as a
dependency.

1. Add `flake.nix` and `.envrc` to the repo root.
   - Flake input: ghostty source (pin a tag/commit).
   - Build `libghostty-vt` via Zig + CMake from the input.
   - Dev shell: Go 1.25+, Zig, pkg-config, with `PKG_CONFIG_PATH`
     pointing at the built `libghostty-vt-static.pc`.
   - Reference: go-libghostty's own `flake.nix` is the closest
     template.
2. `go get github.com/mitchellh/go-libghostty@<sha>` — pin a known-good
   commit.
3. Throwaway smoke binary in `cmd/libghostty-smoke/main.go`:
   `NewTerminal`, `VTWrite("hello\r\n")`, read cursor and a cell, print.
   Verifies the toolchain links and the binding works.
4. `.gitignore`: add `result*` (Nix), keep `bin/`.

Exit criterion: `nix develop -c go run ./cmd/libghostty-smoke` prints
the expected output. `go build ./...` is green.

## Milestone 1: libghostty adapter (1–2 days)

Goal: `vt.New` returns a libghostty-backed `Model` that satisfies the
existing interface. Hand-rolled emulator stays compiled but unused.

Files:

- `internal/vt/ghostty.go` — new. Type `ghosttyTerm` wrapping
  `*libghostty.Terminal` and a reusable `*libghostty.RenderState`.
- `internal/vt/types.go` — drop `ColorIndexed`; add `Italic`,
  `Strikethrough` to `Cell`.
- `internal/vt/model.go` — `New` switches construction to
  `ghosttyTerm`.

Tasks:

1. **`Feed(p []byte) error`**: call `t.VTWrite(p)`. Always returns nil.
2. **`Resize(cols, rows int) error`**: call `t.Resize(uint16(cols),
   uint16(rows), 0, 0)` and propagate the error. Pixel dims `0,0` —
   we don't drive image protocols.
3. **`Snapshot() Snapshot`**:
   - Call `rs.Update(t)` to refresh the render state.
   - Read cursor: `CursorX`, `CursorY`, `CursorVisible`.
   - Read alt screen: `t.ActiveScreen() == ScreenAlternate`.
   - Iterate rows via `NewRenderStateRowIterator(rs)`; for each row use
     `NewRenderStateRowCells(row)` and walk cells.
   - For each cell:
     - Width tag → `Cell.Width`:
       - `CellWideNarrow` → 1
       - `CellWideWide` → 2 (the lead cell)
       - `CellWideSpacerTail` → 0 (continuation)
       - `CellWideSpacerHead` → 1, blank text (soft-wrap padding)
     - Text: prefer `cell.Codepoint()` for single-codepoint cells;
       use `cells.Graphemes()` when the cell tag is
       `CellContentCodepointGrapheme` (combining marks, ZWJ
       sequences). Join codepoints into a UTF-8 string.
     - Style → `Cell.Bold`, `Cell.Dim` (← `Faint`), `Cell.Italic`,
       `Cell.Underline` (`s.Underline() != UnderlineNone`),
       `Cell.Inverse`, `Cell.Strikethrough`.
     - Colors → translate `StyleColor` to our `Color`:
       - `StyleColorNone` → `ColorDefault`
       - `StyleColorPalette` → `ColorPalette` with `Index = Palette`
       - `StyleColorRGB` → `ColorRGB` with `R,G,B`
   - Copy everything out before unlocking — borrowed `*GridRef` /
     `*RenderState*` lifetimes are only valid until the next mutating
     call on the terminal, but the pump's `mu` already gates that.
4. **Lifecycle**: `ghosttyTerm` owns the terminal and render state.
   Plumb a `Close()` if the existing `Model` interface needs to grow,
   or finalize via `runtime.SetFinalizer` for now (the cgo allocation
   is small and the terminal is a process-lifetime object in
   practice).

Exit criterion: a hand-written sanity test feeds `"hello\r\n"` and
confirms `VisibleLines()[0] == "hello"` against the libghostty backend.

## Milestone 2: green the existing test suite (1–2 days)

Goal: existing tests serve as the regression oracle. Adjust
expectations only where libghostty is more correct.

1. Run `go test ./internal/vt/`. Triage:
   - **Expected-pass**: `TestPlainText`, `TestCRLF`, `TestBackspace`,
     `TestCursorMovement`, `TestEraseDisplay`, `TestEraseLine`,
     `TestLineWrap`, `TestSGRExplicitZeroResetsAfterBold`,
     `TestSGR256ColorIndexZero`, `TestCSIDeleteChars`,
     `TestCSIInsertChars`, `TestVisibleTextStripsTrailingSpaces`,
     `TestScrollOnOverflow`.
   - **Expectation update**: `TestSGRBoldColor` — `ColorIndexed` is
     gone; assertion becomes `Kind == ColorPalette && Index == 2`.
   - **Verify**: `TestWideChar` — `Cell.Width` mapping (2 + 0
     continuation) survives `VisibleText`'s skip-zero-width rule.
   - **Verify**: `TestCombiningMark` — `Graphemes()` returns
     `[]uint32`; we join into a string. The test checks
     `strings.Contains(row[0].Text, "́")` which should still hold.
   - **Verify**: `TestAltScreen` — confirm `?1049l` returns to primary
     and the primary content is intact in libghostty's model.
   - **Verify**: `TestResize` — libghostty reflows on resize. Existing
     test asserts only that "hello" / "world" survive; should still
     pass, but document the behavior change.
2. Run `go test ./internal/pump/ ./internal/trace/ ./internal/play/`. Trace
   writing stores raw event bytes, and playback bundle parsing should pass
   unchanged.
3. Run `go test ./tuitest/ -run TestMenu` (the real e2e). This is the
   primary proof. Failures here indicate libghostty's parsing of
   Bubble Tea output diverges from the hand-rolled parser; investigate
   case-by-case.
4. Run `go test -race ./...`. We hold a single mutex around all model
   access; libghostty is single-goroutine; should be clean.

Exit criterion: every test in the repo green against the libghostty
backend, with a brief CHANGELOG note for any expectations that moved.

## Milestone 3: cut over (½ day)

Goal: one source of truth.

1. Delete `internal/vt/term.go`.
2. Audit dependencies: drop `github.com/mattn/go-runewidth` and
   `github.com/clipperhouse/uax29/v2` if no longer referenced. Run
   `go mod tidy`.
3. Update `README.md`:
   - Remove the "VT backend" item from the Deviations section.
   - Add a "Build" section: `nix develop` is required;
     vanilla `go build` will fail without `libghostty-vt` on the
     pkg-config path.
   - Note the platform requirement (CGO + Zig toolchain to build
     libghostty-vt).
4. Add a one-line cross-reference at the top of `plan.md` pointing at
   this document.

Exit criterion: `internal/vt/` contains only the libghostty wrapper,
types, visible-text helper, and tests. `go test -race ./...` green in
the dev shell.

## Milestone 4: capabilities unlocked by libghostty (optional, 1–2 days)

These were deferred or stubbed in the original plan; libghostty makes
them cheap. Each is independently mergeable.

1. **DECCKM-aware cursor keys.** Expose `term.Mode(libghostty.ModeDECCKM)`
   from `vt.Model` (extend the interface). `internal/input` reads it
   when encoding `Up/Down/Left/Right` and chooses CSI form (`ESC [ A`)
   vs SS3 form (`ESC O A`). Removes a documented v0 limitation.
2. **Bracketed-paste-aware `Paste`.** Same pattern with
   `ModeBracketedPaste`. Emit markers only when the app enabled them;
   otherwise stream the text. Removes the other documented v0
   limitation.
3. **Scrollback retention.** Add `tuitest.Scrollback(n int)` option;
   wire to `WithMaxScrollback(uint(n))` at construction. Add
   `term.Scrollback() []string` reading via `Point{Tag:
   PointTagHistory}`.
4. **Cell snapshots (Tier 2).** Was M4 in `plan.md`, deferred in the
   POC. Style data is already in the adapter; serializer + comparator
   is straightforward. Default: text + bold + underline; opt-in flag
   for color.
5. **`Title()` accessor.** Bubble Tea apps set it via OSC 0/2; cheap
   to expose for assertions.

Pick based on what real fixtures need. None block M3.

## Milestone 5: CI + reproducibility (½–1 day)

1. GitHub Actions: install Zig, build libghostty-vt, **cache the
   compiled artifact across runs** (this was already in the original
   plan's risk register).
2. Add a macOS runner alongside Linux. Paul's primary dev platform is
   darwin; we should validate it.
3. Flake harness from `plan.md` M5 — 200× menu test under `-race` —
   re-run against the new backend.

Exit criterion: green CI on Linux and macOS; flake test 0% flaky.

## Risks and mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| go-libghostty API churn (no stability promise) | High | Pin commit; isolate to `internal/vt/ghostty.go`; rest of codebase unaffected |
| libghostty-vt build chain flakes in CI | Medium | Cache compiled artifact; pin ghostty source via flake input |
| Subtle behavioral divergence vs. hand-rolled emulator surfaces in the menu fixture | Medium | `term_test.go` is the oracle; M2 adjudicates failures case-by-case |
| Resize reflow semantics change breaks a fixture | Low | Document; existing `TestResize` doesn't trigger reflow |
| Snapshot construction too slow on hot wait loops | Low | `RenderState` is purpose-built for this; profile during M2 menu e2e if it shows |
| API gap: no DECSTBM scrolling-region getter | Low | Not used in v0; file upstream issue if needed later |
| Pure-Go fallback wanted for environments without Zig | Low | Document as v1 ask; the deleted hand-rolled emulator lives in git history |

## Total estimate

5–8 working days for M0–M3 (the actual swap and cleanup). M4–M5 add
another 2–4 days of polish.
