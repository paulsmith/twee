package vt

import "strings"

// VisibleText returns the visible viewport as plain text. Continuation
// cells (the second cell of a wide character) are skipped. Trailing
// spaces on each line are stripped. Lines are joined with "\n".
func VisibleText(s Snapshot) string {
	lines := VisibleLines(s)
	return strings.Join(lines, "\n")
}

// VisibleLines returns one string per row, trailing spaces stripped.
func VisibleLines(s Snapshot) []string {
	out := make([]string, len(s.Lines))
	for i, line := range s.Lines {
		var sb strings.Builder
		for _, c := range line.Cells {
			if c.Width == 0 {
				continue
			}
			if c.Text == "" {
				sb.WriteByte(' ')
				continue
			}
			sb.WriteString(c.Text)
		}
		out[i] = strings.TrimRight(sb.String(), " ")
	}
	return out
}
