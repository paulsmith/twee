package main

import (
	"fmt"
	"os"
	"time"

	"github.com/paulsmith/twee/internal/play"
)

func init() {
	register("play", runPlay)
	registerUsage("play", `twee play <bundle.twee> [--backend auto|kitty|iterm2|sixel] [--speed N] [--step] [--max-idle 2s] [--no-mouse-annotations]
Play a .twee trace bundle in the current terminal.

Controls:
  space   pause/resume
  .       step one event
  >       jump forward 1s
  r       restart
  q       quit

Flags:
  --backend <name>    graphics backend: auto, kitty, iterm2, or sixel (default auto;
                      iterm2 and sixel are experimental)
  --speed <float>      playback speed multiplier (default 1.0)
  --step               start paused; use . to advance one event
  --max-idle <duration>
                       cap long gaps between events (default 2s; 0 disables)
  --no-mouse-annotations
                       hide transient visual feedback for recorded mouse input
  --verbose            print a summary to stderr after exit

Backend selection:
  auto tries Kitty, then iTerm2, then Sixel. Graphics playback requires a
  direct terminal; tmux and screen passthrough are not supported. Sixel also
  requires the terminal to report reliable native pixel geometry.`)
}

func runPlay(args []string) {
	path, opts := parsePlayArgs(args)
	if path == "" {
		fatalUsage("play: expected one bundle path")
	}
	if !play.ValidSpeed(opts.Speed) {
		fmt.Fprintln(os.Stderr, "twee play: --speed must be > 0")
		os.Exit(1)
	}
	if fake := os.Getenv("TWEE_PLAY_FAKE_BACKEND"); fake != "" {
		opts.Backend = play.Backend(fake)
		opts.SkipPreflight = true
		opts.SkipRaw = true
		if opts.Backend == play.BackendSixel {
			opts.DisplayPixelWidth, opts.DisplayPixelHeight = 800, 600
		}
	} else if os.Getenv("TWEE_PLAY_FAKE_KITTY") == "1" {
		opts.Backend = play.BackendKitty
		opts.SkipPreflight = true
		opts.SkipRaw = true
	} else {
		pixelWidth, pixelHeight := nativeDisplayPixels()
		opts.DisplayPixelWidth = pixelWidth
		opts.DisplayPixelHeight = pixelHeight
	}
	if err := play.Run(path, opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parsePlayArgs(args []string) (string, play.Options) {
	opts := play.Options{Speed: 1, MaxIdle: 2 * time.Second}
	var parsed struct {
		Backend            string   `arg:"--backend"`
		Speed              *float64 `arg:"--speed"`
		Step               bool     `arg:"--step"`
		MaxIdle            string   `arg:"--max-idle"`
		NoMouseAnnotations bool     `arg:"--no-mouse-annotations"`
		Verbose            bool     `arg:"--verbose"`
		Path               string   `arg:"positional,required"`
	}
	if err := parseArg("play", &parsed, args); err != nil {
		fatalUsage("play: %v", err)
	}
	if parsed.Speed != nil {
		opts.Speed = *parsed.Speed
	}
	if parsed.Backend != "" {
		opts.Backend = play.Backend(parsed.Backend)
	}
	if opts.Backend == "" {
		opts.Backend = play.BackendAuto
	}
	if !play.ValidBackend(opts.Backend) {
		fatalUsage("play: bad --backend value %q", parsed.Backend)
	}
	if !play.ValidSpeed(opts.Speed) {
		fatalUsage("play: bad --speed value %q", fmt.Sprint(opts.Speed))
	}
	if parsed.MaxIdle != "" {
		opts.MaxIdle = parsePlayDuration(parsed.MaxIdle)
	}
	opts.Step = parsed.Step
	opts.Verbose = parsed.Verbose
	opts.DisableMouseAnnotations = parsed.NoMouseAnnotations
	return parsed.Path, opts
}

func parsePlayDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		fatalUsage("play: bad --max-idle value %q", s)
	}
	return d
}
