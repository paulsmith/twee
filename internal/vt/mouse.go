package vt

import (
	"errors"
	"fmt"

	"github.com/paulsmith/twee/internal/input"
)

// MouseModel is the optional VT capability for inspecting and encoding mouse
// input. Model intentionally remains narrow so playback models and test fakes
// that do not support application-directed mouse input need not implement it.
//
// Calls must be externally serialized with Feed, Resize, Snapshot, and other
// mouse calls.
type MouseModel interface {
	EncodeMouse(events []input.MouseEvent) (MouseEncodingResult, error)
	MouseState() (MouseState, error)
}

// MouseTracking is an application-selected terminal mouse tracking mode.
type MouseTracking string

const (
	MouseTrackingNone   MouseTracking = "none"
	MouseTrackingX10    MouseTracking = "x10"
	MouseTrackingNormal MouseTracking = "normal"
	MouseTrackingButton MouseTracking = "button"
	MouseTrackingAny    MouseTracking = "any"
)

// MouseFormat is an application-selected terminal mouse report format.
type MouseFormat string

const (
	MouseFormatX10       MouseFormat = "x10"
	MouseFormatUTF8      MouseFormat = "utf8"
	MouseFormatSGR       MouseFormat = "sgr"
	MouseFormatURxvt     MouseFormat = "urxvt"
	MouseFormatSGRPixels MouseFormat = "sgr_pixels"
)

// MouseRawModes exposes the individual retained DECSET mode bits. Multiple
// bits may be true at once, so they do not identify the scalar tracking or
// format state used by Ghostty's encoder.
type MouseRawModes struct {
	TrackingX10    bool
	TrackingNormal bool
	TrackingButton bool
	TrackingAny    bool

	FormatUTF8      bool
	FormatSGR       bool
	FormatURxvt     bool
	FormatSGRPixels bool
}

// MouseState is a truthful view of the mouse state available from the pinned
// libghostty API. Tracking and Format are authoritative only when their
// matching Known field is true. Candidate fields hold conservative singleton
// interpretations of the raw bits; they are not effective state and must never
// be published as such. EncodeMouse may probe the configured encoder's
// tracking behavior for one command, but that observation is deliberately not
// stored in MouseState. Raw always contains the individual mode bits.
type MouseState struct {
	// Enabled is libghostty's aggregate raw tracking-bit state. Without
	// effective getters it can remain true after the effective scalar mode
	// has returned to none.
	Enabled bool
	Raw     MouseRawModes

	// Tracking and Format are safe to publish only when Known is true.
	Tracking      MouseTracking
	TrackingKnown bool
	// TrackingCandidate is a non-authoritative diagnostic label populated when
	// exactly one raw tracking bit is set. Encoding compatibility uses a
	// command-local behavioral probe instead.
	TrackingCandidate MouseTracking

	Format      MouseFormat
	FormatKnown bool
	// FormatCandidate is a non-authoritative singleton interpretation used for
	// diagnostics and limited conservative format preflight.
	FormatCandidate MouseFormat
}

// MouseEventEncoding records whether one input event produced a report and
// its exact bytes. A mode may legitimately filter an event (the release in a
// mode-9 click), so Produced=false is represented explicitly.
type MouseEventEncoding struct {
	Produced bool
	Bytes    []byte
}

// MouseEncodingResult is the preflight result for one complete normalized
// event batch. Bytes is the concatenation of every produced report.
type MouseEncodingResult struct {
	Bytes       []byte
	Events      []MouseEventEncoding
	ReportCount int
	// State is the externally reportable state returned by MouseState; it does
	// not include any command-local tracking observation used during encoding.
	State MouseState
	Size  Size
}

// MouseErrorReason classifies failures before an encoded batch is returned.
type MouseErrorReason string

const (
	MouseErrorUnsupportedBackend MouseErrorReason = "unsupported_backend"
	MouseErrorInvalidBatch       MouseErrorReason = "invalid_batch"
	MouseErrorInvalidCoordinate  MouseErrorReason = "invalid_coordinate"
	MouseErrorTrackingDisabled   MouseErrorReason = "tracking_disabled"
	MouseErrorIncompatible       MouseErrorReason = "incompatible_tracking"
	MouseErrorX10Modifiers       MouseErrorReason = "x10_modifiers"
	MouseErrorAmbiguousTracking  MouseErrorReason = "ambiguous_tracking"
	MouseErrorAmbiguousFormat    MouseErrorReason = "ambiguous_format"
	MouseErrorLegacyCoordinate   MouseErrorReason = "legacy_coordinate"
	MouseErrorSGRPixels          MouseErrorReason = "sgr_pixels"
	MouseErrorEncoding           MouseErrorReason = "encoding"
	MouseErrorMissingReport      MouseErrorReason = "missing_report"
	MouseErrorUnexpectedReport   MouseErrorReason = "unexpected_report"
	MouseErrorState              MouseErrorReason = "state"
)

