package codegen

import (
	"strconv"
	"strings"
)

// statusMouseFilter removes mouse reports addressed to the parent-owned status
// row while preserving all other terminal input verbatim. It retains only an
// incomplete possible mouse report across reads.
type statusMouseFilter struct{ pending []byte }

func (f *statusMouseFilter) Feed(in []byte, childRows int, statusVisible bool) []byte {
	if !statusVisible {
		out := append(append([]byte(nil), f.pending...), in...)
		f.pending = nil
		return out
	}
	b := append([]byte(nil), f.pending...)
	b = append(b, in...)
	f.pending = nil
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); {
		if b[i] != '\x1b' || i+1 >= len(b) || b[i+1] != '[' {
			out = append(out, b[i])
			i++
			continue
		}
		end, row, incomplete, mouse := parseMouseReport(b[i:])
		if incomplete {
			f.pending = append(f.pending, b[i:]...)
			break
		}
		if !mouse {
			out = append(out, b[i])
			i++
			continue
		}
		if row <= childRows {
			out = append(out, b[i:i+end]...)
		}
		i += end
	}
	return out
}

func (f *statusMouseFilter) Flush() []byte {
	out := f.pending
	f.pending = nil
	return out
}

// parseMouseReport recognizes SGR cell, URxvt, and legacy X10 reports. It
// reports only complete, well-formed reports as mouse input; malformed CSI is
// deliberately left to the normal decoder unchanged.
func parseMouseReport(b []byte) (end, row int, incomplete, mouse bool) {
	if len(b) < 2 || b[0] != '\x1b' || b[1] != '[' {
		return 0, 0, false, false
	}
	if len(b) == 2 {
		return 0, 0, true, false
	}
	if b[2] == 'M' {
		if len(b) < 6 {
			return 0, 0, true, false
		}
		return 6, int(b[5]) - 32, false, true
	}
	if b[2] == '<' {
		return parseDelimitedMouse(b, 3, true)
	}
	if b[2] >= '0' && b[2] <= '9' {
		return parseDelimitedMouse(b, 2, false)
	}
	return 0, 0, false, false
}

func parseDelimitedMouse(b []byte, start int, sgr bool) (end, row int, incomplete, mouse bool) {
	for i := start; i < len(b); i++ {
		switch c := b[i]; {
		case c >= '0' && c <= '9', c == ';':
			continue
		case (sgr && (c == 'M' || c == 'm')) || (!sgr && c == 'M'):
			parts := strings.Split(string(b[start:i]), ";")
			if len(parts) != 3 {
				return 0, 0, false, false
			}
			for _, part := range parts {
				if part == "" {
					return 0, 0, false, false
				}
			}
			y, err := strconv.Atoi(parts[2])
			if err != nil || y < 1 {
				return 0, 0, false, false
			}
			return i + 1, y, false, true
		default:
			return 0, 0, false, false
		}
	}
	return 0, 0, true, false
}
