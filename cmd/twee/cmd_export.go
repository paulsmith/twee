package main

import (
	"fmt"
	"os"
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
  --ffmpeg <path>      ffmpeg binary (default: found on PATH)`)
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
		Out      string   `arg:"--out,required"`
		Speed    *float64 `arg:"--speed"`
		MaxIdle  string   `arg:"--max-idle"`
		FontSize *float64 `arg:"--font-size"`
		FPSCap   *int     `arg:"--fps-cap"`
		FFmpeg   string   `arg:"--ffmpeg"`
		Path     string   `arg:"positional,required"`
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
	return parsed.Path, parsed.Out, opts
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
