package engine

import (
	"errors"
	"fmt"

	"github.com/paulsmith/twee/internal/input"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/vt"
)

// Click sends a press and release at a zero-based terminal cell.
func (t *Term) Click(x, y int, button input.MouseButton, modifiers []input.MouseModifier) error {
	return t.mouseInput(input.NewClick(x, y, button, modifiers))
}

// Hover sends buttonless motion at a zero-based terminal cell.
func (t *Term) Hover(x, y int, modifiers []input.MouseModifier) error {
	return t.mouseInput(input.NewHover(x, y, modifiers))
}

// Scroll sends ticks vertical wheel reports at a zero-based terminal cell.
func (t *Term) Scroll(
	x, y int,
	direction input.ScrollDirection,
	ticks int,
	modifiers []input.MouseModifier,
) error {
	return t.mouseInput(input.NewScroll(x, y, direction, ticks, modifiers))
}

// Drag sends a press, cell-by-cell motion, and release between two zero-based
// terminal cells. A zero-length drag has click wire semantics.
func (t *Term) Drag(
	fromX, fromY, toX, toY int,
	button input.MouseButton,
	modifiers []input.MouseModifier,
) error {
	return t.mouseInput(input.NewDrag(fromX, fromY, toX, toY, button, modifiers))
}

// MouseState returns the current VT backend mouse state. Pump serialization
// keeps the inspection from racing child output that changes DECSET modes.
func (t *Term) MouseState() (vt.MouseState, error) {
	return t.pump.MouseState()
}

func (t *Term) mouseInput(gesture input.MouseGesture) error {
	t.inputMu.Lock()
	defer t.inputMu.Unlock()

	// Validate endpoints against the live model before gesture expansion.
	// Drag expansion allocates one event per Bresenham cell, so letting a
	// hostile MinInt/MaxInt endpoint reach Expand could overflow its distance
	// arithmetic or attempt an enormous allocation. EncodeMouse repeats this
	// check under the pump lock as the authoritative preflight.
	size := t.pump.Snapshot().Size
	if err := prevalidateMouseCoordinates(gesture, size); err != nil {
		return err
	}

	events, err := gesture.Expand()
	if err != nil {
		return invalidRequest(err.Error(), mouseGestureDetails(gesture), err)
	}

	// EncodeMouse holds pump.mu across live dimensions/mode inspection and
	// complete-batch encoding, then releases it before this potentially
	// blocking PTY write.
	encoded, err := t.pump.EncodeMouse(events)
	if err != nil {
		return classifyMouseEncodeError(err, gesture.Kind)
	}
	if err := writeAll(t.inputWriter, encoded.Bytes); err != nil {
		return inputIO(err)
	}

	t.recordInput(mouseGestureDescription(gesture))
	t.cfgMu.Lock()
	tr := t.tr
	t.cfgMu.Unlock()
	if tr != nil {
		tr.WriteMouseInput(mouseTraceInput(gesture), encoded.Bytes)
	}
	return nil
}

func prevalidateMouseCoordinates(gesture input.MouseGesture, size vt.Size) error {
	validate := func(point input.MousePoint) error {
		if point.X >= 0 && point.Y >= 0 && point.X < size.Cols && point.Y < size.Rows {
			return nil
		}
		details := map[string]any{
			"gesture": gesture.Kind.String(),
			"x":       point.X,
			"y":       point.Y,
			"cols":    size.Cols,
			"rows":    size.Rows,
		}
		return invalidRequest(
			fmt.Sprintf(
				"mouse coordinate (%d,%d) outside %dx%d viewport",
				point.X, point.Y, size.Cols, size.Rows,
			),
			details,
			nil,
		)
	}

	switch gesture.Kind {
	case input.MouseGestureClick, input.MouseGestureHover, input.MouseGestureScroll:
		return validate(gesture.Point)
	case input.MouseGestureDrag:
		if err := validate(gesture.From); err != nil {
			return err
		}
		return validate(gesture.To)
	default:
		return nil
	}
}

