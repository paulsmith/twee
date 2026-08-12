package play

import (
	"fmt"
	"io"
	"strings"
)

// writeStatusRow paints one parent-owned, tmux-like row at the bottom of the
// terminal. Playback frames never include this row while it is visible.
func writeStatusRow(w io.Writer, terminalCols, terminalRows int, toast, status string) error {
	if terminalCols <= 0 || terminalRows <= 0 {
		return nil
	}
	line := status
	if toast != "" {
		line += " │ " + toast
	}
	line += " │ twee play │ space pause  . step  > +1s  [/] marker  m list  -/+ speed  r restart  h status  q quit"
	line = sanitizeFooterLine(line, terminalCols)
	padding := max(terminalCols-footerLineWidth(line), 0)
	_, err := fmt.Fprintf(w, "\x1b[%d;1H\x1b[0m\x1b[2K\x1b[7m%s%s\x1b[0m\x1b[H",
		terminalRows, line, strings.Repeat(" ", padding))
	return err
}

func clearStatusRow(w io.Writer, row int) error {
	if row <= 0 {
		return nil
	}
	_, err := fmt.Fprintf(w, "\x1b[%d;1H\x1b[0m\x1b[2K\x1b[H", row)
	return err
}
