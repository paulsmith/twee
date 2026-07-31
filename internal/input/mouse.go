package input

import (
	"fmt"
	"strings"
)

// MaxScrollTicks is the largest scroll gesture accepted by Expand. A scroll
// tick becomes one terminal mouse report, so bounding ticks also bounds the
// amount of memory and work a single request can require.
const MaxScrollTicks = 100

// MouseAction is the normalized action represented by a MouseEvent.
type MouseAction uint8

const (
	MouseActionNone MouseAction = iota
	MouseActionPress
	MouseActionRelease
	MouseActionMotion
)

func (a MouseAction) String() string {
	switch a {
	case MouseActionPress:
		return "press"
	case MouseActionRelease:
		return "release"
	case MouseActionMotion:
		return "motion"
	default:
		return "unknown"
	}
}

// MouseButton is a normalized terminal mouse button. Wheel buttons are
// internal event values produced by scroll expansion; the public gesture
// button options are left, middle, and right.
type MouseButton uint8

const (
	ButtonNone MouseButton = iota
	ButtonLeft
	ButtonMiddle
	ButtonRight
	ButtonWheelUp
	ButtonWheelDown
)

func (b MouseButton) String() string {
	switch b {
	case ButtonLeft:
		return "left"
	case ButtonMiddle:
		return "middle"
	case ButtonRight:
		return "right"
	case ButtonWheelUp:
		return "wheel-up"
	case ButtonWheelDown:
		return "wheel-down"
	default:
		return "unknown"
	}
}

// ParseMouseButton parses a user-facing mouse button name.
func ParseMouseButton(name string) (MouseButton, error) {
	switch strings.ToLower(name) {
	case "left":
		return ButtonLeft, nil
	case "middle":
		return ButtonMiddle, nil
	case "right":
		return ButtonRight, nil
	default:
		return ButtonNone, fmt.Errorf("unknown mouse button %q", name)
	}
}

// MouseModifier is a modifier held during a mouse gesture.
type MouseModifier uint8

const (
	ModifierShift MouseModifier = iota + 1
	ModifierAlt
	ModifierCtrl
)

func (m MouseModifier) String() string {
	switch m {
	case ModifierShift:
		return "shift"
	case ModifierAlt:
		return "alt"
	case ModifierCtrl:
		return "ctrl"
	default:
		return "unknown"
	}
}

// ParseMouseModifier parses a user-facing mouse modifier name.
func ParseMouseModifier(name string) (MouseModifier, error) {
	switch strings.ToLower(name) {
	case "shift":
		return ModifierShift, nil
	case "alt":
		return ModifierAlt, nil
	case "ctrl":
		return ModifierCtrl, nil
	default:
		return 0, fmt.Errorf("unknown mouse modifier %q", name)
	}
}

// MouseModifiers is the normalized modifier bit set carried by a MouseEvent.
type MouseModifiers uint8

const MouseModifiersNone MouseModifiers = 0

const (
	MouseModifiersShift MouseModifiers = 1 << iota
	MouseModifiersAlt
	MouseModifiersCtrl
)

const allMouseModifiers = MouseModifiersShift | MouseModifiersAlt | MouseModifiersCtrl

// NormalizeMouseModifiers validates modifiers, rejecting unknown and duplicate
// values, and returns the event bit set.
func NormalizeMouseModifiers(modifiers []MouseModifier) (MouseModifiers, error) {
	var result MouseModifiers
	for _, modifier := range modifiers {
		var bit MouseModifiers
		switch modifier {
		case ModifierShift:
			bit = MouseModifiersShift
		case ModifierAlt:
			bit = MouseModifiersAlt
		case ModifierCtrl:
			bit = MouseModifiersCtrl
		default:
			return 0, fmt.Errorf("unknown mouse modifier %d", modifier)
		}
		if result&bit != 0 {
			return 0, fmt.Errorf("duplicate mouse modifier %q", modifier)
		}
		result |= bit
	}
	return result, nil
}

// Valid reports whether m contains only supported modifier bits.
func (m MouseModifiers) Valid() bool {
	return m&^allMouseModifiers == 0
}

// Strings returns modifier display names in stable shift, alt, ctrl order.
func (m MouseModifiers) Strings() []string {
	var result []string
	if m&MouseModifiersShift != 0 {
		result = append(result, "shift")
	}
	if m&MouseModifiersAlt != 0 {
		result = append(result, "alt")
	}
	if m&MouseModifiersCtrl != 0 {
		result = append(result, "ctrl")
	}
	return result
}

// ScrollDirection is the direction of a vertical wheel gesture.
type ScrollDirection uint8

