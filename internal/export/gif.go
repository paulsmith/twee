package export

import (
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"os"
	"time"
)

// gifSink accumulates paletted frames and encodes the GIF on close. The
// standard library has no streaming GIF append API, so memory scales with
// frame count.
type gifSink struct {
	path      string
	g         gif.GIF
	elapsed   time.Duration
	emittedCS int
}

func newGIFSink(path string) (*gifSink, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return &gifSink{path: path}, nil
}

func (s *gifSink) add(img *image.RGBA, d time.Duration) error {
	s.g.Image = append(s.g.Image, palettize(img))
	s.elapsed += d
	cs := roundedCentiseconds(s.elapsed) - s.emittedCS
	if cs < 2 {
		cs = 2 // browsers treat 0-1cs as 100ms.
	}
	s.emittedCS += cs
	s.g.Delay = append(s.g.Delay, cs)
	s.g.Disposal = append(s.g.Disposal, gif.DisposalNone)
	return nil
}

func (s *gifSink) close() error {
	f, err := os.Create(s.path)
	if err != nil {
		return err
	}
	if err := gif.EncodeAll(f, &s.g); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func roundedCentiseconds(d time.Duration) int {
	return int((d + 5*time.Millisecond) / (10 * time.Millisecond))
}

// palettize converts to a paletted frame. Terminal frames almost always use
// 256 or fewer distinct colors, so an exact palette is tried first; otherwise
// it falls back to Floyd-Steinberg dithering on the Plan9 palette.
func palettize(img *image.RGBA) *image.Paletted {
	b := img.Bounds()
	seen := make(map[color.RGBA]int)
	var pal color.Palette
	exact := true
	for y := b.Min.Y; y < b.Max.Y && exact; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if _, ok := seen[c]; ok {
				continue
			}
			if len(pal) == 256 {
				exact = false
				break
			}
			seen[c] = len(pal)
			pal = append(pal, c)
		}
	}
	if !exact {
		out := image.NewPaletted(b, palette.Plan9)
		draw.FloydSteinberg.Draw(out, b, img, b.Min)
		return out
	}
	out := image.NewPaletted(b, pal)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out.SetColorIndex(x, y, uint8(seen[img.RGBAAt(x, y)]))
		}
	}
	return out
}
