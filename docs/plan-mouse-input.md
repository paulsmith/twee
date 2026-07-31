# Implementation Plan: Mouse Input

**Status:** Planned  
**Date:** 2026-07-31

## Summary

Add cell-coordinate mouse automation to `twee` and `tuitest`:

- click;
- hover;
- vertical wheel scrolling; and
- drag.

The implementation will translate high-level gestures into the mouse protocol
currently selected by the child TUI, then write the encoded reports to the
child PTY. The pinned `go-libghostty` already provides a mouse encoder for X10,
UTF-8, SGR, URxvt, and SGR-Pixels formats, so twee should use that encoder
rather than maintain its own escape-sequence implementation.

The first release is synthetic, application-directed input. It does not add an
attached terminal UI or capture a physical mouse.

## Motivation

Herdr demonstrates how approachable terminal software becomes when mouse
input is treated as a first-class capability. Its attached client uses the
mouse for pane focus, split resizing, context menus, selection, and selective
gesture forwarding:

<https://herdr.dev/docs/quick-start/>

Twee is headless and owns one child TUI rather than a multiplexer layout. The
useful analogue is therefore the ability for a test or agent to send a click,
wheel gesture, hover, or drag to the child at a visible terminal cell.

This also closes the limitation currently documented in `README.md`:

> No mouse input (`click`/`hover`/`drag`). Keys, paste, type, signal only.

## Scope

### In scope

- Zero-based cell coordinates in the visible viewport.
- Left, middle, and right buttons.
- Shift, Alt, and Ctrl modifiers.
- Click, hover, vertical scroll, and drag gestures.
- Tracking modes 9, 1000, 1002, and 1003.
- X10, UTF-8, SGR, and URxvt wire formats.
- CLI, daemon RPC, `run`/`do` scripts, and `tuitest`.
- Mode-aware validation with no partial gesture writes.
- Diagnostics, traces, playback toasts, and export input overlays.
- Headless unit, integration, race, and PTY end-to-end tests.
- Correcting `find` results to use cell coordinates so an agent can safely
  feed a match position into a mouse command.

### Out of scope

- Physical mouse capture in `twee codegen`.
- A Herdr-style attached client, panes, menus, selection, clipboard, or
  hyperlinks.
- Scrolling twee's own terminal viewport or retained scrollback.
- Raw stateful `press` and `release` operations across separate RPCs.
- SGR-Pixels (`DECSET 1016`) until the session has real pixel geometry.
- Horizontal wheel gestures in the first release.
- Unrelated DECCKM, keyboard encoder, or bracketed-paste behavior changes.

## User-facing API

All coordinates are required, zero-based terminal cells:

```text
0 <= x < cols
0 <= y < rows
```

They do not refer to pixels, bytes, grapheme indices, or scrollback lines.

### CLI

```sh
twee click --x N --y N \
  [--button left|middle|right] \
  [--modifier shift|alt|ctrl]...

twee hover --x N --y N \
  [--modifier shift|alt|ctrl]...

twee scroll --x N --y N \
  --direction up|down \
  [--ticks N] \
  [--modifier shift|alt|ctrl]...

twee drag \
  --from-x N --from-y N \
  --to-x N --to-y N \
  [--button left|middle|right] \
  [--modifier shift|alt|ctrl]...
```

Defaults:

- `button`: `left`;
- `ticks`: `1`.

`--modifier` is repeatable. Unknown and duplicate modifiers are invalid.

Successful mouse commands return the same empty success data as `type`, `key`,
and `paste`:

```json
{"ok":true,"data":null}
```

### RPC and scripts

The RPC vocabulary matches the CLI vocabulary:

```json
{"op":"click","args":{"x":12,"y":4}}
{"op":"click","args":{"x":12,"y":4,"button":"right","modifiers":["ctrl"]}}
{"op":"hover","args":{"x":20,"y":8}}
{"op":"scroll","args":{"x":20,"y":8,"direction":"down","ticks":3}}
{"op":"drag","args":{"from_x":4,"from_y":2,"to_x":30,"to_y":12}}
```

