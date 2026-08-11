package engine

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/paulsmith/twee/internal/pump"
	"github.com/paulsmith/twee/internal/vt"
)

// Diagnostic is a retained failure observation. Snapshot, modes, mouse state,
// and recent output are captured atomically by the pump; inputs and trace state
// are bounded best-effort context collected immediately afterward.
type Diagnostic struct {
	CapturedAt      time.Time
	Generation      uint64
	Snapshot        Snapshot
	Presentation    vt.Presentation
	PresentationErr error
	Mouse           vt.MouseState
	MouseErr        error
	RecentOutput    []byte
	RecentInputs    []InputEvent
	Command         []string
	ExitCode        *int
	TracePath       string
	TraceStatus     string
}

// WaitError retains the terminal observation that caused a wait to fail.
type WaitError struct {
	Operation  string
	Cause      error
	Diagnostic Diagnostic
}

func (e *WaitError) Error() string {
	if e.Operation == "" {
		return fmt.Sprintf("%v\n%s", e.Cause, e.Diagnostic.String())
	}
	return fmt.Sprintf("%s: %v\n%s", e.Operation, e.Cause, e.Diagnostic.String())
}

func (e *WaitError) Unwrap() error { return e.Cause }

// DiagnosticFromError returns a wait's retained failure observation.
func DiagnosticFromError(err error) (Diagnostic, bool) {
	var waitErr *WaitError
	if !errors.As(err, &waitErr) {
		return Diagnostic{}, false
	}
	return waitErr.Diagnostic, true
}

// CaptureDiagnostic captures the current terminal state and bounded context.
func (t *Term) CaptureDiagnostic() Diagnostic {
	return t.diagnosticFromCapture(t.pump.Capture())
}

func (t *Term) diagnosticFromCapture(capture pump.Capture) Diagnostic {
	diagnostic := Diagnostic{
		CapturedAt:      capture.CapturedAt,
		Generation:      capture.Generation,
		Snapshot:        FromVT(capture.Snapshot),
		Presentation:    capture.Presentation,
		PresentationErr: capture.PresentationErr,
		Mouse:           capture.Mouse,
		MouseErr:        capture.MouseErr,
		RecentOutput:    append([]byte(nil), capture.RecentOutput...),
		RecentInputs:    t.RecentInputs(),
		Command:         t.Cmd(),
	}
	select {
	case <-t.runner.ExitedCh():
		code := t.runner.ExitCode()
		diagnostic.ExitCode = &code
	default:
	}
	t.cfgMu.Lock()
	if t.tracePath != "" {
		diagnostic.TracePath = t.tracePath
		diagnostic.TraceStatus = "active"
	} else if t.finalizedTracePath != "" {
		diagnostic.TracePath = t.finalizedTracePath
		diagnostic.TraceStatus = "finalized"
	}
	t.cfgMu.Unlock()
	return diagnostic
}

func (t *Term) waitError(operation string, cause error, capture pump.Capture) error {
	return &WaitError{Operation: operation, Cause: cause, Diagnostic: t.diagnosticFromCapture(capture)}
}

// DiagnosticScreenText returns bounded viewport text for failure envelopes.
func DiagnosticScreenText(snapshot Snapshot) (text string, totalBytes int, truncated bool) {
	text = VisibleSnapshotText(snapshot)
	totalBytes = len(text)
	const limit = 64 * 1024
	if len(text) <= limit {
		return text, totalBytes, false
	}
	text = text[:limit]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text, totalBytes, true
}

