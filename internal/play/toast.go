package play

import (
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/paulsmith/twee/internal/trace"
)

type toast struct {
	text string
}

// FormatEventToast formats an input or resize event as the one-line
// "footer" text twee play shows for the most recent such event
// ("[12.345s] -> Enter", "[1.000s] -> resize 100x30", ...). It returns
// "" for event types that don't get a footer line (output, exit).
// Exported so internal/export can reuse the identical formatting for
// its --input-overlay footer strip instead of drifting from play's.
func FormatEventToast(ev Event) string {
	prefix := fmt.Sprintf("[%06.3fs] \u2192 ", float64(ev.TMS)/1000)
	switch ev.Type {
	case trace.EventTypeInput:
		switch ev.Kind {
		case trace.InputKindKey:
			key := ev.Key
			if key == "" {
				key = printableBytes(ev.Bytes)
			}
			return prefix + key
		case trace.InputKindType:
			return prefix + "type " + strconv.Quote(string(ev.Bytes))
		case trace.InputKindPaste:
			return prefix + "paste " + strconv.Quote(displayPaste(ev.Bytes))
		case trace.InputKindMouse:
			return prefix + formatMouseToast(ev.Mouse)
		default:
			if ev.Kind != "" {
				return prefix + string(ev.Kind)
			}
			return prefix + "input"
		}
	case trace.EventTypeResize:
		return fmt.Sprintf("%sresize %dx%d", prefix, ev.Cols, ev.Rows)
	default:
		return ""
	}
}

func formatMouseToast(mouse *trace.MouseInput) string {
	if mouse == nil {
		return "mouse"
	}
	switch mouse.Gesture {
	case "click":
		return fmt.Sprintf("click %s @(%d,%d)", defaultMouseButton(mouse.Button), coord(mouse.X), coord(mouse.Y))
	case "hover":
		return fmt.Sprintf("hover @(%d,%d)", coord(mouse.X), coord(mouse.Y))
	case "scroll":
		return fmt.Sprintf("scroll %s x%d @(%d,%d)", mouse.Direction, mouse.Ticks, coord(mouse.X), coord(mouse.Y))
	case "drag":
		return fmt.Sprintf(
			"drag %s (%d,%d)->(%d,%d)",
			defaultMouseButton(mouse.Button),
			coord(mouse.FromX), coord(mouse.FromY), coord(mouse.ToX), coord(mouse.ToY),
		)
	default:
		if mouse.Gesture != "" {
			return mouse.Gesture
		}
		return "mouse"
	}
}

func defaultMouseButton(button string) string {
	if button == "" {
		return "left"
	}
	return button
}

func coord(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func displayPaste(b []byte) string {
	s := string(b)
	const start = "\x1b[200~"
	const end = "\x1b[201~"
	if len(s) >= len(start)+len(end) && s[:len(start)] == start && s[len(s)-len(end):] == end {
		return s[len(start) : len(s)-len(end)]
	}
	return s
}

func printableBytes(b []byte) string {
	if len(b) == 0 {
		return "input"
	}
	if utf8.Valid(b) {
		return strconv.Quote(string(b))
	}
	return fmt.Sprintf("% x", b)
}

func formatStatus(mode string, speed float64, cursor, total int) string {
	return fmt.Sprintf("%s %.1f× │ %d/%d events", mode, speed, cursor, total)
}
