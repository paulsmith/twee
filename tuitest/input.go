package tuitest

import "github.com/paulsmith/twee/internal/input"

// Key is a named key.
type Key = input.Key

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

// Common control shortcuts.
var (
	CtrlC = input.Ctrl('C')
	CtrlD = input.Ctrl('D')
	CtrlN = input.Ctrl('N')
	CtrlP = input.Ctrl('P')
)

// Ctrl returns a Key for Ctrl+letter.
func Ctrl(letter byte) Key { return input.Ctrl(letter) }
