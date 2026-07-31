package play

import (
	"bytes"
	"image"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/render"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/vt"
)

type fakeModel struct {
	cols, rows int
	text       string
}

func (m *fakeModel) Feed(p []byte) error {
	m.text += string(p)
	return nil
}

func (m *fakeModel) Resize(cols, rows int) error {
	m.cols, m.rows = cols, rows
	return nil
}

func (m *fakeModel) Snapshot() vt.Snapshot {
	cells := make([]vt.Cell, m.cols)
	for i := range cells {
		cells[i] = vt.Cell{Text: " ", Width: 1}
	}
	for i, r := range m.text {
		if i >= len(cells) {
			break
		}
		cells[i] = vt.Cell{Text: string(r), Width: 1}
	}
	lines := make([]vt.Line, m.rows)
	if m.rows > 0 {
		lines[0] = vt.Line{Cells: cells}
	}
	for i := 1; i < m.rows; i++ {
		lines[i] = vt.Line{Cells: make([]vt.Cell, m.cols)}
	}
	return vt.Snapshot{Size: vt.Size{Cols: m.cols, Rows: m.rows}, Lines: lines}
}

type frameRecord struct {
	cols   int
	rows   int
	toast  string
	status string
	size   image.Rectangle
	img    *image.RGBA
}

type fakeSink struct {
	frames []frameRecord
}

func (s *fakeSink) Emit(img *image.RGBA, cols, rows int, toast, status string) error {
	s.frames = append(s.frames, frameRecord{
		cols: cols, rows: rows, toast: toast, status: status, size: img.Bounds(), img: img,
	})
	return nil
}

func TestLoopStepAdvancesExactlyOneEvent(t *testing.T) {
	cmds := make(chan command, 4)
	sink := &fakeSink{}
	l := testLoop(loopConfig{
		Events: []Event{
			{TMS: 0, Type: "output", Bytes: []byte("A")},
			{TMS: 0, Type: "output", Bytes: []byte("B")},
			{TMS: 100, Type: "output", Bytes: []byte("C")},
		},
		Step: true,
		Cmds: cmds,
		Sink: sink,
	})
	now := time.Unix(0, 0)
	l.tick(now)
	if l.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", l.cursor)
	}

	cmds <- cmdStep
	l.tick(now.Add(time.Millisecond))
	if l.cursor != 1 || l.model.(*fakeModel).text != "A" {
		t.Fatalf("after one step cursor=%d text=%q, want 1/A", l.cursor, l.model.(*fakeModel).text)
	}

	cmds <- cmdStep
	l.tick(now.Add(2 * time.Millisecond))
	if l.cursor != 2 || l.model.(*fakeModel).text != "AB" {
		t.Fatalf("after two steps cursor=%d text=%q, want 2/AB", l.cursor, l.model.(*fakeModel).text)
	}
	if !strings.Contains(lastFrame(t, sink).status, "step 1.0") {
		t.Fatalf("status = %q, want step mode", lastFrame(t, sink).status)
	}
}

func TestLoopToastPersistsUntilNextDisplayedEvent(t *testing.T) {
	cmds := make(chan command, 1)
	sink := &fakeSink{}
	l := testLoop(loopConfig{
		Events: []Event{
			{TMS: 0, Type: "input", Kind: "key", Key: "Enter"},
			{TMS: 700, Type: "output", Bytes: []byte("A")},
			{TMS: 800, Type: "resize", Cols: 12, Rows: 4},
		},
		Step: true,
		Cmds: cmds,
		Sink: sink,
	})
	now := time.Unix(0, 0)
	l.tick(now)

	cmds <- cmdStep
	l.tick(now.Add(time.Millisecond))
	if got := lastFrame(t, sink).toast; !strings.Contains(got, "Enter") {
		t.Fatalf("toast = %q, want Enter", got)
	}

	cmds <- cmdStep
	l.tick(now.Add(700 * time.Millisecond))
	if got := lastFrame(t, sink).toast; !strings.Contains(got, "Enter") {
		t.Fatalf("toast after output = %q, want previous input", got)
	}

	cmds <- cmdStep
	l.tick(now.Add(800 * time.Millisecond))
	if got := lastFrame(t, sink).toast; !strings.Contains(got, "resize 12x4") {
		t.Fatalf("toast after resize = %q, want resize", got)
	}
}

