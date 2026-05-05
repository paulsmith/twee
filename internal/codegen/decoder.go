// Package codegen records an interactive terminal session as replayable
// twee RPC script operations.
package codegen

import (
	"bytes"
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/paulsmith/research/twee/internal/input"
)

const controlPrefix = 0x1d // Ctrl+]

type inputKind int

const (
	inputType inputKind = iota
	inputKey
	inputPaste
	inputControl
	inputUnknown
)

type inputEvent struct {
	kind    inputKind
	text    string
	key     string
	control byte
	bytes   []byte
	warning string
}

// Decoder preserves partial recorder-control prefixes across raw terminal
// reads. DecodeBytes is still useful for complete buffers and unit tests.
type Decoder struct {
	pending []byte
}

func (d *Decoder) Decode(b []byte) []inputEvent {
	if len(d.pending) > 0 {
		joined := make([]byte, 0, len(d.pending)+len(b))
		joined = append(joined, d.pending...)
		joined = append(joined, b...)
		b = joined
		d.pending = nil
	}
	if len(b) == 0 {
		return nil
	}
	events, pending := decode(b, true)
	d.pending = append(d.pending[:0], pending...)
	return events
}

func (d *Decoder) Flush() []inputEvent {
	if len(d.pending) == 0 {
		return nil
	}
	events := DecodeBytes(d.pending)
	d.pending = nil
	return events
}

var knownSeqs = []struct {
	seq []byte
	key string
}{
	{[]byte("\x1b[3~"), input.Name(input.KeyDelete)},
	{[]byte("\x1b[5~"), input.Name(input.KeyPageUp)},
	{[]byte("\x1b[6~"), input.Name(input.KeyPageDown)},
	{[]byte("\x1b[A"), input.Name(input.KeyUp)},
	{[]byte("\x1b[B"), input.Name(input.KeyDown)},
	{[]byte("\x1b[C"), input.Name(input.KeyRight)},
	{[]byte("\x1b[D"), input.Name(input.KeyLeft)},
	{[]byte("\x1b[H"), input.Name(input.KeyHome)},
	{[]byte("\x1b[F"), input.Name(input.KeyEnd)},
	{[]byte("\x1bOA"), input.Name(input.KeyUp)},
	{[]byte("\x1bOB"), input.Name(input.KeyDown)},
	{[]byte("\x1bOC"), input.Name(input.KeyRight)},
	{[]byte("\x1bOD"), input.Name(input.KeyLeft)},
	{[]byte("\x1bOH"), input.Name(input.KeyHome)},
	{[]byte("\x1bOF"), input.Name(input.KeyEnd)},
}

var (
	pasteStart = []byte("\x1b[200~")
	pasteEnd   = []byte("\x1b[201~")
)

// DecodeBytes converts raw terminal input bytes into semantic recorder events.
// Unknown byte sequences are returned with their original bytes so callers can
// still pass them through to the child process.
func DecodeBytes(b []byte) []inputEvent {
	events, _ := decode(b, false)
	return events
}

