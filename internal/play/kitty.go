package play

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/paulsmith/twee/internal/render"
)

const defaultKittyChunkSize = 4096

type kittySink struct {
	w            io.Writer
	imageID      int
	placementID  int
	chunkSize    int
	terminalCols int
	lastRows     int
}

func newKittySink(w io.Writer, terminalCols int) *kittySink {
	imageID := int(uint32(time.Now().UnixNano()^int64(os.Getpid()))) & 0x7fffffff
	if imageID == 0 {
		imageID = 1
	}
	return &kittySink{
		w:            w,
		imageID:      imageID,
		placementID:  1,
		chunkSize:    defaultKittyChunkSize,
		terminalCols: terminalCols,
	}
}

func (s *kittySink) Emit(img *image.RGBA, cols, rows int, toast, status string) error {
	if _, err := io.WriteString(s.w, "\x1b[H"); err != nil {
		return err
	}
	if s.lastRows > 0 && s.lastRows != rows {
		if err := clearFooter(s.w, s.lastRows); err != nil {
			return err
		}
	}
	if err := writeKittyPNG(s.w, img, kittyPlacement{
		imageID:     s.imageID,
		placementID: s.placementID,
		cols:        cols,
		rows:        rows,
		chunkSize:   s.chunkSize,
	}); err != nil {
		return err
	}
	err := writeFooter(s.w, s.terminalCols, cols, rows, toast, status)
	if err == nil {
		s.lastRows = rows
	}
	return err
}

// Close implements frameSink. Kitty images live only in the alternate screen,
// so leaving it performs all required cleanup and no protocol bytes are needed
// here. Keeping Close silent also preserves the legacy Kitty output exactly.
func (s *kittySink) Close() error { return nil }

func clearFooter(w io.Writer, rows int) error {
	if rows <= 0 {
		return nil
	}
	_, err := fmt.Fprintf(w, "\x1b[%d;1H\x1b[2K\x1b[%d;1H\x1b[2K",
		rows+1, rows+2)
	return err
}

type kittyPlacement struct {
	imageID     int
	placementID int
	cols        int
	rows        int
	chunkSize   int
}

func writeKittyPNG(w io.Writer, img image.Image, p kittyPlacement) error {
	var png bytes.Buffer
	if err := render.EncodePNG(&png, img); err != nil {
		return err
	}
	return writeKittyData(w, png.Bytes(), p)
}

func writeKittyData(w io.Writer, data []byte, p kittyPlacement) error {
	if p.chunkSize <= 0 {
		p.chunkSize = defaultKittyChunkSize
	}
	if p.imageID <= 0 {
		p.imageID = 1
	}
	if p.placementID <= 0 {
		p.placementID = 1
	}
	if p.cols <= 0 {
		p.cols = 1
	}
	if p.rows <= 0 {
		p.rows = 1
	}
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(data)))
	base64.StdEncoding.Encode(encoded, data)
	for first := true; len(encoded) > 0 || first; first = false {
		n := p.chunkSize
		if n > len(encoded) {
			n = len(encoded)
		}
		chunk := encoded[:n]
		encoded = encoded[n:]
		more := 0
		if len(encoded) > 0 {
			more = 1
		}
		var header string
		if first {
			header = fmt.Sprintf("\x1b_Ga=T,i=%d,p=%d,t=d,f=100,c=%d,r=%d,q=2,C=1,m=%d;",
				p.imageID, p.placementID, p.cols, p.rows, more)
		} else {
			header = fmt.Sprintf("\x1b_Gq=2,m=%d;", more)
		}
		if _, err := w.Write([]byte(header)); err != nil {
			return err
		}
		if _, err := w.Write(chunk); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\x1b\\"); err != nil {
			return err
		}
		if len(encoded) == 0 {
			break
		}
	}
	return nil
}

func sanitizeFooterLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && size == 1 {
			r = '?'
		}
		s = s[size:]
		switch {
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	clean := []rune(b.String())
	if len(clean) <= width {
		return string(clean)
	}
	if width == 1 {
		return "\u2026"
	}
	return string(clean[:width-1]) + "\u2026"
}
