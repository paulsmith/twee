package play

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"io"

	"github.com/paulsmith/twee/internal/render"
)

type iterm2Sink struct {
	w            io.Writer
	terminalCols int
}

func newITerm2Sink(w io.Writer, terminalCols int) *iterm2Sink {
	return &iterm2Sink{w: w, terminalCols: terminalCols}
}

func (s *iterm2Sink) Emit(img *image.RGBA, cols, rows int, toast, status string) error {
	// iTerm2 attaches inline images to cells. Clearing the alternate screen
	// before each frame removes the previous attachment even when a trace
	// resizes to a smaller frame.
	if _, err := io.WriteString(s.w, "\x1b[H\x1b[2J"); err != nil {
		return err
	}
	if err := writeITerm2PNG(s.w, img, cols, rows); err != nil {
		return err
	}
	width := s.terminalCols
	if width <= 0 {
		width = cols
	}
	toast = sanitizeFooterLine(toast, width)
	status = sanitizeFooterLine(status, width)
	_, err := fmt.Fprintf(s.w, "\x1b[%d;1H\x1b[2K%s\x1b[%d;1H\x1b[2K%s\x1b[H",
		rows+1, toast, rows+2, status)
	return err
}

func (s *iterm2Sink) Close() error { return nil }

func writeITerm2PNG(w io.Writer, img image.Image, cols, rows int) error {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	var png bytes.Buffer
	if err := render.EncodePNG(&png, img); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w,
		"\x1b]1337;File=size=%d;width=%d;height=%d;preserveAspectRatio=0;inline=1:",
		png.Len(), cols, rows); err != nil {
		return err
	}
	encoder := base64.NewEncoder(base64.StdEncoding, w)
	if _, err := io.Copy(encoder, &png); err != nil {
		_ = encoder.Close()
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\x1b\\")
	return err
}