func TestLoopPauseForwardRestartAndMaxIdle(t *testing.T) {
	cmds := make(chan command, 8)
	sink := &fakeSink{}
	l := testLoop(loopConfig{
		Events: []Event{
			{TMS: 10_000, Type: "output", Bytes: []byte("L")},
			{TMS: 11_000, Type: "resize", Cols: 12, Rows: 4},
		},
		MaxIdle: 2 * time.Second,
		Cmds:    cmds,
		Sink:    sink,
	})
	now := time.Unix(0, 0)
	l.tick(now)
	if l.playT != 8*time.Second || l.cursor != 0 {
		t.Fatalf("after idle snap playT=%s cursor=%d, want 8s/0", l.playT, l.cursor)
	}

	l.tick(now.Add(2 * time.Second))
	if l.cursor != 1 || l.model.(*fakeModel).text != "L" {
		t.Fatalf("after idle cap cursor=%d text=%q, want output dispatched", l.cursor, l.model.(*fakeModel).text)
	}

	cmds <- cmdPause
	l.tick(now.Add(3 * time.Second))
	frozen := l.playT
	l.tick(now.Add(8 * time.Second))
	if l.playT != frozen {
		t.Fatalf("paused playT advanced from %s to %s", frozen, l.playT)
	}

	cmds <- cmdFwd1s
	l.tick(now.Add(9 * time.Second))
	if l.cursor != 2 || l.rows != 4 {
		t.Fatalf("forward cursor=%d rows=%d, want resize dispatched", l.cursor, l.rows)
	}

	cmds <- cmdRestart
	l.tick(now.Add(10 * time.Second))
	if l.cursor != 0 || l.playT != 0 || l.model.(*fakeModel).text != "" || l.rows != 3 {
		t.Fatalf("restart cursor=%d playT=%s text=%q rows=%d", l.cursor, l.playT, l.model.(*fakeModel).text, l.rows)
	}
}

func TestLoopQuitReturnsDone(t *testing.T) {
	cmds := make(chan command, 1)
	l := testLoop(loopConfig{Cmds: cmds, Sink: &fakeSink{}})
	cmds <- cmdQuit
	if !l.tick(time.Unix(0, 0)) {
		t.Fatal("tick returned false after quit command")
	}
}

func TestLoopClosedCommandChannelEmitsCurrentFrameThenDone(t *testing.T) {
	cmds := make(chan command)
	close(cmds)
	sink := &fakeSink{}
	l := testLoop(loopConfig{
		Events: []Event{{TMS: 0, Type: "output", Bytes: []byte("A")}},
		Cmds:   cmds,
		Sink:   sink,
	})
	if !l.tick(time.Unix(0, 0)) {
		t.Fatal("tick returned false after closed command channel")
	}
	if len(sink.frames) == 0 {
		t.Fatal("closed command channel exited before emitting current frame")
	}
}

func TestLoopEmitsEndScreenWhenPlaybackEnds(t *testing.T) {
	cmds := make(chan command, 1)
	sink := &fakeSink{}
	l := testLoop(loopConfig{
		Events: []Event{
			{TMS: 0, Type: "output", Bytes: []byte("A")},
			// Exit is metadata: it ends playback without changing the screen,
			// so any difference between the two frames is the end screen.
			{TMS: 1000, Type: "exit", Code: 0},
		},
		Cmds: cmds,
		Sink: sink,
	})
	now := time.Unix(0, 0)
	l.tick(now)
	if l.atEnd {
		t.Fatal("playback ended before final event")
	}
	l.tick(now.Add(2 * time.Second))
	if !l.atEnd {
		t.Fatal("playback did not end")
	}

	if len(sink.frames) != 2 {
		t.Fatalf("emitted %d frames, want 2", len(sink.frames))
	}
	before, after := sink.frames[0].img, sink.frames[1].img
	if !bytes.Equal(renderedFrame(t, l, false).Pix, before.Pix) {
		t.Fatal("frame before end does not match plain render of the model")
	}
	if !bytes.Equal(renderedFrame(t, l, true).Pix, after.Pix) {
		t.Fatal("frame at end does not match end-screen render of the model")
	}
}

