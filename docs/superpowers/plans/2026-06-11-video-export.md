# Video Export Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `twee export <bundle.twee> -o out.{gif,mp4,webm}` — headless replay of a recording into an animated GIF (pure stdlib) or MP4/WebM (external ffmpeg), one frame per visible screen change.

**Architecture:** New package `internal/export` walks the event stream with a logical clock (fps-cap merge windows, SHA256 dedup excluding cursor), renders changed snapshots via `internal/render`, composites them onto a fixed letterbox canvas, and feeds `(image, duration)` pairs to a `sink` (gifSink or ffmpegSink). The CLI verb lives in `cmd/twee/cmd_export.go`.

**Tech Stack:** Go stdlib only (`image/gif`, `image/draw`, `archive/zip` via existing `play.OpenBundle`); ffmpeg as an optional runtime external binary. Build/test inside the Nix dev shell: `nix develop -c go test ./...`.

**Spec:** `docs/superpowers/specs/2026-06-11-video-export-design.md` — read it before starting.

**Version control:** This repo uses jj, never raw git. Commit with `jj commit -m "<msg>"` (the working copy is automatically the commit; no staging step).

---

## File Structure

- Modify: `internal/play/loop.go` — rename `engineSnapshot` → `EngineSnapshot` (export for reuse)
- Create: `internal/export/replay.go` — logical-clock walker producing `(vt.Snapshot, duration)` frames
- Create: `internal/export/replay_test.go`
- Create: `internal/export/canvas.go` — render + letterbox compositor (even-padded fixed canvas)
- Create: `internal/export/canvas_test.go`
- Create: `internal/export/sink.go` — `sink` interface
- Create: `internal/export/gif.go` + `internal/export/gif_test.go` — gifSink
- Create: `internal/export/ffmpeg.go` + `internal/export/ffmpeg_test.go` — ffmpegSink
- Create: `internal/export/export.go` — `Export(path string, opts Options) error` entry point
- Create: `internal/export/export_test.go`
- Create: `cmd/twee/cmd_export.go` + `cmd/twee/cmd_export_test.go` — CLI verb

---

### Task 1: Export `EngineSnapshot` from internal/play

`internal/export` needs the `vt.Snapshot` → `engine.Snapshot` conversion that is currently private to the play loop.

**Files:**
- Modify: `internal/play/loop.go` (functions `engineSnapshot` at ~line 367)

- [ ] **Step 1: Rename and document**

In `internal/play/loop.go`, rename `engineSnapshot` to `EngineSnapshot` and add a doc comment:

```go
// EngineSnapshot converts a VT snapshot to the renderer's input type.
func EngineSnapshot(s vt.Snapshot) engine.Snapshot {
```

Update the single caller in `loop.go` (`emitFrame`, the line `es := engineSnapshot(snap)`) and any callers in `internal/play/*_test.go` (search: `grep -rn engineSnapshot internal/play cmd`). Leave `fromVTColor` unexported — it is only called from `EngineSnapshot`.

- [ ] **Step 2: Build and test**

Run: `nix develop -c go test ./internal/play/ ./cmd/twee/`
Expected: PASS (pure rename, no behavior change)

- [ ] **Step 3: Commit**

```bash
jj commit -m "play: export EngineSnapshot for reuse by export"
```

---

### Task 2: Replay walker (`internal/export/replay.go`)

The core timing/dedup logic. Pure: takes events and a callback, no I/O, no rendering. TDD this carefully — it is where all the spec's timing rules live.

**Files:**
- Create: `internal/export/replay.go`
- Create: `internal/export/replay_test.go`

**Semantics (from the spec):**
- Adjusted timeline: each inter-event trace-time gap is capped at `MaxIdle` (when > 0), then the running total is divided by `Speed`. Equivalently: `adjT[i] = adjT[i-1] + min(rawGap, maxIdle)/speed`.
- fps-cap is a **merge window**: snapshot/hash at most once per `1s/FPSCap` of adjusted time. All events whose adjusted time falls inside the current window are drained before snapshotting; the snapshot is taken at the window's end time.
- Dedup hash excludes cursor position/visibility.
- The pending frame is emitted when the hash changes; its duration is `newFrameT - pendingFrameT`.
- Initial pending frame: the blank model snapshot at adjusted t=0.
- Trailing frame: duration from pending frame time to the adjusted end time (exit event or last event), capped at 3s, floored at one window.

- [ ] **Step 1: Write failing tests**

Create `internal/export/replay_test.go`:

