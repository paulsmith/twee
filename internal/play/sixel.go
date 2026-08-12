package play

import (
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"io"
)

type sixelSink struct {
	w            io.Writer
	terminalCols int
	terminalRows int
}

func newSixelSink(w io.Writer, size terminalSize) *sixelSink {
	return &sixelSink{w: w, terminalCols: size.Cols, terminalRows: size.Rows}
}

func (s *sixelSink) SetTerminalSize(cols, rows int) {
	s.terminalCols = cols
	s.terminalRows = rows
}

func (s *sixelSink) Emit(img *image.RGBA, cols, rows int, toast, status string, statusVisible bool) error {
	// Sixel has no portable placement identifier. Erasing the alternate screen
	// makes every opaque frame overwrite-safe, including after a resize.
	if _, err := io.WriteString(s.w, "\x1b[H\x1b[2J"); err != nil {
		return err
	}
	if err := encodeSixel(s.w, img); err != nil {
		return err
	}
	if statusVisible {
		return writeStatusRow(s.w, s.terminalCols, s.terminalRows, toast, status)
	}
	return nil
}

func (s *sixelSink) Close() error { return nil }

// encodeSixel writes a deterministic, opaque sixel DCS. It uses an exact
// palette when possible and the standard Plan 9 palette with deterministic
// Floyd-Steinberg dithering otherwise, so the protocol never exceeds its 256
// color-register limit.
func encodeSixel(w io.Writer, img *image.RGBA) error {
	paletted := sixelPalettize(img)
	bounds := paletted.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return fmt.Errorf("sixel: empty image")
	}
	// P2=1 makes zero bits transparent, which lets successive palette planes
	// compose without clearing one another. The sink erases the screen before
	// each DCS, and every source pixel is present in one opaque color plane.
	if _, err := fmt.Fprintf(w, "\x1bP0;1;0q\"1;1;%d;%d", width, height); err != nil {
		return err
	}

	used := make([]bool, len(paletted.Palette))
	for _, index := range paletted.Pix {
		used[int(index)] = true
	}
	for index, c := range paletted.Palette {
		if !used[index] {
			continue
		}
		r, g, b, _ := c.RGBA()
		if _, err := fmt.Fprintf(w, "#%d;2;%d;%d;%d", index,
			percent16(r), percent16(g), percent16(b)); err != nil {
			return err
		}
	}

	data := make([]byte, width)
	firstBand := true
	for bandY := bounds.Min.Y; bandY < bounds.Max.Y; bandY += 6 {
		if !firstBand {
			if _, err := io.WriteString(w, "-"); err != nil {
				return err
			}
		}
		firstBand = false
		firstColor := true
		for index := range paletted.Palette {
			if !used[index] || !sixelBandUses(paletted, uint8(index), bandY) {
				continue
			}
			if !firstColor {
				if _, err := io.WriteString(w, "$"); err != nil {
					return err
				}
			}
			firstColor = false
			if _, err := fmt.Fprintf(w, "#%d", index); err != nil {
				return err
			}
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				bits := byte(0)
				for dy := 0; dy < 6 && bandY+dy < bounds.Max.Y; dy++ {
					if paletted.ColorIndexAt(x, bandY+dy) == uint8(index) {
						bits |= 1 << dy
					}
				}
				data[x-bounds.Min.X] = '?' + bits
			}
			if err := writeSixelRLE(w, data); err != nil {
				return err
			}
		}
	}
	_, err := io.WriteString(w, "\x1b\\")
	return err
}

func percent16(v uint32) uint32 { return (v*100 + 32767) / 65535 }

func sixelBandUses(img *image.Paletted, index uint8, bandY int) bool {
	b := img.Bounds()
	for y := bandY; y < bandY+6 && y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.ColorIndexAt(x, y) == index {
				return true
			}
		}
	}
	return false
}

func writeSixelRLE(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n := 1
		for n < len(data) && data[n] == data[0] {
			n++
		}
		if n >= 4 {
			if _, err := fmt.Fprintf(w, "!%d%c", n, data[0]); err != nil {
				return err
			}
		} else {
			if _, err := w.Write(data[:n]); err != nil {
				return err
			}
		}
		data = data[n:]
	}
	return nil
}

func sixelPalettize(img *image.RGBA) *image.Paletted {
	bounds := img.Bounds()
	seen := make(map[color.RGBA]int)
	colors := make(color.Palette, 0, 256)
	exact := true
	for y := bounds.Min.Y; y < bounds.Max.Y && exact; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)
			c.A = 255
			if _, ok := seen[c]; ok {
				continue
			}
			if len(colors) == 256 {
				exact = false
				break
			}
			seen[c] = len(colors)
			colors = append(colors, c)
		}
	}
	if !exact {
		out := image.NewPaletted(bounds, palette.Plan9)
		draw.FloydSteinberg.Draw(out, bounds, img, bounds.Min)
		return out
	}
	if len(colors) == 0 {
		colors = append(colors, color.RGBA{A: 255})
	}
	out := image.NewPaletted(bounds, colors)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)
			c.A = 255
			out.SetColorIndex(x, y, uint8(seen[c]))
		}
	}
	return out
}
