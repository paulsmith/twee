package engine

import (
	"errors"
	"math"
	"testing"

	"github.com/paulsmith/twee/internal/input"
	"github.com/paulsmith/twee/internal/vt"
)

func TestClassifyMouseEncodeError(t *testing.T) {
	tests := []struct {
		name   string
		reason vt.MouseErrorReason
		kind   RequestErrorKind
	}{
		{"invalid batch", vt.MouseErrorInvalidBatch, RequestErrorInvalidArgument},
		{"invalid coordinate", vt.MouseErrorInvalidCoordinate, RequestErrorInvalidArgument},
		{"disabled", vt.MouseErrorTrackingDisabled, RequestErrorFailedPrecondition},
		{"incompatible", vt.MouseErrorIncompatible, RequestErrorFailedPrecondition},
		{"legacy overflow", vt.MouseErrorLegacyCoordinate, RequestErrorFailedPrecondition},
		{"pixels", vt.MouseErrorSGRPixels, RequestErrorFailedPrecondition},
		{"unsupported backend", vt.MouseErrorUnsupportedBackend, RequestErrorFailedPrecondition},
		{"missing report", vt.MouseErrorMissingReport, RequestErrorFailedPrecondition},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classifyMouseEncodeError(&vt.MouseEncodeError{
				Reason:   tt.reason,
				Gesture:  input.MouseGestureDrag,
				X:        80,
				Y:        4,
				Cols:     80,
				Rows:     24,
				Tracking: vt.MouseTrackingNormal,
				Format:   vt.MouseFormatX10,
				Required: []vt.MouseTracking{vt.MouseTrackingButton, vt.MouseTrackingAny},
			}, input.MouseGestureDrag)
			var requestErr *RequestError
			if !errors.As(err, &requestErr) {
				t.Fatalf("error type = %T, want *RequestError", err)
			}
			if requestErr.Kind != tt.kind {
				t.Fatalf("kind = %v, want %v", requestErr.Kind, tt.kind)
			}
			details, ok := requestErr.Details.(map[string]any)
			if !ok {
				t.Fatalf("details type = %T", requestErr.Details)
			}
			if got := details["gesture"]; got != "drag" {
				t.Fatalf("gesture detail = %#v, want drag", got)
			}
		})
	}
}

func TestCoordinateErrorDetails(t *testing.T) {
	err := classifyMouseEncodeError(&vt.MouseEncodeError{
		Reason: vt.MouseErrorInvalidCoordinate,
		X:      80,
		Y:      4,
		Cols:   80,
		Rows:   24,
	}, input.MouseGestureClick)
	var requestErr *RequestError
	if !errors.As(err, &requestErr) {
		t.Fatalf("error type = %T, want *RequestError", err)
	}
	details := requestErr.Details.(map[string]any)
	for key, want := range map[string]int{"x": 80, "y": 4, "cols": 80, "rows": 24} {
		if got := details[key]; got != want {
			t.Fatalf("details[%q] = %#v, want %d", key, got, want)
		}
	}
}

func TestMouseErrorDetailsUseRequestedGestureWhenBackendOmitsIt(t *testing.T) {
	err := classifyMouseEncodeError(
		&vt.MouseEncodeError{Reason: vt.MouseErrorUnsupportedBackend},
		input.MouseGestureHover,
	)
	var requestErr *RequestError
	if !errors.As(err, &requestErr) {
		t.Fatalf("error type = %T, want *RequestError", err)
	}
	details := requestErr.Details.(map[string]any)
	if got := details["gesture"]; got != "hover" {
		t.Fatalf("gesture detail = %#v, want hover", got)
	}
}

func TestMouseBackendContractFailuresRemainInternal(t *testing.T) {
	for _, reason := range []vt.MouseErrorReason{
		vt.MouseErrorState,
		vt.MouseErrorEncoding,
		vt.MouseErrorUnexpectedReport,
	} {
		t.Run(string(reason), func(t *testing.T) {
			err := classifyMouseEncodeError(
				&vt.MouseEncodeError{Reason: reason},
				input.MouseGestureClick,
			)
			var requestErr *RequestError
			if errors.As(err, &requestErr) {
				t.Fatalf("error = %#v, want untyped internal failure", requestErr)
			}
		})
	}
}

func TestMouseTraceInputPreservesZeroCoordinates(t *testing.T) {
	got := mouseTraceInput(input.NewClick(
		0, 0, input.ButtonNone,
		[]input.MouseModifier{input.ModifierCtrl, input.ModifierShift},
	))
	if got.Gesture != "click" || got.Button != "left" {
		t.Fatalf("trace identity = gesture %q button %q", got.Gesture, got.Button)
	}
	if got.X == nil || *got.X != 0 || got.Y == nil || *got.Y != 0 {
		t.Fatalf("trace coordinates = (%v,%v), want explicit zero pointers", got.X, got.Y)
	}
	if len(got.Modifiers) != 2 || got.Modifiers[0] != "shift" || got.Modifiers[1] != "ctrl" {
		t.Fatalf("trace modifiers = %#v, want stable shift,ctrl order", got.Modifiers)
	}
}

func TestMouseGestureDescriptions(t *testing.T) {
	tests := []struct {
		gesture input.MouseGesture
		want    string
	}{
		{input.NewClick(2, 1, input.ButtonNone, nil), "Click left @(2,1)"},
		{input.NewHover(2, 1, nil), "Hover @(2,1)"},
		{input.NewScroll(2, 1, input.ScrollDown, 3, nil), "Scroll down x3 @(2,1)"},
		{input.NewDrag(1, 1, 3, 2, input.ButtonRight, nil), "Drag right (1,1)->(3,2)"},
	}
	for _, tt := range tests {
		if got := mouseGestureDescription(tt.gesture); got != tt.want {
			t.Errorf("description = %q, want %q", got, tt.want)
		}
	}
}

func TestDragExtremeEndpointsRejectedBeforeExpansion(t *testing.T) {
	term := startEngineTerm(t, []string{"/bin/sh", "-c", "sleep 30"}, 10, 3)
	tests := []struct {
		name                         string
		fromX, fromY, toX, toY, x, y int
	}{
		{"max from x", math.MaxInt, 0, 0, 0, math.MaxInt, 0},
		{"min from y", 0, math.MinInt, 0, 0, 0, math.MinInt},
		{"max to x", 0, 0, math.MaxInt, 0, math.MaxInt, 0},
		{"min to y", 0, 0, 0, math.MinInt, 0, math.MinInt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := term.Drag(
				tt.fromX, tt.fromY, tt.toX, tt.toY,
				input.ButtonLeft, nil,
			)
			var requestErr *RequestError
			if !errors.As(err, &requestErr) {
				t.Fatalf("Drag error = %v (%T), want *RequestError", err, err)
			}
			if requestErr.Kind != RequestErrorInvalidArgument {
				t.Fatalf("error kind = %v, want invalid argument", requestErr.Kind)
			}
			details, ok := requestErr.Details.(map[string]any)
			if !ok {
				t.Fatalf("details type = %T", requestErr.Details)
			}
			for key, want := range map[string]int{
				"x": tt.x, "y": tt.y, "cols": 10, "rows": 3,
			} {
				if got := details[key]; got != want {
					t.Fatalf("details[%q] = %#v, want %d", key, got, want)
				}
			}
		})
	}
	if got := len(term.RecentInputs()); got != 0 {
		t.Fatalf("failed extreme drags recorded %d diagnostics, want 0", got)
	}
}
