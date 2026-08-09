package export

import (
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func solidFrame(c color.RGBA, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestGIFSinkRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.gif")
	s, err := newGIFSink(path)
	if err != nil {
		t.Fatal(err)
	}
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	if err := s.add(solidFrame(red, 10, 10), 500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := s.add(solidFrame(blue, 10, 10), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := s.close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 2 {
		t.Fatalf("got %d frames, want 2", len(g.Image))
	}
	if g.Delay[0] != 50 || g.Delay[1] != 100 {
		t.Errorf("delays = %v, want [50 100] (centiseconds)", g.Delay)
	}
	if got := g.Image[0].At(5, 5); !sameColor(got, red) {
		t.Errorf("frame 0 pixel = %v, want exact red (no dithering)", got)
	}
	assertFileMode(t, path, 0o600)
}

func sameColor(a color.Color, b color.RGBA) bool {
	r, g, bl, _ := a.RGBA()
	return uint8(r>>8) == b.R && uint8(g>>8) == b.G && uint8(bl>>8) == b.B
}

func TestGIFSinkDelayRemainderCarry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.gif")
	s, err := newGIFSink(path)
	if err != nil {
		t.Fatal(err)
	}
	// 3 frames x 33.33ms: naive rounding gives 3+3+3=9cs; carry gives
	// 3+3+4=10cs (total 100ms preserved).
	red := solidFrame(color.RGBA{255, 0, 0, 255}, 4, 4)
	green := solidFrame(color.RGBA{0, 255, 0, 255}, 4, 4)
	d := 33333333 * time.Nanosecond
	frames := []*image.RGBA{red, green, red}
	for _, f := range frames {
		if err := s.add(f, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.close(); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(path)
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, dl := range g.Delay {
		total += dl
	}
	if total != 10 {
		t.Errorf("total delay = %dcs, want 10cs (remainder carry)", total)
	}
}

func TestGIFSinkClampsMinimumDelay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.gif")
	s, _ := newGIFSink(path)
	red := solidFrame(color.RGBA{255, 0, 0, 255}, 4, 4)
	if err := s.add(red, 5*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := s.close(); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(path)
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if g.Delay[0] < 2 {
		t.Errorf("delay = %dcs, want >= 2 (browser minimum)", g.Delay[0])
	}
}

func TestGIFSinkAbortPreservesDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.gif")
	if err := os.WriteFile(path, []byte("previous artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := newGIFSink(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.add(solidFrame(color.RGBA{R: 255, A: 255}, 4, 4), time.Second); err != nil {
		t.Fatal(err)
	}
	s.abort()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "previous artifact" {
		t.Fatalf("destination after abort = %q", got)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, ".out.gif.*.tmp")); err != nil || len(matches) != 0 {
		t.Fatalf("temporary outputs after abort = %v, err = %v", matches, err)
	}
}

func TestGIFSinkPreservesDestinationMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	path := filepath.Join(t.TempDir(), "out.gif")
	if err := os.WriteFile(path, []byte("previous artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := newGIFSink(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abort()
	if err := s.add(solidFrame(color.RGBA{R: 255, A: 255}, 4, 4), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := s.close(); err != nil {
		t.Fatal(err)
	}
	assertFileMode(t, path, 0o600)
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %04o, want %04o", path, got, want)
	}
}
