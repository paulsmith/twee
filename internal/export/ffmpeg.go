package export

import (
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/paulsmith/twee/internal/render"
)

// ffmpegSink spools frames as PNGs into a temp directory and runs one ffmpeg
// concat-demuxer invocation on close. ffmpeg executes with the temp dir as its
// working directory, so the list uses bare relative filenames.
type ffmpegSink struct {
	outPath string
	ffmpeg  string
	dir     string
	n       int
	list    strings.Builder
	lastRef string
}

func newFFmpegSink(outPath, ffmpeg string) (*ffmpegSink, error) {
	abs, err := filepath.Abs(outPath)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "twee-export-")
	if err != nil {
		return nil, err
	}
	return &ffmpegSink{outPath: abs, ffmpeg: ffmpeg, dir: dir}, nil
}

func (s *ffmpegSink) add(img *image.RGBA, d time.Duration) error {
	s.n++
	name := fmt.Sprintf("frame-%06d.png", s.n)
	f, err := os.Create(filepath.Join(s.dir, name))
	if err != nil {
		return err
	}
	if err := render.EncodePNG(f, img); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	s.noteFrame(name, d)
	return nil
}

func (s *ffmpegSink) noteFrame(name string, d time.Duration) {
	fmt.Fprintf(&s.list, "file %s\nduration %.6f\n", name, d.Seconds())
	s.lastRef = name
}

// concatList returns the demuxer input. The final file entry is repeated
// because the concat demuxer otherwise ignores the last duration directive.
func (s *ffmpegSink) concatList() string {
	if s.lastRef == "" {
		return s.list.String()
	}
	return s.list.String() + "file " + s.lastRef + "\n"
}

func ffmpegArgs(listName, outPath string) []string {
	args := []string{"-y", "-f", "concat", "-i", listName, "-fps_mode", "vfr",
		"-pix_fmt", "yuv420p"}
	if strings.HasSuffix(strings.ToLower(outPath), ".webm") {
		args = append(args, "-c:v", "libvpx-vp9")
	}
	return append(args, outPath)
}

func (s *ffmpegSink) close() error {
	const listName = "list.txt"
	if err := os.WriteFile(filepath.Join(s.dir, listName), []byte(s.concatList()), 0o644); err != nil {
		return err
	}
	cmd := exec.Command(s.ffmpeg, ffmpegArgs(listName, s.outPath)...)
	cmd.Dir = s.dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w\n%s\nframes kept in %s for debugging",
			err, tail(stderr.String(), 2000), s.dir)
	}
	return os.RemoveAll(s.dir)
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
