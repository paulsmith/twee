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
	mp4 := ffmpegArgs("list.txt", "/abs/out.mp4", "medium")
	joined := strings.Join(mp4, " ")
	for _, want := range []string{"-f concat", "-fps_mode vfr", "-pix_fmt yuv420p",
		"-c:v libx264", "-crf 23", "-preset medium", "/abs/out.mp4"} {
		if !strings.Contains(joined, want) {
			t.Errorf("mp4 args %q missing %q", joined, want)
		}
	}
	webm := strings.Join(ffmpegArgs("list.txt", "/abs/out.webm", "medium"), " ")
	for _, want := range []string{"-c:v libvpx-vp9", "-crf 33", "-b:v 0"} {
		if !strings.Contains(webm, want) {
			t.Errorf("webm args %q missing %q", webm, want)
		}
	}
}

// TestFFmpegArgsQualityPresets pins down the CRF/preset each --quality
// level maps to, for both containers.
func TestFFmpegArgsQualityPresets(t *testing.T) {
	tests := []struct {
		quality string
		mp4Want []string
		webmCRF string
	}{
		{"low", []string{"-crf 28", "-preset veryfast"}, "-crf 40"},
		{"medium", []string{"-crf 23", "-preset medium"}, "-crf 33"},
		{"high", []string{"-crf 18", "-preset slow"}, "-crf 24"},
	}
	for _, tt := range tests {
		t.Run(tt.quality, func(t *testing.T) {
			mp4 := strings.Join(ffmpegArgs("list.txt", "/abs/out.mp4", tt.quality), " ")
			for _, want := range tt.mp4Want {
				if !strings.Contains(mp4, want) {
					t.Errorf("mp4 %s args %q missing %q", tt.quality, mp4, want)
				}
			}
			webm := strings.Join(ffmpegArgs("list.txt", "/abs/out.webm", tt.quality), " ")
			if !strings.Contains(webm, tt.webmCRF) {
				t.Errorf("webm %s args %q missing %q", tt.quality, webm, tt.webmCRF)
			}
		})
	}
}

// TestFFmpegArgsUnknownQualityFallsBackToMedium guards ffmpegArgs against
// a bad quality string reaching it directly (bypassing
// Options.normalize, which is what the CLI and Export always go
// through): it must fall back to "medium" instead of a zero-value
// lookup (crf 0, empty preset).
func TestFFmpegArgsUnknownQualityFallsBackToMedium(t *testing.T) {
	got := strings.Join(ffmpegArgs("list.txt", "/abs/out.mp4", "bogus"), " ")
	want := strings.Join(ffmpegArgs("list.txt", "/abs/out.mp4", "medium"), " ")
	if got != want {
		t.Errorf("args for unknown quality = %q, want same as medium %q", got, want)
	}
}

func TestFFmpegSinkIntegration(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	out := filepath.Join(t.TempDir(), "out.mp4")
	s, err := newFFmpegSink(out, ffmpeg, "medium")
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
