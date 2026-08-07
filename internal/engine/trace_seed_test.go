package engine

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/paulsmith/twee/internal/vt"
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

func TestSnapshotDiffOutputReconstructsStyledCells(t *testing.T) {
	beforeModel := vt.New(12, 3)
	if err := beforeModel.Feed([]byte("\x1b[44mleft    \x1b[0m\r\nA界B")); err != nil {
		t.Fatal(err)
	}
	before := beforeModel.Snapshot()

	afterModel := vt.New(12, 3)
	if err := afterModel.Feed([]byte("\x1b[41mright   \x1b[0m\r\nAxyB")); err != nil {
		t.Fatal(err)
	}
	after := afterModel.Snapshot()
	diff := SnapshotDiffOutput(before, after)
	if len(diff) == 0 {
		t.Fatal("empty diff")
	}
	if bytes.Contains(diff, []byte("\x1b[2J")) {
		t.Fatalf("incremental diff clears screen: %q", diff)
	}

	dst := vt.New(12, 3)
	if err := dst.Feed(TraceSeedOutput(before)); err != nil {
		t.Fatal(err)
	}
	if err := dst.Feed(diff); err != nil {
		t.Fatal(err)
	}
	if got := normalizeBlankCells(dst.Snapshot().Lines); !reflect.DeepEqual(got, normalizeBlankCells(after.Lines)) {
		t.Fatalf("cells after diff do not match target:\n got: %#v\nwant: %#v", got, after.Lines)
	}
}

func normalizeBlankCells(lines []vt.Line) []vt.Line {
	out := make([]vt.Line, len(lines))
	for row, line := range lines {
		out[row].Cells = append([]vt.Cell(nil), line.Cells...)
		for col := range out[row].Cells {
			if out[row].Cells[col].Text == " " {
				out[row].Cells[col].Text = ""
			}
		}
	}
	return out
}

func TestSnapshotDiffOutputSkipsUnchangedFrame(t *testing.T) {
	m := vt.New(8, 2)
	if err := m.Feed([]byte("steady")); err != nil {
		t.Fatal(err)
	}
	s := m.Snapshot()
	if got := SnapshotDiffOutput(s, s); len(got) != 0 {
		t.Fatalf("unchanged diff = %q", got)
	}
}

func TestSnapshotDiffOutputTreatsBlankCellRepresentationsAsEqual(t *testing.T) {
	before := vt.Snapshot{Size: vt.Size{Cols: 1, Rows: 1}, Lines: []vt.Line{{Cells: []vt.Cell{{Width: 1}}}}}
	after := vt.Snapshot{Size: vt.Size{Cols: 1, Rows: 1}, Lines: []vt.Line{{Cells: []vt.Cell{{Text: " ", Width: 1}}}}}
	if got := SnapshotDiffOutput(before, after); len(got) != 0 {
		t.Fatalf("equivalent blank diff = %q", got)
	}
}

func TestSnapshotDiffOutputSeedsAfterResize(t *testing.T) {
	before := vt.New(8, 2).Snapshot()
	afterModel := vt.New(10, 3)
	if err := afterModel.Feed([]byte("resized")); err != nil {
		t.Fatal(err)
	}
	got := SnapshotDiffOutput(before, afterModel.Snapshot())
	if !bytes.Contains(got, []byte("\x1b[2J")) {
		t.Fatalf("resize did not seed full frame: %q", got)
	}
}

func TestTraceSeedOutputReconstructsCursorStyle(t *testing.T) {
	src := vt.New(20, 4)
	if err := src.Feed([]byte("\x1b[6 q")); err != nil {
		t.Fatal(err)
	}
	dst := vt.New(20, 4)
	if err := dst.Feed(TraceSeedOutput(src.Snapshot())); err != nil {
		t.Fatal(err)
	}
	if got := dst.Snapshot().Cursor.Style; got != vt.CursorStyleBar {
		t.Fatalf("style=%v", got)
	}
}