All coordinate fields must be presence-checked. Plain Go `int` fields are not
sufficient because an omitted coordinate would otherwise silently become
zero, which is a valid coordinate. Use pointers or another explicit required
field mechanism in RPC argument types. The same rule applies where omission
and an explicit zero have different meanings, such as `ticks`.

### `tuitest`

The public Go surface should mirror the CLI:

```go
term.Click(12, 4)
term.Click(
    12,
    4,
    tuitest.WithButton(tuitest.RightButton),
    tuitest.WithMouseModifiers(tuitest.CtrlModifier),
)

term.Hover(20, 8)
term.Scroll(20, 8, tuitest.ScrollDown, 3)
term.Drag(4, 2, 30, 12)
```

All methods return `error`. An option supplied to a gesture that cannot use it
must fail rather than be ignored.

## Gesture semantics

One high-level gesture is one atomic RPC operation, one logical PTY write, one
diagnostic entry, and one trace input event.

### Click

```text
press(x, y)
release(x, y)
```

Tracking mode 9 legitimately filters the release, so a successful mode-9 click
writes only the press report.

### Hover

```text
motion(x, y, no button)
```

Hover requires any-event tracking mode 1003.

Repeated hover commands at the same cell should each send a report. Do not
deduplicate across separate high-level commands.

### Scroll

Each wheel tick is a button press with no release:

- up: button 4, code 64;
- down: button 5, code 65.

`ticks` must be positive and bounded to prevent an accidental or hostile RPC
from generating an unbounded in-memory report stream. Pick and document a
conservative maximum during implementation.

Horizontal wheel gestures are deferred. Xterm convention uses button 6 for
right and button 7 for left when they are added later.

### Drag

```text
press(start)
motion(each Bresenham cell after start, including end)
release(end)
```

Each motion event carries the pressed button. A zero-length drag should be
treated as a click.

Cell-by-cell Bresenham interpolation is a deterministic twee policy, not a
terminal-protocol requirement.

## Protocol acceptance rules

Validate the whole gesture before writing anything:

| Gesture | Accepted tracking modes |
|---|---|
| Click | 9, 1000, 1002, 1003 |
| Hover | 1003 |
| Scroll | 1000, 1002, 1003 |
| Drag | 1002, 1003 |

Additional rules:

- All gestures fail when mouse tracking is disabled.
- Tracking mode 9 supports left/middle/right press only. Reject modifiers,
  wheel gestures, hover, and drag.
- Legacy X10 format cannot represent arbitrary large coordinates. Reject
  zero-based coordinates above 222 with a protocol-specific precondition
  error rather than accepting libghostty's empty result.
- Reject effective SGR-Pixels format until real pixel geometry is implemented.
- A nil byte slice from `MouseEncoder.Encode` is filtering, not proof of
  success.
- Never let an unsupported drag degrade into press/release without motion.
- Never write a prefix of a gesture and then discover that a later report
  cannot be encoded.

The protocol reference is:

<https://invisible-island.net/xterm/ctlseqs/ctlseqs.html>

## Libghostty prerequisite: effective mouse state

### The problem

The current pinned APIs provide:

- `Terminal.ModeGet`, which exposes individual DECSET bits;
- `Terminal.MouseTracking`, which only says whether any tracking mode is
  active; and
- `MouseEncoder.SetOptFromTerminal`, which copies the terminal's effective
  tracking mode and format into the encoder.

They do not expose the effective tracking or format enum.

Ghostty retains the individual mode bits independently while also storing the
effective tracking and format as scalar state updated by the most recently
processed mode. Consequently, more than one raw mode bit can be true, and a
fixed priority over `ModeGet` booleans can disagree with the encoder.

### Preferred solution

Add effective getters to the libghostty C API and `go-libghostty`:

```go
Terminal.MouseTrackingMode()
Terminal.MouseFormat()
```

Update the Ghostty source pin in `flake.nix` and the Go binding pin in
`go.mod` together. This is a prerequisite for:

- authoritative gesture compatibility validation;
- authoritative SGR-Pixels rejection; and
- stable `mouse_tracking` and `mouse_format` query fields.

### Fallback if the prerequisite is not available

Twee may expose aggregate and raw mode booleans and conservatively reject any
state with the raw 1016 bit enabled. It must not invent an authoritative
effective tracking/format string from a fixed priority rule.

