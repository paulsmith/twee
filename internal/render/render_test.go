package render

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/paulsmith/twee/internal/engine"
)

func TestRenderEmitsPNG(t *testing.T) {
	snap := engine.Snapshot{
		Cols: 10, Rows: 2,
		Lines: []engine.Line{
			{Cells: makeCells("hello     ")},
			{Cells: makeCells("world!    ")},
		},
	}
	img, err := Render(snap, Default())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if img.Bounds().Empty() {
		t.Fatal("empty image")
	}
	var buf bytes.Buffer
	if err := EncodePNG(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	cfg, err := png.DecodeConfig(&buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		t.Errorf("got %dx%d", cfg.Width, cfg.Height)
	}
}

func TestPNGBytes(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))

	b, err := PNGBytes(img)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg.Width != 1 || cfg.Height != 1 {
		t.Fatalf("decoded size = %dx%d, want 1x1", cfg.Width, cfg.Height)
	}
}

func TestEncodePNGFile(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	path := filepath.Join(t.TempDir(), "frame.png")

	if err := EncodePNGFile(path, img); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg.Width != 1 || cfg.Height != 1 {
		t.Fatalf("decoded size = %dx%d, want 1x1", cfg.Width, cfg.Height)
	}
}

func TestRenderUsesRequestedPixelSize(t *testing.T) {
	snap := engine.Snapshot{
		Cols: 10, Rows: 2,
		Lines: []engine.Line{
			{Cells: makeCells("hello     ")},
			{Cells: makeCells("world!    ")},
		},
	}
	img, err := Render(snap, Options{PixelWidth: 123, PixelHeight: 45})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got := img.Bounds().Dx(); got != 123 {
		t.Errorf("width = %d, want 123", got)
	}
	if got := img.Bounds().Dy(); got != 45 {
		t.Errorf("height = %d, want 45", got)
	}
}

func TestFaceForRuneUsesFallbackForAnyPrimaryMiss(t *testing.T) {
	primary := &testFace{glyphs: map[rune]bool{'A': true}}
	firstFallback := &testFace{glyphs: map[rune]bool{'B': true}}
	secondFallback := &testFace{glyphs: map[rune]bool{'C': true}}
	fallbackFaces := []font.Face{firstFallback, secondFallback}

	tests := []struct {
		name string
		r    rune
		want font.Face
	}{
		{name: "primary hit", r: 'A', want: primary},
		{name: "first fallback hit", r: 'B', want: firstFallback},
		{name: "later fallback hit", r: 'C', want: secondFallback},
		{name: "all faces miss", r: 'D', want: primary},
	}

	for _, tt := range tests {
		if got := faceForRune(primary, fallbackFaces, tt.r); got != tt.want {
			t.Fatalf("%s: faceForRune(%U) = %p, want %p", tt.name, tt.r, got, tt.want)
		}
	}
}

func TestFallbackFontsCoverKnownTerminalSymbols(t *testing.T) {
	face, err := Face(14)
	if err != nil {
		t.Fatalf("face: %v", err)
	}
	monoFace, err := FallbackFace(14)
	if err != nil {
		t.Fatalf("fallback face: %v", err)
	}
	fallbackFaces, err := FallbackFaces(14)
	if err != nil {
		t.Fatalf("fallback faces: %v", err)
	}
	if len(fallbackFaces) == 0 || fallbackFaces[0] != monoFace {
		t.Fatal("Noto Sans Mono is not the first fallback face")
	}

	for _, r := range []rune{'⎿', '⏵', '⏺', '✢', '✳', '✻', '✽', '▰'} {
		if _, ok := face.GlyphAdvance(r); ok {
			t.Fatalf("primary face unexpectedly has %U %q", r, r)
		}
		if got := faceForRune(face, fallbackFaces, r); got == face {
			t.Fatalf("faceForRune(%U) did not use a fallback", r)
		}
	}
}

func makeCells(s string) []engine.Cell {
	out := make([]engine.Cell, 0, len(s))
	for _, r := range s {
		out = append(out, engine.Cell{Text: string(r), Width: 1})
	}
	return out
}

type testFace struct {
	glyphs map[rune]bool
}

func (f *testFace) Close() error {
	return nil
}

func (f *testFace) Glyph(fixed.Point26_6, rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	return image.Rectangle{}, nil, image.Point{}, fixed.I(1), false
}

func (f *testFace) GlyphBounds(r rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	return fixed.Rectangle26_6{}, fixed.I(1), f.glyphs[r]
}

func (f *testFace) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	return fixed.I(1), f.glyphs[r]
}

func (f *testFace) Kern(rune, rune) fixed.Int26_6 {
	return 0
}

func (f *testFace) Metrics() font.Metrics {
	return font.Metrics{}
}
