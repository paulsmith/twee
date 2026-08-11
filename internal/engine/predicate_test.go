package engine

import "testing"

func ptr[T any](v T) *T { return &v }

func TestCellPredicateMatchesEveryPopulatedField(t *testing.T) {
	cell := Cell{
		Text: "界", Width: 2,
		Fg:   Color{Kind: ColorPalette, Index: 1},
		Bg:   Color{Kind: ColorRGB, R: 0, G: 2, B: 3},
		Bold: true, Underline: true,
	}
	predicate := CellPredicate{
		Text: ptr("界"), Width: ptr(2), Fg: ptr(Color{Kind: ColorPalette, Index: 1}),
		Bg: ptr(Color{Kind: ColorRGB, R: 0, G: 2, B: 3}), Bold: ptr(true),
		Dim: ptr(false), Underline: ptr(true), Inverse: ptr(false),
	}
	if !predicate.Matches(cell) {
		t.Fatal("matching predicate failed")
	}
	predicate.Bold = ptr(false)
	if predicate.Matches(cell) {
		t.Fatal("explicit false style matched true cell")
	}
}

func TestCellPredicateDistinguishesBlankAndWideContinuation(t *testing.T) {
	empty := ""
	if !(CellPredicate{Text: &empty, Width: ptr(0)}).Matches(Cell{Text: "", Width: 0}) {
		t.Fatal("wide continuation did not match empty text and width zero")
	}
	if (CellPredicate{Width: ptr(0)}).Matches(Cell{Text: "", Width: 1}) {
		t.Fatal("blank narrow cell matched continuation width")
	}
}

func TestCellPredicateMatchesIndexedAndPaletteByIndex(t *testing.T) {
	predicate := CellPredicate{Fg: ptr(Color{Kind: ColorPalette, Index: 0})}
	if !predicate.Matches(Cell{Fg: Color{Kind: ColorIndexed, Index: 0}}) {
		t.Fatal("palette predicate did not match equivalent indexed color")
	}
	if predicate.Matches(Cell{Fg: Color{Kind: ColorRGB}}) {
		t.Fatal("palette predicate matched RGB color")
	}
}

func TestRegionMatchesClipsWithoutOverflowAndRejectsEmptyIntersection(t *testing.T) {
	snapshot := Snapshot{Cols: 3, Rows: 2, Lines: []Line{
		{Cells: []Cell{{Text: "a"}, {Text: "b"}, {Text: "c"}}},
		{Cells: []Cell{{Text: "d"}, {Text: "e"}, {Text: "f"}}},
	}}
	predicate := CellPredicate{Text: ptr("f")}
	if !RegionMatches(snapshot, &Rect{X: 2, Y: 1, W: int(^uint(0) >> 1), H: int(^uint(0) >> 1)}, RegionMatchAny, predicate) {
		t.Fatal("clipped huge region did not find matching cell")
	}
	if RegionMatches(snapshot, &Rect{X: 3, Y: 0, W: 1, H: 1}, RegionMatchAny, predicate) {
		t.Fatal("off-screen region matched")
	}
	if RegionMatches(snapshot, &Rect{X: 3, Y: 0, W: 1, H: 1}, RegionMatchAll, predicate) {
		t.Fatal("empty intersection satisfied all")
	}
}

func TestRegionEvaluationSummary(t *testing.T) {
	snapshot := Snapshot{Cols: 3, Rows: 1, Lines: []Line{{Cells: []Cell{{Text: "a"}, {Text: "b"}, {Text: "a"}}}}}
	evaluation := EvaluateRegion(snapshot, &Rect{X: 1, Y: 0, W: 9, H: 1}, RegionMatchAll, CellPredicate{Text: ptr("a")})
	if evaluation.Matches {
		t.Fatal("mixed region unexpectedly matched all")
	}
	summary := evaluation.Summary
	if summary.TotalCells != 2 || summary.MatchingCells != 1 {
		t.Fatalf("summary counts = %d/%d", summary.MatchingCells, summary.TotalCells)
	}
	if summary.Clipped == nil || *summary.Clipped != (Rect{X: 1, Y: 0, W: 2, H: 1}) {
		t.Fatalf("clipped = %+v", summary.Clipped)
	}
	if summary.FirstMismatch == nil || summary.FirstMismatch.X != 1 || summary.FirstMismatch.Cell.Text != "b" {
		t.Fatalf("first mismatch = %+v", summary.FirstMismatch)
	}
}

func TestRegionMatchesAnyAndAll(t *testing.T) {
	snapshot := Snapshot{Cols: 2, Rows: 1, Lines: []Line{{Cells: []Cell{{Bold: true}, {Bold: false}}}}}
	predicate := CellPredicate{Bold: ptr(true)}
	if !RegionMatches(snapshot, nil, RegionMatchAny, predicate) {
		t.Fatal("any did not match one cell")
	}
	if RegionMatches(snapshot, nil, RegionMatchAll, predicate) {
		t.Fatal("all matched mixed cells")
	}
	predicate.Bold = ptr(false)
	if !RegionMatches(snapshot, &Rect{X: 1, Y: 0, W: 1, H: 1}, RegionMatchAll, predicate) {
		t.Fatal("all did not match one-cell region")
	}
}
