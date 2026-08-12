package play

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/render"
)

func TestWriteITerm2PNG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	img.SetRGBA(1, 0, color.RGBA{B: 255, A: 255})
	var out bytes.Buffer
	if err := writeITerm2PNG(&out, img, 12, 4); err != nil {
		t.Fatal(err)
	}
	var encodedPNG bytes.Buffer
	if err := render.EncodePNG(&encodedPNG, img); err != nil {
		t.Fatal(err)
	}
	wantBytes := "\x1b]1337;File=size=" + strconv.Itoa(encodedPNG.Len()) +
		";width=12;height=4;preserveAspectRatio=0;inline=1:" +
		base64.StdEncoding.EncodeToString(encodedPNG.Bytes()) + "\x1b\\"
	if out.String() != wantBytes {
		t.Fatalf("sequence bytes = %q, want %q", out.String(), wantBytes)
	}
	const prefix = "\x1b]1337;File=size="
	if !strings.HasPrefix(out.String(), prefix) || !strings.HasSuffix(out.String(), "\x1b\\") {
		t.Fatalf("sequence framing = %q", out.String())
	}
	colon := bytes.IndexByte(out.Bytes(), ':')
	header := string(out.Bytes()[:colon])
	for _, want := range []string{"width=12", "height=4", "preserveAspectRatio=0", "inline=1"} {
		if !strings.Contains(header, want) {
			t.Fatalf("header %q missing %q", header, want)
		}
	}
	payload := out.Bytes()[colon+1 : out.Len()-2]
	decoded, err := base64.StdEncoding.DecodeString(string(payload))
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	got, err := png.Decode(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("png: %v", err)
	}
	if got.Bounds() != img.Bounds() {
		t.Fatalf("bounds = %v, want %v", got.Bounds(), img.Bounds())
	}
}

func TestITerm2SinkClearsAndPinsStatusRow(t *testing.T) {
	var out bytes.Buffer
	sink := &iterm2Sink{w: &out, terminalCols: 80, terminalRows: 24}
	if err := sink.Emit(image.NewRGBA(image.Rect(0, 0, 1, 1)), 2, 3, "toast", "status", true); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"\x1b[H\x1b[2J", "\x1b]1337;File=", "width=2;height=3",
		"\x1b[24;1H\x1b[0m\x1b[2K\x1b[7mstatus │ toast", "\x1b[H",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q in %q", want, out.String())
		}
	}
}