func decode(b []byte, stream bool) ([]inputEvent, []byte) {
	var events []inputEvent
	for len(b) > 0 {
		if b[0] == controlPrefix {
			if len(b) == 1 {
				if stream {
					return events, append([]byte(nil), b...)
				}
				events = append(events, inputEvent{
					kind:    inputUnknown,
					bytes:   b[:1],
					warning: "incomplete Ctrl+] recorder command",
				})
				break
			}
			events = append(events, inputEvent{kind: inputControl, control: b[1]})
			b = b[2:]
			continue
		}

		if bytes.HasPrefix(b, pasteStart) {
			end := bytes.Index(b[len(pasteStart):], pasteEnd)
			if end < 0 {
				if stream {
					return events, append([]byte(nil), b...)
				}
				events = append(events, unknownEvent(b, "unterminated bracketed paste"))
				break
			}
			contentStart := len(pasteStart)
			contentEnd := contentStart + end
			rawEnd := contentEnd + len(pasteEnd)
			events = append(events, inputEvent{
				kind:  inputPaste,
				text:  string(b[contentStart:contentEnd]),
				bytes: append([]byte(nil), b[:rawEnd]...),
			})
			b = b[rawEnd:]
			continue
		}

		if ev, n, ok := decodeKnownKey(b); ok {
			events = append(events, ev)
			b = b[n:]
			continue
		}
		if stream && partialKnownKey(b) {
			return events, append([]byte(nil), b...)
		}

		if b[0] == 0x1b {
			if len(b) == 1 {
				if stream {
					return events, append([]byte(nil), b...)
				}
				events = append(events, keyEvent(input.Name(input.KeyEscape), b[:1]))
				break
			}
			if b[1] != '[' && b[1] != 'O' {
				events = append(events, keyEvent(input.Name(input.KeyEscape), b[:1]))
				b = b[1:]
				continue
			}
			if stream && incompleteEscape(b) {
				return events, append([]byte(nil), b...)
			}
			n := unknownEscapeLen(b)
			events = append(events, unknownEvent(b[:n], "unknown escape sequence"))
			b = b[n:]
			continue
		}

		if ev, ok := decodeControl(b[0]); ok {
			events = append(events, ev)
			b = b[1:]
			continue
		}

		n, incomplete := printableRunLen(b)
		if n > 0 {
			events = append(events, inputEvent{
				kind:  inputType,
				text:  string(b[:n]),
				bytes: append([]byte(nil), b[:n]...),
			})
			b = b[n:]
			continue
		}
		if stream && incomplete {
			return events, append([]byte(nil), b...)
		}

		events = append(events, unknownEvent(b[:1], "unknown input byte"))
		b = b[1:]
	}
	return events, nil
}

func decodeKnownKey(b []byte) (inputEvent, int, bool) {
	switch b[0] {
	case '\r', '\n':
		return keyEvent(input.Name(input.KeyEnter), b[:1]), 1, true
	case '\t':
		return keyEvent(input.Name(input.KeyTab), b[:1]), 1, true
	case 0x7f:
		return keyEvent(input.Name(input.KeyBackspace), b[:1]), 1, true
	}
	for _, k := range knownSeqs {
		if bytes.HasPrefix(b, k.seq) {
			return keyEvent(k.key, k.seq), len(k.seq), true
		}
	}
	return inputEvent{}, 0, false
}

func partialKnownKey(b []byte) bool {
	for _, k := range knownSeqs {
		if len(b) < len(k.seq) && bytes.HasPrefix(k.seq, b) {
			return true
		}
	}
	return false
}

func incompleteEscape(b []byte) bool {
	if len(b) < 2 || (b[1] != '[' && b[1] != 'O') {
		return false
	}
	for i := 2; i < len(b); i++ {
		if b[i] >= 0x40 && b[i] <= 0x7e {
			return false
		}
	}
	return true
}

func decodeControl(c byte) (inputEvent, bool) {
	if c >= 0x01 && c <= 0x1a {
		switch c {
		case '\t', '\n', '\r':
			return inputEvent{}, false
		}
		return keyEvent(input.Name(input.Ctrl('A'+c-1)), []byte{c}), true
	}
	return inputEvent{}, false
}

func keyEvent(key string, b []byte) inputEvent {
	return inputEvent{kind: inputKey, key: key, bytes: append([]byte(nil), b...)}
}

func unknownEvent(b []byte, msg string) inputEvent {
	return inputEvent{
		kind:    inputUnknown,
		bytes:   append([]byte(nil), b...),
		warning: fmt.Sprintf("%s omitted from script: % x", msg, b),
	}
}

func unknownEscapeLen(b []byte) int {
	if len(b) < 2 {
		return len(b)
	}
	if b[1] != '[' && b[1] != 'O' {
		return 2
	}
	for i := 2; i < len(b); i++ {
		if b[i] >= 0x40 && b[i] <= 0x7e {
			return i + 1
		}
	}
	return len(b)
}

func printableRunLen(b []byte) (int, bool) {
	n := 0
	for n < len(b) {
		if b[n] < 0x20 || b[n] == 0x7f {
			break
		}
		if !utf8.FullRune(b[n:]) {
			return n, true
		}
		r, size := utf8.DecodeRune(b[n:])
		if r == utf8.RuneError && size == 1 {
			break
		}
		if !unicode.IsPrint(r) {
			break
		}
		n += size
	}
	return n, false
}
