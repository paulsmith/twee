package engine

import (
	"errors"
	"math"
	"reflect"
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

func TestObservedTrackingPopulatesMouseErrorDetails(t *testing.T) {
	tests := []struct {
		name         string
		script       string
		run          func(*Term) error
		wantMessage  string
		wantTracking string
		wantRequired []string
	}{
		{
			name: "multi-bit normal hover",
			script: "printf '\\033[?1003h\\033[?1002h\\033[?1000h" +
				"\\033[?1006hREADY'; sleep 30",
			run:          func(term *Term) error { return term.Hover(1, 1, nil) },
			wantMessage:  "vt: hover is incompatible with mouse tracking mode normal",
			wantTracking: "normal",
			wantRequired: []string{"any"},
		},
		{
			name: "multi-bit X10 modifiers",
			script: "printf '\\033[?1003h\\033[?1002h\\033[?9h" +
				"\\033[?1006hREADY'; sleep 30",
			run: func(term *Term) error {
				return term.Click(
					1, 1, input.ButtonLeft,
					[]input.MouseModifier{input.ModifierShift},
				)
			},
			wantMessage:  "vt: mouse tracking mode x10 does not support modifiers",
			wantTracking: "x10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := startEngineTerm(t, []string{"/bin/sh", "-c", tt.script}, 80, 24)
			if err := term.WaitForText("READY"); err != nil {
				t.Fatalf("WaitForText READY: %v", err)
			}

			err := tt.run(term)
			var requestErr *RequestError
			if !errors.As(err, &requestErr) {
				t.Fatalf("error = %v (%T), want *RequestError", err, err)
			}
			if requestErr.Kind != RequestErrorFailedPrecondition {
				t.Fatalf("error kind = %v, want failed precondition", requestErr.Kind)
			}
			if requestErr.Message != tt.wantMessage {
				t.Fatalf("error message = %q, want %q", requestErr.Message, tt.wantMessage)
			}
			details, ok := requestErr.Details.(map[string]any)
			if !ok {
				t.Fatalf("details type = %T", requestErr.Details)
			}
			if got := details["mouse_tracking"]; got != tt.wantTracking {
				t.Fatalf("mouse_tracking detail = %#v, want %q", got, tt.wantTracking)
			}
			gotRequired, hasRequired := details["required_tracking"]
			if tt.wantRequired == nil {
				if hasRequired {
					t.Fatalf("unexpected required_tracking detail = %#v", gotRequired)
				}
			} else if !reflect.DeepEqual(gotRequired, tt.wantRequired) {
				t.Fatalf("required_tracking detail = %#v, want %#v", gotRequired, tt.wantRequired)
			}
			if got := len(term.RecentInputs()); got != 0 {
				t.Fatalf("failed gesture recorded %d diagnostics, want 0", got)
			}

			state, stateErr := term.MouseState()
			if stateErr != nil {
				t.Fatalf("MouseState: %v", stateErr)
			}
			if state.TrackingKnown || state.Tracking != "" {
				t.Fatalf("command-local observation leaked into MouseState: %#v", state)
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
