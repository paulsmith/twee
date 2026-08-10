package snapshot

import (
	"fmt"
	"strings"
)

// UnifiedDiff returns a human-readable line-by-line diff of expected
// vs actual. Returns "" when equal. Output uses leading-space context
// lines and `-`/`+` for changes (similar to unified diff but without
// minimal hunks — sufficient for v0).
func UnifiedDiff(expected, actual string) string {
	if expected == actual {
		return ""
	}
	wl := strings.Split(expected, "\n")
	gl := strings.Split(actual, "\n")
	var sb strings.Builder
	sb.WriteString("--- expected\n+++ actual\n@@ @@\n")
	maxLen := max(len(gl), len(wl))
	for i := range maxLen {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		switch {
		case w == g:
			fmt.Fprintf(&sb, " %s\n", w)
		case w == "":
			fmt.Fprintf(&sb, "+%s\n", g)
		case g == "":
			fmt.Fprintf(&sb, "-%s\n", w)
		default:
			fmt.Fprintf(&sb, "-%s\n+%s\n", w, g)
		}
	}
	return sb.String()
}
