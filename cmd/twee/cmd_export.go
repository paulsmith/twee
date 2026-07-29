package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/paulsmith/twee/internal/export"
)

func init() {
	register("export", runExport)
	registerUsage("export", `twee export <bundle.twee> -o <out.gif|out.mp4|out.webm>
Export a .twee trace bundle to a video file. The format is chosen by the
output extension. GIF is encoded in pure Go; MP4 and WebM require ffmpeg.

Frames are emitted only when the screen visibly changes (the cursor is not
rendered). Timing is faithful to the recording by default.

Flags:
  -o <path>            output file (required); .gif, .mp4, or .webm
  --speed <float>      playback speed multiplier (default 1.0)
  --max-idle <duration>
                       cap idle gaps (default 0 = faithful; note: 'twee play'
                       defaults to 2s)
  --font-size <pt>     render font size in points (default 14)
  --fps-cap <int>      max frames per second of video time (default 30)
  --ffmpeg <path>      ffmpeg binary (default: found on PATH)
  --crop <x,y,w,h>     render only this cell rectangle of the screen;
                       w,h must be > 0 and x,y must be >= 0. A frame whose
                       actual screen is smaller than the rectangle renders
                       the intersection, blank-filling the rest.
  --input-overlay      append a footer strip below the frames showing the
                       most recent input or resize event, like 'twee
                       play's footer. A new such event always produces its
                       own frame, even when the screen itself didn't
                       change.
  --quality low|medium|high
                       ffmpeg encoder preset for mp4/webm (default: medium,
                       which reproduces the output from before this flag
                       existed). Usage error for .gif output: the pure-Go
                       GIF encoder has no quality/CRF knob.`)
}

func runExport(args []string) {
	path, out, opts := parseExportArgs(args)
	if err := export.Export(path, out, opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseExportArgs(args []string) (path, out string, opts export.Options) {
	opts = export.Options{Speed: 1, FPSCap: 30, FontSize: 14}
	var parsed struct {
		Out          string   `arg:"--out,required"`
		Speed        *float64 `arg:"--speed"`
		MaxIdle      string   `arg:"--max-idle"`
		FontSize     *float64 `arg:"--font-size"`
		FPSCap       *int     `arg:"--fps-cap"`
		FFmpeg       string   `arg:"--ffmpeg"`
		Crop         string   `arg:"--crop"`
		InputOverlay bool     `arg:"--input-overlay"`
		Quality      string   `arg:"--quality"`
		Path         string   `arg:"positional,required"`
	}
	if err := parseArg("export", &parsed, exportArgsForParser(args)); err != nil {
		fatalUsage("export: %v", err)
	}
	if parsed.Speed != nil {
		opts.Speed = *parsed.Speed
	}
	if opts.Speed <= 0 {
		fatalUsage("export: --speed must be > 0")
	}
	if parsed.MaxIdle != "" {
		d, err := time.ParseDuration(parsed.MaxIdle)
		if err != nil || d < 0 {
			fatalUsage("export: bad --max-idle value %q", parsed.MaxIdle)
		}
		opts.MaxIdle = d
	}
	if parsed.FontSize != nil {
		opts.FontSize = *parsed.FontSize
	}
	if opts.FontSize <= 0 {
		fatalUsage("export: --font-size must be > 0")
	}
	if parsed.FPSCap != nil {
		opts.FPSCap = *parsed.FPSCap
	}
	if opts.FPSCap <= 0 {
		fatalUsage("export: --fps-cap must be > 0")
	}
	opts.FFmpeg = parsed.FFmpeg
	if parsed.Crop != "" {
		rect, err := parseCropFlag(parsed.Crop)
		if err != nil {
			fatalUsage("export: %v", err)
		}
		opts.Crop = &rect
	}
	opts.InputOverlay = parsed.InputOverlay
	if parsed.Quality != "" {
		quality, err := parseQualityFlag(parsed.Quality, parsed.Out)
		if err != nil {
			fatalUsage("export: %v", err)
		}
		opts.Quality = quality
	}
	return parsed.Path, parsed.Out, opts
}

// parseQualityFlag validates --quality's value and its combination with
// outPath's extension, returning a usage-error-shaped error naming
// what's wrong. Split out as a pure function for the same reason
// parseCropFlag is: unit-testable without exiting the test process.
func parseQualityFlag(quality, outPath string) (string, error) {
	switch quality {
	case "low", "medium", "high":
	default:
		return "", fmt.Errorf("--quality must be low, medium, or high (got %q)", quality)
	}
	if ext := strings.ToLower(filepath.Ext(outPath)); ext == ".gif" {
		return "", fmt.Errorf("--quality is not supported for .gif output (the pure-Go GIF encoder has no quality/CRF knob)")
	}
	return quality, nil
}

// parseCropFlag parses --crop's "x,y,w,h" cell-coordinate value,
// returning a usage-error-shaped error naming what's wrong. Split out
// as a pure function (rather than calling fatalUsage directly) so it's
// unit-testable without exiting the test process.
func parseCropFlag(s string) (export.CropRect, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return export.CropRect{}, fmt.Errorf("--crop must be x,y,w,h (got %q)", s)
	}
	var vals [4]int
	for i, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return export.CropRect{}, fmt.Errorf("--crop must be x,y,w,h of integers (got %q)", s)
		}
		vals[i] = v
	}
	rect := export.CropRect{X: vals[0], Y: vals[1], W: vals[2], H: vals[3]}
	if rect.W <= 0 || rect.H <= 0 {
		return export.CropRect{}, fmt.Errorf("--crop w,h must be > 0 (got %d,%d)", rect.W, rect.H)
	}
	if rect.X < 0 || rect.Y < 0 {
		return export.CropRect{}, fmt.Errorf("--crop x,y must be >= 0 (got %d,%d)", rect.X, rect.Y)
	}
	return rect, nil
}

func exportArgsForParser(args []string) []string {
	out := append([]string(nil), args...)
	for i, a := range out {
		switch {
		case a == "-o":
			out[i] = "--out"
		case strings.HasPrefix(a, "-o="):
			out[i] = "--out=" + strings.TrimPrefix(a, "-o=")
		}
	}
	return out
}