```go
package export

import (
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/play"
	"github.com/paulsmith/twee/internal/vt"
)

// fakeModel records fed bytes and exposes a settable snapshot. Snapshot
// content is keyed off a generation counter so tests control when the
// screen "changes".
type fakeModel struct {
	gen  int
	cur  vt.Cursor
	cols int
	rows int
}

func (m *fakeModel) Feed(p []byte) error {
	// Convention for tests: feeding "x" changes the screen; feeding
	// "c" moves only the cursor.
	for _, b := range p {
		switch b {
		case 'x':
			m.gen++
		case 'c':
			m.cur.Col++
		}
	}
	return nil
}

func (m *fakeModel) Resize(cols, rows int) error { m.cols, m.rows = cols, rows; return nil }

func (m *fakeModel) Snapshot() vt.Snapshot {
	return vt.Snapshot{
		Size:   vt.Size{Cols: m.cols, Rows: m.rows},
		Cursor: m.cur,
		Lines:  []vt.Line{{Cells: []vt.Cell{{Text: string(rune('a' + m.gen%26)), Width: 1}}}},
	}
}

type frame struct {
	snap vt.Snapshot
	d    time.Duration
}

func collect(t *testing.T, events []play.Event, opts Options) []frame {
	t.Helper()
	var out []frame
	err := replay(events, 80, 24, opts,
		func(cols, rows int) vt.Model { return &fakeModel{cols: cols, rows: rows} },
		func(s vt.Snapshot, d time.Duration) error {
			out = append(out, frame{s, d})
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func out(tms int64, s string) play.Event {
	return play.Event{TMS: tms, Type: "output", Bytes: []byte(s)}
}

func TestReplayEmitsFrameOnChange(t *testing.T) {
	frames := collect(t, []play.Event{out(1000, "x"), out(3000, "x")}, Options{})
	// blank frame [0,1s), frame A [1s,3s), trailing frame B
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3", len(frames))
	}
	if frames[0].d != time.Second {
		t.Errorf("blank frame duration = %v, want 1s", frames[0].d)
	}
	if frames[1].d != 2*time.Second {
		t.Errorf("frame A duration = %v, want 2s", frames[1].d)
	}
}

func TestReplayCursorOnlyChangeIsDeduped(t *testing.T) {
	frames := collect(t, []play.Event{out(1000, "x"), out(2000, "c"), out(4000, "x")}, Options{})
	// The cursor move at t=2s must NOT split frame A.
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3", len(frames))
	}
	if frames[1].d != 3*time.Second {
		t.Errorf("frame A duration = %v, want 3s (cursor move merged)", frames[1].d)
	}
}

func TestReplayFPSCapMergesBursts(t *testing.T) {
	// 10 changes within 100ms at 30fps cap → at most ceil(100ms/33.3ms)+1
	// emitted frames, and total duration preserved.
	var evs []play.Event
	for i := 0; i < 10; i++ {
		evs = append(evs, out(int64(1000+i*10), "x"))
	}
	frames := collect(t, evs, Options{FPSCap: 30})
	if len(frames) > 6 {
		t.Errorf("got %d frames, want <= 6 (burst must collapse)", len(frames))
	}
	var total time.Duration
	for _, f := range frames {
		total += f.d
	}
	// end = last event adjusted time = 1.09s, trailing floor = one window.
	want := 1090*time.Millisecond + time.Second/30
	if total != want {
		t.Errorf("total duration = %v, want %v", total, want)
	}
}

func TestReplaySpeedAndMaxIdle(t *testing.T) {
	// gap of 10s capped to 2s, then /2 speed → 1s.
	frames := collect(t, []play.Event{out(0, "x"), out(10000, "x")},
		Options{Speed: 2, MaxIdle: 2 * time.Second})
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(frames))
	}
	if frames[0].d != time.Second {
		t.Errorf("frame duration = %v, want 1s (10s gap → maxIdle 2s → /2)", frames[0].d)
	}
}

func TestReplayTrailingCap(t *testing.T) {
	// 10 minutes of idle before exit → trailing frame capped at 3s.
	evs := []play.Event{out(0, "x"), {TMS: 600000, Type: "exit"}}
	frames := collect(t, evs, Options{})
	last := frames[len(frames)-1]
	if last.d != 3*time.Second {
		t.Errorf("trailing duration = %v, want 3s cap", last.d)
	}
}

func TestReplayResizeUpdatesModel(t *testing.T) {
	evs := []play.Event{{TMS: 0, Type: "resize", Cols: 100, Rows: 30}, out(100, "x")}
	frames := collect(t, evs, Options{})
	last := frames[len(frames)-1].snap
	if last.Size.Cols != 100 || last.Size.Rows != 30 {
		t.Errorf("snapshot size = %dx%d, want 100x30", last.Size.Cols, last.Size.Rows)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `nix develop -c go test ./internal/export/`
Expected: FAIL — `undefined: Options`, `undefined: replay`

- [ ] **Step 3: Implement `replay.go`**

```go
// Package export renders .twee recordings to video files (GIF, MP4, WebM).
package export

import (
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/paulsmith/twee/internal/play"
	"github.com/paulsmith/twee/internal/vt"
)