func classifyMouseEncodeError(err error, gesture input.MouseGestureKind) error {
	mouseErr, ok := errors.AsType[*vt.MouseEncodeError](err)
	if !ok {
		// The VT contract promises typed validation and capability errors.
		// Preserve an unexpected backend failure as an internal error rather
		// than falsely labelling it as either an argument or PTY I/O failure.
		return fmt.Errorf("encode mouse input: %w", err)
	}

	details := mouseEncodeErrorDetails(mouseErr)
	if _, ok := details["gesture"]; !ok && gesture != input.MouseGestureNone {
		details["gesture"] = gesture.String()
	}
	switch mouseErr.Reason {
	case vt.MouseErrorInvalidBatch, vt.MouseErrorInvalidCoordinate:
		return invalidRequest(mouseErr.Error(), details, err)
	case vt.MouseErrorState, vt.MouseErrorEncoding, vt.MouseErrorUnexpectedReport:
		// State inspection failures and contradictions in the encoder
		// contract are backend failures, not terminal preconditions the
		// caller can satisfy.
		return fmt.Errorf("encode mouse input: %w", err)
	default:
		return failedPrecondition(mouseErr.Error(), details, err)
	}
}

func mouseEncodeErrorDetails(err *vt.MouseEncodeError) map[string]any {
	details := make(map[string]any)
	if err.Gesture != input.MouseGestureNone {
		details["gesture"] = err.Gesture.String()
	}
	switch err.Reason {
	case vt.MouseErrorInvalidCoordinate, vt.MouseErrorLegacyCoordinate:
		details["x"] = err.X
		details["y"] = err.Y
		details["cols"] = err.Cols
		details["rows"] = err.Rows
	}
	if err.Tracking != "" {
		details["mouse_tracking"] = string(err.Tracking)
	} else if err.Reason == vt.MouseErrorTrackingDisabled {
		details["mouse_tracking"] = string(vt.MouseTrackingNone)
	}
	if err.Format != "" {
		details["mouse_format"] = string(err.Format)
	}
	if len(err.Required) > 0 {
		required := make([]string, len(err.Required))
		for i, tracking := range err.Required {
			required[i] = string(tracking)
		}
		details["required_tracking"] = required
	}
	return details
}

func mouseGestureDetails(gesture input.MouseGesture) map[string]any {
	details := map[string]any{"gesture": gesture.Kind.String()}
	switch gesture.Kind {
	case input.MouseGestureClick, input.MouseGestureHover, input.MouseGestureScroll:
		details["x"] = gesture.Point.X
		details["y"] = gesture.Point.Y
	case input.MouseGestureDrag:
		details["from_x"] = gesture.From.X
		details["from_y"] = gesture.From.Y
		details["to_x"] = gesture.To.X
		details["to_y"] = gesture.To.Y
	}
	return details
}

func mouseGestureDescription(gesture input.MouseGesture) string {
	button := gesture.Button
	if button == input.ButtonNone {
		button = input.ButtonLeft
	}
	switch gesture.Kind {
	case input.MouseGestureClick:
		return fmt.Sprintf("Click %s @(%d,%d)", button, gesture.Point.X, gesture.Point.Y)
	case input.MouseGestureHover:
		return fmt.Sprintf("Hover @(%d,%d)", gesture.Point.X, gesture.Point.Y)
	case input.MouseGestureScroll:
		return fmt.Sprintf(
			"Scroll %s x%d @(%d,%d)",
			gesture.Direction, gesture.Ticks, gesture.Point.X, gesture.Point.Y,
		)
	case input.MouseGestureDrag:
		return fmt.Sprintf(
			"Drag %s (%d,%d)->(%d,%d)",
			button, gesture.From.X, gesture.From.Y, gesture.To.X, gesture.To.Y,
		)
	default:
		return "Mouse unknown"
	}
}

func mouseTraceInput(gesture input.MouseGesture) trace.MouseInput {
	modifiers, _ := input.NormalizeMouseModifiers(gesture.Modifiers)
	mouse := trace.MouseInput{
		Gesture:   gesture.Kind.String(),
		Modifiers: modifiers.Strings(),
	}
	button := gesture.Button
	if button == input.ButtonNone {
		button = input.ButtonLeft
	}

	switch gesture.Kind {
	case input.MouseGestureClick:
		mouse.X = new(gesture.Point.X)
		mouse.Y = new(gesture.Point.Y)
		mouse.Button = button.String()
	case input.MouseGestureHover:
		mouse.X = new(gesture.Point.X)
		mouse.Y = new(gesture.Point.Y)
	case input.MouseGestureScroll:
		mouse.X = new(gesture.Point.X)
		mouse.Y = new(gesture.Point.Y)
		mouse.Direction = gesture.Direction.String()
		mouse.Ticks = gesture.Ticks
	case input.MouseGestureDrag:
		mouse.FromX = new(gesture.From.X)
		mouse.FromY = new(gesture.From.Y)
		mouse.ToX = new(gesture.To.X)
		mouse.ToY = new(gesture.To.Y)
		mouse.Button = button.String()
	}
	return mouse
}

//go:fix inline
func mouseIntPointer(value int) *int { return new(value) }
