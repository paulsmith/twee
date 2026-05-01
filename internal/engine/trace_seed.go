package engine

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/paulsmith/research/twee/internal/vt"
)

func traceSeedOutput(s vt.Snapshot) []byte {
	if s.Size.Cols <= 0 || s.Size.Rows <= 0 {
		return nil
	}
	var b bytes.Buffer
	b.WriteString("\x1b[0m")
	if s.AltScreen {
		b.WriteString("\x1b[?1049h")
	}
	b.WriteString("\x1b[2J")
	for y := 0; y < s.Size.Rows && y < len(s.Lines); y++ {
		fmt.Fprintf(&b, "\x1b[%d;1H", y+1)
		for _, c := range s.Lines[y].Cells {
			if c.Width == 0 {
				continue
			}
			b.WriteString(cellSGR(c))
			writeCellText(&b, c.Text)
			if c.Text == "" {
				b.WriteByte(' ')
			}
		}
	}
	b.WriteString("\x1b[0m")
	if s.Cursor.Visible {
		b.WriteString("\x1b[?25h")
	} else {
		b.WriteString("\x1b[?25l")
	}
	row := clamp1(s.Cursor.Row+1, s.Size.Rows)
	col := clamp1(s.Cursor.Col+1, s.Size.Cols)
	fmt.Fprintf(&b, "\x1b[%d;%dH", row, col)
	return b.Bytes()
}

func cellSGR(c vt.Cell) string {
	params := []string{"0"}
	if c.Bold {
		params = append(params, "1")
	}
	if c.Dim {
		params = append(params, "2")
	}
	if c.Italic {
		params = append(params, "3")
	}
	if c.Underline {
		params = append(params, "4")
	}
	if c.Inverse {
		params = append(params, "7")
	}
	if c.Strikethrough {
		params = append(params, "9")
	}
	params = append(params, colorSGR(c.Fg, false)...)
	params = append(params, colorSGR(c.Bg, true)...)
	return "\x1b[" + strings.Join(params, ";") + "m"
}

func colorSGR(c vt.Color, bg bool) []string {
	switch c.Kind {
	case vt.ColorPalette:
		n := int(c.Index)
		if n < 8 {
			base := 30
			if bg {
				base = 40
			}
			return []string{strconv.Itoa(base + n)}
		}
		if n < 16 {
			base := 90
			if bg {
				base = 100
			}
			return []string{strconv.Itoa(base + n - 8)}
		}
		prefix := "38"
		if bg {
			prefix = "48"
		}
		return []string{prefix, "5", strconv.Itoa(n)}
	case vt.ColorRGB:
		prefix := "38"
		if bg {
			prefix = "48"
		}
		return []string{prefix, "2", strconv.Itoa(int(c.R)), strconv.Itoa(int(c.G)), strconv.Itoa(int(c.B))}
	default:
		if bg {
			return []string{"49"}
		}
		return []string{"39"}
	}
}

func writeCellText(b *bytes.Buffer, text string) {
	for _, r := range text {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
}

func clamp1(v, max int) int {
	if v < 1 {
		return 1
	}
	if max > 0 && v > max {
		return max
	}
	return v
}