func TestLoopRestartAtEndResumesPlaybackWithoutEndScreen(t *testing.T) {
	cmds := make(chan command, 1)
	sink := &fakeSink{}
	l := testLoop(loopConfig{
		Events: []Event{
			{TMS: 0, Type: "output", Bytes: []byte("A")},
			{TMS: 1000, Type: "exit", Code: 0},
		},
		Cmds: cmds,
		Sink: sink,
	})
	now := time.Unix(0, 0)
	l.tick(now)
	l.tick(now.Add(2 * time.Second))
	if !l.atEnd {
		t.Fatal("playback did not end")
	}

	cmds <- cmdRestart
	l.tick(now.Add(3 * time.Second))
	if l.atEnd {
		t.Fatal("restart did not clear end state")
	}
	if !bytes.Equal(renderedFrame(t, l, false).Pix, lastFrame(t, sink).img.Pix) {
		t.Fatal("frame after restart still carries the end screen")
	}

	l.tick(now.Add(5 * time.Second))
	if l.cursor == 0 || l.model.(*fakeModel).text != "A" {
		t.Fatalf("playback did not resume after restart: cursor=%d text=%q",
			l.cursor, l.model.(*fakeModel).text)
	}
}

func TestLoopRestartHonorsStepMode(t *testing.T) {
	cmds := make(chan command, 8)
	sink := &fakeSink{}
	l := testLoop(loopConfig{
		Events: []Event{
			{TMS: 0, Type: "output", Bytes: []byte("A")},
			{TMS: 100, Type: "output", Bytes: []byte("B")},
		},
		Step: true,
		Cmds: cmds,
		Sink: sink,
	})
	now := time.Unix(0, 0)
	l.tick(now)

	cmds <- cmdStep
	l.tick(now.Add(time.Millisecond))
	cmds <- cmdStep
	l.tick(now.Add(2 * time.Millisecond))
	if !l.atEnd {
		t.Fatal("stepping through all events did not reach the end")
	}

	cmds <- cmdRestart
	l.tick(now.Add(3 * time.Millisecond))
	if !l.paused || !l.stepMode {
		t.Fatalf("restart dropped step mode: paused=%v stepMode=%v", l.paused, l.stepMode)
	}
	if got := lastFrame(t, sink).status; !strings.Contains(got, "step 1.0") {
		t.Fatalf("status after restart = %q, want step mode", got)
	}

	l.tick(now.Add(10 * time.Second))
	if l.cursor != 0 || l.model.(*fakeModel).text != "" {
		t.Fatalf("playback advanced after restart in step mode: cursor=%d text=%q",
			l.cursor, l.model.(*fakeModel).text)
	}

	cmds <- cmdStep
	l.tick(now.Add(11 * time.Second))
	if l.cursor != 1 || l.model.(*fakeModel).text != "A" {
		t.Fatalf("step after restart: cursor=%d text=%q, want 1/A",
			l.cursor, l.model.(*fakeModel).text)
	}
}

func TestLoopEndScreenWhenLastEventIsOutput(t *testing.T) {
	cmds := make(chan command, 1)
	sink := &fakeSink{}
	l := testLoop(loopConfig{
		Events: []Event{{TMS: 0, Type: "output", Bytes: []byte("A")}},
		Cmds:   cmds,
		Sink:   sink,
	})
	l.tick(time.Unix(0, 0))
	if !l.atEnd {
		t.Fatal("playback did not end after final output event")
	}
	if !bytes.Equal(renderedFrame(t, l, true).Pix, lastFrame(t, sink).img.Pix) {
		t.Fatal("frame at end does not match end-screen render of the model")
	}
}