// Options controls an export run.
type Options struct {
	Speed    float64       // playback speed multiplier; default 1.0
	MaxIdle  time.Duration // cap per-gap idle trace time; 0 = faithful
	FontSize float64       // render font size in points; default 14
	FPSCap   int           // max frames per second of logical time; default 30
	FFmpeg   string        // ffmpeg binary; default looked up on PATH
}

const trailingCap = 3 * time.Second

func (o *Options) normalize() {
	if o.Speed <= 0 {
		o.Speed = 1
	}
	if o.FontSize <= 0 {
		o.FontSize = 14
	}
	if o.FPSCap <= 0 {
		o.FPSCap = 30
	}
}

// replay walks events on a logical clock and calls emit with each visibly
// distinct frame and its display duration. Frames arrive in order and
// durations sum to the adjusted length of the recording (idle tail capped).
func replay(events []play.Event, cols, rows int, opts Options,
	newModel func(int, int) vt.Model,
	emit func(vt.Snapshot, time.Duration) error) error {

	opts.normalize()
	window := time.Second / time.Duration(opts.FPSCap)
	model := newModel(cols, rows)

	// Adjusted timeline: cap each raw gap at MaxIdle, divide by Speed.
	adjusted := make([]time.Duration, len(events))
	var prev, adj time.Duration
	for i, ev := range events {
		gap := ev.TraceTime() - prev
		if gap < 0 {
			gap = 0
		}
		if opts.MaxIdle > 0 && gap > opts.MaxIdle {
			gap = opts.MaxIdle
		}
		prev = ev.TraceTime()
		adj += time.Duration(float64(gap) / opts.Speed)
		adjusted[i] = adj
	}

	pendingSnap := model.Snapshot()
	pendingHash := hashNoCursor(pendingSnap)
	var pendingT time.Duration

	// checkpoint snapshots at adjusted time t and emits the pending frame
	// if the screen visibly changed.
	checkpoint := func(t time.Duration) error {
		snap := model.Snapshot()
		h := hashNoCursor(snap)
		if h == pendingHash {
			return nil
		}
		if err := emit(pendingSnap, t-pendingT); err != nil {
			return err
		}
		pendingSnap, pendingHash, pendingT = snap, h, t
		return nil
	}

	i := 0
	for i < len(events) {
		// Drain every event inside the current fps-cap window, then
		// snapshot once at the window's end.
		windowEnd := adjusted[i] - adjusted[i]%window + window
		for i < len(events) && adjusted[i] < windowEnd {
			if err := apply(model, events[i]); err != nil {
				return err
			}
			i++
		}
		if err := checkpoint(windowEnd); err != nil {
			return err
		}
	}

	end := time.Duration(0)
	if len(events) > 0 {
		end = adjusted[len(events)-1]
	}
	tail := end - pendingT
	if tail > trailingCap {
		tail = trailingCap
	}
	if tail < window {
		tail = window
	}
	return emit(pendingSnap, tail)
}

func apply(model vt.Model, ev play.Event) error {
	switch ev.Type {
	case "output":
		return model.Feed(ev.Bytes)
	case "resize":
		if ev.Cols > 0 && ev.Rows > 0 {
			return model.Resize(ev.Cols, ev.Rows)
		}
	}
	// input and exit events do not affect the screen.
	return nil
}

// hashNoCursor hashes the snapshot with the cursor zeroed: the renderer
// does not draw the cursor, so cursor-only movement must not create frames.
func hashNoCursor(s vt.Snapshot) [32]byte {
	s.Cursor = vt.Cursor{}
	b, _ := json.Marshal(s)
	return sha256.Sum256(b)
}
```

Note: `play.Event.traceTime()` is unexported, so the code above calls a new exported accessor. Add it in `internal/play/bundle.go` right below `traceTime`:

```go
// TraceTime returns the event's timestamp as a duration from session start.
func (e Event) TraceTime() time.Duration { return e.traceTime() }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `nix develop -c go test ./internal/export/ ./internal/play/`
Expected: PASS. If a timing test fails, re-check the window-boundary arithmetic against the test's expected values before touching the tests — the tests encode the spec.

- [ ] **Step 5: Commit**

```bash
jj commit -m "export: logical-clock replay walker with fps-cap windows and dedup"
```

---

### Task 3: Letterbox canvas compositor (`internal/export/canvas.go`)

Renders each snapshot at a fixed font size and centers it on a fixed, even-dimensioned black canvas sized for the bundle's max grid. `render.Render` stretches when given pixel dims, so padding happens here.

**Files:**
- Create: `internal/export/canvas.go`
- Create: `internal/export/canvas_test.go`

- [ ] **Step 1: Write failing tests**