This fallback is safe but may reject a valid state where 1016 remains set and
a later format is effective. The dependency change is therefore preferred.

## Mode query

After the effective getters exist, `twee mode` should always include:

```json
{
  "mouse": true,
  "mouse_tracking": "button",
  "mouse_format": "sgr"
}
```

Tracking values:

```text
none | x10 | normal | button | any
```

Format values:

```text
x10 | utf8 | sgr | urxvt | sgr_pixels
```

Remove `omitempty` from the existing `mouse` boolean so false is explicit for
automation. The format remains meaningful when tracking is `none`.

Do not combine this work with changing DECCKM-aware key encoding or
bracketed-paste behavior. Those can use related mode plumbing in separate
changes.

## Internal architecture

### Semantic input types

Add `internal/input/mouse.go` containing backend-independent:

- actions;
- buttons;
- modifiers;
- normalized mouse events;
- validation helpers;
- display names; and
- high-level gesture expansion.

This package must not expose libghostty types.

### Optional VT capability

Keep the base `vt.Model` interface narrow:

```go
type Model interface {
    Feed([]byte) error
    Resize(cols, rows int) error
    Snapshot() Snapshot
}
```

Add a separate capability implemented by the Ghostty backend, conceptually:

```go
type MouseModel interface {
    EncodeMouse(events []input.MouseEvent) (MouseEncodingResult, error)
}
```

This avoids adding mouse-only methods to playback/export models and test
fakes. `pump.EncodeMouse` should type-assert the capability.

The precise result type should carry the effective tracking/format and enough
per-event information to verify that the required reports were produced.

### Ghostty adapter

`internal/vt/ghostty.go` should own reusable:

- `libghostty.MouseEncoder`; and
- `libghostty.MouseEvent`.

Constructor failure must close every previously allocated C handle. The
finalizer must close the mouse event and encoder before the terminal. An
explicit model `Close` would be preferable longer term, but is not required
for the mouse feature.

For every high-level gesture, while holding the pump mutex:

1. Read the current model dimensions.
2. Read the effective tracking and format.
3. Reject incompatible tracking or SGR-Pixels.
4. Call `SetOptFromTerminal`.
5. Configure synthetic geometry:

   ```text
   ScreenWidth  = cols
   ScreenHeight = rows
   CellWidth    = 1
   CellHeight   = 1
   Padding      = 0
   ```

6. Map cell `(x, y)` to the synthetic cell center `(x+0.5, y+0.5)`.
7. Reset command-local motion-deduplication state.
8. Set `any_button_pressed` explicitly:
   - true for pressed-button drag motion;
   - false for release and cleanup.
9. Clear the reusable event's button for hover.
10. Encode the complete event batch into memory.
11. Verify the expected reports were produced.
12. Return the combined bytes without writing the PTY.

`engine.Config` is not updated by `Resize`, so encoding must read dimensions
from the current VT model rather than cached startup configuration.

### Pump serialization

All access to the libghostty terminal, mouse encoder, and reusable event must
remain under `pump.mu`. Mode inspection and complete gesture encoding must be
one pump-locked operation; separate `Modes()` and `EncodeMouse()` calls would
introduce a time-of-check/time-of-use race with `Feed`.

Release `pump.mu` before writing to the PTY. A potentially blocking PTY write
must not stop the output pump from parsing child output.

### Engine input serialization

The daemon handles connections concurrently, while current input writes are
not serialized and ignore short writes. Add `inputMu` to `engine.Term`.

Use this lock order consistently:

```text
inputMu
  -> pump.mu for inspection and complete encoding
  -> release pump.mu
  -> short-write-safe PTY write
  -> diagnostic and trace bookkeeping
  -> release inputMu
```

Put these operations under `inputMu`:

- `Type`;
- `Key`;
- `Paste`;
- `Click`;
- `Hover`;
- `Scroll`;
- `Drag`; and
- `Resize`.

This prevents a resize or concurrent input RPC from interleaving with a
gesture. Pump code must never try to acquire `inputMu`.

Refactor existing input methods through one write-all helper so every logical
input is contiguous.

