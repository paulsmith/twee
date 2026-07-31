package vt

import (
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/input"
)

func newMouseTestTerm(t *testing.T, cols, rows int) *ghosttyTerm {
	t.Helper()
	term := newGhosttyTerm(cols, rows)
	t.Cleanup(func() {
		runtime.SetFinalizer(term, nil)
		term.finalize()
	})
	return term
}

func enableMouse(t *testing.T, term *ghosttyTerm, modes string) {
	t.Helper()
	if err := term.Feed([]byte(modes)); err != nil {
		t.Fatal(err)
	}
}

func expandMouse(t *testing.T, gesture input.MouseGesture) []input.MouseEvent {
	t.Helper()
	events, err := gesture.Expand()
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestMouseStateRawAndConservativeDerivation(t *testing.T) {
	term := newMouseTestTerm(t, 80, 24)

	state, err := term.MouseState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Enabled || !state.TrackingKnown || state.Tracking != MouseTrackingNone ||
		state.TrackingCandidate != MouseTrackingNone ||
		!state.FormatKnown || state.Format != MouseFormatX10 ||
		state.FormatCandidate != MouseFormatX10 {
		t.Fatalf("initial state = %#v", state)
	}

	enableMouse(t, term, "\x1b[?1002h\x1b[?1006h")
	state, err = term.MouseState()
	if err != nil {
		t.Fatal(err)
	}
	if !state.Enabled || state.TrackingKnown || state.Tracking != "" ||
		state.TrackingCandidate != MouseTrackingButton ||
		state.FormatKnown || state.Format != "" ||
		state.FormatCandidate != MouseFormatSGR ||
		!state.Raw.TrackingButton || !state.Raw.FormatSGR {
		t.Fatalf("button/SGR state = %#v", state)
	}

	enableMouse(t, term, "\x1b[?1000h\x1b[?1005h")
	state, err = term.MouseState()
	if err != nil {
		t.Fatal(err)
	}
	if state.TrackingKnown || state.FormatKnown {
		t.Fatalf("ambiguous raw state claimed effective values: %#v", state)
	}
	if state.TrackingCandidate != "" || state.FormatCandidate != "" {
		t.Fatalf("ambiguous raw state claimed candidates: %#v", state)
	}
	if !state.Raw.TrackingNormal || !state.Raw.TrackingButton ||
		!state.Raw.FormatUTF8 || !state.Raw.FormatSGR {
		t.Fatalf("raw bits = %#v", state.Raw)
	}
}

func TestMouseStateTrackingSingletonTransitionIsNotAuthoritative(t *testing.T) {
	term := newMouseTestTerm(t, 80, 24)

	// Ghostty retains the normal-mode raw bit but disabling the most recently
	// enabled any-event mode resets its effective scalar state to none.
	enableMouse(t, term, "\x1b[?1000h\x1b[?1003h\x1b[?1003l")
	state, err := term.MouseState()
	if err != nil {
		t.Fatal(err)
	}
	if !state.Enabled || state.TrackingKnown || state.Tracking != "" ||
		state.TrackingCandidate != MouseTrackingNormal ||
		!state.Raw.TrackingNormal || state.Raw.TrackingAny {
		t.Fatalf("state = %#v", state)
	}

	result, err := term.EncodeMouse(expandMouse(t,
		input.NewClick(1, 1, input.ButtonLeft, nil),
	))
	if !errors.Is(err, ErrMouseMissingReport) {
		t.Fatalf("error = %v, want missing report precondition", err)
	}
	if len(result.Bytes) != 0 || result.ReportCount != 0 || len(result.Events) != 0 {
		t.Fatalf("failed encoding leaked partial result: %#v", result)
	}
}

func TestMouseStateFormatSingletonTransitionIsNotAuthoritative(t *testing.T) {
	term := newMouseTestTerm(t, 300, 24)

	// The raw UTF-8 bit remains set, while disabling the later SGR mode resets
	// Ghostty's effective scalar format to legacy X10.
	enableMouse(t, term, "\x1b[?1000h\x1b[?1005h\x1b[?1006h\x1b[?1006l")
	state, err := term.MouseState()
	if err != nil {
		t.Fatal(err)
	}
	if state.FormatKnown || state.Format != "" ||
		state.FormatCandidate != MouseFormatUTF8 ||
		!state.Raw.FormatUTF8 || state.Raw.FormatSGR {
		t.Fatalf("state = %#v", state)
	}

	// The raw UTF-8 candidate allows preflight, but the effective X10 encoder
	// cannot represent x=223. Report verification rejects the whole batch.
	result, err := term.EncodeMouse(expandMouse(t,
		input.NewClick(223, 0, input.ButtonLeft, nil),
	))
	if !errors.Is(err, ErrMouseMissingReport) {
		t.Fatalf("error = %v, want missing report precondition", err)
	}
	if len(result.Bytes) != 0 || result.ReportCount != 0 || len(result.Events) != 0 {
		t.Fatalf("failed encoding leaked partial result: %#v", result)
	}
}

func TestEncodeMouseExactWireFormats(t *testing.T) {
	tests := []struct {
		name  string
		size  Size
		modes string
		input input.MouseGesture
		want  string
	}{
		{
			name: "X10", size: Size{Cols: 80, Rows: 24},
			modes: "\x1b[?1000h",
			input: input.NewClick(0, 0, input.ButtonLeft, nil),
			want:  "\x1b[M !!\x1b[M#!!",
		},
		{
			name: "UTF8 large coordinate", size: Size{Cols: 300, Rows: 24},
			modes: "\x1b[?1000h\x1b[?1005h",
			input: input.NewClick(223, 0, input.ButtonLeft, nil),
			want:  "\x1b[M \xc4\x80!\x1b[M#\xc4\x80!",
		},
		{
			name: "SGR modifiers and right button", size: Size{Cols: 80, Rows: 24},
			modes: "\x1b[?1000h\x1b[?1006h",
			input: input.NewClick(12, 4, input.ButtonRight, []input.MouseModifier{
				input.ModifierShift, input.ModifierAlt, input.ModifierCtrl,
			}),
			want: "\x1b[<30;13;5M\x1b[<30;13;5m",
		},
		{
			name: "URxvt legacy release code", size: Size{Cols: 80, Rows: 24},
			modes: "\x1b[?1000h\x1b[?1015h",
			input: input.NewClick(2, 3, input.ButtonLeft, []input.MouseModifier{
				input.ModifierCtrl,
			}),
			want: "\x1b[48;3;4M\x1b[51;3;4M",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			term := newMouseTestTerm(t, tc.size.Cols, tc.size.Rows)
			enableMouse(t, term, tc.modes)
			result, err := term.EncodeMouse(expandMouse(t, tc.input))
			if err != nil {
				t.Fatal(err)
			}
			if string(result.Bytes) != tc.want {
				t.Fatalf("bytes = %q, want %q", result.Bytes, tc.want)
			}
			if result.ReportCount != len(result.Events) {
				t.Fatalf("reports = %d, events = %#v", result.ReportCount, result.Events)
			}
			if result.Size != tc.size {
				t.Fatalf("size = %#v, want %#v", result.Size, tc.size)
			}
		})
	}
}

