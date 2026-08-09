package play

import (
	"bytes"
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/trace"
)

func TestDrawMouseAnnotationDrawsEveryRecordedGesture(t *testing.T) {
	point := func(v int) *int { return &v }
	tests := []struct {
		name  string
		mouse trace.MouseInput
	}{
		{"left click", trace.MouseInput{Gesture: "click", X: point(2), Y: point(1), Button: "left"}},
		{"right click", trace.MouseInput{Gesture: "click", X: point(2), Y: point(1), Button: "right"}},
		{"middle click", trace.MouseInput{Gesture: "click", X: point(2), Y: point(1), Button: "middle"}},
		{"hover", trace.MouseInput{Gesture: "hover", X: point(2), Y: point(1)}},
		{"scroll up", trace.MouseInput{Gesture: "scroll", X: point(2), Y: point(1), Direction: "up", Ticks: 1}},
		{"scroll down", trace.MouseInput{Gesture: "scroll", X: point(2), Y: point(1), Direction: "down", Ticks: 1}},
		{"drag", trace.MouseInput{Gesture: "drag", FromX: point(1), FromY: point(1), ToX: point(3), ToY: point(1), Button: "left"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img := annotationCanvas()
			if !drawMouseAnnotation(img, &tt.mouse, 5, 3, 0.5) {
				t.Fatal("drawMouseAnnotation returned false")
			}
			if bytes.Equal(img.Pix, annotationCanvas().Pix) {
				t.Fatal("annotation did not modify image")
			}
		})
	}
}

func TestDrawMouseAnnotationClickButtonsHaveDistinctShapesAndColors(t *testing.T) {
	point := func(v int) *int { return &v }
	left := annotationCanvas()
	right := annotationCanvas()
	middle := annotationCanvas()
	drawMouseAnnotation(left, &trace.MouseInput{Gesture: "click", X: point(2), Y: point(1), Button: "left"}, 5, 3, 0.5)
	drawMouseAnnotation(right, &trace.MouseInput{Gesture: "click", X: point(2), Y: point(1), Button: "right"}, 5, 3, 0.5)
	drawMouseAnnotation(middle, &trace.MouseInput{Gesture: "click", X: point(2), Y: point(1), Button: "middle"}, 5, 3, 0.5)
	if bytes.Equal(left.Pix, right.Pix) || bytes.Equal(left.Pix, middle.Pix) || bytes.Equal(right.Pix, middle.Pix) {
		t.Fatal("button annotations should have distinct shapes and colors")
	}
}

func TestDrawMouseAnnotationAnimationChangesFrame(t *testing.T) {
	point := func(v int) *int { return &v }
	mouse := &trace.MouseInput{Gesture: "click", X: point(2), Y: point(1), Button: "left"}
	start, end := annotationCanvas(), annotationCanvas()
	drawMouseAnnotation(start, mouse, 5, 3, 0)
	drawMouseAnnotation(end, mouse, 5, 3, 1)
	if bytes.Equal(start.Pix, end.Pix) {
		t.Fatal("animation phases produced identical frames")
	}
}

func TestDrawMouseAnnotationHasContrastOnDarkAndLightBackgrounds(t *testing.T) {
	x, y := 2, 1
	mouse := &trace.MouseInput{Gesture: "click", X: &x, Y: &y, Button: "left"}
	tests := []struct {
		name       string
		background color.RGBA
		contrast   func(color.RGBA) bool
	}{
		{
			name:       "dark background has white outline",
			background: color.RGBA{A: 255},
			contrast: func(c color.RGBA) bool {
				return c.R >= 230 && c.G >= 230 && c.B >= 230
			},
		},
		{
			name:       "light background has black outline",
			background: color.RGBA{R: 255, G: 255, B: 255, A: 255},
			contrast: func(c color.RGBA) bool {
				return c.R <= 25 && c.G <= 25 && c.B <= 25
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img := annotationCanvasWithBackground(tt.background)
			if !drawMouseAnnotation(img, mouse, 5, 3, 0.25) {
				t.Fatal("drawMouseAnnotation returned false")
			}
			var sawContrast, sawColor bool
			for py := img.Bounds().Min.Y; py < img.Bounds().Max.Y; py++ {
				for px := img.Bounds().Min.X; px < img.Bounds().Max.X; px++ {
					c := img.RGBAAt(px, py)
					sawContrast = sawContrast || tt.contrast(c)
					sawColor = sawColor || (c.R < 140 && c.G > 160 && c.B > 190)
				}
			}
			if !sawContrast || !sawColor {
				t.Fatalf("outline/color present = %v/%v, want true/true", sawContrast, sawColor)
			}
		})
	}
}