const (
	ScrollDirectionNone ScrollDirection = iota
	ScrollUp
	ScrollDown
)

func (d ScrollDirection) String() string {
	switch d {
	case ScrollUp:
		return "up"
	case ScrollDown:
		return "down"
	default:
		return "unknown"
	}
}

// ParseScrollDirection parses a user-facing scroll direction.
func ParseScrollDirection(name string) (ScrollDirection, error) {
	switch strings.ToLower(name) {
	case "up":
		return ScrollUp, nil
	case "down":
		return ScrollDown, nil
	default:
		return ScrollDirectionNone, fmt.Errorf("unknown scroll direction %q", name)
	}
}

// MouseGestureKind identifies one high-level mouse command.
type MouseGestureKind uint8

const (
	MouseGestureNone MouseGestureKind = iota
	MouseGestureClick
	MouseGestureHover
	MouseGestureScroll
	MouseGestureDrag
)

func (g MouseGestureKind) String() string {
	switch g {
	case MouseGestureClick:
		return "click"
	case MouseGestureHover:
		return "hover"
	case MouseGestureScroll:
		return "scroll"
	case MouseGestureDrag:
		return "drag"
	default:
		return "unknown"
	}
}

// MousePoint is a zero-based terminal cell coordinate.
type MousePoint struct {
	X int
	Y int
}

// MouseGesture is a backend-independent high-level mouse command. Fields not
// used by its Kind must retain their zero value.
type MouseGesture struct {
	Kind      MouseGestureKind
	Point     MousePoint
	From      MousePoint
	To        MousePoint
	Button    MouseButton
	Modifiers []MouseModifier
	Direction ScrollDirection
	Ticks     int
}

// MouseEvent is a backend-independent normalized mouse event at a zero-based
// terminal cell. Gesture lets the VT layer validate compatibility and verify
// that an entire high-level operation was encoded.
type MouseEvent struct {
	Gesture   MouseGestureKind
	Action    MouseAction
	Button    MouseButton
	Modifiers MouseModifiers
	X         int
	Y         int
}

// NewClick constructs a click gesture.
func NewClick(x, y int, button MouseButton, modifiers []MouseModifier) MouseGesture {
	return MouseGesture{
		Kind: MouseGestureClick, Point: MousePoint{X: x, Y: y},
		Button: button, Modifiers: modifiers,
	}
}

// NewHover constructs a hover gesture.
func NewHover(x, y int, modifiers []MouseModifier) MouseGesture {
	return MouseGesture{
		Kind: MouseGestureHover, Point: MousePoint{X: x, Y: y},
		Modifiers: modifiers,
	}
}

// NewScroll constructs a vertical scroll gesture.
func NewScroll(x, y int, direction ScrollDirection, ticks int, modifiers []MouseModifier) MouseGesture {
	return MouseGesture{
		Kind: MouseGestureScroll, Point: MousePoint{X: x, Y: y},
		Modifiers: modifiers, Direction: direction, Ticks: ticks,
	}
}

// NewDrag constructs a drag gesture.
func NewDrag(fromX, fromY, toX, toY int, button MouseButton, modifiers []MouseModifier) MouseGesture {
	return MouseGesture{
		Kind:   MouseGestureDrag,
		From:   MousePoint{X: fromX, Y: fromY},
		To:     MousePoint{X: toX, Y: toY},
		Button: button, Modifiers: modifiers,
	}
}

