package play

import (
	"bytes"
	"image"
	"image/color"
	"testing"

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
		{"scroll up", trace.MouseInput{Gesture: "scroll", X: point(2), Y: point(1), Direction: "up"}},
		{"scroll down", trace.MouseInput{Gesture: "scroll", X: point(2), Y: point(1), Direction: "down"}},
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
	img := image.NewRGBA(image.Rect(0, 0, 100, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 100; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 12, G: 16, B: 24, A: 255})
		}
	}
	return img
}
