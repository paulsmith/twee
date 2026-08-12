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

// Export replays the bundle at path and writes a replay artifact to outPath.
// The format is chosen by extension: .gif, .html, .mp4, .webm, or .cast.
func Export(path, outPath string, opts Options) error {
	_, err := ExportWithResult(path, outPath, opts)
	return err
}

// Result reports format-specific conversion information.
type Result struct {
	OmittedEvents int
}

// ExportWithResult replays the bundle at path and writes a replay artifact to
// outPath. For cast output, Result reports records omitted because no faithful
// asciicast representation exists.
func ExportWithResult(path, outPath string, opts Options) (Result, error) {
	opts.normalize()

	ext := strings.ToLower(filepath.Ext(outPath))
	if ext != ".gif" && ext != ".html" && ext != ".mp4" && ext != ".webm" && ext != ".cast" {
		return Result{}, fmt.Errorf("twee export: unsupported output format %q (use .gif, .html, .mp4, .webm, or .cast)", ext)
	}
	if ext == ".cast" {
		result, err := exportCast(path, outPath, opts.IncludeInput)
		return Result{OmittedEvents: result.OmittedEvents}, err
	}

	var ffmpeg string
	if ext == ".mp4" || ext == ".webm" {
		ffmpeg = opts.FFmpeg
		if ffmpeg == "" {
			ffmpeg = "ffmpeg"
		}
		resolved, err := exec.LookPath(ffmpeg)
		if err != nil {
			return Result{}, fmt.Errorf("twee export: mp4/webm output requires ffmpeg: %w", err)
		}
		ffmpeg = resolved
	}

	b, err := play.OpenBundle(path)
	if err != nil {
		return Result{}, err
	}
	cv, err := newCanvas(b.MaxCols, b.MaxRows, opts.FontSize, opts.Crop, opts.InputOverlay)
	if err != nil {
		return Result{}, fmt.Errorf("twee export: %w", err)
	}

	snk, err := newSink(ext, outPath, ffmpeg, opts.Quality)
	if err != nil {
		return Result{}, fmt.Errorf("twee export: %w", err)
	}
	defer snk.abort()
	err = replay(b.Events, b.Manifest.Cols, b.Manifest.Rows, opts, vt.New,
		func(s vt.Snapshot, overlay string, d time.Duration) error {
			img, err := cv.compose(s, overlay)
			if err != nil {
				return err
			}
			return snk.add(img, d)
		})
	if err != nil {
		return Result{}, fmt.Errorf("twee export: %w", err)
	}
	if err := snk.close(); err != nil {
		return Result{}, fmt.Errorf("twee export: %w", err)
	}
	return Result{}, nil
}

func newSink(ext, outPath, ffmpeg, quality string) (sink, error) {
	switch ext {
	case ".gif":
		return newGIFSink(outPath)
	case ".html":
		return newHTMLSink(outPath)
	case ".mp4", ".webm":
		return newFFmpegSink(outPath, ffmpeg, quality)
	default:
		panic("unsupported extension checked by Export")
	}
}