// Expand validates the gesture's semantic options and expands it into a
// complete normalized event batch. Viewport bounds and active VT protocol
// compatibility are intentionally validated later, under the pump mutex.
func (g MouseGesture) Expand() ([]MouseEvent, error) {
	modifiers, err := NormalizeMouseModifiers(g.Modifiers)
	if err != nil {
		return nil, err
	}

	switch g.Kind {
	case MouseGestureClick:
		if err := g.validateOnly(MouseGestureClick); err != nil {
			return nil, err
		}
		button, err := gestureButton(g.Button)
		if err != nil {
			return nil, err
		}
		return clickEvents(g.Point, button, modifiers), nil

	case MouseGestureHover:
		if err := g.validateOnly(MouseGestureHover); err != nil {
			return nil, err
		}
		if g.Button != ButtonNone {
			return nil, fmt.Errorf("hover does not accept a button")
		}
		return []MouseEvent{{
			Gesture: MouseGestureHover, Action: MouseActionMotion,
			Button: ButtonNone, Modifiers: modifiers, X: g.Point.X, Y: g.Point.Y,
		}}, nil

	case MouseGestureScroll:
		if err := g.validateOnly(MouseGestureScroll); err != nil {
			return nil, err
		}
		if g.Button != ButtonNone {
			return nil, fmt.Errorf("scroll does not accept a button")
		}
		if g.Ticks <= 0 || g.Ticks > MaxScrollTicks {
			return nil, fmt.Errorf("scroll ticks must be between 1 and %d", MaxScrollTicks)
		}
		var button MouseButton
		switch g.Direction {
		case ScrollUp:
			button = ButtonWheelUp
		case ScrollDown:
			button = ButtonWheelDown
		default:
			return nil, fmt.Errorf("invalid scroll direction %q", g.Direction)
		}
		events := make([]MouseEvent, g.Ticks)
		for i := range events {
			events[i] = MouseEvent{
				Gesture: MouseGestureScroll, Action: MouseActionPress,
				Button: button, Modifiers: modifiers, X: g.Point.X, Y: g.Point.Y,
			}
		}
		return events, nil

	case MouseGestureDrag:
		if err := g.validateOnly(MouseGestureDrag); err != nil {
			return nil, err
		}
		button, err := gestureButton(g.Button)
		if err != nil {
			return nil, err
		}
		if g.From == g.To {
			// A zero-length drag has click semantics, including mode support.
			return clickEvents(g.From, button, modifiers), nil
		}
		points := bresenham(g.From, g.To)
		events := make([]MouseEvent, 0, len(points)+1)
		events = append(events, MouseEvent{
			Gesture: MouseGestureDrag, Action: MouseActionPress,
			Button: button, Modifiers: modifiers, X: g.From.X, Y: g.From.Y,
		})
		for _, point := range points[1:] {
			events = append(events, MouseEvent{
				Gesture: MouseGestureDrag, Action: MouseActionMotion,
				Button: button, Modifiers: modifiers, X: point.X, Y: point.Y,
			})
		}
		events = append(events, MouseEvent{
			Gesture: MouseGestureDrag, Action: MouseActionRelease,
			Button: button, Modifiers: modifiers, X: g.To.X, Y: g.To.Y,
		})
		return events, nil

	default:
		return nil, fmt.Errorf("unknown mouse gesture %d", g.Kind)
	}
}

func (g MouseGesture) validateOnly(kind MouseGestureKind) error {
	switch kind {
	case MouseGestureClick, MouseGestureHover:
		if g.From != (MousePoint{}) || g.To != (MousePoint{}) ||
			g.Direction != ScrollDirectionNone || g.Ticks != 0 {
			return fmt.Errorf("%s has options for another mouse gesture", kind)
		}
	case MouseGestureScroll:
		if g.From != (MousePoint{}) || g.To != (MousePoint{}) {
			return fmt.Errorf("scroll has options for another mouse gesture")
		}
	case MouseGestureDrag:
		if g.Point != (MousePoint{}) || g.Direction != ScrollDirectionNone || g.Ticks != 0 {
			return fmt.Errorf("drag has options for another mouse gesture")
		}
	}
	return nil
}

func gestureButton(button MouseButton) (MouseButton, error) {
	if button == ButtonNone {
		return ButtonLeft, nil
	}
	switch button {
	case ButtonLeft, ButtonMiddle, ButtonRight:
		return button, nil
	default:
		return ButtonNone, fmt.Errorf("invalid gesture mouse button %q", button)
	}
}

func clickEvents(point MousePoint, button MouseButton, modifiers MouseModifiers) []MouseEvent {
	return []MouseEvent{
		{
			Gesture: MouseGestureClick, Action: MouseActionPress,
			Button: button, Modifiers: modifiers, X: point.X, Y: point.Y,
		},
		{
			Gesture: MouseGestureClick, Action: MouseActionRelease,
			Button: button, Modifiers: modifiers, X: point.X, Y: point.Y,
		},
	}
}

// bresenham returns every cell on the line, including both endpoints.
func bresenham(from, to MousePoint) []MousePoint {
	x, y := from.X, from.Y
	dx := abs(to.X - from.X)
	dy := -abs(to.Y - from.Y)
	sx, sy := -1, -1
	if from.X < to.X {
		sx = 1
	}
	if from.Y < to.Y {
		sy = 1
	}
	err := dx + dy

	points := make([]MousePoint, 0, max(dx, -dy)+1)
	for {
		points = append(points, MousePoint{X: x, Y: y})
		if x == to.X && y == to.Y {
			return points
		}
		twice := 2 * err
		if twice >= dy {
			err += dy
			x += sx
		}
		if twice <= dx {
			err += dx
			y += sy
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
