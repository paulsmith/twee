package export

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/paulsmith/twee/internal/play"
	"github.com/paulsmith/twee/internal/vt"
)

// Export replays the bundle at path and writes a video to outPath. The
// container is chosen by extension: .gif, .mp4, or .webm.
func Export(path, outPath string, opts Options) error {
	opts.normalize()

	ext := strings.ToLower(filepath.Ext(outPath))
	if ext != ".gif" && ext != ".mp4" && ext != ".webm" {
		return fmt.Errorf("twee export: unsupported output format %q (use .gif, .mp4, or .webm)", ext)
	}

	var ffmpeg string
	if ext == ".mp4" || ext == ".webm" {
		ffmpeg = opts.FFmpeg
		if ffmpeg == "" {
			ffmpeg = "ffmpeg"
		}
		resolved, err := exec.LookPath(ffmpeg)
		if err != nil {
			return fmt.Errorf("twee export: mp4/webm output requires ffmpeg: %w", err)
		}
		ffmpeg = resolved
	}

	b, err := play.OpenBundle(path)
	if err != nil {
		return err
	}
	cv, err := newCanvas(b.MaxCols, b.MaxRows, opts.FontSize, opts.Crop, opts.InputOverlay)
	if err != nil {
		return fmt.Errorf("twee export: %w", err)
	}

	snk, err := newSink(ext, outPath, ffmpeg)
	if err != nil {
		return fmt.Errorf("twee export: %w", err)
	}
	err = replay(b.Events, b.Manifest.Cols, b.Manifest.Rows, opts, vt.New,
		func(s vt.Snapshot, overlay string, d time.Duration) error {
			img, err := cv.compose(s, overlay)
			if err != nil {
				return err
			}
			return snk.add(img, d)
		})
	if err != nil {
		return fmt.Errorf("twee export: %w", err)
	}
	if err := snk.close(); err != nil {
		return fmt.Errorf("twee export: %w", err)
	}
	return nil
}

func newSink(ext, outPath, ffmpeg string) (sink, error) {
	switch ext {
	case ".gif":
		return newGIFSink(outPath)
	case ".mp4", ".webm":
		return newFFmpegSink(outPath, ffmpeg)
	default:
		panic("unsupported extension checked by Export")
	}
}
