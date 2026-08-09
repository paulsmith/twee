package play

import (
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/engine"
)

func contentSnapshot(cols, rows int) engine.Snapshot {
	lines := make([]engine.Line, rows)
	for y := range lines {
		cells := make([]engine.Cell, cols)
		for x := range cells {
			cells[x] = engine.Cell{Text: "x", Width: 1}
		}
		lines[y] = engine.Line{Cells: cells}
	}
	return engine.Snapshot{Cols: cols, Rows: rows, Lines: lines}
}

func rowText(snap engine.Snapshot, y, x0, x1 int) string {
	var b strings.Builder
	if y >= len(snap.Lines) {
		return ""
	}
	cells := snap.Lines[y].Cells
	for x := x0; x < x1 && x < len(cells); x++ {
		b.WriteString(cells[x].Text)
	}
	return b.String()
}

func snapText(snap engine.Snapshot) string {
	var b strings.Builder
	for y := range snap.Lines {
		b.WriteString(rowText(snap, y, 0, snap.Cols))
		b.WriteString("\n")
	}
	return b.String()
}

func TestOverlayEndScreenDrawsCenteredBox(t *testing.T) {
	// 40x10 frame: box is 22x4 at cols 9-30, rows 3-6.
	snap := contentSnapshot(40, 10)
	overlayEndScreen(&snap)

	hbar := strings.Repeat("─", 20)
	want := []struct {
		y    int
		text string
	}{
		{3, "┌" + hbar + "┐"},
		{4, "│  End of playback   │"},
		{5, "│ r restart · q quit │"},
		{6, "└" + hbar + "┘"},
	}
	for _, w := range want {
		if got := rowText(snap, w.y, 9, 31); got != w.text {
			t.Errorf("row %d = %q, want %q", w.y, got, w.text)
		}
	}

	for x := 12; x <= 26; x++ {
		c := snap.Lines[4].Cells[x]
		if !c.Bold {
			t.Errorf("title cell %d not bold", x)
		}
	}
	for y := 3; y <= 6; y++ {
		for x := 9; x <= 30; x++ {
			if snap.Lines[y].Cells[x].Dim {
				t.Errorf("banner cell (%d,%d) is dim", x, y)
			}
		}
	}
}

func TestOverlayEndScreenDimsSurroundingCells(t *testing.T) {
	snap := contentSnapshot(40, 10)
	overlayEndScreen(&snap)

	for y := range 10 {
		for x := range 40 {
			inBox := y >= 3 && y <= 6 && x >= 9 && x <= 30
			c := snap.Lines[y].Cells[x]
			if inBox {
				continue
			}
			if !c.Dim {
				t.Errorf("cell (%d,%d) not dimmed", x, y)
			}
			if c.Text != "x" {
				t.Errorf("cell (%d,%d) text = %q, want original content", x, y, c.Text)
			}
		}
	}
}

func TestOverlayEndScreenFallsBackToBareTextWhenBoxTooWide(t *testing.T) {
	// Box needs 22 cols; at 18 cols the text is drawn without a border.
	snap := contentSnapshot(18, 4)
	overlayEndScreen(&snap)

	all := snapText(snap)
	if !strings.Contains(all, "End of playback") {
		t.Errorf("missing title in:\n%s", all)
	}
	if !strings.Contains(all, "r restart · q quit") {
		t.Errorf("missing hint in:\n%s", all)
	}
	if strings.ContainsAny(all, "┌│─") {
		t.Errorf("unexpected border in:\n%s", all)
	}
}

func TestOverlayEndScreenFallsBackToBareTextWhenFrameTooShort(t *testing.T) {
	// Box needs 4 rows; at 3 rows the text is drawn without a border.
	snap := contentSnapshot(40, 3)
	overlayEndScreen(&snap)

	all := snapText(snap)
	if !strings.Contains(all, "End of playback") {
		t.Errorf("missing title in:\n%s", all)
	}
	if !strings.Contains(all, "r restart · q quit") {
		t.Errorf("missing hint in:\n%s", all)
	}
	if strings.ContainsAny(all, "┌│─") {
		t.Errorf("unexpected border in:\n%s", all)
	}
}

func TestOverlayEndScreenSingleRowShowsTitleOnly(t *testing.T) {
	snap := contentSnapshot(20, 1)
	overlayEndScreen(&snap)

	all := snapText(snap)
	if !strings.Contains(all, "End of playback") {
		t.Errorf("missing title in:\n%s", all)
	}
	if strings.Contains(all, "q quit") {
		t.Errorf("hint should not fit in one row:\n%s", all)
	}
}

func TestOverlayEndScreenTruncatesTitleOnNarrowFrame(t *testing.T) {
	snap := contentSnapshot(6, 1)
	overlayEndScreen(&snap)

	if got := rowText(snap, 0, 0, 6); got != "End of" {
		t.Errorf("row 0 = %q, want truncated title", got)
	}
}

func TestOverlayEndScreenBlanksWideGlyphsStraddlingBoxEdges(t *testing.T) {
	// Box covers cols 9-30. A wide glyph whose base or continuation cell is
	// overwritten by the border must not leave a clipped half-glyph or an
	// orphaned Width=0 continuation next to the box.
	snap := contentSnapshot(40, 10)
	row := snap.Lines[4].Cells
	row[8] = engine.Cell{Text: "世", Width: 2}
	row[9] = engine.Cell{Width: 0}
	row[30] = engine.Cell{Text: "界", Width: 2}
	row[31] = engine.Cell{Width: 0}
	overlayEndScreen(&snap)

	if c := snap.Lines[4].Cells[8]; c.Text != " " || c.Width != 1 {
		t.Errorf("left straddling base = %+v, want blanked single-width cell", c)
	}
	if c := snap.Lines[4].Cells[31]; c.Text != " " || c.Width != 1 {
		t.Errorf("right orphaned continuation = %+v, want blanked single-width cell", c)
	}
}

func TestOverlayEndScreenHandlesDegenerateSnapshots(t *testing.T) {
	empty := engine.Snapshot{}
	overlayEndScreen(&empty) // must not panic

	// Lines may be missing or short; the banner must still be drawn.
	bare := engine.Snapshot{Cols: 40, Rows: 10}
	overlayEndScreen(&bare)
	if got := rowText(bare, 4, 9, 31); got != "│  End of playback   │" {
		t.Errorf("row 4 = %q, want banner on extended lines", got)
	}
}