// MouseEncodeError carries structured details used by the engine and RPC
// layers to map mouse failures without parsing error text.
type MouseEncodeError struct {
	Reason MouseErrorReason

	Gesture input.MouseGestureKind
	Event   int
	X       int
	Y       int
	Cols    int
	Rows    int

	Tracking MouseTracking
	Format   MouseFormat
	Required []MouseTracking

	// Candidate fields record conservative singleton raw-bit interpretations
	// available during preflight. Unlike Tracking and Format, they are not
	// authoritative effective state and should not be exposed as such in RPC
	// details.
	TrackingCandidate MouseTracking
	FormatCandidate   MouseFormat

	Err error
}

func (e *MouseEncodeError) Error() string {
	switch e.Reason {
	case MouseErrorUnsupportedBackend:
		return "vt: active backend does not support mouse encoding"
	case MouseErrorInvalidBatch:
		return fmt.Sprintf("vt: invalid %s mouse event batch", e.Gesture)
	case MouseErrorInvalidCoordinate:
		return fmt.Sprintf(
			"vt: mouse coordinate (%d,%d) outside %dx%d viewport",
			e.X, e.Y, e.Cols, e.Rows,
		)
	case MouseErrorTrackingDisabled:
		return "vt: mouse tracking is disabled"
	case MouseErrorIncompatible:
		tracking := string(e.Tracking)
		if tracking == "" && e.TrackingCandidate != "" {
			tracking = "raw " + string(e.TrackingCandidate) + " candidate"
		}
		return fmt.Sprintf(
			"vt: %s is incompatible with mouse tracking mode %s",
			e.Gesture, tracking,
		)
	case MouseErrorX10Modifiers:
		if e.TrackingKnown() {
			return "vt: mouse tracking mode x10 does not support modifiers"
		}
		return "vt: raw x10 mouse tracking candidate does not support modifiers"
	case MouseErrorAmbiguousTracking:
		return "vt: effective mouse tracking mode is ambiguous"
	case MouseErrorAmbiguousFormat:
		return "vt: effective mouse report format is ambiguous"
	case MouseErrorLegacyCoordinate:
		return fmt.Sprintf(
			"vt: legacy x10 mouse format cannot encode coordinate (%d,%d)",
			e.X, e.Y,
		)
	case MouseErrorSGRPixels:
		return "vt: SGR-Pixels mouse format requires real pixel geometry"
	case MouseErrorEncoding:
		return fmt.Sprintf("vt: encode mouse event %d: %v", e.Event, e.Err)
	case MouseErrorMissingReport:
		return fmt.Sprintf("vt: mouse event %d did not produce a required report", e.Event)
	case MouseErrorUnexpectedReport:
		return fmt.Sprintf("vt: filtered mouse event %d unexpectedly produced a report", e.Event)
	case MouseErrorState:
		return fmt.Sprintf("vt: inspect mouse state: %v", e.Err)
	default:
		return fmt.Sprintf("vt: mouse encoding failed (%s)", e.Reason)
	}
}

// TrackingKnown reports whether the error carries authoritative tracking
// state rather than only a raw-bit candidate.
func (e *MouseEncodeError) TrackingKnown() bool {
	return e.Tracking != ""
}

func (e *MouseEncodeError) Unwrap() error {
	return e.Err
}

// Is allows errors.Is to match any MouseEncodeError with the target reason.
func (e *MouseEncodeError) Is(target error) bool {
	var other *MouseEncodeError
	return errors.As(target, &other) && (other.Reason == "" || e.Reason == other.Reason)
}

var (
	ErrMouseUnsupportedBackend = &MouseEncodeError{Reason: MouseErrorUnsupportedBackend}
	ErrMouseInvalidBatch       = &MouseEncodeError{Reason: MouseErrorInvalidBatch}
	ErrMouseInvalidCoordinate  = &MouseEncodeError{Reason: MouseErrorInvalidCoordinate}
	ErrMouseTrackingDisabled   = &MouseEncodeError{Reason: MouseErrorTrackingDisabled}
	ErrMouseIncompatible       = &MouseEncodeError{Reason: MouseErrorIncompatible}
	ErrMouseX10Modifiers       = &MouseEncodeError{Reason: MouseErrorX10Modifiers}
	ErrMouseAmbiguousTracking  = &MouseEncodeError{Reason: MouseErrorAmbiguousTracking}
	ErrMouseAmbiguousFormat    = &MouseEncodeError{Reason: MouseErrorAmbiguousFormat}
	ErrMouseLegacyCoordinate   = &MouseEncodeError{Reason: MouseErrorLegacyCoordinate}
	ErrMouseSGRPixels          = &MouseEncodeError{Reason: MouseErrorSGRPixels}
	ErrMouseEncoding           = &MouseEncodeError{Reason: MouseErrorEncoding}
	ErrMouseMissingReport      = &MouseEncodeError{Reason: MouseErrorMissingReport}
	ErrMouseUnexpectedReport   = &MouseEncodeError{Reason: MouseErrorUnexpectedReport}
	ErrMouseState              = &MouseEncodeError{Reason: MouseErrorState}
)