func TestLoopMouseAnnotationAnimatesThenEmitsOneCleanFrame(t *testing.T) {
	cmds := make(chan command, 1)
	sink := &fakeSink{}
	l := testLoop(loopConfig{
		Events: []Event{{TMS: 0, Type: "input", Kind: "mouse", Mouse: testMouseClick(2, 1, "left")}},
		Step:   true,
		Cmds:   cmds,
		Sink:   sink,
	})
	now := time.Unix(0, 0)
	l.tick(now)
	baseline := lastFrame(t, sink).img

	cmds <- cmdStep
	l.tick(now.Add(time.Millisecond))
	first := lastFrame(t, sink).img
	if bytes.Equal(first.Pix, renderedFrame(t, l, true).Pix) {
		t.Fatal("mouse event did not overlay the final rendered frame")
	}
	if bytes.Equal(baseline.Pix, first.Pix) {
		t.Fatal("mouse-only event did not force a rendered frame")
	}

	framesAfterStart := len(sink.frames)
	l.tick(now.Add(time.Millisecond + mouseAnnotationFramePeriod + time.Millisecond))
	if len(sink.frames) != framesAfterStart+1 {
		t.Fatalf("animation frames = %d, want %d", len(sink.frames), framesAfterStart+1)
	}
	if bytes.Equal(first.Pix, lastFrame(t, sink).img.Pix) {
		t.Fatal("animation frame did not change")
	}

	l.tick(now.Add(time.Millisecond + mouseAnnotationDuration))
	if !bytes.Equal(lastFrame(t, sink).img.Pix, renderedFrame(t, l, true).Pix) {
		t.Fatal("expired annotation did not restore the clean final frame")
	}
	framesAfterExpiry := len(sink.frames)
	l.tick(now.Add(time.Millisecond + mouseAnnotationDuration + mouseAnnotationFramePeriod))
	if len(sink.frames) != framesAfterExpiry {
		t.Fatalf("frames after clean expiry = %d, want %d", len(sink.frames), framesAfterExpiry)
	}
}

func TestLoopMouseAnnotationExpiresWhilePausedAndRestartClearsIt(t *testing.T) {
	cmds := make(chan command, 2)
	sink := &fakeSink{}
	l := testLoop(loopConfig{
		Events: []Event{
			{TMS: 0, Type: "input", Kind: "mouse", Mouse: testMouseClick(0, 0, "left")},
			{TMS: 10_000, Type: "output", Bytes: []byte("later")},
		},
		Step: true, Cmds: cmds, Sink: sink,
	})
	now := time.Unix(0, 0)
	l.tick(now)
	cmds <- cmdStep
	l.tick(now.Add(time.Millisecond))
	if l.mouse == nil {
		t.Fatal("mouse annotation was not started for zero coordinate")
	}
	if !l.paused {
		t.Fatal("step mode did not remain paused")
	}
	l.tick(now.Add(mouseAnnotationDuration + 3*time.Millisecond))
	if l.mouse != nil {
		t.Fatal("annotation did not expire while playback was paused")
	}

	cmds <- cmdRestart
	l.tick(now.Add(mouseAnnotationDuration + 4*time.Millisecond))
	if l.mouse != nil {
		t.Fatal("restart retained an annotation")
	}
	if !bytes.Equal(lastFrame(t, sink).img.Pix, renderedFrame(t, l, false).Pix) {
		t.Fatal("restart did not redraw a clean frame")
	}
}