```go
package export

import (
	"image/color"
	"testing"

	"github.com/paulsmith/twee/internal/vt"
)

func snapOfSize(cols, rows int) vt.Snapshot {
	lines := make([]vt.Line, rows)
	for i := range lines {
		cells := make([]vt.Cell, cols)
		for j := range cells {
			cells[j] = vt.Cell{Text: "M", Width: 1,
				Fg: vt.Color{Kind: vt.ColorRGB, R: 255, G: 255, B: 255},
				Bg: vt.Color{Kind: vt.ColorRGB, R: 255, G: 0, B: 0}}
		}
		lines[i] = vt.Line{Cells: cells}
	}
	return vt.Snapshot{Size: vt.Size{Cols: cols, Rows: rows}, Lines: lines}
}

func TestCanvasDimensionsAreEven(t *testing.T) {
	c, err := newCanvas(81, 25, 14) // odd cell counts likely → odd pixels
	if err != nil {
		t.Fatal(err)
	}
	b := c.bounds()
	if b.Dx()%2 != 0 || b.Dy()%2 != 0 {
		t.Errorf("canvas %dx%d: dimensions must be even", b.Dx(), b.Dy())
	}
}

func TestCanvasLetterboxesSmallerSnapshot(t *testing.T) {
	c, err := newCanvas(80, 24, 14)
	if err != nil {
		t.Fatal(err)
	}
	img, err := c.compose(snapOfSize(40, 12)) // half-size grid, red bg
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != c.bounds() {
		t.Fatalf("frame bounds %v != canvas bounds %v", img.Bounds(), c.bounds())
	}
	// Corner must be letterbox black; center must be content red.
	if got := img.RGBAAt(0, 0); got != (color.RGBA{0, 0, 0, 255}) {
		t.Errorf("corner = %v, want black", got)
	}
	cx, cy := img.Bounds().Dx()/2, img.Bounds().Dy()/2
	if got := img.RGBAAt(cx, cy); got.R < 200 || got.G > 50 {
		t.Errorf("center = %v, want red content", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `nix develop -c go test ./internal/export/`
Expected: FAIL — `undefined: newCanvas`

- [ ] **Step 3: Implement `canvas.go`**

```go
package export

import (
	"image"
	"image/color"
	"image/draw"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/play"
	"github.com/paulsmith/twee/internal/render"
	"github.com/paulsmith/twee/internal/vt"
)

// canvas composes per-snapshot renders onto a fixed black background sized
// for the recording's largest grid, padded to even pixel dimensions (the
// yuv420p pixel format requires even width and height).
type canvas struct {
	w, h     int
	fontSize float64
}

func newCanvas(maxCols, maxRows int, fontSize float64) (*canvas, error) {
	// Render an empty snapshot of the max grid to learn pixel dimensions
	// at this font size; render.Render computes them deterministically.
	probe, err := render.Render(engine.Snapshot{Cols: maxCols, Rows: maxRows},
		render.Options{SizePt: fontSize})
	if err != nil {
		return nil, err
	}
	w, h := probe.Bounds().Dx(), probe.Bounds().Dy()
	return &canvas{w: w + w%2, h: h + h%2, fontSize: fontSize}, nil
}

func (c *canvas) bounds() image.Rectangle { return image.Rect(0, 0, c.w, c.h) }

