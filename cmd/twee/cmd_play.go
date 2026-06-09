package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/paulsmith/twee/internal/play"
)

func init() {
	register("play", runPlay)
	registerUsage("play", `twee play <bundle.twee> [--speed N] [--step] [--max-idle 2s]
Play a .twee trace bundle in the current terminal.

Controls:
  space   pause/resume
  .       step one event
  >       jump forward 1s
  r       restart
  q       quit

Flags:
  --speed <float>      playback speed multiplier (default 1.0)
  --step               start paused; use . to advance one event
  --max-idle <duration>
                       cap long gaps between events (default 2s; 0 disables)
  --verbose            print a summary to stderr after exit`)
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
	if os.Getenv("TWEE_PLAY_FAKE_KITTY") == "1" {
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
		Speed   *float64 `arg:"--speed"`
		Step    bool     `arg:"--step"`
		MaxIdle string   `arg:"--max-idle"`
		Verbose bool     `arg:"--verbose"`
		Path    string   `arg:"positional,required"`
	}
	if err := parseArg("play", &parsed, args); err != nil {
		fatalUsage("play: %v", err)
	}
	if parsed.Speed != nil {
		opts.Speed = *parsed.Speed
	}
	if !play.ValidSpeed(opts.Speed) {
		fatalUsage("play: bad --speed value %q", fmt.Sprint(opts.Speed))
	}
	if parsed.MaxIdle != "" {
		opts.MaxIdle = parsePlayDuration(parsed.MaxIdle)
	}
	opts.Step = parsed.Step
	opts.Verbose = parsed.Verbose
	return parsed.Path, opts
}

func parsePlaySpeed(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || !play.ValidSpeed(v) {
		fatalUsage("play: bad --speed value %q", s)
	}
	return v
}

func parsePlayDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		fatalUsage("play: bad --max-idle value %q", s)
	}
	return d
}
