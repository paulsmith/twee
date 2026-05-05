package engine

import (
	"testing"

	"github.com/paulsmith/research/twee/internal/vt"
)

func TestTraceSeedOutputReconstructsVisibleText(t *testing.T) {
	src := vt.New(20, 4)
	if err := src.Feed([]byte("\x1b[?25lhello\r\n\x1b[31mred")); err != nil {
		t.Fatal(err)
	}
	snap := src.Snapshot()
	seed := TraceSeedOutput(snap)
	if len(seed) == 0 {
		t.Fatal("empty seed")
	}

	dst := vt.New(20, 4)
	if err := dst.Feed(seed); err != nil {
		t.Fatal(err)
	}
	got := vt.VisibleText(dst.Snapshot())
	want := vt.VisibleText(snap)
	if got != want {
		t.Fatalf("visible text after seed:\n%q\nwant:\n%q", got, want)
	}
	if dst.Snapshot().Cursor.Visible {
		t.Fatal("cursor visible after seeding hidden cursor")
	}
}
