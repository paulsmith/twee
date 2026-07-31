package play

import (
	"image"
	"math"
	"strings"

	"github.com/paulsmith/twee/internal/trace"
)

// drawMouseAnnotation draws a transient annotation for a recorded semantic
// mouse gesture. phase is normally in [0, 1], where zero is the instant the
// gesture is received. It returns false for incomplete, unknown, or
// out-of-bounds metadata so a bad trace cannot interrupt playback.
func drawMouseAnnotation(img *image.RGBA, mouse *trace.MouseInput, cols, rows int, phase float64) bool {
	if img == nil || mouse == nil || cols <= 0 || rows <= 0 || img.Bounds().Empty() {
		return false
	}
	phase = clamp01(phase)
	switch strings.ToLower(mouse.Gesture) {
	case "click":
		x, y, ok := mouseCellCenter(img, cols, rows, mouse.X, mouse.Y)
		if !ok {
			return false
		}
		color, shape := mouseButtonStyle(mouse.Button)
		scale := mouseCellScale(img, cols, rows)
		radius := scale * (0.32 + 0.75*phase)
		alpha := uint8(math.Round(220 * (1 - 0.72*phase)))
		switch shape {
		case mouseShapeDiamond:
			drawDiamond(img, x, y, radius, math.Max(1, scale*0.09), color, alpha)
		case mouseShapeSquare:
			drawSquare(img, x, y, radius, math.Max(1, scale*0.09), color, alpha)
		default:
			drawRing(img, x, y, radius, math.Max(1, scale*0.09), color, alpha)
		}
		return true
	case "hover":
		x, y, ok := mouseCellCenter(img, cols, rows, mouse.X, mouse.Y)
		if !ok {
			return false
		}
		scale := mouseCellScale(img, cols, rows)
		drawCrosshair(img, x, y, scale*(0.32+0.24*phase), math.Max(1, scale*0.08), mouseCyan, uint8(math.Round(170*(1-phase))))
		return true
	case "scroll":
		x, y, ok := mouseCellCenter(img, cols, rows, mouse.X, mouse.Y)
		if !ok || (mouse.Direction != "up" && mouse.Direction != "down") {
			return false
		}
		scale := mouseCellScale(img, cols, rows)
		drawScrollChevrons(img, x, y, scale*(0.34+0.12*phase), mouse.Direction == "up", uint8(math.Round(210*(1-0.65*phase))))
		return true
	case "drag":
		x0, y0, ok := mouseCellCenter(img, cols, rows, mouse.FromX, mouse.FromY)
		if !ok {
			return false
		}
		x1, y1, ok := mouseCellCenter(img, cols, rows, mouse.ToX, mouse.ToY)
		if !ok {
			return false
		}
		color, _ := mouseButtonStyle(mouse.Button)
		scale := mouseCellScale(img, cols, rows)
		ex, ey := x0+(x1-x0)*phase, y0+(y1-y0)*phase
		alpha := uint8(math.Round(220 * (1 - 0.55*phase)))
		drawLine(img, x0, y0, ex, ey, math.Max(1, scale*0.10), color, alpha)
		drawDisk(img, x0, y0, math.Max(2, scale*0.14), color, alpha)
		drawRing(img, ex, ey, math.Max(2, scale*0.22), math.Max(1, scale*0.08), color, alpha)
		return true
	default:
		return false
	}
}

type mouseShape uint8

const (
	mouseShapeRing mouseShape = iota
	mouseShapeDiamond
	mouseShapeSquare
)

type mouseColor struct{ r, g, b uint8 }

var (
	mouseCyan    = mouseColor{53, 220, 245}
	mouseMagenta = mouseColor{241, 78, 205}
	mouseAmber   = mouseColor{250, 184, 55}
	mouseGreen   = mouseColor{89, 220, 136}
)

func mouseButtonStyle(button string) (mouseColor, mouseShape) {
	switch strings.ToLower(button) {
	case "right":
		return mouseMagenta, mouseShapeDiamond
	case "middle":
		return mouseAmber, mouseShapeSquare
	default:
		return mouseCyan, mouseShapeRing
	}
}

func mouseCellCenter(img *image.RGBA, cols, rows int, x, y *int) (float64, float64, bool) {
	if x == nil || y == nil || *x < 0 || *x >= cols || *y < 0 || *y >= rows {
		return 0, 0, false
	}
	bounds := img.Bounds()
	return float64(bounds.Min.X) + (float64(*x)+0.5)*float64(bounds.Dx())/float64(cols),
		float64(bounds.Min.Y) + (float64(*y)+0.5)*float64(bounds.Dy())/float64(rows), true
}

func mouseCellScale(img *image.RGBA, cols, rows int) float64 {
	return math.Max(3, math.Min(float64(img.Bounds().Dx())/float64(cols), float64(img.Bounds().Dy())/float64(rows)))
}

