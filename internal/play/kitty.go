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
	"unicode"
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
	terminalRows int
	lastRows     int
	statusRow    int
}

func newKittySink(w io.Writer, size terminalSize) *kittySink {
	imageID := int(uint32(time.Now().UnixNano()^int64(os.Getpid()))) & 0x7fffffff
	if imageID == 0 {
		imageID = 1
	}
	return &kittySink{
		w:            w,
		imageID:      imageID,
		placementID:  1,
		chunkSize:    defaultKittyChunkSize,
		terminalCols: size.Cols,
		terminalRows: size.Rows,
	}
}

func (s *kittySink) SetTerminalSize(cols, rows int) {
	s.terminalCols = cols
	s.terminalRows = rows
}

func (s *kittySink) Emit(img *image.RGBA, cols, rows int, toast, status string, statusVisible bool) error {
	if _, err := io.WriteString(s.w, "\x1b[H"); err != nil {
		return err
	}
	if s.lastRows > rows {
		if err := clearImageRows(s.w, rows); err != nil {
			return err
		}
	}
	if s.statusRow > 0 && (!statusVisible || s.statusRow != s.terminalRows) {
		if err := clearStatusRow(s.w, s.statusRow); err != nil {
			return err
		}
		s.statusRow = 0
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
	if statusVisible {
		if err := writeStatusRow(s.w, s.terminalCols, s.terminalRows, toast, status); err != nil {
			return err
		}
		s.statusRow = s.terminalRows
	}
	s.lastRows = rows
	return nil
}

// Close implements frameSink. Kitty images live only in the alternate screen,
// so leaving it performs all required cleanup and no protocol bytes are needed
// here. Keeping Close silent also preserves the legacy Kitty output exactly.
func (s *kittySink) Close() error { return nil }

func clearImageRows(w io.Writer, rows int) error {
	if rows <= 0 {
		return nil
	}
	_, err := fmt.Fprintf(w, "\x1b[%d;1H\x1b[0J\x1b[H", rows+1)
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
		n := min(p.chunkSize, len(encoded))
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
	clean := b.String()
	if footerLineWidth(clean) <= width {
		return clean
	}
	if width == 1 {
		return "…"
	}
	b.Reset()
	used := 0
	for _, r := range clean {
		cellWidth := footerRuneWidth(r)
		if used+cellWidth > width-1 {
			break
		}
		b.WriteRune(r)
		used += cellWidth
	}
	return b.String() + "…"
}

func footerLineWidth(s string) int {
	width := 0
	for _, r := range s {
		width += footerRuneWidth(r)
	}
	return width
}

func footerRuneWidth(r rune) int {
	if r == 0 || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == 0x200d {
		return 0
	}
	if r >= 0x1100 && (r <= 0x115f || r >= 0x2e80 && r <= 0xa4cf || r >= 0xac00 && r <= 0xd7a3 || r >= 0xf900 && r <= 0xfaff || r >= 0x1f300 && r <= 0x1faff || r >= 0xff01 && r <= 0xff60) {
		return 2
	}
	return 1
}
