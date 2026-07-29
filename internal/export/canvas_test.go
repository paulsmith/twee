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
			cells[j] = vt.Cell{
				Text: "M", Width: 1,
				Fg: vt.Color{Kind: vt.ColorRGB, R: 255, G: 255, B: 255},
				Bg: vt.Color{Kind: vt.ColorRGB, R: 255, G: 0, B: 0},
			}
		}
		lines[i] = vt.Line{Cells: cells}
	}
	return vt.Snapshot{Size: vt.Size{Cols: cols, Rows: rows}, Lines: lines}
}

func TestCanvasDimensionsAreEven(t *testing.T) {
	c, err := newCanvas(81, 25, 14, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	b := c.bounds()
	if b.Dx()%2 != 0 || b.Dy()%2 != 0 {
		t.Errorf("canvas %dx%d: dimensions must be even", b.Dx(), b.Dy())
	}
}

func TestCanvasLetterboxesSmallerSnapshot(t *testing.T) {
	c, err := newCanvas(80, 24, 14, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	img, err := c.compose(snapOfSize(40, 12), "")
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != c.bounds() {
		t.Fatalf("frame bounds %v != canvas bounds %v", img.Bounds(), c.bounds())
	}
	if got := img.RGBAAt(0, 0); got != (color.RGBA{0, 0, 0, 255}) {
		t.Errorf("corner = %v, want black", got)
	}
	cx, cy := img.Bounds().Dx()/2, img.Bounds().Dy()/2
	if got := img.RGBAAt(cx, cy); got.R < 200 || got.G > 50 {
		t.Errorf("center = %v, want red content", got)
	}
}

// TestCanvasCropMatchesEquivalentUncroppedSize pins down crop's output
// frame dimensions: rendering a WxH crop of an 80x24 grid must produce
// exactly the same pixel bounds as rendering an uncropped WxH grid, since
// a crop is meant to behave like "pretend the recording were only this
// big" for rendering purposes.
func TestCanvasCropMatchesEquivalentUncroppedSize(t *testing.T) {
	cropped, err := newCanvas(80, 24, 14, &CropRect{X: 5, Y: 2, W: 10, H: 3}, false)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := newCanvas(10, 3, 14, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if cropped.bounds() != plain.bounds() {
		t.Fatalf("cropped bounds %v != equivalent plain bounds %v", cropped.bounds(), plain.bounds())
	}
}

// TestCanvasComposeWithOversizedCropDoesNotError pins down the
// blank-fill rule: a crop rectangle bigger than the actual recorded
// screen must render the intersection, not fail mid-export.
func TestCanvasComposeWithOversizedCropDoesNotError(t *testing.T) {
	small := snapOfSize(4, 2)
	c, err := newCanvas(4, 2, 14, &CropRect{X: 0, Y: 0, W: 10, H: 6}, false)
	if err != nil {
		t.Fatal(err)
	}
	img, err := c.compose(small, "")
	if err != nil {
		t.Fatalf("compose with oversized crop rect: %v", err)
	}
	if img.Bounds() != c.bounds() {
		t.Fatalf("frame bounds %v != canvas bounds %v", img.Bounds(), c.bounds())
	}
}

// TestCanvasOverlayAddsFooterRowHeight pins down that --input-overlay
// makes the canvas exactly one text row taller than the same content
// without it, so the overlay adds rows rather than shrinking the frame.
func TestCanvasOverlayAddsFooterRowHeight(t *testing.T) {
	plain, err := newCanvas(10, 3, 14, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	withOverlay, err := newCanvas(10, 3, 14, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if withOverlay.bounds().Dx() != plain.bounds().Dx() {
		t.Errorf("overlay canvas width = %d, want unchanged %d", withOverlay.bounds().Dx(), plain.bounds().Dx())
	}
	if withOverlay.bounds().Dy() <= plain.bounds().Dy() {
		t.Fatalf("overlay canvas height = %d, want taller than %d", withOverlay.bounds().Dy(), plain.bounds().Dy())
	}

	// The extra height must be exactly one row's worth: 4 rows vs. 3
	// rows (no overlay) differ by the same amount as 3 rows with the
	// overlay's one extra row.
	fourRows, err := newCanvas(10, 4, 14, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if withOverlay.bounds().Dy() != fourRows.bounds().Dy() {
		t.Errorf("overlay height %d != equivalent 4-row canvas height %d",
			withOverlay.bounds().Dy(), fourRows.bounds().Dy())
	}
}

func TestCropVTSnapshotIntersectsAndBlankFills(t *testing.T) {
	src := snapOfSize(4, 2)
	// Rect partly off the bottom/right edge of the 4x2 source.
	out := cropVTSnapshot(src, CropRect{X: 2, Y: 1, W: 4, H: 4})
	if out.Size.Cols != 4 || out.Size.Rows != 4 {
		t.Fatalf("cropped size = %dx%d, want 4x4", out.Size.Cols, out.Size.Rows)
	}
	// (0,0) in the output maps to (2,1) in the source: still real content.
	if out.Lines[0].Cells[0].Text != "M" {
		t.Errorf("intersection cell = %q, want M", out.Lines[0].Cells[0].Text)
	}
	// Rows/cols beyond the source's 4x2 bounds must be blank, not a
	// zero-value (Width=0) cell that the renderer would skip oddly.
	last := out.Lines[3].Cells[3]
	if last.Text != " " || last.Width != 1 {
		t.Errorf("out-of-bounds cell = %+v, want blank space", last)
	}
}

func TestOverlayRowPadsAndTruncates(t *testing.T) {
	line := overlayRow("hi", 5)
	if len(line.Cells) != 5 {
		t.Fatalf("cells = %d, want 5", len(line.Cells))
	}
	got := ""
	for _, c := range line.Cells {
		got += c.Text
		if !c.Inverse {
			t.Errorf("cell %+v not reverse-video", c)
		}
	}
	if got != "hi   " {
		t.Errorf("row text = %q, want %q", got, "hi   ")
	}

	truncated := overlayRow("way too long for three cells", 3)
	if len(truncated.Cells) != 3 {
		t.Fatalf("cells = %d, want 3", len(truncated.Cells))
	}
}