// String renders the retained observation without sampling the live terminal.
func (d Diagnostic) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "command: %v\n", d.Command)
	fmt.Fprintf(&sb, "size: %dx%d\n", d.Snapshot.Cols, d.Snapshot.Rows)
	fmt.Fprintf(&sb, "cursor: (%d,%d)\n", d.Snapshot.Cursor.Col, d.Snapshot.Cursor.Row)
	fmt.Fprintf(&sb, "alt screen: %v\n", d.Snapshot.AltScreen)
	if d.PresentationErr == nil {
		fmt.Fprintf(&sb, "application cursor: %v\n", d.Presentation.Input.ApplicationCursor)
		fmt.Fprintf(&sb, "bracketed paste: %v\n", d.Presentation.Input.BracketedPaste)
		fmt.Fprintf(&sb, "kitty keyboard known: %v\n", d.Presentation.Input.KittyKeyboardKnown)
		fmt.Fprintf(&sb, "kitty keyboard flags: %d\n", d.Presentation.Input.KittyKeyboardFlags)
	} else {
		fmt.Fprintf(&sb, "modes: unavailable (%v)\n", d.PresentationErr)
	}
	if d.MouseErr == nil {
		fmt.Fprintf(&sb, "mouse enabled: %v\n", d.Mouse.Enabled)
		if d.Mouse.TrackingKnown {
			fmt.Fprintf(&sb, "mouse tracking: %s\n", d.Mouse.Tracking)
		} else {
			sb.WriteString("mouse tracking: unknown\n")
		}
		if d.Mouse.FormatKnown {
			fmt.Fprintf(&sb, "mouse format: %s\n", d.Mouse.Format)
		} else {
			sb.WriteString("mouse format: unknown\n")
		}
		fmt.Fprintf(&sb, "mouse raw tracking: x10=%v normal=%v button=%v any=%v\n",
			d.Mouse.Raw.TrackingX10, d.Mouse.Raw.TrackingNormal, d.Mouse.Raw.TrackingButton, d.Mouse.Raw.TrackingAny)
		fmt.Fprintf(&sb, "mouse raw format: utf8=%v sgr=%v urxvt=%v sgr_pixels=%v\n",
			d.Mouse.Raw.FormatUTF8, d.Mouse.Raw.FormatSGR, d.Mouse.Raw.FormatURxvt, d.Mouse.Raw.FormatSGRPixels)
	} else {
		fmt.Fprintf(&sb, "mouse modes: unavailable (%v)\n", d.MouseErr)
	}
	if d.ExitCode != nil {
		fmt.Fprintf(&sb, "exit status: %d\n", *d.ExitCode)
	} else {
		sb.WriteString("exit status: (still running)\n")
	}
	sb.WriteString("--- visible screen ---\n")
	screen, totalScreenBytes, screenTruncated := DiagnosticScreenText(d.Snapshot)
	sb.WriteString(screen)
	sb.WriteByte('\n')
	if screenTruncated {
		fmt.Fprintf(&sb, "--- screen truncated: showing %d of %d bytes ---\n", len(screen), totalScreenBytes)
	}
	sb.WriteString("--- recent input events (last 16) ---\n")
	events := d.RecentInputs
	if len(events) > 16 {
		events = events[len(events)-16:]
	}
	if len(events) == 0 {
		sb.WriteString("(none)\n")
	}
	for _, event := range events {
		fmt.Fprintf(&sb, "  %s\n", DiagnosticInputDescription(event))
	}
	sb.WriteString("--- recent bytes (escaped, last 1KB) ---\n")
	recent := d.RecentOutput
	if len(recent) > 1024 {
		recent = recent[len(recent)-1024:]
	}
	fmt.Fprintf(&sb, "%q\n", string(recent))
	if d.TracePath != "" {
		fmt.Fprintf(&sb, "trace (%s): %s\n", d.TraceStatus, d.TracePath)
	}
	return sb.String()
}

// DiagnosticInputDescription returns a bounded, payload-safe event label.
func DiagnosticInputDescription(event InputEvent) string {
	if event.Kind == "type" || event.Kind == "paste" {
		return event.Kind + " payload redacted"
	}
	const limit = 512
	if len(event.Desc) > limit {
		return event.Desc[:limit] + "…"
	}
	return event.Desc
}
