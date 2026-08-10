// Package input encodes semantic key events as terminal byte sequences.
//
// The profile is xterm-256color with legacy keyboard encoding. Cursor keys
// honor DECCKM when the caller supplies the active input mode. The Kitty
// keyboard protocol is intentionally not modeled.
package input

import (
	"fmt"
	"strings"
)

// Key is a named key. Use Ctrl(letter) for control combinations.
type Key int

const (
	KeyNone Key = iota
	KeyEnter
	KeyEscape
	KeyTab
	KeyBackspace
	KeyDelete
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	keyCtrlBase = 0x100
)

// Ctrl returns a Key for Ctrl+letter. letter must be a-z or A-Z.
func Ctrl(letter byte) Key {
	if letter >= 'a' && letter <= 'z' {
		letter -= 'a' - 'A'
	}
	return Key(keyCtrlBase + int(letter))
}

// Encode returns the bytes that represent k. Returns nil for KeyNone.
func Encode(k Key) []byte {
	return EncodeWithModes(k, KeyModes{})
}

// KeyModes are the modeled terminal modes that affect semantic key encoding.
// Kitty keyboard protocol state is not included because the VT backend does
// not currently expose it reliably.
type KeyModes struct {
	ApplicationCursor bool
}

// EncodeWithModes returns the bytes that represent k under modes. DECCKM
// changes the four semantic cursor keys from CSI to SS3 sequences.
func EncodeWithModes(k Key, modes KeyModes) []byte {
	if k >= keyCtrlBase {
		c := byte(int(k) - keyCtrlBase)
		// Ctrl+letter -> letter & 0x1f
		return []byte{c & 0x1f}
	}
	switch k {
	case KeyEnter:
		return []byte{'\r'}
	case KeyEscape:
		return []byte{0x1b}
	case KeyTab:
		return []byte{'\t'}
	case KeyBackspace:
		return []byte{0x7f}
	case KeyDelete:
		return []byte("\x1b[3~")
	case KeyUp:
		if modes.ApplicationCursor {
			return []byte("\x1bOA")
		}
		return []byte("\x1b[A")
	case KeyDown:
		if modes.ApplicationCursor {
			return []byte("\x1bOB")
		}
		return []byte("\x1b[B")
	case KeyRight:
		if modes.ApplicationCursor {
			return []byte("\x1bOC")
		}
		return []byte("\x1b[C")
	case KeyLeft:
		if modes.ApplicationCursor {
			return []byte("\x1bOD")
		}
		return []byte("\x1b[D")
	case KeyHome:
		return []byte("\x1b[H")
	case KeyEnd:
		return []byte("\x1b[F")
	case KeyPageUp:
		return []byte("\x1b[5~")
	case KeyPageDown:
		return []byte("\x1b[6~")
	}
	return nil
}

// Name returns a debug name for k.
func Name(k Key) string {
	if k >= keyCtrlBase {
		return fmt.Sprintf("Ctrl+%c", byte(int(k)-keyCtrlBase))
	}
	switch k {
	case KeyEnter:
		return "Enter"
	case KeyEscape:
		return "Escape"
	case KeyTab:
		return "Tab"
	case KeyBackspace:
		return "Backspace"
	case KeyDelete:
		return "Delete"
	case KeyUp:
		return "Up"
	case KeyDown:
		return "Down"
	case KeyLeft:
		return "Left"
	case KeyRight:
		return "Right"
	case KeyHome:
		return "Home"
	case KeyEnd:
		return "End"
	case KeyPageUp:
		return "PageUp"
	case KeyPageDown:
		return "PageDown"
	}
	return "Unknown"
}

// Parse converts a key name like "Enter" or "Ctrl+C" back to a Key.
func Parse(name string) (Key, error) {
	switch name {
	case "Enter":
		return KeyEnter, nil
	case "Escape", "Esc":
		return KeyEscape, nil
	case "Tab":
		return KeyTab, nil
	case "Backspace":
		return KeyBackspace, nil
	case "Delete", "Del":
		return KeyDelete, nil
	case "Up":
		return KeyUp, nil
	case "Down":
		return KeyDown, nil
	case "Left":
		return KeyLeft, nil
	case "Right":
		return KeyRight, nil
	case "Home":
		return KeyHome, nil
	case "End":
		return KeyEnd, nil
	case "PageUp", "PgUp":
		return KeyPageUp, nil
	case "PageDown", "PgDn":
		return KeyPageDown, nil
	}
	if strings.HasPrefix(name, "Ctrl+") && len(name) == 6 {
		c := name[5]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		if c >= 'A' && c <= 'Z' {
			return Ctrl(c), nil
		}
	}
	return KeyNone, fmt.Errorf("unknown key %q", name)
}

// EncodePaste wraps text in the bracketed-paste markers. v0 always
// emits the markers; apps that didn't enable bracketed paste will see
// them as literal input.
func EncodePaste(text string) []byte {
	out := make([]byte, 0, len(text)+12)
	out = append(out, "\x1b[200~"...)
	out = append(out, text...)
	out = append(out, "\x1b[201~"...)
	return out
}