func (c *canvas) compose(snap vt.Snapshot) (*image.RGBA, error) {
	content, err := render.Render(play.EngineSnapshot(snap),
		render.Options{SizePt: c.fontSize})
	if err != nil {
		return nil, err
	}
	out := image.NewRGBA(c.bounds())
	draw.Draw(out, out.Bounds(), &image.Uniform{C: color.RGBA{0, 0, 0, 255}},
		image.Point{}, draw.Src)
	cb := content.Bounds()
	off := image.Pt((c.w-cb.Dx())/2, (c.h-cb.Dy())/2)
	draw.Draw(out, cb.Add(off), content, cb.Min, draw.Src)
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `nix develop -c go test ./internal/export/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
jj commit -m "export: letterbox canvas compositor with even-dimension padding"
```

---

### Task 4: Sink interface and gifSink

**Files:**
- Create: `internal/export/sink.go`
- Create: `internal/export/gif.go`
- Create: `internal/export/gif_test.go`

- [ ] **Step 1: Write `sink.go`** (interface only, no test needed)

```go
package export

import (
	"image"
	"time"
)

// sink consumes composed frames and writes the output file on close.
type sink interface {
	add(img *image.RGBA, d time.Duration) error
	close() error
}
```

- [ ] **Step 2: Write failing gifSink tests**

```go
package export

import (
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func solidFrame(c color.RGBA, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestGIFSinkRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.gif")
	s, err := newGIFSink(path)
	if err != nil {
		t.Fatal(err)
	}
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	if err := s.add(solidFrame(red, 10, 10), 500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := s.add(solidFrame(blue, 10, 10), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := s.close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 2 {
		t.Fatalf("got %d frames, want 2", len(g.Image))
	}
	if g.Delay[0] != 50 || g.Delay[1] != 100 {
		t.Errorf("delays = %v, want [50 100] (centiseconds)", g.Delay)
	}
	// Exact-color path: a solid frame has 1 color; decoded pixel must be exact.
	if got := g.Image[0].At(5, 5); !sameColor(got, red) {
		t.Errorf("frame 0 pixel = %v, want exact red (no dithering)", got)
	}
}

func sameColor(a color.Color, b color.RGBA) bool {
	r, g, bl, _ := a.RGBA()
	return uint8(r>>8) == b.R && uint8(g>>8) == b.G && uint8(bl>>8) == b.B
}

func TestGIFSinkDelayRemainderCarry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.gif")
	s, err := newGIFSink(path)
	if err != nil {
		t.Fatal(err)
	}
	// 3 frames × 33.33ms: naive rounding gives 3+3+3=9cs; carry gives
	// 3+3+4=10cs (total 100ms preserved).
	red := solidFrame(color.RGBA{255, 0, 0, 255}, 4, 4)
	green := solidFrame(color.RGBA{0, 255, 0, 255}, 4, 4)
	d := 33333333 * time.Nanosecond
	frames := []*image.RGBA{red, green, red}
	for _, f := range frames {
		if err := s.add(f, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.close(); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(path)
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, dl := range g.Delay {
		total += dl
	}
	if total != 10 {
		t.Errorf("total delay = %dcs, want 10cs (remainder carry)", total)
	}
}

func TestGIFSinkClampsMinimumDelay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.gif")
	s, _ := newGIFSink(path)
	red := solidFrame(color.RGBA{255, 0, 0, 255}, 4, 4)
	if err := s.add(red, 5*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := s.close(); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(path)
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if g.Delay[0] < 2 {
		t.Errorf("delay = %dcs, want >= 2 (browser minimum)", g.Delay[0])
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `nix develop -c go test ./internal/export/`
Expected: FAIL — `undefined: newGIFSink`

- [ ] **Step 4: Implement `gif.go`**

```go
package export

import (
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"os"
	"time"
)

// gifSink accumulates paletted frames (1 byte/pixel) and encodes the GIF on
// close — stdlib image/gif has no streaming append API, so memory scales
// with frame count. Change-driven export keeps that modest in practice.
type gifSink struct {
	path  string
	g     gif.GIF
	carry time.Duration // sub-centisecond remainder carried between frames
}

func newGIFSink(path string) (*gifSink, error) {
	// Fail early on an unwritable path rather than after rendering.
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return &gifSink{path: path}, nil
}

func (s *gifSink) add(img *image.RGBA, d time.Duration) error {
	s.g.Image = append(s.g.Image, palettize(img))
	total := d + s.carry
	cs := int(total / (10 * time.Millisecond))
	s.carry = total - time.Duration(cs)*10*time.Millisecond
	if cs < 2 {
		cs = 2 // browsers treat 0–1 as 100ms
	}
	s.g.Delay = append(s.g.Delay, cs)
	s.g.Disposal = append(s.g.Disposal, gif.DisposalNone)
	return nil
}

func (s *gifSink) close() error {
	f, err := os.Create(s.path)
	if err != nil {
		return err
	}
	if err := gif.EncodeAll(f, &s.g); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// palettize converts to a paletted frame. Terminal frames almost always use
// ≤256 distinct colors, so an exact palette is tried first (sharper than
// dithering); otherwise fall back to Floyd–Steinberg on the Plan9 palette.
func palettize(img *image.RGBA) *image.Paletted {
	b := img.Bounds()
	seen := make(map[color.RGBA]int)
	var pal color.Palette
	exact := true
	for y := b.Min.Y; y < b.Max.Y && exact; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if _, ok := seen[c]; !ok {
				if len(pal) == 256 {
					exact = false
					break
				}
				seen[c] = len(pal)
				pal = append(pal, c)
			}
		}
	}
	if !exact {
		out := image.NewPaletted(b, palette.Plan9)
		draw.FloydSteinberg.Draw(out, b, img, b.Min)
		return out
	}
	out := image.NewPaletted(b, pal)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out.SetColorIndex(x, y, uint8(seen[img.RGBAAt(x, y)]))
		}
	}
	return out
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `nix develop -c go test ./internal/export/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
jj commit -m "export: GIF sink with exact-color palette and delay remainder carry"
```

---

### Task 5: ffmpegSink

**Files:**
- Create: `internal/export/ffmpeg.go`
- Create: `internal/export/ffmpeg_test.go`

- [ ] **Step 1: Write failing tests**

