package export

import (
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/play"
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
	// 10 changes within 100ms at 30fps cap -> at most ceil(100ms/33.3ms)+1
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
