package play

import (
	"image"
	"strings"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/render"
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
}

type fakeSink struct {
	frames []frameRecord
}

func (s *fakeSink) Emit(img *image.RGBA, cols, rows int, toast, status string) error {
	s.frames = append(s.frames, frameRecord{
		cols: cols, rows: rows, toast: toast, status: status, size: img.Bounds(),
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
	cmds := make(chan command, 2)
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