## Error model

Add typed engine errors so daemon handlers do not map every mouse failure to
`IO`.

### `INVALID_ARGUMENT`

- Missing coordinate.
- Coordinate outside the visible viewport.
- Unknown or duplicate button/modifier.
- Invalid direction.
- Zero, negative, or excessive scroll ticks.
- Invalid drag endpoint.

Coordinate failures should include structured details:

```json
{
  "x": 80,
  "y": 4,
  "cols": 80,
  "rows": 24
}
```

### `FAILED_PRECONDITION`

Add this code to the closed RPC error set.

Use it for:

- mouse tracking disabled;
- gesture incompatible with the effective tracking mode;
- modifier requested under tracking mode 9;
- legacy coordinate not representable;
- effective SGR-Pixels without real geometry; and
- the active VT backend not supporting mouse encoding.

Example details:

```json
{
  "gesture": "drag",
  "mouse_tracking": "normal",
  "required_tracking": ["button", "any"]
}
```

### `IO`

Reserve for actual PTY write failures.

Failed validation or encoding must write zero bytes and create no diagnostic
or trace event.

## `find` coordinate correction

`handleFind` currently searches rendered line strings with Go string
operations. Its `x` and `w` are therefore byte offsets, whereas `cell`,
`region`, `cursor`, and mouse input address VT cells.

Change `find` to map text matches back to terminal cell spans. Test at least:

- ASCII;
- multibyte narrow characters;
- double-width characters;
- combining graphemes; and
- a match beginning after one or more non-ASCII cells.

The result must be safe to use directly:

```sh
match="$(twee find --pattern Submit)"
twee click \
  --x "$(jq -r '.data[0].x' <<<"$match")" \
  --y "$(jq -r '.data[0].y' <<<"$match")"
```

## RPC, CLI, and public API wiring

### RPC

Add:

- `OpClick`;
- `OpHover`;
- `OpScroll`; and
- `OpDrag`.

Add presence-aware argument types and handlers beside the current input
handlers. Strict unknown-field decoding remains mandatory.

`run` and `do` gain the operations through normal dispatcher registration;
they should not require a separate execution path.

### CLI

Add `cmd/twee/cmd_mouse.go` with:

- command registration;
- complete static help;
- duplicate-flag checks;
- presence validation;
- button/modifier parsing; and
- RPC construction.

Also update central integration points:

- `commandSummaries` in `cmd/twee/main.go`;
- the global `--name` verb whitelist in `cmd/twee/arg_parser.go`;
- top-level command tests; and
- completion scaffolding when it becomes functional.

### `tuitest`

Expose aliases/options in `tuitest/input.go` and add the high-level methods to
the public `Term`.

Do not expose libghostty enums or wire-format details.

## Tracing, playback, and export

Record one successful high-level gesture as:

```json
{
  "type": "input",
  "kind": "mouse",
  "bytes_b64": "...",
  "mouse": {
    "gesture": "click",
    "x": 12,
    "y": 4,
    "button": "left",
    "modifiers": []
  }
}
```

The nested mouse object avoids ambiguity around valid zero coordinates.

Update:

- the private trace JSON event;
- `play.Event`;
- the duplicate JSON decoder shape in `internal/play/bundle.go`;
- trace/bundle tests; and
- `FormatEventToast`.

Examples:

```text
[01.250s] -> click left @(12,4)
[02.100s] -> scroll down x3 @(20,8)
[03.000s] -> drag left (4,2)->(30,12)
```

Playback must not feed mouse input bytes back into the VT model. Input events
remain annotations. Export already reuses the playback toast formatter and
forces a frame for qualifying input events, so it should not gain a separate
mouse formatter.

Keep the trace bundle at version 1 if the new fields remain optional and old
bundles continue to decode. Add an explicit old-bundle compatibility test.

## Test strategy

The feature is testable in the exe.dev VM over SSH. Twee creates its own PTY
for the child, independent of the terminal used for the SSH connection. No
display server or physical mouse is required for cell-based application mouse
input.

### Mouse fixture

Create a dependency-free Go program under `fixtures/mouse` that:

