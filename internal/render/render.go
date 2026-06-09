package render

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/paulsmith/twee/internal/engine"
)

// Options controls a render pass.
type Options struct {
	SizePt float64 // font size in points; default 14

	// PixelWidth and PixelHeight request an exact output image size. When
	// both are > 0 and SizePt is 0, the renderer picks a font size that fits
	// the requested cell dimensions.
	PixelWidth  int
	PixelHeight int
}

// Default returns sensible options.
func Default() Options { return Options{SizePt: 14} }

// Render rasterizes the snapshot and returns the resulting RGBA image.
func Render(snap engine.Snapshot, opts Options) (*image.RGBA, error) {
	sizePt, w, h, err := resolveSize(snap, opts)
	if err != nil {
		return nil, err
	}
	if w <= 0 || h <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
	}
	face, err := Face(sizePt)
	if err != nil {
		return nil, err
	}
	fallbackFaces, err := FallbackFaces(sizePt)
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	// Default background: black.
	bg := color.RGBA{0, 0, 0, 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)

	for y := 0; y < snap.Rows && y < len(snap.Lines); y++ {
		line := snap.Lines[y].Cells
		for x := 0; x < snap.Cols && x < len(line); x++ {
			drawCell(img, snap.Cols, snap.Rows, x, y, face, fallbackFaces, line[x])
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

func resolveSize(snap engine.Snapshot, opts Options) (sizePt float64, w int, h int, err error) {
	sizePt = opts.SizePt
	if sizePt == 0 {
		sizePt = 14
	}

	face, err := Face(sizePt)
	if err != nil {
		return 0, 0, 0, err
	}
	cw, ch := cellMetrics(face)
	w = cw * snap.Cols
	h = ch * snap.Rows

	if opts.PixelWidth <= 0 || opts.PixelHeight <= 0 {
		return sizePt, w, h, nil
	}
	w, h = opts.PixelWidth, opts.PixelHeight

	if opts.SizePt != 0 || snap.Cols <= 0 || snap.Rows <= 0 {
		return sizePt, w, h, nil
	}

	targetCW := float64(opts.PixelWidth) / float64(snap.Cols)
	targetCH := float64(opts.PixelHeight) / float64(snap.Rows)
	sizeFromW := sizePt
	sizeFromH := sizePt
	if cw > 0 {
		sizeFromW = sizePt * targetCW / float64(cw)
	}
	if ch > 0 {
		sizeFromH = sizePt * targetCH / float64(ch)
	}
	sizePt = sizeFromH
	if sizeFromW > 0 && sizeFromW < sizePt {
		sizePt = sizeFromW
	}
	if sizePt <= 0 {
		sizePt = 14
	}
	return sizePt, w, h, nil
}

func drawCell(img *image.RGBA, cols, rows, cx, cy int, face font.Face, fallbackFaces []font.Face, c engine.Cell) {
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

	rect := cellRect(img.Bounds(), cols, rows, cx, cy, width)
	draw.Draw(img, rect, &image.Uniform{C: bg}, image.Point{}, draw.Src)

	if c.Text == "" {
		return
	}

	m := face.Metrics()
	dot := fixed.Point26_6{
		X: fixed.I(rect.Min.X),
		Y: fixed.I(rect.Min.Y) + m.Ascent,
	}
	drawText(img, fg, face, fallbackFaces, dot, c.Text)

	if c.Bold {
		dot = fixed.Point26_6{
			X: fixed.I(rect.Min.X + 1),
			Y: fixed.I(rect.Min.Y) + m.Ascent,
		}
		drawText(img, fg, face, fallbackFaces, dot, c.Text)
	}

	if c.Underline {
		uy := rect.Max.Y - 1
		for x := rect.Min.X; x < rect.Max.X; x++ {
			img.Set(x, uy, fg)
		}
	}
}

func drawText(img *image.RGBA, fg color.RGBA, face font.Face, fallbackFaces []font.Face, dot fixed.Point26_6, text string) {
	d := &font.Drawer{
		Dst: img,
		Src: &image.Uniform{C: fg},
		Dot: dot,
	}
	var currentFace font.Face
	var runes []rune
	flush := func() {
		if len(runes) == 0 {
			return
		}
		d.Face = currentFace
		d.DrawString(string(runes))
		runes = runes[:0]
	}
	for _, r := range text {
		nextFace := faceForRune(face, fallbackFaces, r)
		if currentFace == nil {
			currentFace = nextFace
		}
		if nextFace != currentFace {
			flush()
			currentFace = nextFace
		}
		runes = append(runes, r)
	}
	flush()
}

func faceForRune(face font.Face, fallbackFaces []font.Face, r rune) font.Face {
	if _, ok := face.GlyphAdvance(r); ok {
		return face
	}
	for _, fallbackFace := range fallbackFaces {
		if fallbackFace == nil {
			continue
		}
		if _, ok := fallbackFace.GlyphAdvance(r); ok {
			return fallbackFace
		}
	}
	return face
}

func cellRect(bounds image.Rectangle, cols, rows, cx, cy, width int) image.Rectangle {
	if cols <= 0 || rows <= 0 {
		return image.Rectangle{}
	}
	if width <= 0 {
		width = 1
	}
	x1 := bounds.Min.X + cx*bounds.Dx()/cols
	x2 := bounds.Min.X + (cx+width)*bounds.Dx()/cols
	if x2 > bounds.Max.X {
		x2 = bounds.Max.X
	}
	y1 := bounds.Min.Y + cy*bounds.Dy()/rows
	y2 := bounds.Min.Y + (cy+1)*bounds.Dy()/rows
	if y2 > bounds.Max.Y {
		y2 = bounds.Max.Y
	}
	return image.Rect(x1, y1, x2, y2)
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
