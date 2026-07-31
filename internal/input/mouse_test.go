package input

import (
	"reflect"
	"strings"
	"testing"
)

func TestMouseGestureClick(t *testing.T) {
	got, err := NewClick(12, 4, ButtonRight, []MouseModifier{ModifierShift, ModifierCtrl}).Expand()
	if err != nil {
		t.Fatal(err)
	}
	want := []MouseEvent{
		{
			Gesture: MouseGestureClick, Action: MouseActionPress, Button: ButtonRight,
			Modifiers: MouseModifiersShift | MouseModifiersCtrl, X: 12, Y: 4,
		},
		{
			Gesture: MouseGestureClick, Action: MouseActionRelease, Button: ButtonRight,
			Modifiers: MouseModifiersShift | MouseModifiersCtrl, X: 12, Y: 4,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events:\n got: %#v\nwant: %#v", got, want)
	}

	defaultButton, err := NewClick(0, 0, ButtonNone, nil).Expand()
	if err != nil {
		t.Fatal(err)
	}
	if defaultButton[0].Button != ButtonLeft || defaultButton[1].Button != ButtonLeft {
		t.Fatalf("default button: %#v", defaultButton)
	}
}

func TestMouseGestureHover(t *testing.T) {
	got, err := NewHover(20, 8, []MouseModifier{ModifierAlt}).Expand()
	if err != nil {
		t.Fatal(err)
	}
	want := []MouseEvent{{
		Gesture: MouseGestureHover, Action: MouseActionMotion, Button: ButtonNone,
		Modifiers: MouseModifiersAlt, X: 20, Y: 8,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestMouseGestureScroll(t *testing.T) {
	for _, tc := range []struct {
		name      string
		direction ScrollDirection
		button    MouseButton
	}{
		{name: "up", direction: ScrollUp, button: ButtonWheelUp},
		{name: "down", direction: ScrollDown, button: ButtonWheelDown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewScroll(5, 7, tc.direction, 3, nil).Expand()
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 3 {
				t.Fatalf("len = %d, want 3", len(got))
			}
			for i, event := range got {
				if event.Gesture != MouseGestureScroll || event.Action != MouseActionPress ||
					event.Button != tc.button || event.X != 5 || event.Y != 7 {
					t.Errorf("event %d = %#v", i, event)
				}
			}
		})
	}
}

func TestMouseGestureDragLines(t *testing.T) {
	tests := []struct {
		name       string
		from, to   MousePoint
		wantMotion []MousePoint
	}{
		{
			name: "horizontal", from: MousePoint{1, 2}, to: MousePoint{4, 2},
			wantMotion: []MousePoint{{2, 2}, {3, 2}, {4, 2}},
		},
		{
			name: "vertical", from: MousePoint{2, 1}, to: MousePoint{2, 4},
			wantMotion: []MousePoint{{2, 2}, {2, 3}, {2, 4}},
		},
		{
			name: "diagonal", from: MousePoint{0, 0}, to: MousePoint{3, 3},
			wantMotion: []MousePoint{{1, 1}, {2, 2}, {3, 3}},
		},
		{
			name: "shallow", from: MousePoint{0, 0}, to: MousePoint{4, 2},
			wantMotion: []MousePoint{{1, 1}, {2, 1}, {3, 2}, {4, 2}},
		},
		{
			name: "reverse", from: MousePoint{4, 2}, to: MousePoint{0, 0},
			wantMotion: []MousePoint{{3, 1}, {2, 1}, {1, 0}, {0, 0}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewDrag(
				tc.from.X, tc.from.Y, tc.to.X, tc.to.Y,
				ButtonMiddle, []MouseModifier{ModifierCtrl},
			).Expand()
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.wantMotion)+2 {
				t.Fatalf("len = %d, want %d: %#v", len(got), len(tc.wantMotion)+2, got)
			}
			if got[0].Action != MouseActionPress || got[0].X != tc.from.X || got[0].Y != tc.from.Y {
				t.Errorf("press = %#v", got[0])
			}
			for i, point := range tc.wantMotion {
				event := got[i+1]
				if event.Action != MouseActionMotion || event.X != point.X || event.Y != point.Y {
					t.Errorf("motion %d = %#v, want %v", i, event, point)
				}
				if event.Button != ButtonMiddle || event.Modifiers != MouseModifiersCtrl {
					t.Errorf("motion state %d = %#v", i, event)
				}
			}
			release := got[len(got)-1]
			if release.Action != MouseActionRelease || release.X != tc.to.X || release.Y != tc.to.Y {
				t.Errorf("release = %#v", release)
			}
		})
	}
}

func TestMouseGestureZeroLengthDragIsClick(t *testing.T) {
	got, err := NewDrag(3, 4, 3, 4, ButtonLeft, nil).Expand()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Gesture != MouseGestureClick || got[1].Gesture != MouseGestureClick {
		t.Fatalf("events = %#v", got)
	}
}

func TestMouseGestureValidation(t *testing.T) {
	tests := []struct {
		name    string
		gesture MouseGesture
		want    string
	}{
		{
			name: "unknown gesture", gesture: MouseGesture{Kind: 99},
			want: "unknown mouse gesture",
		},
		{
			name: "unknown button", gesture: NewClick(1, 2, 99, nil),
			want: "invalid gesture mouse button",
		},
		{
			name: "wheel is not click button", gesture: NewClick(1, 2, ButtonWheelUp, nil),
			want: "invalid gesture mouse button",
		},
		{
			name: "unknown modifier", gesture: NewHover(1, 2, []MouseModifier{99}),
			want: "unknown mouse modifier",
		},
		{
			name: "duplicate modifier", gesture: NewHover(1, 2, []MouseModifier{ModifierAlt, ModifierAlt}),
			want: "duplicate mouse modifier",
		},
		{
			name: "zero ticks", gesture: NewScroll(1, 2, ScrollUp, 0, nil),
			want: "scroll ticks",
		},
		{
			name: "negative ticks", gesture: NewScroll(1, 2, ScrollUp, -1, nil),
			want: "scroll ticks",
		},
		{
			name: "excessive ticks", gesture: NewScroll(1, 2, ScrollUp, MaxScrollTicks+1, nil),
			want: "scroll ticks",
		},
		{
			name: "unknown direction", gesture: NewScroll(1, 2, 99, 1, nil),
			want: "invalid scroll direction",
		},
		{
			name: "hover button", gesture: MouseGesture{
				Kind: MouseGestureHover, Point: MousePoint{1, 2}, Button: ButtonLeft,
			},
			want: "hover does not accept",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.gesture.Expand()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestMouseParsersAndNames(t *testing.T) {
	if got, err := ParseMouseButton("RIGHT"); err != nil || got != ButtonRight {
		t.Fatalf("ParseMouseButton = %v, %v", got, err)
	}
	if got, err := ParseMouseModifier("Ctrl"); err != nil || got != ModifierCtrl {
		t.Fatalf("ParseMouseModifier = %v, %v", got, err)
	}
	if got, err := ParseScrollDirection("DOWN"); err != nil || got != ScrollDown {
		t.Fatalf("ParseScrollDirection = %v, %v", got, err)
	}
	if got := (MouseModifiersShift | MouseModifiersAlt | MouseModifiersCtrl).Strings(); !reflect.DeepEqual(
		got, []string{"shift", "alt", "ctrl"},
	) {
		t.Fatalf("modifier strings = %#v", got)
	}
}