```go
package export

import (
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConcatListFormat(t *testing.T) {
	s := &ffmpegSink{}
	s.noteFrame("frame-000001.png", 500*time.Millisecond)
	s.noteFrame("frame-000002.png", 1250*time.Millisecond)
	got := s.concatList()
	want := strings.Join([]string{
		"file frame-000001.png",
		"duration 0.500000",
		"file frame-000002.png",
		"duration 1.250000",
		"file frame-000002.png", // concat demuxer drops the final
		"",                      // duration unless the file repeats
	}, "\n")
	if got != want {
		t.Errorf("concat list:\n%s\nwant:\n%s", got, want)
	}
}

func TestFFmpegArgs(t *testing.T) {
	mp4 := ffmpegArgs("list.txt", "/abs/out.mp4")
	joined := strings.Join(mp4, " ")
	for _, want := range []string{"-f concat", "-fps_mode vfr", "-pix_fmt yuv420p", "/abs/out.mp4"} {
		if !strings.Contains(joined, want) {
			t.Errorf("mp4 args %q missing %q", joined, want)
		}
	}
	webm := strings.Join(ffmpegArgs("list.txt", "/abs/out.webm"), " ")
	if !strings.Contains(webm, "-c:v libvpx-vp9") {
		t.Errorf("webm args %q missing libvpx-vp9", webm)
	}
}

func TestFFmpegSinkIntegration(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	out := filepath.Join(t.TempDir(), "out.mp4")
	s, err := newFFmpegSink(out, ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	red := solidFrame(color.RGBA{255, 0, 0, 255}, 64, 64)
	blue := solidFrame(color.RGBA{0, 0, 255, 255}, 64, 64)
	for _, f := range []*image.RGBA{red, blue} {
		if err := s.add(f, 500*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.close(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(out)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("output missing or empty: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `nix develop -c go test ./internal/export/`
Expected: FAIL — `undefined: ffmpegSink`

- [ ] **Step 3: Implement `ffmpeg.go`**

```go
package export

import (
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/paulsmith/twee/internal/render"
)

// ffmpegSink spools frames as PNGs into a temp directory and runs one
// ffmpeg concat-demuxer invocation on close. ffmpeg executes with the temp
// dir as its working directory so the list uses bare relative filenames and
// needs no path escaping.
type ffmpegSink struct {
	outPath string // absolute
	ffmpeg  string
	dir     string
	n       int
	list    strings.Builder
	lastRef string
}

func newFFmpegSink(outPath, ffmpeg string) (*ffmpegSink, error) {
	abs, err := filepath.Abs(outPath)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "twee-export-")
	if err != nil {
		return nil, err
	}
	return &ffmpegSink{outPath: abs, ffmpeg: ffmpeg, dir: dir}, nil
}

func (s *ffmpegSink) add(img *image.RGBA, d time.Duration) error {
	s.n++
	name := fmt.Sprintf("frame-%06d.png", s.n)
	f, err := os.Create(filepath.Join(s.dir, name))
	if err != nil {
		return err
	}
	if err := render.EncodePNG(f, img); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	s.noteFrame(name, d)
	return nil
}

func (s *ffmpegSink) noteFrame(name string, d time.Duration) {
	fmt.Fprintf(&s.list, "file %s\nduration %.6f\n", name, d.Seconds())
	s.lastRef = name
}

// concatList returns the demuxer input. The final file entry is repeated:
// the concat demuxer otherwise ignores the last duration directive.
func (s *ffmpegSink) concatList() string {
	if s.lastRef == "" {
		return s.list.String()
	}
	return s.list.String() + "file " + s.lastRef + "\n"
}

func ffmpegArgs(listName, outPath string) []string {
	args := []string{"-y", "-f", "concat", "-i", listName, "-fps_mode", "vfr",
		"-pix_fmt", "yuv420p"}
	if strings.HasSuffix(strings.ToLower(outPath), ".webm") {
		args = append(args, "-c:v", "libvpx-vp9")
	}
	return append(args, outPath)
}

func (s *ffmpegSink) close() error {
	const listName = "list.txt"
	if err := os.WriteFile(filepath.Join(s.dir, listName), []byte(s.concatList()), 0o644); err != nil {
		return err
	}
	cmd := exec.Command(s.ffmpeg, ffmpegArgs(listName, s.outPath)...)
	cmd.Dir = s.dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w\n%s\nframes kept in %s for debugging",
			err, tail(stderr.String(), 2000), s.dir)
	}
	return os.RemoveAll(s.dir)
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `nix develop -c go test ./internal/export/`
Expected: PASS (integration test skips if ffmpeg absent; if ffmpeg is on PATH it must pass)

- [ ] **Step 5: Commit**

```bash
jj commit -m "export: ffmpeg concat sink for MP4/WebM"
```

---

### Task 6: `Export` entry point

Glue: open bundle, build canvas, pick sink by extension, run replay.

**Files:**
- Create: `internal/export/export.go`
- Create: `internal/export/export_test.go`

- [ ] **Step 1: Write failing test**

The test builds a synthetic .twee zip (same technique as `internal/play/bundle_test.go` — check that file for the helper pattern and reuse its shape):

