// Package export renders .twee recordings to replay artifacts (GIF, HTML,
// MP4, and WebM).
package export

import (
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/paulsmith/twee/internal/play"
	"github.com/paulsmith/twee/internal/vt"
)

// Options controls an export run.
type Options struct {
	Speed    float64       // playback speed multiplier; default 1.0
	MaxIdle  time.Duration // cap per-gap idle trace time; 0 = faithful
	FontSize float64       // render font size in points; default 14
	FPSCap   int           // max frames per second of logical time; default 30
	FFmpeg   string        // ffmpeg binary; default looked up on PATH

	// Crop, when non-nil, renders only this cell-coordinate rectangle of
	// the screen instead of the whole recorded grid. A frame whose
	// actual screen is smaller than the rectangle renders the
	// intersection and blank-fills the rest rather than erroring.
	Crop *CropRect

	// InputOverlay appends a one-row footer strip below each frame
	// showing the most recently seen input or resize event, formatted
	// the same way "twee play"'s footer is. A qualifying event forces a
	// frame even when the screen itself didn't change, so the overlay
	// can't be skipped by the emit-on-screen-change rule.
	InputOverlay bool

	// Quality selects the ffmpeg encoder preset for mp4/webm output:
	// "low", "medium" (default), or "high" — see ffmpegArgs for the
	// concrete CRF/preset values. Ignored for GIF and HTML, which have no
	// such knob; the CLI rejects --quality with those formats as a usage
	// error rather than silently ignoring it.
	Quality string
}

// CropRect is a cell-coordinate rectangle selecting the portion of the
// screen Options.Crop renders.
type CropRect struct {
	X, Y, W, H int
}

const trailingCap = 3 * time.Second

func (o *Options) normalize() {
	if o.Speed <= 0 {
		o.Speed = 1
	}
	if o.FontSize <= 0 {
		o.FontSize = 14
	}
	if o.FPSCap <= 0 {
		o.FPSCap = 30
	}
	switch o.Quality {
	case "low", "medium", "high":
	default:
		o.Quality = "medium"
	}
}

// frameKey identifies a visually distinct frame: the screen content
// (hashNoCursor) plus, when --input-overlay is active, the current
// overlay text. Comparing the pair directly (rather than hashing them
// together) keeps the "did anything change" check a plain ==.
type frameKey struct {
	hash    [32]byte
	overlay string
}

// replay walks events on a logical clock and calls emit with each visibly
// distinct frame, its current overlay text (see Options.InputOverlay;
// "" when disabled or no qualifying event has occurred yet), and its
// display duration. Frames arrive in order and durations sum to the
// adjusted length of the recording, with idle tail capped.
func replay(events []play.Event, cols, rows int, opts Options,
	newModel func(int, int) vt.Model,
	emit func(snap vt.Snapshot, overlay string, d time.Duration) error,
) error {
	opts.normalize()
	window := time.Second / time.Duration(opts.FPSCap)
	model := newModel(cols, rows)

	adjusted := adjustedTimeline(events, opts)
	pendingSnap := model.Snapshot()
	var pendingOverlay string
	pendingKey := frameKey{hashNoCursor(pendingSnap), pendingOverlay}
	var pendingT time.Duration
	checkpointSet := false
	var nextCheckpoint time.Duration
	dirty := false
	var overlay string

	checkpoint := func(t time.Duration) error {
		checkpointSet = true
		nextCheckpoint = t + window
		dirty = false

		snap := model.Snapshot()
		key := frameKey{hashNoCursor(snap), overlay}
		if key == pendingKey {
			return nil
		}
		if d := t - pendingT; d > 0 {
			if err := emit(pendingSnap, pendingOverlay, d); err != nil {
				return err
			}
		}
		pendingSnap, pendingOverlay, pendingKey, pendingT = snap, overlay, key, t
		return nil
	}

	for i, ev := range events {
		t := adjusted[i]
		if dirty && t >= nextCheckpoint {
			if err := checkpoint(nextCheckpoint); err != nil {
				return err
			}
		}
		if err := apply(model, ev); err != nil {
			return err
		}
		// A new input/resize event must produce a frame of its own even
		// when it doesn't touch the screen — otherwise the overlay could
		// update and revert between two emitted frames without ever
		// being seen, since the emit-on-screen-change rule wouldn't
		// consider it a reason to checkpoint at all.
		forceFrame := false
		if opts.InputOverlay {
			if txt := play.FormatEventToast(ev); txt != "" {
				overlay = txt
				forceFrame = true
			}
		}
		if !screenEvent(ev) && !forceFrame {
			continue
		}
		if !checkpointSet || t >= nextCheckpoint {
			if err := checkpoint(t); err != nil {
				return err
			}
			continue
		}
		dirty = true
	}

	end := time.Duration(0)
	if len(events) > 0 {
		end = adjusted[len(events)-1]
		if dirty {
			if err := checkpoint(end); err != nil {
				return err
			}
		}
		if !checkpointSet {
			if err := checkpoint(end); err != nil {
				return err
			}
		}
	}
	if !checkpointSet {
		if err := checkpoint(0); err != nil {
			return err
		}
	}
	tail := end - pendingT
	if tail > trailingCap {
		tail = trailingCap
	}
	if tail < window {
		tail = window
	}
	return emit(pendingSnap, pendingOverlay, tail)
}

func adjustedTimeline(events []play.Event, opts Options) []time.Duration {
	adjusted := make([]time.Duration, len(events))
	var prev, adj time.Duration
	for i, ev := range events {
		t := ev.TraceTime()
		gap := t - prev
		if gap < 0 {
			gap = 0
		}
		if opts.MaxIdle > 0 && gap > opts.MaxIdle {
			gap = opts.MaxIdle
		}
		prev = t
		adj += time.Duration(float64(gap) / opts.Speed)
		adjusted[i] = adj
	}
	return adjusted
}

func apply(model vt.Model, ev play.Event) error {
	switch ev.Type {
	case "output":
		return model.Feed(ev.Bytes)
	case "resize":
		if ev.Cols > 0 && ev.Rows > 0 {
			return model.Resize(ev.Cols, ev.Rows)
		}
	}
	// input and exit events do not affect the screen.
	return nil
}

func screenEvent(ev play.Event) bool {
	switch ev.Type {
	case "output":
		return true
	case "resize":
		return ev.Cols > 0 && ev.Rows > 0
	default:
		return false
	}
}

// hashNoCursor hashes the snapshot with the cursor zeroed: the renderer does
// not draw the cursor, so cursor-only movement must not create frames.
func hashNoCursor(s vt.Snapshot) [32]byte {
	s.Cursor = vt.Cursor{}
	b, _ := json.Marshal(s)
	return sha256.Sum256(b)
}
