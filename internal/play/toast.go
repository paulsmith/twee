package play

import (
	"fmt"
	"strconv"
	"unicode/utf8"
)

type toast struct {
	text string
}

func formatEventToast(ev Event) string {
	prefix := fmt.Sprintf("[%06.3fs] \u2192 ", float64(ev.TMS)/1000)
	switch ev.Type {
	case "input":
		switch ev.Kind {
		case "key":
			key := ev.Key
			if key == "" {
				key = printableBytes(ev.Bytes)
			}
			return prefix + key
		case "type":
			return prefix + "type " + strconv.Quote(string(ev.Bytes))
		case "paste":
			return prefix + "paste " + strconv.Quote(displayPaste(ev.Bytes))
		default:
			if ev.Kind != "" {
				return prefix + ev.Kind
			}
			return prefix + "input"
		}
	case "resize":
		return fmt.Sprintf("%sresize %dx%d", prefix, ev.Cols, ev.Rows)
	default:
		return ""
	}
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
	return fmt.Sprintf("%s %.1f\u00d7 \u2022 %d/%d events \u2022 space=pause .=step >=+1s r=restart q=quit",
		mode, speed, cursor, total)
}