```go
package export

import (
	"archive/zip"
	"encoding/base64"
	"fmt"
	"image/gif"
	"os"
	"path/filepath"
	"testing"
)

func writeTestBundle(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.twee")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	mw, _ := zw.Create("manifest.json")
	fmt.Fprint(mw, `{"version":1,"command":["true"],"cols":20,"rows":5}`)
	ew, _ := zw.Create("events.jsonl")
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	fmt.Fprintf(ew, `{"t_ms":100,"type":"output","bytes_b64":"%s"}`+"\n", b64("hello"))
	fmt.Fprintf(ew, `{"t_ms":600,"type":"output","bytes_b64":"%s"}`+"\n", b64("\r\nworld"))
	fmt.Fprint(ew, `{"t_ms":1000,"type":"exit","code":0}`+"\n")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExportGIFEndToEnd(t *testing.T) {
	bundle := writeTestBundle(t)
	out := filepath.Join(t.TempDir(), "out.gif")
	if err := Export(bundle, out, Options{}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) < 2 {
		t.Errorf("got %d frames, want >= 2 (two distinct screens)", len(g.Image))
	}
	if g.Image[0].Bounds().Dx()%2 != 0 || g.Image[0].Bounds().Dy()%2 != 0 {
		t.Errorf("frame dims %v not even", g.Image[0].Bounds())
	}
}

func TestExportUnknownExtension(t *testing.T) {
	bundle := writeTestBundle(t)
	err := Export(bundle, filepath.Join(t.TempDir(), "out.avi"), Options{})
	if err == nil {
		t.Fatal("want error for unsupported extension")
	}
}

func TestExportMP4RequiresFFmpeg(t *testing.T) {
	bundle := writeTestBundle(t)
	err := Export(bundle, filepath.Join(t.TempDir(), "out.mp4"),
		Options{FFmpeg: "/nonexistent/ffmpeg"})
	if err == nil {
		t.Fatal("want preflight error when ffmpeg missing")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `nix develop -c go test ./internal/export/`
Expected: FAIL — `undefined: Export`

- [ ] **Step 3: Implement `export.go`**

```go
package export

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/paulsmith/twee/internal/play"
	"github.com/paulsmith/twee/internal/vt"
)