func TestLoopMouseAnnotationReplacementAndInvalidMetadata(t *testing.T) {
	sink := &fakeSink{}
	l := testLoop(loopConfig{
		Events: []Event{
			{TMS: 0, Type: "input", Kind: "mouse", Mouse: testMouseClick(1, 1, "left")},
			{TMS: 0, Type: "input", Kind: "mouse", Mouse: testMouseClick(2, 1, "right")},
			{TMS: 0, Type: "input", Kind: "mouse", Mouse: &trace.MouseInput{Gesture: "click"}},
		},
		Sink: sink,
	})
	l.tick(time.Unix(0, 0))
	if l.mouse == nil || l.mouse.mouse.Button != "right" || l.mouse.mouse.X == nil || *l.mouse.mouse.X != 2 {
		t.Fatalf("active annotation = %#v, want latest valid right click", l.mouse)
	}
	if l.mouseGeneration != 2 {
		t.Fatalf("mouse generation = %d, want 2", l.mouseGeneration)
	}
	if bytes.Equal(lastFrame(t, sink).img.Pix, renderedFrame(t, l, true).Pix) {
		t.Fatal("latest valid mouse annotation was not drawn")
	}
}

func TestLoopResizeClearsMouseAnnotationAndDisableOptionSkipsIt(t *testing.T) {
	t.Run("resize", func(t *testing.T) {
		sink := &fakeSink{}
		l := testLoop(loopConfig{
			Events: []Event{
				{TMS: 0, Type: "input", Kind: "mouse", Mouse: testMouseClick(1, 1, "left")},
				{TMS: 0, Type: "resize", Cols: 12, Rows: 4},
			}, Sink: sink,
		})
		l.tick(time.Unix(0, 0))
		if l.mouse != nil {
			t.Fatal("resize retained an annotation with stale viewport coordinates")
		}
		if !bytes.Equal(lastFrame(t, sink).img.Pix, renderedFrame(t, l, true).Pix) {
			t.Fatal("resize frame retained a mouse annotation")
		}
	})
	t.Run("disabled", func(t *testing.T) {
		sink := &fakeSink{}
		l := testLoop(loopConfig{
			Events: []Event{{TMS: 0, Type: "input", Kind: "mouse", Mouse: testMouseClick(1, 1, "left")}},
			Sink:   sink, DisableMouseAnnotations: true,
		})
		l.tick(time.Unix(0, 0))
		if l.mouse != nil {
			t.Fatal("disabled annotations started an active animation")
		}
		if !bytes.Equal(lastFrame(t, sink).img.Pix, renderedFrame(t, l, true).Pix) {
			t.Fatal("disabled annotations changed frame pixels")
		}
	})
}

func testMouseClick(x, y int, button string) *trace.MouseInput {
	return &trace.MouseInput{Gesture: "click", X: intPtr(x), Y: intPtr(y), Button: button}
}

