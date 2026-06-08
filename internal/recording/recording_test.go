package recording

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/vt"
)

func TestRecordAndReplay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	r, err := New(path, Header{Command: []string{"fixture"}, Cols: 20, Rows: 3})
	if err != nil {
		t.Fatal(err)
	}
	r.WriteOutput([]byte("hello"), time.Now())
	r.WriteOutput([]byte("\r\nworld"), time.Now())
	r.WriteResize(30, 5)
	r.WriteOutput([]byte("\r\n!"), time.Now())
	r.WriteExit(0)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	m := vt.New(20, 3)
	if _, err := ReplayInto(path, m); err != nil {
		t.Fatal(err)
	}
	got := vt.VisibleLines(m.Snapshot())
	want := []string{"hello", "world", "!", "", ""}
	if len(got) < len(want) {
		t.Fatalf("got %d lines, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("line %d = %q, want %q", i, got[i], w)
		}
	}
	s := m.Snapshot()
	if s.Size.Cols != 30 || s.Size.Rows != 5 {
		t.Errorf("size after replay = %+v", s.Size)
	}
}