// Export replays the bundle at path and writes a video to outPath. The
// container is chosen by extension: .gif (pure Go), .mp4 or .webm (ffmpeg).
func Export(path, outPath string, opts Options) error {
	opts.normalize()

	var snk sink
	switch strings.ToLower(filepath.Ext(outPath)) {
	case ".gif":
		s, err := newGIFSink(outPath)
		if err != nil {
			return fmt.Errorf("twee export: %w", err)
		}
		snk = s
	case ".mp4", ".webm":
		ffmpeg := opts.FFmpeg
		if ffmpeg == "" {
			ffmpeg = "ffmpeg"
		}
		resolved, err := exec.LookPath(ffmpeg)
		if err != nil {
			return fmt.Errorf("twee export: mp4/webm output requires ffmpeg: %w", err)
		}
		s, err := newFFmpegSink(outPath, resolved)
		if err != nil {
			return fmt.Errorf("twee export: %w", err)
		}
		snk = s
	default:
		return fmt.Errorf("twee export: unsupported output format %q (use .gif, .mp4, or .webm)", filepath.Ext(outPath))
	}

	b, err := play.OpenBundle(path)
	if err != nil {
		return err
	}
	cv, err := newCanvas(b.MaxCols, b.MaxRows, opts.FontSize)
	if err != nil {
		return fmt.Errorf("twee export: %w", err)
	}
	err = replay(b.Events, b.Manifest.Cols, b.Manifest.Rows, opts, vt.New,
		func(s vt.Snapshot, d time.Duration) error {
			img, err := cv.compose(s)
			if err != nil {
				return err
			}
			return snk.add(img, d)
		})
	if err != nil {
		return fmt.Errorf("twee export: %w", err)
	}
	if err := snk.close(); err != nil {
		return fmt.Errorf("twee export: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `nix develop -c go test ./internal/export/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
jj commit -m "export: Export entry point with format dispatch and ffmpeg preflight"
```

---

### Task 7: CLI verb (`cmd/twee/cmd_export.go`)

**Files:**
- Create: `cmd/twee/cmd_export.go`
- Create: `cmd/twee/cmd_export_test.go`

- [ ] **Step 1: Write failing test** (flag parsing, following `parse_helpers_test.go` style)

```go
package main

import (
	"testing"
	"time"
)

func TestParseExportArgs(t *testing.T) {
	path, out, opts := parseExportArgs([]string{
		"demo.twee", "-o", "demo.mp4",
		"--speed", "2", "--max-idle", "1s", "--font-size", "12", "--fps-cap", "15",
	})
	if path != "demo.twee" || out != "demo.mp4" {
		t.Errorf("path/out = %q/%q", path, out)
	}
	if opts.Speed != 2 || opts.MaxIdle != time.Second ||
		opts.FontSize != 12 || opts.FPSCap != 15 {
		t.Errorf("opts = %+v", opts)
	}
}

func TestParseExportArgsDefaults(t *testing.T) {
	_, _, opts := parseExportArgs([]string{"demo.twee", "-o", "demo.gif"})
	if opts.Speed != 1 || opts.MaxIdle != 0 || opts.FPSCap != 30 {
		t.Errorf("defaults wrong: %+v", opts)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop -c go test ./cmd/twee/ -run TestParseExport`
Expected: FAIL — `undefined: parseExportArgs`

- [ ] **Step 3: Implement `cmd_export.go`**

Follow `cmd_play.go`'s pattern exactly (same `parseArg` helper):

```go
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/paulsmith/twee/internal/export"
)

func init() {
	register("export", runExport)
	registerUsage("export", `twee export <bundle.twee> -o <out.gif|out.mp4|out.webm>
Export a .twee trace bundle to a video file. The format is chosen by the
output extension. GIF is encoded in pure Go; MP4 and WebM require ffmpeg.

Frames are emitted only when the screen visibly changes (the cursor is not
rendered). Timing is faithful to the recording by default.

Flags:
  -o <path>            output file (required); .gif, .mp4, or .webm
  --speed <float>      playback speed multiplier (default 1.0)
  --max-idle <duration>
                       cap idle gaps (default 0 = faithful; note: 'twee play'
                       defaults to 2s)
  --font-size <pt>     render font size in points (default 14)
  --fps-cap <int>      max frames per second of video time (default 30)
  --ffmpeg <path>      ffmpeg binary (default: found on PATH)`)
}

func runExport(args []string) {
	path, out, opts := parseExportArgs(args)
	if err := export.Export(path, out, opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseExportArgs(args []string) (path, out string, opts export.Options) {
	opts = export.Options{Speed: 1, FPSCap: 30, FontSize: 14}
	var parsed struct {
		Out      string   `arg:"-o,required"`
		Speed    *float64 `arg:"--speed"`
		MaxIdle  string   `arg:"--max-idle"`
		FontSize *float64 `arg:"--font-size"`
		FPSCap   *int     `arg:"--fps-cap"`
		FFmpeg   string   `arg:"--ffmpeg"`
		Path     string   `arg:"positional,required"`
	}
	if err := parseArg("export", &parsed, args); err != nil {
		fatalUsage("export: %v", err)
	}
	if parsed.Speed != nil {
		opts.Speed = *parsed.Speed
	}
	if opts.Speed <= 0 {
		fatalUsage("export: --speed must be > 0")
	}
	if parsed.MaxIdle != "" {
		d, err := time.ParseDuration(parsed.MaxIdle)
		if err != nil || d < 0 {
			fatalUsage("export: bad --max-idle value %q", parsed.MaxIdle)
		}
		opts.MaxIdle = d
	}
	if parsed.FontSize != nil {
		opts.FontSize = *parsed.FontSize
	}
	if parsed.FPSCap != nil {
		opts.FPSCap = *parsed.FPSCap
	}
	if opts.FPSCap <= 0 {
		fatalUsage("export: --fps-cap must be > 0")
	}
	opts.FFmpeg = parsed.FFmpeg
	return parsed.Path, parsed.Out, opts
}
```

Note: check `cmd/twee/arg_parser.go` for the exact struct-tag conventions
(`arg:"-o,required"` syntax may differ — mirror whatever `cmd_play.go` and
other commands actually use; adjust the tags, not the behavior).

- [ ] **Step 4: Run tests to verify they pass**

Run: `nix develop -c go test ./cmd/twee/ -run TestParseExport`
Expected: PASS

- [ ] **Step 5: Full test suite + manual smoke test**

Run: `nix develop -c go test ./...`
Expected: all PASS.

Then a real end-to-end check (any .twee bundle; record one if none exists):

```bash
nix develop -c go run ./cmd/twee export <some>.twee -o /tmp/out.gif
nix develop -c go run ./cmd/twee export <some>.twee -o /tmp/out.mp4  # if ffmpeg installed
```

Open the outputs and confirm they play with sensible timing.

- [ ] **Step 6: Commit**

```bash
jj commit -m "cli: add twee export verb for video export"
```

---

### Task 8: Documentation

**Files:**
- Modify: `README.md` (add `export` to the command list/examples, wherever `play` is documented)

- [ ] **Step 1: Document the verb**

Add a short section mirroring the usage text: formats, ffmpeg requirement for mp4/webm, faithful-timing default, no cursor in output, `--max-idle` difference from `play`.

- [ ] **Step 2: Commit**

```bash
jj commit -m "docs: document twee export"
```