// renderedFrame renders the loop's current model the way emitFrame does,
// with or without the end screen, for comparison against emitted frames.
func renderedFrame(t *testing.T, l *loop, end bool) *image.RGBA {
	t.Helper()
	es := EngineSnapshot(l.model.Snapshot())
	if end {
		overlayEndScreen(&es)
	}
	img, err := render.Render(es, render.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestLoopUsesStaticRenderOptions(t *testing.T) {
	sink := &fakeSink{}
	l := testLoop(loopConfig{
		Sink:          sink,
		RenderOptions: render.Options{PixelWidth: 123, PixelHeight: 45},
	})
	l.tick(time.Unix(0, 0))

	got := lastFrame(t, sink).size
	if got.Dx() != 123 || got.Dy() != 45 {
		t.Fatalf("frame size = %dx%d, want 123x45", got.Dx(), got.Dy())
	}
}

func TestLoopExpandsWideFrameToAvailableWidth(t *testing.T) {
	sink := &fakeSink{}
	l := testLoop(loopConfig{
		Events: []Event{{TMS: 0, Type: "resize", Cols: 20, Rows: 4}},
		Sink:   sink,
		DisplayPixels: displayPixels{
			Width:  1000,
			Height: 500,
		},
		TerminalSize: terminalSize{
			Cols: 100,
			Rows: 25,
		},
	})
	l.tick(time.Unix(0, 0))

	frame := lastFrame(t, sink)
	if frame.cols != 100 || frame.rows != 20 {
		t.Fatalf("placement = %dx%d, want 100x20", frame.cols, frame.rows)
	}
	if got := frame.size; got.Dx() != 1000 || got.Dy() != 400 {
		t.Fatalf("frame size = %dx%d, want 1000x400", got.Dx(), got.Dy())
	}
}

func TestLoopExpandsTallFrameToAvailableHeight(t *testing.T) {
	sink := &fakeSink{}
	l := testLoop(loopConfig{
		Events: []Event{{TMS: 0, Type: "resize", Cols: 10, Rows: 20}},
		Sink:   sink,
		DisplayPixels: displayPixels{
			Width:  1000,
			Height: 500,
		},
		TerminalSize: terminalSize{
			Cols: 100,
			Rows: 25,
		},
	})
	l.tick(time.Unix(0, 0))

	frame := lastFrame(t, sink)
	if frame.cols != 12 || frame.rows != 23 {
		t.Fatalf("placement = %dx%d, want 12x23", frame.cols, frame.rows)
	}
	if got := frame.size; got.Dx() != 120 || got.Dy() != 460 {
		t.Fatalf("frame size = %dx%d, want 120x460", got.Dx(), got.Dy())
	}
}

// TestEngineSnapshotCarriesPaletteColorAndStyleFromTrace is a regression
// test for a bug in EngineSnapshot's predecessor (see its doc comment):
// a 256-color SGR sequence recorded in a real .twee trace must survive
// playback conversion as engine.ColorPalette (not silently relabeled
// engine.ColorIndexed, the 16-color ANSI kind), and italic/strikethrough
// must not be dropped.
func TestEngineSnapshotCarriesPaletteColorAndStyleFromTrace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "colors.twee")
	tr, err := trace.New(path, trace.Manifest{Command: []string{"true"}, Cols: 10, Rows: 1})
	if err != nil {
		t.Fatal(err)
	}
	// SGR 38;5;99 selects xterm 256-color palette index 99. SGR 3 and 9
	// are italic and strikethrough.
	tr.WriteOutput([]byte("\x1b[38;5;99m\x1b[3m\x1b[9mZ"), time.Now())
	tr.WriteExit(0)
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}

	bundle, err := OpenBundle(path)
	if err != nil {
		t.Fatalf("OpenBundle: %v", err)
	}

	model := vt.New(bundle.Manifest.Cols, bundle.Manifest.Rows)
	for _, ev := range bundle.Events {
		if ev.Type != "output" {
			continue
		}
		if err := model.Feed(ev.Bytes); err != nil {
			t.Fatalf("Feed: %v", err)
		}
	}

	snap := EngineSnapshot(model.Snapshot())
	cell := snap.Lines[0].Cells[0]
	if cell.Text != "Z" {
		t.Fatalf("cell text = %q, want Z", cell.Text)
	}
	if cell.Fg.Kind != engine.ColorPalette {
		t.Fatalf("fg kind = %v, want ColorPalette (256-color); a numeric-cast bug "+
			"would report ColorIndexed (16-color ANSI) instead, since they share iota 1", cell.Fg.Kind)
	}
	if cell.Fg.Index != 99 {
		t.Fatalf("fg index = %d, want 99", cell.Fg.Index)
	}
	if !cell.Italic {
		t.Fatal("cell.Italic = false, want true (dropped by the old conversion)")
	}
	if !cell.Strikethrough {
		t.Fatal("cell.Strikethrough = false, want true (dropped by the old conversion)")
	}
}

func testLoop(cfg loopConfig) *loop {
	cfg.Cols = 10
	cfg.Rows = 3
	cfg.Speed = 1
	cfg.NewModel = func(cols, rows int) vt.Model {
		return &fakeModel{cols: cols, rows: rows}
	}
	return newLoop(cfg)
}

func lastFrame(t *testing.T, sink *fakeSink) frameRecord {
	t.Helper()
	if len(sink.frames) == 0 {
		t.Fatal("no frames emitted")
	}
	return sink.frames[len(sink.frames)-1]
}
