package engine

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/paulsmith/twee/internal/pump"
	"github.com/paulsmith/twee/internal/vt"
)

// Rect is a rectangle of terminal cells used to exclude an area from a stable
// screen wait. Rectangles are clipped to each live snapshot's viewport.
type Rect struct {
	X, Y int
	W, H int
}

// WaitOption configures a wait call.
type WaitOption func(*WaitOpts)

// WaitOpts is exposed so tuitest can re-export Option helpers without
// type aliasing pain.
type WaitOpts struct {
	Timeout time.Duration
	Ctx     context.Context
}

// WithTimeout overrides the timeout for one call.
func WithTimeout(d time.Duration) WaitOption {
	return func(o *WaitOpts) { o.Timeout = d }
}

// WithContext lets the wait be canceled externally.
func WithContext(ctx context.Context) WaitOption {
	return func(o *WaitOpts) { o.Ctx = ctx }
}

func (t *Term) waitOpts(opts []WaitOption) WaitOpts {
	o := WaitOpts{Timeout: t.cfg.DefaultTimeout, Ctx: context.Background()}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WaitForText waits until s appears as a substring of any visible line.
func (t *Term) WaitForText(s string, opts ...WaitOption) error {
	o := t.waitOpts(opts)
	err := t.pump.Wait(o.Ctx, o.Timeout, func(snap vt.Snapshot) bool {
		for _, line := range vt.VisibleLines(snap) {
			if strings.Contains(line, s) {
				return true
			}
		}
		return false
	})
	if err != nil {
		return fmt.Errorf("WaitForText(%q): %w\n%s", s, err, t.Diagnostic())
	}
	return nil
}

// WaitForNoText waits until s no longer appears in any visible line.
func (t *Term) WaitForNoText(s string, opts ...WaitOption) error {
	o := t.waitOpts(opts)
	err := t.pump.Wait(o.Ctx, o.Timeout, func(snap vt.Snapshot) bool {
		for _, line := range vt.VisibleLines(snap) {
			if strings.Contains(line, s) {
				return false
			}
		}
		return true
	})
	if err != nil {
		return fmt.Errorf("WaitForNoText(%q): %w\n%s", s, err, t.Diagnostic())
	}
	return nil
}

// WaitForTextRegex waits until re matches the visible viewport joined by \n.
func (t *Term) WaitForTextRegex(re *regexp.Regexp, opts ...WaitOption) error {
	o := t.waitOpts(opts)
	err := t.pump.Wait(o.Ctx, o.Timeout, func(snap vt.Snapshot) bool {
		return re.MatchString(vt.VisibleText(snap))
	})
	if err != nil {
		return fmt.Errorf("WaitForTextRegex(%q): %w\n%s", re, err, t.Diagnostic())
	}
	return nil
}

// WaitForStableScreen waits for at least quietFor with no model-changing
// output. Note: unlike the other WaitForXxx methods, this one treats the
// pump closing (session ended) the same as genuine stability and returns
// nil rather than an IsSessionEnded error — see pump.Pump.WaitStable and
// IsSessionEnded's doc comment for why.
func (t *Term) WaitForStableScreen(quietFor time.Duration, opts ...WaitOption) error {
	o := t.waitOpts(opts)
	err := t.pump.WaitStable(o.Ctx, quietFor, o.Timeout)
	if err != nil {
		return fmt.Errorf("WaitForStableScreen: %w\n%s", err, t.Diagnostic())
	}
	return nil
}

// WaitForStableScreenExcept waits for the visible screen outside excluded to
// stop changing for quietFor. Unlike WaitForStableScreen, it compares
// snapshots because output inside an excluded region must not reset the quiet
// window. Callers with no excluded regions should use WaitForStableScreen to
// retain its lower-cost output-based behavior.
func (t *Term) WaitForStableScreenExcept(quietFor time.Duration, excluded []Rect, opts ...WaitOption) error {
	o := t.waitOpts(opts)
	rects := make([]pump.Rect, len(excluded))
	for i, rect := range excluded {
		rects[i] = pump.Rect{X: rect.X, Y: rect.Y, W: rect.W, H: rect.H}
	}
	err := t.pump.WaitStableExcept(o.Ctx, quietFor, o.Timeout, rects)
	if err != nil {
		return fmt.Errorf("WaitForStableScreenExcept: %w\n%s", err, t.Diagnostic())
	}
	return nil
}

// IsSessionEnded reports whether err — as returned by one of Term's
// WaitForXxx methods — means the wait was cut short by the session ending
// (the pump closed: the child exited, or the daemon tore the session
// down) rather than by the deadline firing. Callers that want to
// distinguish "the app is just slow" from "the app is gone" should check
// this before falling back to a plain timeout.
//
// WaitForStableScreen never produces an error this reports true for:
// pump.Pump.WaitStable treats a closed pump as trivially stable (nothing
// will ever change again) and returns nil, not ErrClosed. That shortcut
// is deliberately preserved — tuitest's WaitForStableScreen regression
// tests spawn children that print and exit immediately, well inside the
// quiet window, and rely on the closed-pump-is-stable behavior to pass;
// flipping it to an error here would make those (and any real app that
// happens to exit right after its last paint) report SESSION_ENDED
// instead of the success a "did the screen stop changing" wait is really
// asking about.
func IsSessionEnded(err error) bool {
	return errors.Is(err, pump.ErrClosed)
}

// WaitUntil waits until fn(snapshot) returns true.
func (t *Term) WaitUntil(fn func(Snapshot) bool, opts ...WaitOption) error {
	o := t.waitOpts(opts)
	err := t.pump.Wait(o.Ctx, o.Timeout, func(snap vt.Snapshot) bool {
		return fn(FromVT(snap))
	})
	if err != nil {
		return fmt.Errorf("WaitUntil: %w\n%s", err, t.Diagnostic())
	}
	return nil
}

// WaitForCursorAt waits until the cursor is at (col, row).
func (t *Term) WaitForCursorAt(col, row int, opts ...WaitOption) error {
	o := t.waitOpts(opts)
	err := t.pump.Wait(o.Ctx, o.Timeout, func(snap vt.Snapshot) bool {
		return snap.Cursor.Col == col && snap.Cursor.Row == row
	})
	if err != nil {
		return fmt.Errorf("WaitForCursorAt(%d,%d): %w\n%s", col, row, err, t.Diagnostic())
	}
	return nil
}

// WaitForExit blocks until the child exits or timeout fires.
func (t *Term) WaitForExit(opts ...WaitOption) (int, error) {
	o := t.waitOpts(opts)
	timer := time.NewTimer(o.Timeout)
	defer timer.Stop()
	select {
	case <-t.runner.ExitedCh():
		return t.runner.ExitCode(), nil
	case <-timer.C:
		return 0, fmt.Errorf("WaitForExit: timeout after %s", o.Timeout)
	case <-o.Ctx.Done():
		return 0, o.Ctx.Err()
	}
}
