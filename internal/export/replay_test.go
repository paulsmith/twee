package export

import (
	"strings"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/play"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/vt"
)

// fakeModel records fed bytes and exposes a settable snapshot. Snapshot
// content is keyed off a generation counter so tests control when the screen
// changes.
type fakeModel struct {
	gen  int
	cur  vt.Cursor
	cols int
	rows int
}

func (m *fakeModel) Feed(p []byte) error {
	// Convention for tests: feeding "x" changes the screen; feeding "c"
	// moves only the cursor.
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

func (m *fakeModel) Resize(cols, rows int) error {
	m.cols, m.rows = cols, rows
	return nil
}

func (m *fakeModel) Snapshot() vt.Snapshot {
	return vt.Snapshot{
		Size:   vt.Size{Cols: m.cols, Rows: m.rows},
		Cursor: m.cur,
		Lines: []vt.Line{{
			Cells: []vt.Cell{{Text: string(rune('a' + m.gen%26)), Width: 1}},
		}},
	}
}

type frame struct {
	snap    vt.Snapshot
	overlay string
	d       time.Duration
}

func collect(t *testing.T, events []play.Event, opts Options) []frame {
	t.Helper()
	var out []frame
	err := replay(events, 80, 24, opts,
		func(cols, rows int) vt.Model { return &fakeModel{cols: cols, rows: rows} },
		func(s vt.Snapshot, overlay string, d time.Duration) error {
			out = append(out, frame{s, overlay, d})
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

func keyInput(tms int64, key string) play.Event {
	return play.Event{TMS: tms, Type: "input", Kind: "key", Key: key}
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
	// 10 changes within 100ms at 30fps cap -> at most ceil(100ms/33.3ms)+1
	// emitted frames, and total duration preserved.
	var evs []play.Event
	for i := range 10 {
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

func TestReplaySnapshotsDirtyFPSWindowBeforeLaterEvent(t *testing.T) {
	frames := collect(t, []play.Event{
		out(0, "x"),
		out(20, "x"),
		out(40, "x"),
	}, Options{FPSCap: 30})
	if !hasFrameText(frames, "c") {
		t.Fatalf("frames = %v, want intermediate screen from dirty fps window", frameTexts(frames))
	}
}

func hasFrameText(frames []frame, want string) bool {
	for _, f := range frames {
		if frameText(f) == want {
			return true
		}
	}
	return false
}

func frameTexts(frames []frame) []string {
	out := make([]string, len(frames))
	for i, f := range frames {
		out[i] = frameText(f)
	}
	return out
}

func frameText(f frame) string {
	if len(f.snap.Lines) == 0 || len(f.snap.Lines[0].Cells) == 0 {
		return ""
	}
	return f.snap.Lines[0].Cells[0].Text
}

func TestReplaySpeedAndMaxIdle(t *testing.T) {
	// gap of 10s capped to 2s, then /2 speed -> 1s.
	frames := collect(t, []play.Event{out(0, "x"), out(10000, "x")},
		Options{Speed: 2, MaxIdle: 2 * time.Second})
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(frames))
	}
	if frames[0].d != time.Second {
		t.Errorf("frame duration = %v, want 1s (10s gap -> maxIdle 2s -> /2)", frames[0].d)
	}
}

func TestReplayTrailingCap(t *testing.T) {
	// 10 minutes of idle before exit -> trailing frame capped at 3s.
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

// TestReplayInputOverlayForcesFrameOnUnchangedScreen pins down the
// --input-overlay interaction with the emit-on-screen-change rule: a key
// event between two otherwise-unbroken output events produces no frame
// split at all without the overlay (the screen doesn't visibly change
// again until the trailing frame), but must split the frame once the
// overlay is enabled — otherwise a new overlay value could take effect
// and later change again without ever being visible in its own frame.
func TestReplayInputOverlayForcesFrameOnUnchangedScreen(t *testing.T) {
	events := []play.Event{
		out(1000, "x"),
		keyInput(1500, "Enter"),
		out(4000, "c"), // cursor-only change: doesn't split the frame either
	}

	without := collect(t, events, Options{})
	if len(without) != 2 {
		t.Fatalf("without overlay: got %d frames, want 2 (blank, then the single unbroken 'x' frame)", len(without))
	}

	with := collect(t, events, Options{InputOverlay: true})
	if len(with) != 3 {
		t.Fatalf("with overlay: got %d frames, want 3 (the key event must split the 'x' frame in two)", len(with))
	}
	last := with[len(with)-1]
	if !strings.Contains(last.overlay, "Enter") {
		t.Fatalf("last frame overlay = %q, want it to mention Enter", last.overlay)
	}
	// The screen content itself is unaffected by the overlay forcing an
	// extra frame — only the frame boundaries and overlay text differ.
	if frameText(with[0]) != frameText(without[0]) {
		t.Errorf("first frame content changed by enabling overlay")
	}
	if frameText(last) != frameText(without[len(without)-1]) {
		t.Errorf("final frame content changed by enabling overlay")
	}
}

func TestReplayMouseInputIsAnnotationOnly(t *testing.T) {
	x, y := 12, 4
	events := []play.Event{
		out(1000, "x"),
		{
			TMS: 1500, Type: "input", Kind: "mouse",
			// If input bytes were incorrectly fed, this "x" would mutate
			// fakeModel and change the rendered cell.
			Bytes: []byte("x"),
			Mouse: &trace.MouseInput{
				Gesture: "click", X: &x, Y: &y, Button: "left",
				Modifiers: []string{},
			},
		},
	}

	frames := collect(t, events, Options{InputOverlay: true})
	last := frames[len(frames)-1]
	if got := frameText(last); got != "b" {
		t.Fatalf("screen after annotated mouse input = %q, want %q", got, "b")
	}
	if !strings.Contains(last.overlay, "click left @(12,4)") {
		t.Fatalf("mouse overlay = %q", last.overlay)
	}
}