func TestEncodeMouseGesturesAndStateReset(t *testing.T) {
	term := newMouseTestTerm(t, 80, 24)
	enableMouse(t, term, "\x1b[?1003h\x1b[?1006h")

	click, err := term.EncodeMouse(expandMouse(t,
		input.NewClick(1, 1, input.ButtonRight, nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(click.Bytes), "\x1b[<2;2;2M\x1b[<2;2;2m"; got != want {
		t.Fatalf("click = %q, want %q", got, want)
	}

	// Hover must clear the reusable event's previous button. Resetting the
	// encoder also makes identical high-level hover commands report again.
	for i := 0; i < 2; i++ {
		hover, hoverErr := term.EncodeMouse(expandMouse(t,
			input.NewHover(1, 1, nil),
		))
		if hoverErr != nil {
			t.Fatal(hoverErr)
		}
		if got, want := string(hover.Bytes), "\x1b[<35;2;2M"; got != want {
			t.Fatalf("hover %d = %q, want %q", i, got, want)
		}
	}

	scroll, err := term.EncodeMouse(expandMouse(t,
		input.NewScroll(4, 5, input.ScrollDown, 3, nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(scroll.Bytes), strings.Repeat("\x1b[<65;5;6M", 3); got != want {
		t.Fatalf("scroll = %q, want %q", got, want)
	}
	if scroll.ReportCount != 3 {
		t.Fatalf("scroll reports = %d", scroll.ReportCount)
	}
}

func TestEncodeMouseDragBresenham(t *testing.T) {
	term := newMouseTestTerm(t, 80, 24)
	enableMouse(t, term, "\x1b[?1002h\x1b[?1006h")

	result, err := term.EncodeMouse(expandMouse(t,
		input.NewDrag(0, 0, 3, 3, input.ButtonLeft, nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b[<0;1;1M" +
		"\x1b[<32;2;2M" +
		"\x1b[<32;3;3M" +
		"\x1b[<32;4;4M" +
		"\x1b[<0;4;4m"
	if string(result.Bytes) != want {
		t.Fatalf("drag = %q, want %q", result.Bytes, want)
	}
	if result.ReportCount != 5 {
		t.Fatalf("reports = %d", result.ReportCount)
	}
}

func TestEncodeMouseMode9ClickFiltersOnlyRelease(t *testing.T) {
	term := newMouseTestTerm(t, 80, 24)
	enableMouse(t, term, "\x1b[?9h")

	result, err := term.EncodeMouse(expandMouse(t,
		input.NewClick(0, 0, input.ButtonMiddle, nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(result.Bytes), "\x1b[M!!!"; got != want {
		t.Fatalf("bytes = %q, want %q", got, want)
	}
	if result.ReportCount != 1 ||
		!result.Events[0].Produced || result.Events[1].Produced {
		t.Fatalf("event reports = %#v", result.Events)
	}
}

func TestEncodeMouseTrackingAcceptance(t *testing.T) {
	gestures := []struct {
		name    string
		gesture input.MouseGesture
		allowed map[MouseTracking]bool
	}{
		{
			name: "click", gesture: input.NewClick(1, 1, input.ButtonLeft, nil),
			allowed: map[MouseTracking]bool{
				MouseTrackingX10: true, MouseTrackingNormal: true,
				MouseTrackingButton: true, MouseTrackingAny: true,
			},
		},
		{
			name: "hover", gesture: input.NewHover(1, 1, nil),
			allowed: map[MouseTracking]bool{MouseTrackingAny: true},
		},
		{
			name: "scroll", gesture: input.NewScroll(1, 1, input.ScrollUp, 1, nil),
			allowed: map[MouseTracking]bool{
				MouseTrackingNormal: true, MouseTrackingButton: true, MouseTrackingAny: true,
			},
		},
		{
			name: "drag", gesture: input.NewDrag(1, 1, 2, 2, input.ButtonLeft, nil),
			allowed: map[MouseTracking]bool{MouseTrackingButton: true, MouseTrackingAny: true},
		},
	}
	modes := []struct {
		tracking MouseTracking
		sequence string
	}{
		{MouseTrackingX10, "\x1b[?9h"},
		{MouseTrackingNormal, "\x1b[?1000h"},
		{MouseTrackingButton, "\x1b[?1002h"},
		{MouseTrackingAny, "\x1b[?1003h"},
	}
	for _, gesture := range gestures {
		for _, mode := range modes {
			t.Run(gesture.name+"/"+string(mode.tracking), func(t *testing.T) {
				term := newMouseTestTerm(t, 80, 24)
				enableMouse(t, term, mode.sequence+"\x1b[?1006h")
				result, err := term.EncodeMouse(expandMouse(t, gesture.gesture))
				if gesture.allowed[mode.tracking] {
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					if result.ReportCount == 0 {
						t.Fatal("successful gesture produced no reports")
					}
				} else if !errors.Is(err, ErrMouseIncompatible) {
					t.Fatalf("error = %v, want incompatible", err)
				}
			})
		}
	}
}

func TestEncodeMousePreconditionErrors(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		term := newMouseTestTerm(t, 80, 24)
		_, err := term.EncodeMouse(expandMouse(t,
			input.NewClick(1, 1, input.ButtonLeft, nil),
		))
		if !errors.Is(err, ErrMouseTrackingDisabled) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("X10 modifiers", func(t *testing.T) {
		term := newMouseTestTerm(t, 80, 24)
		enableMouse(t, term, "\x1b[?9h")
		_, err := term.EncodeMouse(expandMouse(t,
			input.NewClick(1, 1, input.ButtonLeft, []input.MouseModifier{input.ModifierShift}),
		))
		if !errors.Is(err, ErrMouseX10Modifiers) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("ambiguous tracking", func(t *testing.T) {
		term := newMouseTestTerm(t, 80, 24)
		enableMouse(t, term, "\x1b[?1000h\x1b[?1003h")
		_, err := term.EncodeMouse(expandMouse(t,
			input.NewClick(1, 1, input.ButtonLeft, nil),
		))
		if !errors.Is(err, ErrMouseAmbiguousTracking) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("ambiguous format", func(t *testing.T) {
		term := newMouseTestTerm(t, 80, 24)
		enableMouse(t, term, "\x1b[?1000h\x1b[?1005h\x1b[?1006h")
		_, err := term.EncodeMouse(expandMouse(t,
			input.NewClick(1, 1, input.ButtonLeft, nil),
		))
		if !errors.Is(err, ErrMouseAmbiguousFormat) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("SGR pixels even when other format active later", func(t *testing.T) {
		term := newMouseTestTerm(t, 80, 24)
		enableMouse(t, term, "\x1b[?1003h\x1b[?1016h\x1b[?1006h")
		_, err := term.EncodeMouse(expandMouse(t,
			input.NewHover(1, 1, nil),
		))
		if !errors.Is(err, ErrMouseSGRPixels) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestEncodeMouseCoordinatesAndLiveResize(t *testing.T) {
	term := newMouseTestTerm(t, 10, 5)
	enableMouse(t, term, "\x1b[?1000h\x1b[?1006h")
	for _, point := range []input.MousePoint{{-1, 0}, {0, -1}, {10, 0}, {0, 5}} {
		_, err := term.EncodeMouse(expandMouse(t,
			input.NewClick(point.X, point.Y, input.ButtonLeft, nil),
		))
		if !errors.Is(err, ErrMouseInvalidCoordinate) {
			t.Errorf("point %v error = %v", point, err)
		}
		var mouseErr *MouseEncodeError
		if !errors.As(err, &mouseErr) ||
			mouseErr.Cols != 10 || mouseErr.Rows != 5 ||
			mouseErr.X != point.X || mouseErr.Y != point.Y {
			t.Errorf("point %v details = %#v", point, mouseErr)
		}
	}

	if err := term.Resize(20, 8); err != nil {
		t.Fatal(err)
	}
	result, err := term.EncodeMouse(expandMouse(t,
		input.NewClick(19, 7, input.ButtonLeft, nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	if result.Size != (Size{Cols: 20, Rows: 8}) ||
		string(result.Bytes) != "\x1b[<0;20;8M\x1b[<0;20;8m" {
		t.Fatalf("post-resize result = %#v", result)
	}
}

func TestEncodeMouseLegacyCoordinateBoundary(t *testing.T) {
	term := newMouseTestTerm(t, 300, 300)
	enableMouse(t, term, "\x1b[?1000h")

	result, err := term.EncodeMouse(expandMouse(t,
		input.NewClick(222, 222, input.ButtonLeft, nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x1b, '[', 'M', ' ', 0xff, 0xff,
		0x1b, '[', 'M', '#', 0xff, 0xff,
	}
	if !reflect.DeepEqual(result.Bytes, want) {
		t.Fatalf("boundary bytes = %v, want %v", result.Bytes, want)
	}

	_, err = term.EncodeMouse(expandMouse(t,
		input.NewClick(223, 1, input.ButtonLeft, nil),
	))
	if !errors.Is(err, ErrMouseLegacyCoordinate) {
		t.Fatalf("x=223 error = %v", err)
	}
}

func TestEncodeMouseInvalidBatchAndRecovery(t *testing.T) {
	term := newMouseTestTerm(t, 80, 24)
	enableMouse(t, term, "\x1b[?1002h\x1b[?1006h")

	bad := expandMouse(t, input.NewDrag(1, 1, 3, 1, input.ButtonLeft, nil))
	bad[len(bad)-2].Action = input.MouseActionRelease
	if result, err := term.EncodeMouse(bad); !errors.Is(err, ErrMouseInvalidBatch) ||
		len(result.Bytes) != 0 {
		t.Fatalf("invalid result = %#v, error = %v", result, err)
	}

	// A failed batch must not leave button, geometry, or dedupe state behind.
	result, err := term.EncodeMouse(expandMouse(t,
		input.NewClick(2, 2, input.ButtonLeft, nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(result.Bytes), "\x1b[<0;3;3M\x1b[<0;3;3m"; got != want {
		t.Fatalf("recovery click = %q, want %q", got, want)
	}
}
