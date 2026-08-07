package codegen

import (
	"fmt"
	"io"
	"strings"
	"unicode"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type statusBar struct {
	w          io.Writer
	enabled    bool
	rows       int
	cols       int
	ascii      bool
	phase      int
	toast      string
	composited bool
}

func (s *statusBar) draw(script *scriptController, trace *traceController) {
	if !s.enabled || s.composited || s.rows < 1 {
		return
	}
	line := s.line(script, trace)
	line = truncateStatus(line, s.cols)
	// Save/restore the application's cursor. The child viewport deliberately
	// excludes this row, so it cannot overwrite the parent-owned status line.
	fmt.Fprintf(s.w, "\x1b7\x1b[%d;1H\x1b[2K%s\x1b8", s.rows, line)
}
func (s *statusBar) line(script *scriptController, trace *traceController) string {
	line := fmt.Sprintf("%s twee wrap │ ^]q quit ^]s script ^]t trace │ script %s │ trace %s", s.mark(script.state), s.scriptText(script), s.traceText(trace))
	if s.toast != "" {
		line += " │ " + s.toast
	}
	return sanitizeStatus(line, s.ascii)
}

func sanitizeStatus(in string, ascii bool) string {
	var b strings.Builder
	for _, r := range in {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			b.WriteRune(' ')
			continue
		}
		if ascii && r > 0x7e {
			b.WriteRune('?')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (s *statusBar) mark(state recorderState) string {
	if s.ascii {
		switch state {
		case recorderRecording:
			return "*"
		case recorderFinalized:
			return "+"
		case recorderFailed:
			return "!"
		default:
			return "o"
		}
	}
	switch state {
	case recorderRecording:
		return spinnerFrames[s.phase%len(spinnerFrames)]
	case recorderFinalized:
		return "✓"
	case recorderFailed:
		return "✕"
	default:
		return "○"
	}
}

func (s *statusBar) scriptText(c *scriptController) string {
	switch c.state {
	case recorderRecording:
		partial := ""
		if c.partial {
			partial = " partial"
		}
		return s.mark(c.state) + partial + " " + c.path
	case recorderFinalized:
		partial := ""
		if c.partial {
			partial = " partial"
		}
		return s.mark(c.state) + partial + " " + c.path
	case recorderFailed:
		return s.mark(c.state) + " failed"
	default:
		return s.mark(c.state) + " off"
	}
}
func (s *statusBar) traceText(c *traceController) string {
	switch c.state {
	case recorderRecording:
		return s.mark(c.state) + " " + c.path
	case recorderFinalized:
		return s.mark(c.state) + " " + c.path
	case recorderFailed:
		return s.mark(c.state) + " failed"
	default:
		return s.mark(c.state) + " off"
	}
}

func truncateStatus(s string, width int) string {
	if width <= 0 || statusWidth(s) <= width {
		return s
	}
	if width == 1 {
		return "."
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := statusRuneWidth(r)
		if used+w > width-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "."
}

func statusWidth(s string) int {
	n := 0
	for _, r := range s {
		n += statusRuneWidth(r)
	}
	return n
}
func statusRuneWidth(r rune) int {
	if r == 0 || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == 0x200d {
		return 0
	}
	if r >= 0x1100 && (r <= 0x115f || r >= 0x2e80 && r <= 0xa4cf || r >= 0xac00 && r <= 0xd7a3 || r >= 0xf900 && r <= 0xfaff || r >= 0x1f300 && r <= 0x1faff || r >= 0xff01 && r <= 0xff60) {
		return 2
	}
	return 1
}

func statusASCII(term string) bool {
	return term == "dumb" || !strings.Contains(strings.ToUpper(term), "UTF") && term == ""
}