1. Verifies stdin is a TTY.
2. Puts stdin in raw mode with `golang.org/x/term`.
3. Enables a requested tracking mode and format.
4. Writes `READY` only after writing its DECSET sequences.
5. Parses mouse reports from a byte stream.
6. Prints normalized received events for test assertions.
7. Restores terminal modes before exit.

The parser must handle escape sequences split across reads and multiple
reports coalesced into one read. It must not assume one `Read` equals one
mouse event.

Waiting for `READY` ensures the output pump has parsed the mode changes before
the first mouse RPC.

### Validation matrix

| Layer | Cases | Assertions |
|---|---|---|
| Gesture unit tests | Click, hover, horizontal/vertical/diagonal drag, zero-length drag, scroll N | Exact normalized events, buttons, modifiers, endpoints, bounded expansion |
| Tracking filters | None, 9, 1000, 1002, 1003 | No partial gestures; click count; hover/drag/scroll eligibility |
| Wire formats | X10, UTF-8, SGR, URxvt | Exact press/release/motion/wheel bytes |
| Coordinates | `(0,0)`, bottom-right, negative, equal-to-size, post-resize, X10 222/223 boundary | Correct 1-based wire values or typed rejection |
| Modifiers | Shift, Alt, Ctrl, combinations | Correct `+4`, `+8`, `+16`; mode-9 rejection |
| Wheel | Up/down, three ticks | Codes 64/65, exactly N presses, no releases |
| Pixel mode | 1003 plus 1016 | `FAILED_PRECONDITION`, zero PTY bytes |
| Mode transitions | Enable/disable modes; multiple bits in different orders | Encoder follows effective last mode; query is truthful |
| Encoder state | Failed drag, click then hover, repeated hover, resize/mode changes | No stale button, position, deduplication, or geometry state |
| Write serialization | Short writer, concurrent input and resize | Write-all behavior, contiguous logical inputs, race-clean |
| Trace | Every gesture, failed gesture, old bundle | One event per success, exact bytes, no failed event, compatibility |
| Play/export | Mouse toasts | Stable text and forced overlay frame |
| RPC/CLI/script | Missing/unknown fields, bad values, `do`, `run --script` | Strict validation and consistent errors |
| PTY E2E | SGR 1003 fixture, X10 click, UTF-8 large coordinate, URxvt release, 1016 rejection | Child receives intended reports through a real raw PTY |

### Required commands

Run all Go builds and tests inside the Nix development shell:

```sh
nix develop -c go test ./...
nix develop -c go test -race ./...
```

No Bash fixture is planned. If a Bash script is added during implementation,
run `shellcheck` as required by `AGENTS.md`.

### What cannot be validated in this SSH session

- Physical host-mouse capture for `codegen`.
- SGR-Pixels against real terminal pixel geometry.
- A Herdr-style attached terminal UI.
- macOS-specific PTY behavior or multiple real terminal emulators.

Those items are explicitly deferred and do not block the cell-based feature.

## Milestones

### Milestone 0: Effective libghostty mouse state

Goal: make effective tracking and format queryable.

Work:

1. Add effective getters to libghostty's C API.
2. Wrap them in `go-libghostty`.
3. Add upstream tests for multiple mode bits set in different orders.
4. Update twee's Ghostty source and Go binding pins together.
5. Add a twee smoke test for the new getters.

Exit criteria:

- Effective tracking and format match `SetOptFromTerminal`.
- Tests cover last-set mode behavior.
- The Nix shell and all existing twee tests remain green.

### Milestone 1: Semantic input and Ghostty capability

Goal: encode complete validated mouse gestures without writing a PTY.

Work:

1. Add semantic mouse types and gesture expansion.
2. Add optional `vt.MouseModel`.
3. Create and clean up Ghostty mouse handles.
4. Implement synthetic geometry and cell-center mapping.
5. Implement mode validation, button state, and complete batch encoding.
6. Add exact-byte and state-reset unit tests.

Exit criteria:

- Every supported gesture produces the expected reports.
- Unsupported gestures return typed errors and no partial bytes.
- Resize updates geometry.

### Milestone 2: Atomic engine and RPC

Goal: write one complete gesture safely to the child PTY.

Work:

