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
	c, err := newCanvas(81, 25, 14)
	if err != nil {
		t.Fatal(err)
	}
	b := c.bounds()
	if b.Dx()%2 != 0 || b.Dy()%2 != 0 {
		t.Errorf("canvas %dx%d: dimensions must be even", b.Dx(), b.Dy())
	}
}

func TestCanvasLetterboxesSmallerSnapshot(t *testing.T) {
	c, err := newCanvas(80, 24, 14)
	if err != nil {
		t.Fatal(err)
	}
	img, err := c.compose(snapOfSize(40, 12))
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
