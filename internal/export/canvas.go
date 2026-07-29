package export

import (
	"image"
	"image/color"
	"image/draw"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/play"
	"github.com/paulsmith/twee/internal/render"
	"github.com/paulsmith/twee/internal/vt"
)

// canvas composes per-snapshot renders onto a fixed black background sized for
// the recording's largest grid, padded to even pixel dimensions for yuv420p.
type canvas struct {
	w, h     int
	fontSize float64
	crop     *CropRect // nil = render the full recorded grid
	overlay  bool      // append one footer row of --input-overlay text
}

func newCanvas(maxCols, maxRows int, fontSize float64, crop *CropRect, overlay bool) (*canvas, error) {
	probeCols, probeRows := maxCols, maxRows
	if crop != nil {
		probeCols, probeRows = crop.W, crop.H
	}
	if overlay {
		probeRows++
	}
	probe, err := render.Render(engine.Snapshot{Cols: probeCols, Rows: probeRows},
		render.Options{SizePt: fontSize})
	if err != nil {
		return nil, err
	}
	w, h := probe.Bounds().Dx(), probe.Bounds().Dy()
	return &canvas{w: w + w%2, h: h + h%2, fontSize: fontSize, crop: crop, overlay: overlay}, nil
}

func (c *canvas) bounds() image.Rectangle { return image.Rect(0, 0, c.w, c.h) }

// compose renders snap (cropped to c.crop, if set) onto the canvas,
// letterboxed and centered. overlayText, when c.overlay is true, is
// drawn as an extra footer row of reverse-video cells below the
// content — the --input-overlay footer strip.
func (c *canvas) compose(snap vt.Snapshot, overlayText string) (*image.RGBA, error) {
	if c.crop != nil {
		snap = cropVTSnapshot(snap, *c.crop)
	}
	es := play.EngineSnapshot(snap)
	if c.overlay {
		es.Lines = append(es.Lines, overlayRow(overlayText, es.Cols))
		es.Rows++
	}
	content, err := render.Render(es, render.Options{SizePt: c.fontSize})
	if err != nil {
		return nil, err
	}
	out := image.NewRGBA(c.bounds())
	draw.Draw(out, out.Bounds(), &image.Uniform{C: color.RGBA{0, 0, 0, 255}},
		image.Point{}, draw.Src)
	cb := content.Bounds()
	off := image.Pt((c.w-cb.Dx())/2, (c.h-cb.Dy())/2)
	draw.Draw(out, cb.Add(off), content, cb.Min, draw.Src)
	return out, nil
}

// cropVTSnapshot extracts the r rectangle (cell coordinates) from snap,
// blank-filling any row or column outside snap's actual bounds instead
// of erroring — the screen can be smaller than the crop rect, e.g.
// before a later resize event grows it.
func cropVTSnapshot(snap vt.Snapshot, r CropRect) vt.Snapshot {
	w, h := r.W, r.H
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	out := vt.Snapshot{
		Size:      vt.Size{Cols: w, Rows: h},
		AltScreen: snap.AltScreen,
		Lines:     make([]vt.Line, h),
	}
	for row := 0; row < h; row++ {
		cells := make([]vt.Cell, w)
		for col := range cells {
			cells[col] = vt.Cell{Text: " ", Width: 1}
		}
		srcY := r.Y + row
		if srcY >= 0 && srcY < len(snap.Lines) {
			srcCells := snap.Lines[srcY].Cells
			for col := 0; col < w; col++ {
				srcX := r.X + col
				if srcX >= 0 && srcX < len(srcCells) {
					cells[col] = srcCells[srcX]
				}
			}
		}
		out.Lines[row] = vt.Line{Cells: cells}
	}
	return out
}

// overlayRow renders text as one engine.Line of cols cells: the footer
// strip --input-overlay appends below the frame. Cells are reverse-video
// so the strip reads as a status bar distinct from recorded terminal
// content, echoing how "twee play"'s footer rows sit visually apart from
// the image above them.
func overlayRow(text string, cols int) engine.Line {
	if cols < 0 {
		cols = 0
	}
	runes := []rune(text)
	cells := make([]engine.Cell, cols)
	for i := range cells {
		ch := " "
		if i < len(runes) {
			ch = string(runes[i])
		}
		cells[i] = engine.Cell{Text: ch, Width: 1, Inverse: true}
	}
	return engine.Line{Cells: cells}
}