1. Add `inputMu`.
2. Add a short-write-safe input helper.
3. Put existing input and resize operations under the input lock.
4. Add engine mouse methods.
5. Add typed engine errors and RPC error mapping.
6. Add RPC operations, presence-aware args, and handlers.

Exit criteria:

- Concurrent inputs never interleave.
- A failed gesture writes and records nothing.
- RPC errors distinguish invalid arguments, failed preconditions, and I/O.

### Milestone 3: CLI, `tuitest`, and cell-correct `find`

Goal: expose a consistent public automation surface.

Work:

1. Add four CLI verbs and central registrations.
2. Add parser and help tests.
3. Add `tuitest` methods/options.
4. Verify `run` and `do` scripts.
5. Convert `find` offsets and widths to cell coordinates.
6. Add Unicode/wide-cell `find` tests.

Exit criteria:

- CLI, RPC, scripts, and `tuitest` use the same semantics.
- A `find` result can be passed directly to `click`.

### Milestone 4: Trace, fixture, E2E, and docs

Goal: make the feature observable, reproducible, and documented.

Work:

1. Add structured trace metadata.
2. Update bundle decoding and toast formatting.
3. Add compatibility, playback, and export tests.
4. Add the streaming mouse fixture.
5. Add end-to-end tests across representative modes/formats.
6. Update README command tables, examples, mode output, JSON schema, error
   table, trace documentation, and limitations.

Exit criteria:

- Full and race test suites pass in the Nix shell.
- The real PTY fixture receives intended gestures.
- README no longer lists cell-based mouse input as unsupported.
- Deferred host-capture, scrollback, horizontal wheel, and pixel-mode work is
  documented.

## Expected effort

Estimate: five twee changes plus the libghostty API addition, approximately
5–7 engineering days excluding upstream review latency.

Suggested landing order:

1. Libghostty effective-state getters and twee pin update.
2. Semantic input plus optional VT capability.
3. Engine serialization plus RPC.
4. CLI, `tuitest`, and `find`.
5. Trace, E2E tests, and documentation.

Each stage should leave the repository buildable and tested.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| Raw DECSET bits disagree with effective encoder state | Add authoritative libghostty getters; do not infer priority |
| Pixel-space encoder receives cell coordinates | Configure explicit synthetic 1x1-cell geometry |
| Unsupported event silently encodes to no bytes | Preflight and verify the entire gesture before one PTY write |
| Drag degrades into a click | Require 1002/1003 and verify every required motion report |
| Concurrent RPCs interleave gesture reports | Serialize all logical input with `inputMu` |
| Resize races coordinate validation | Put resize under the same input lock and read live VT dimensions |
| Reusable encoder leaks button/event state | Explicitly set/clear all state and test command transitions |
| Missing JSON coordinates become `(0,0)` | Use presence-aware RPC fields |
| `find` targets wrong cells after Unicode | Return cell offsets/widths rather than byte offsets |
| Trace consumers drift | Update both trace writer and play decoder; retain old-bundle test |
| Test environment lacks a physical mouse | Exercise the complete synthetic path through an internal raw PTY |

## Deferred follow-ups

### SGR-Pixels

Supporting mode 1016 requires:

- real PTY `ws_xpixel` and `ws_ypixel`;
- cell pixel dimensions passed to `Terminal.Resize`;
- start/resize configuration and trace metadata; and
- a public decision between accepting pixel coordinates and mapping a cell to
  a pixel corner or center.

### `codegen` host-mouse capture

Interactive recording would require `codegen` to:

- enable mouse reporting in the user's outer terminal;
- distinguish its own controls from child-directed mouse reports;
- translate host geometry into child cells; and
- restore terminal modes reliably on exit, signal, and panic.

It should be designed and tested separately.

### Viewport scrolling

When an application has not enabled mouse reporting, a real terminal normally
uses the wheel for its own history. Twee does not retain scrollback today.
Application-directed `twee scroll` must not pretend to implement viewport
scrolling; that belongs with future scrollback retention.

### Raw mouse events

Low-level press/release/motion APIs would require ownership and cleanup rules
for held buttons across concurrent clients, timeouts, disconnects, and failed
scripts. Keep v1 gesture-level and atomic.
