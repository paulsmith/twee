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
}

func newCanvas(maxCols, maxRows int, fontSize float64) (*canvas, error) {
	probe, err := render.Render(engine.Snapshot{Cols: maxCols, Rows: maxRows},
		render.Options{SizePt: fontSize})
	if err != nil {
		return nil, err
	}
	w, h := probe.Bounds().Dx(), probe.Bounds().Dy()
	return &canvas{w: w + w%2, h: h + h%2, fontSize: fontSize}, nil
}

func (c *canvas) bounds() image.Rectangle { return image.Rect(0, 0, c.w, c.h) }

func (c *canvas) compose(snap vt.Snapshot) (*image.RGBA, error) {
	content, err := render.Render(play.EngineSnapshot(snap),
		render.Options{SizePt: c.fontSize})
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
