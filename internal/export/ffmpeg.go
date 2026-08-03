package export

import (
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/paulsmith/twee/internal/render"
)

// ffmpegSink spools frames as PNGs into a temp directory and runs one ffmpeg
// concat-demuxer invocation on close. ffmpeg executes with the temp dir as its
// working directory, so the list uses bare relative filenames.
type ffmpegSink struct {
	output  *stagedOutput
	ffmpeg  string
	quality string
	dir     string
	remove  func(string) error
	keepDir bool
	n       int
	list    strings.Builder
	lastRef string
}

func newFFmpegSink(outPath, ffmpeg, quality string) (*ffmpegSink, error) {
	dir, err := os.MkdirTemp("", "twee-export-")
	if err != nil {
		return nil, err
	}
	ext := filepath.Ext(outPath)
	output, temp, err := newStagedOutput(outPath, ext)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	if err := temp.Close(); err != nil {
		output.abort()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return &ffmpegSink{
		output:  output,
		ffmpeg:  ffmpeg,
		quality: quality,
		dir:     dir,
		remove:  os.RemoveAll,
	}, nil
}

func (s *ffmpegSink) add(img *image.RGBA, d time.Duration) error {
	s.n++
	name := fmt.Sprintf("frame-%06d.png", s.n)
	if err := render.EncodePNGFile(filepath.Join(s.dir, name), img); err != nil {
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

// x264Preset is the libx264 (mp4) CRF/preset pair for one --quality
// level. Lower CRF is higher quality (and a slower preset trades encode
// time for compression efficiency at the same CRF).
type x264Preset struct {
	crf    int
	preset string
}

// x264Presets maps --quality to libx264 settings. "medium" (23/medium)
// reproduces libx264's own defaults, so it's what mp4 output looked
// like before --quality existed at all.
var x264Presets = map[string]x264Preset{
	"low":    {crf: 28, preset: "veryfast"},
	"medium": {crf: 23, preset: "medium"},
	"high":   {crf: 18, preset: "slow"},
}

// vp9CRF maps --quality to a libvpx-vp9 (webm) constant-quality CRF.
var vp9CRF = map[string]int{
	"low":    40,
	"medium": 33,
	"high":   24,
}

// ffmpegArgs builds the ffmpeg invocation for the concat-demuxer input
// at listName, writing to outPath. quality (as normalized by
// Options.normalize: "low", "medium", or "high") selects the codec's
// CRF/preset; an unrecognized value falls back to "medium" the same way
// Options.normalize would, so a direct call here can't panic on a bad
// map lookup.
func ffmpegArgs(listName, outPath, quality string) []string {
	args := []string{"-y", "-f", "concat", "-i", listName, "-fps_mode", "vfr",
		"-pix_fmt", "yuv420p"}
	if strings.HasSuffix(strings.ToLower(outPath), ".webm") {
		crf, ok := vp9CRF[quality]
		if !ok {
			crf = vp9CRF["medium"]
		}
		// -b:v 0 puts libvpx-vp9 in constant-quality mode; without it,
		// -crf is ignored in favor of a target-bitrate mode.
		args = append(args, "-c:v", "libvpx-vp9", "-crf", strconv.Itoa(crf), "-b:v", "0")
	} else {
		p, ok := x264Presets[quality]
		if !ok {
			p = x264Presets["medium"]
		}
		args = append(args, "-c:v", "libx264", "-crf", strconv.Itoa(p.crf), "-preset", p.preset)
	}
	return append(args, outPath)
}

func (s *ffmpegSink) close() error {
	const listName = "list.txt"
	if err := os.WriteFile(filepath.Join(s.dir, listName), []byte(s.concatList()), 0o644); err != nil {
		return err
	}
	cmd := exec.Command(s.ffmpeg, ffmpegArgs(listName, s.output.temporary, s.quality)...)
	cmd.Dir = s.dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		s.keepDir = true
		return fmt.Errorf("ffmpeg failed: %w\n%s\nframes kept in %s for debugging",
			err, tail(stderr.String(), 2000), s.dir)
	}
	// Finish all fallible spool cleanup before committing the artifact. Once
	// Rename succeeds, close must not report an error for disposable state.
	if err := s.remove(s.dir); err != nil {
		return fmt.Errorf("remove temporary frames: %w", err)
	}
	s.dir = ""
	return s.output.commit()
}

func (s *ffmpegSink) abort() {
	s.output.abort()
	if s.dir != "" && !s.keepDir {
		_ = s.remove(s.dir)
		s.dir = ""
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
