package render

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/paulsmith/research/twee/internal/engine"
)

// Options controls a render pass.
type Options struct {
	SizePt float64 // font size in points; default 14
}

// Default returns sensible options.
func Default() Options { return Options{SizePt: 14} }

// Render rasterizes the snapshot and returns the resulting RGBA image.
func Render(snap engine.Snapshot, opts Options) (*image.RGBA, error) {
	if opts.SizePt == 0 {
		opts.SizePt = 14
	}
	face, err := Face(opts.SizePt)
	if err != nil {
		return nil, err
	}
	cw, ch := cellMetrics(face)
	w := cw * snap.Cols
	h := ch * snap.Rows
	if w <= 0 || h <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	// Default background: black.
	bg := color.RGBA{0, 0, 0, 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)

	for y := 0; y < snap.Rows && y < len(snap.Lines); y++ {
		line := snap.Lines[y].Cells
		for x := 0; x < snap.Cols && x < len(line); x++ {
			drawCell(img, x, y, cw, ch, face, line[x])
		}
	}
	return img, nil
}

// EncodePNG writes the image as PNG to w.
func EncodePNG(w io.Writer, img image.Image) error {
	return png.Encode(w, img)
}

func cellMetrics(face font.Face) (cw, ch int) {
	adv, _ := face.GlyphAdvance('M')
	cw = adv.Ceil()
	if cw == 0 {
		cw = 8
	}
	m := face.Metrics()
	ch = (m.Ascent + m.Descent).Ceil()
	if ch == 0 {
		ch = 16
	}
	return cw, ch
}

func drawCell(img *image.RGBA, cx, cy, cw, ch int, face font.Face, c engine.Cell) {
	// Width=0: continuation cell of a wide glyph; skip.
	if c.Width == 0 {
		return
	}
	width := c.Width
	if width <= 0 {
		width = 1
	}

	fg := resolveColor(c.Fg, color.RGBA{200, 200, 200, 255})
	bg := resolveColor(c.Bg, color.RGBA{0, 0, 0, 255})
	if c.Inverse {
		fg, bg = bg, fg
	}
	if c.Dim {
		fg = dim(fg)
	}

	rect := image.Rect(cx*cw, cy*ch, (cx+width)*cw, (cy+1)*ch)
	draw.Draw(img, rect, &image.Uniform{C: bg}, image.Point{}, draw.Src)

	if c.Text == "" {
		return
	}

	m := face.Metrics()
	dot := fixed.Point26_6{
		X: fixed.I(cx * cw),
		Y: fixed.I(cy*ch) + m.Ascent,
	}
	d := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: fg},
		Face: face,
		Dot:  dot,
	}
	d.DrawString(c.Text)

	if c.Bold {
		d.Dot = fixed.Point26_6{
			X: fixed.I(cx*cw + 1),
			Y: fixed.I(cy*ch) + m.Ascent,
		}
		d.DrawString(c.Text)
	}

	if c.Underline {
		uy := (cy+1)*ch - 1
		ux1 := cx * cw
		ux2 := (cx + width) * cw
		for x := ux1; x < ux2; x++ {
			img.Set(x, uy, fg)
		}
	}
}

func resolveColor(c engine.Color, fallback color.RGBA) color.RGBA {
	switch c.Kind {
	case engine.ColorRGB:
		return color.RGBA{c.R, c.G, c.B, 255}
	case engine.ColorPalette:
		return palette256(c.Index)
	case engine.ColorIndexed:
		return ansi16(c.Index)
	default:
		return fallback
	}
}

// ansi16 returns a basic 16-color palette entry.
func ansi16(i uint8) color.RGBA {
	tbl := [16]color.RGBA{
		{0, 0, 0, 255}, {178, 24, 24, 255}, {24, 178, 24, 255}, {178, 178, 24, 255},
		{24, 24, 178, 255}, {178, 24, 178, 255}, {24, 178, 178, 255}, {200, 200, 200, 255},
		{100, 100, 100, 255}, {255, 60, 60, 255}, {60, 255, 60, 255}, {255, 255, 60, 255},
		{60, 60, 255, 255}, {255, 60, 255, 255}, {60, 255, 255, 255}, {255, 255, 255, 255},
	}
	return tbl[int(i)%16]
}

// palette256 implements xterm's 256-color palette.
func palette256(i uint8) color.RGBA {
	if i < 16 {
		return ansi16(i)
	}
	if i < 232 {
		v := int(i) - 16
		r := v / 36
		g := (v / 6) % 6
		b := v % 6
		conv := func(n int) uint8 {
			if n == 0 {
				return 0
			}
			return uint8(55 + 40*n)
		}
		return color.RGBA{conv(r), conv(g), conv(b), 255}
	}
	v := uint8(8 + (int(i)-232)*10)
	return color.RGBA{v, v, v, 255}
}

func dim(c color.RGBA) color.RGBA {
	return color.RGBA{c.R / 2, c.G / 2, c.B / 2, c.A}
}
