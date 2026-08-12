package play

import (
	"bytes"
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestEncodeSixelGoldenTwoColors(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 6))
	for y := range 6 {
		for x := range 4 {
			c := color.RGBA{R: 255, A: 255}
			if x >= 2 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var out bytes.Buffer
	if err := encodeSixel(&out, img); err != nil {
		t.Fatal(err)
	}
	want := "\x1bP0;1;0q\"1;1;4;6" +
		"#0;2;100;0;0#1;2;0;0;100" +
		"#0~~??$#1??~~\x1b\\"
	if out.String() != want {
		t.Fatalf("sixel = %q, want %q", out.String(), want)
	}
}

func TestEncodeSixelRLEAndDeterminism(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 5, 1))
	for x := range 5 {
		img.SetRGBA(x, 0, color.RGBA{R: 255, A: 255})
	}
	var first, second bytes.Buffer
	if err := encodeSixel(&first, img); err != nil {
		t.Fatal(err)
	}
	if err := encodeSixel(&second, img); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatal("encoding is not deterministic")
	}
	if !strings.Contains(first.String(), "#0!5@") {
		t.Fatalf("RLE missing from %q", first.String())
	}
}

func TestSixelPalettizeLimitsColors(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 257, 1))
	for x := range 257 {
		img.SetRGBA(x, 0, color.RGBA{R: uint8(x), G: uint8(x >> 8), B: uint8(255 - x), A: 255})
	}
	got := sixelPalettize(img)
	if len(got.Palette) > 256 {
		t.Fatalf("palette = %d colors, want <=256", len(got.Palette))
	}
}

func TestSixelSinkClearsAndPinsStatusRow(t *testing.T) {
	var out bytes.Buffer
	sink := &sixelSink{w: &out, terminalCols: 80, terminalRows: 24}
	if err := sink.Emit(image.NewRGBA(image.Rect(0, 0, 1, 1)), 2, 3, "toast", "status", true); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"\x1b[H\x1b[2J\x1bP0;1;0q",
		"\x1b[24;1H\x1b[0m\x1b[2K\x1b[7mstatus │ toast", "\x1b[H",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q in %q", want, out.String())
		}
	}
}
