package engine

import (
	"fmt"

	"github.com/paulsmith/twee/internal/vt"
)

// Snapshot returns the current terminal state.
func (t *Term) Snapshot() Snapshot {
	return FromVT(t.pump.Snapshot())
}

// VisibleText returns the visible viewport as plain text, trailing
// spaces stripped, lines joined with "\n".
func (t *Term) VisibleText() string {
	return vt.VisibleText(t.pump.Snapshot())
}

// Lines returns one string per row, trailing spaces stripped.
func (t *Term) Lines() []string {
	return vt.VisibleLines(t.pump.Snapshot())
}

// CursorPos returns the cursor position. (Distinct name from the type
// `Cursor` to avoid the embedding shadow when tuitest.Term embeds *Term.)
func (t *Term) CursorPos() Cursor {
	c := t.pump.Snapshot().Cursor
	return Cursor{Col: c.Col, Row: c.Row, Visible: c.Visible, Style: c.Style}
}

// Presentation returns the current VT input modes and cursor presentation.
// The pump serializes this query with child output that changes those modes.
func (t *Term) Presentation() (vt.Presentation, error) { return t.pump.Presentation() }

// ExitCode is valid after the child has exited.
func (t *Term) ExitCode() int { return t.runner.ExitCode() }

// ExitSignal reports the signal that terminated the child, as its
// conventional name (e.g. "SIGTERM"), and true — or ("", false) if it
// instead exited via a normal exit code. Valid after the child has
// exited.
func (t *Term) ExitSignal() (string, bool) { return t.runner.ExitSignal() }

// ExitedCh returns a channel that closes when the child exits.
func (t *Term) ExitedCh() <-chan struct{} { return t.runner.ExitedCh() }

// MarkStopRequested records that an explicit "twee stop" (as opposed to
// the child exiting on its own) asked this session to end. The daemon's
// stop handler calls this before CloseWithGrace so the session's
// tombstone can later say which of the two happened.
func (t *Term) MarkStopRequested() { t.stopRequested.Store(true) }

// StopRequested reports whether MarkStopRequested was ever called.
func (t *Term) StopRequested() bool { return t.stopRequested.Load() }

// SuppressTombstone requests that daemon teardown leave no exit record.
func (t *Term) SuppressTombstone() { t.suppressTombstone.Store(true) }

// TombstoneSuppressed reports whether bulk cleanup suppressed the exit record.
func (t *Term) TombstoneSuppressed() bool { return t.suppressTombstone.Load() }

// RecentBytes returns up to N bytes of recent PTY output, oldest first.
func (t *Term) RecentBytes() []byte { return t.pump.RecentBytes() }

// RecentInputs returns a copy of the input-events ring buffer.
func (t *Term) RecentInputs() []InputEvent {
	t.inputsMu.Lock()
	defer t.inputsMu.Unlock()
	return append([]InputEvent(nil), t.inputs...)
}

// Diagnostic returns a multi-line failure block describing current state.
func (t *Term) Diagnostic() string {
	snap := t.pump.Snapshot()
	lines := vt.VisibleLines(snap)
	var sb diagBuf
	sb.printf("command: %v\n", t.cfg.Cmd)
	sb.printf("size: %dx%d\n", snap.Size.Cols, snap.Size.Rows)
	sb.printf("cursor: (%d,%d)\n", snap.Cursor.Col, snap.Cursor.Row)
	sb.printf("alt screen: %v\n", snap.AltScreen)
	select {
	case <-t.runner.ExitedCh():
		sb.printf("exit status: %d\n", t.runner.ExitCode())
	default:
		sb.printf("exit status: (still running)\n")
	}
	sb.printf("--- visible screen ---\n")
	for _, ln := range lines {
		sb.printf("%s\n", ln)
	}
	sb.printf("--- recent input events (last 16) ---\n")
	evs := t.RecentInputs()
	if n := len(evs); n > 16 {
		evs = evs[n-16:]
	}
	if len(evs) == 0 {
		sb.printf("(none)\n")
	}
	for _, ev := range evs {
		sb.printf("  %s\n", ev.Desc)
	}
	sb.printf("--- recent bytes (escaped, last 1KB) ---\n")
	r := t.pump.RecentBytes()
	if len(r) > 1024 {
		r = r[len(r)-1024:]
	}
	sb.printf("%q\n", string(r))
	if tracePath := t.TracePath(); tracePath != "" {
		sb.printf("trace: %s\n", tracePath)
	}
	return sb.String()
}

type diagBuf struct{ b []byte }

func (s *diagBuf) printf(format string, a ...any) {
	s.b = append(s.b, fmt.Sprintf(format, a...)...)
}
func (s *diagBuf) String() string { return string(s.b) }