func clamp01(v float64) float64 {
	if v < 0 || math.IsNaN(v) {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func drawRing(img *image.RGBA, cx, cy, radius, width float64, color mouseColor, alpha uint8) {
	drawWhere(img, cx-radius-width, cy-radius-width, cx+radius+width, cy+radius+width, func(x, y float64) bool {
		return math.Abs(math.Hypot(x-cx, y-cy)-radius) <= width
	}, color, alpha)
}

func drawDisk(img *image.RGBA, cx, cy, radius float64, color mouseColor, alpha uint8) {
	drawWhere(img, cx-radius, cy-radius, cx+radius, cy+radius, func(x, y float64) bool {
		return math.Hypot(x-cx, y-cy) <= radius
	}, color, alpha)
}

func drawDiamond(img *image.RGBA, cx, cy, radius, width float64, color mouseColor, alpha uint8) {
	drawWhere(img, cx-radius-width, cy-radius-width, cx+radius+width, cy+radius+width, func(x, y float64) bool {
		return math.Abs(math.Abs(x-cx)+math.Abs(y-cy)-radius) <= width
	}, color, alpha)
}

func drawSquare(img *image.RGBA, cx, cy, radius, width float64, color mouseColor, alpha uint8) {
	drawWhere(img, cx-radius-width, cy-radius-width, cx+radius+width, cy+radius+width, func(x, y float64) bool {
		return math.Abs(math.Max(math.Abs(x-cx), math.Abs(y-cy))-radius) <= width
	}, color, alpha)
}

func drawCrosshair(img *image.RGBA, cx, cy, radius, width float64, color mouseColor, alpha uint8) {
	drawWhere(img, cx-radius, cy-radius, cx+radius, cy+radius, func(x, y float64) bool {
		return (math.Abs(y-cy) <= width || math.Abs(x-cx) <= width) &&
			math.Max(math.Abs(x-cx), math.Abs(y-cy)) <= radius
	}, color, alpha)
}

func drawScrollChevrons(img *image.RGBA, cx, cy, scale float64, up bool, alpha uint8) {
	direction := 1.0
	if up {
		direction = -1
	}
	for _, offset := range []float64{-0.42, 0.08, 0.58} {
		y := cy + direction*offset*scale
		drawLine(img, cx-scale*0.34, y-direction*scale*0.16, cx, y+direction*scale*0.16, math.Max(1, scale*0.07), mouseGreen, alpha)
		drawLine(img, cx, y+direction*scale*0.16, cx+scale*0.34, y-direction*scale*0.16, math.Max(1, scale*0.07), mouseGreen, alpha)
	}
}

func drawLine(img *image.RGBA, x0, y0, x1, y1, width float64, color mouseColor, alpha uint8) {
	pad := width + 1
	drawWhere(img, math.Min(x0, x1)-pad, math.Min(y0, y1)-pad, math.Max(x0, x1)+pad, math.Max(y0, y1)+pad, func(x, y float64) bool {
		return pointSegmentDistance(x, y, x0, y0, x1, y1) <= width
	}, color, alpha)
}

func pointSegmentDistance(x, y, x0, y0, x1, y1 float64) float64 {
	dx, dy := x1-x0, y1-y0
	length2 := dx*dx + dy*dy
	if length2 == 0 {
		return math.Hypot(x-x0, y-y0)
	}
	t := ((x-x0)*dx + (y-y0)*dy) / length2
	t = clamp01(t)
	return math.Hypot(x-(x0+t*dx), y-(y0+t*dy))
}

func drawWhere(img *image.RGBA, minX, minY, maxX, maxY float64, keep func(float64, float64) bool, color mouseColor, alpha uint8) {
	if alpha == 0 {
		return
	}
	bounds := img.Bounds()
	x0 := max(bounds.Min.X, int(math.Floor(minX)))
	y0 := max(bounds.Min.Y, int(math.Floor(minY)))
	x1 := min(bounds.Max.X, int(math.Ceil(maxX))+1)
	y1 := min(bounds.Max.Y, int(math.Ceil(maxY))+1)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if keep(float64(x)+0.5, float64(y)+0.5) {
				blendMousePixel(img, x, y, color, alpha)
			}
		}
	}
}

func blendMousePixel(img *image.RGBA, x, y int, color mouseColor, alpha uint8) {
	i := img.PixOffset(x, y)
	a := int(alpha)
	inv := 255 - a
	img.Pix[i+0] = uint8((int(color.r)*a + int(img.Pix[i+0])*inv) / 255)
	img.Pix[i+1] = uint8((int(color.g)*a + int(img.Pix[i+1])*inv) / 255)
	img.Pix[i+2] = uint8((int(color.b)*a + int(img.Pix[i+2])*inv) / 255)
	img.Pix[i+3] = uint8(a + int(img.Pix[i+3])*inv/255)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
