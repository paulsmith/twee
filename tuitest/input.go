package tuitest

import "github.com/paulsmith/twee/internal/input"

// Key is a named key.
type Key = input.Key

// MouseButton selects the button used by Click and Drag.
type MouseButton = input.MouseButton

// MouseModifier is a modifier held for a mouse gesture.
type MouseModifier = input.MouseModifier

// ScrollDirection is the direction of a vertical wheel gesture.
type ScrollDirection = input.ScrollDirection

// Named keys.
const (
	Enter     = input.KeyEnter
	Escape    = input.KeyEscape
	Tab       = input.KeyTab
	Backspace = input.KeyBackspace
	Delete    = input.KeyDelete
	Up        = input.KeyUp
	Down      = input.KeyDown
	Left      = input.KeyLeft
	Right     = input.KeyRight
	Home      = input.KeyHome
	End       = input.KeyEnd
	PageUp    = input.KeyPageUp
	PageDown  = input.KeyPageDown
)

// Mouse buttons accepted by WithButton.
const (
	LeftButton   = input.ButtonLeft
	MiddleButton = input.ButtonMiddle
	RightButton  = input.ButtonRight
)

// Mouse modifiers accepted by WithMouseModifiers.
const (
	ShiftModifier = input.ModifierShift
	AltModifier   = input.ModifierAlt
	CtrlModifier  = input.ModifierCtrl
)

// Vertical scroll directions.
const (
	ScrollUp   = input.ScrollUp
	ScrollDown = input.ScrollDown
)

// Common control shortcuts.
var (
	CtrlC = input.Ctrl('C')
	CtrlD = input.Ctrl('D')
	CtrlN = input.Ctrl('N')
	CtrlP = input.Ctrl('P')
)

// Ctrl returns a Key for Ctrl+letter.
func Ctrl(letter byte) Key { return input.Ctrl(letter) }