func TestDrawMouseAnnotationHasBlackAndWhiteOutlines(t *testing.T) {
	x, y := 2, 1
	img := annotationCanvasWithBackground(color.RGBA{R: 128, G: 128, B: 128, A: 255})
	if !drawMouseAnnotation(img, &trace.MouseInput{Gesture: "click", X: &x, Y: &y, Button: "left"}, 5, 3, 0.25) {
		t.Fatal("drawMouseAnnotation returned false")
	}

	var sawBlack, sawWhite bool
	for py := img.Bounds().Min.Y; py < img.Bounds().Max.Y; py++ {
		for px := img.Bounds().Min.X; px < img.Bounds().Max.X; px++ {
			c := img.RGBAAt(px, py)
			sawBlack = sawBlack || (c.R <= 25 && c.G <= 25 && c.B <= 25)
			sawWhite = sawWhite || (c.R >= 230 && c.G >= 230 && c.B >= 230)
		}
	}
	if !sawBlack || !sawWhite {
		t.Fatalf("black/white outlines present = %v/%v, want true/true", sawBlack, sawWhite)
	}
}

func TestMouseAnnotationTimingIsQuicker(t *testing.T) {
	if mouseAnnotationDuration != 500*time.Millisecond {
		t.Fatalf("mouseAnnotationDuration = %v, want 500ms", mouseAnnotationDuration)
	}
}

func TestDrawMouseAnnotationRejectsMalformedOrOutOfBoundsMetadata(t *testing.T) {
	point := func(v int) *int { return &v }
	tests := []*trace.MouseInput{
		nil,
		{Gesture: "unknown", X: point(1), Y: point(1)},
		{Gesture: "click", X: nil, Y: point(1)},
		{Gesture: "click", X: point(-1), Y: point(1)},
		{Gesture: "click", X: point(5), Y: point(1)},
		{Gesture: "scroll", X: point(1), Y: point(1), Direction: "sideways"},
		{Gesture: "drag", FromX: point(1), FromY: point(1), ToX: point(7), ToY: point(1)},
	}
	for _, mouse := range tests {
		img := annotationCanvas()
		before := append([]byte(nil), img.Pix...)
		if drawMouseAnnotation(img, mouse, 5, 3, 0.5) {
			t.Fatalf("drawMouseAnnotation(%+v) = true, want false", mouse)
		}
		if !bytes.Equal(before, img.Pix) {
			t.Fatalf("drawMouseAnnotation(%+v) modified image", mouse)
		}
	}
}

func TestMouseCellCenterMapsZeroBasedCellToImageCenter(t *testing.T) {
	x, y := 0, 0
	img := image.NewRGBA(image.Rect(10, 20, 110, 80))
	gotX, gotY, ok := mouseCellCenter(img, 5, 3, &x, &y)
	if !ok || gotX != 20 || gotY != 30 {
		t.Fatalf("mouseCellCenter = (%v,%v,%v), want (20,30,true)", gotX, gotY, ok)
	}
}

func annotationCanvas() *image.RGBA {
	return annotationCanvasWithBackground(color.RGBA{R: 12, G: 16, B: 24, A: 255})
}

func annotationCanvasWithBackground(background color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 100, 60))
	for y := range 60 {
		for x := range 100 {
			img.SetRGBA(x, y, background)
		}
	}
	return img
}
