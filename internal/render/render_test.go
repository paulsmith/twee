package render

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/paulsmith/research/twee/internal/engine"
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

func makeCells(s string) []engine.Cell {
	out := make([]engine.Cell, 0, len(s))
	for _, r := range s {
		out = append(out, engine.Cell{Text: string(r), Width: 1})
	}
	return out
}
