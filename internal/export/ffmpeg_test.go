package export

import (
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConcatListFormat(t *testing.T) {
	s := &ffmpegSink{}
	s.noteFrame("frame-000001.png", 500*time.Millisecond)
	s.noteFrame("frame-000002.png", 1250*time.Millisecond)
	got := s.concatList()
	want := strings.Join([]string{
		"file frame-000001.png",
		"duration 0.500000",
		"file frame-000002.png",
		"duration 1.250000",
		"file frame-000002.png",
		"",
	}, "\n")
	if got != want {
		t.Errorf("concat list:\n%s\nwant:\n%s", got, want)
	}
}

func TestFFmpegArgs(t *testing.T) {
	mp4 := ffmpegArgs("list.txt", "/abs/out.mp4")
	joined := strings.Join(mp4, " ")
	for _, want := range []string{"-f concat", "-fps_mode vfr", "-pix_fmt yuv420p", "/abs/out.mp4"} {
		if !strings.Contains(joined, want) {
			t.Errorf("mp4 args %q missing %q", joined, want)
		}
	}
	webm := strings.Join(ffmpegArgs("list.txt", "/abs/out.webm"), " ")
	if !strings.Contains(webm, "-c:v libvpx-vp9") {
		t.Errorf("webm args %q missing libvpx-vp9", webm)
	}
}

func TestFFmpegSinkIntegration(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	out := filepath.Join(t.TempDir(), "out.mp4")
	s, err := newFFmpegSink(out, ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	red := solidFrame(color.RGBA{255, 0, 0, 255}, 64, 64)
	blue := solidFrame(color.RGBA{0, 0, 255, 255}, 64, 64)
	for _, f := range []*image.RGBA{red, blue} {
		if err := s.add(f, 500*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.close(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(out)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("output missing or empty: %v", err)
	}
}
