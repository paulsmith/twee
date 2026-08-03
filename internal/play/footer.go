package play

import (
	"fmt"
	"io"
)

// writeFooter places the two playback footer lines below a frame. It keeps
// footer text from changing terminal control state and limits it to the
// available terminal width.
func writeFooter(w io.Writer, terminalCols, frameCols, rows int, toast, status string) error {
	width := terminalCols
	if width <= 0 {
		width = frameCols
	}
	toast = sanitizeFooterLine(toast, width)
	status = sanitizeFooterLine(status, width)
	_, err := fmt.Fprintf(w, "\x1b[%d;1H\x1b[2K%s\x1b[%d;1H\x1b[2K%s\x1b[H",
		rows+1, toast, rows+2, status)
	return err
}
