package tuitest

import (
	"fmt"

	"github.com/paulsmith/twee/internal/input"
)

// MouseOption configures one high-level mouse gesture.
type MouseOption interface {
	applyMouse(*mouseConfig) error
	allowedMouseGestures() mouseGestureMask
	mouseOptionName() string
}

type mouseGestureMask uint8

const (
	mouseGestureClick mouseGestureMask = 1 << iota
	mouseGestureHover
	mouseGestureScroll
	mouseGestureDrag
)

type mouseConfig struct {
	button    MouseButton
	buttonSet bool
	modifiers []MouseModifier
}

type mouseOption struct {
	name    string
	allowed mouseGestureMask
	apply   func(*mouseConfig) error
}

func (o mouseOption) applyMouse(cfg *mouseConfig) error { return o.apply(cfg) }
func (o mouseOption) allowedMouseGestures() mouseGestureMask {
	return o.allowed
}
func (o mouseOption) mouseOptionName() string { return o.name }

// WithButton selects the left, middle, or right button for Click or Drag.
// Click and Drag default to LeftButton.
func WithButton(button MouseButton) MouseOption {
	return mouseOption{
		name:    "WithButton",
		allowed: mouseGestureClick | mouseGestureDrag,
		apply: func(cfg *mouseConfig) error {
			if cfg.buttonSet {
				return fmt.Errorf("WithButton supplied more than once")
			}
			switch button {
			case LeftButton, MiddleButton, RightButton:
			default:
				return fmt.Errorf("unknown mouse button %d", button)
			}
			cfg.button = button
			cfg.buttonSet = true
			return nil
		},
	}
}

// WithMouseModifiers holds modifiers for a mouse gesture. It is valid for
// Click, Hover, Scroll, and Drag. Unknown and duplicate modifiers are errors.
func WithMouseModifiers(modifiers ...MouseModifier) MouseOption {
	values := append([]MouseModifier(nil), modifiers...)
	return mouseOption{
		name:    "WithMouseModifiers",
		allowed: mouseGestureClick | mouseGestureHover | mouseGestureScroll | mouseGestureDrag,
		apply: func(cfg *mouseConfig) error {
			cfg.modifiers = append(cfg.modifiers, values...)
			return nil
		},
	}
}

// Click presses and releases a mouse button at the zero-based viewport cell.
func (te *Term) Click(x, y int, opts ...MouseOption) error {
	cfg, err := applyMouseOptions("click", mouseGestureClick, opts)
	if err != nil {
		return err
	}
	return te.Term.Click(x, y, cfg.button, cfg.modifiers)
}

// Hover moves the mouse to the zero-based viewport cell without a pressed
// button. It requires any-event mouse tracking (mode 1003).
func (te *Term) Hover(x, y int, opts ...MouseOption) error {
	cfg, err := applyMouseOptions("hover", mouseGestureHover, opts)
	if err != nil {
		return err
	}
	return te.Term.Hover(x, y, cfg.modifiers)
}

// Scroll sends ticks of vertical wheel input at a zero-based viewport cell.
func (te *Term) Scroll(x, y int, direction ScrollDirection, ticks int, opts ...MouseOption) error {
	cfg, err := applyMouseOptions("scroll", mouseGestureScroll, opts)
	if err != nil {
		return err
	}
	return te.Term.Scroll(x, y, direction, ticks, cfg.modifiers)
}

// Drag holds a mouse button while moving cell by cell from the start to the
// end coordinate. A zero-length drag has click semantics.
func (te *Term) Drag(fromX, fromY, toX, toY int, opts ...MouseOption) error {
	cfg, err := applyMouseOptions("drag", mouseGestureDrag, opts)
	if err != nil {
		return err
	}
	return te.Term.Drag(fromX, fromY, toX, toY, cfg.button, cfg.modifiers)
}

func applyMouseOptions(gesture string, allowed mouseGestureMask, opts []MouseOption) (mouseConfig, error) {
	cfg := mouseConfig{button: LeftButton}
	for _, opt := range opts {
		if opt == nil {
			return mouseConfig{}, fmt.Errorf("%s: nil mouse option", gesture)
		}
		if opt.allowedMouseGestures()&allowed == 0 {
			return mouseConfig{}, fmt.Errorf("%s: %s is not applicable", gesture, opt.mouseOptionName())
		}
		if err := opt.applyMouse(&cfg); err != nil {
			return mouseConfig{}, fmt.Errorf("%s: %w", gesture, err)
		}
	}
	if _, err := input.NormalizeMouseModifiers(cfg.modifiers); err != nil {
		return mouseConfig{}, fmt.Errorf("%s: %w", gesture, err)
	}
	return cfg, nil
}
