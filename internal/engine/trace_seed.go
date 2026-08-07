package engine

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/paulsmith/twee/internal/vt"
)

// TraceSeedOutput returns terminal output that reconstructs the visible
// contents of a VT snapshot for trace playback.
func TraceSeedOutput(s vt.Snapshot) []byte {
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
		writeCellSpan(&b, s.Lines[y], 0, s.Size.Cols-1)
	}
	b.WriteString("\x1b[0m")
	b.WriteString(cursorStyleSequence(s.Cursor.Style))
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

// SnapshotDiffOutput returns terminal output that repaints only cells which
// differ between two equally sized snapshots. It deliberately leaves cursor
// shape, visibility, and position to the caller. A size change falls back to
// a complete seed because the caller must establish a new screen geometry.
func SnapshotDiffOutput(before, after vt.Snapshot) []byte {
	if after.Size.Cols <= 0 || after.Size.Rows <= 0 {
		return nil
	}
	if before.Size != after.Size {
		return TraceSeedOutput(after)
	}

	var b bytes.Buffer
	for y := 0; y < after.Size.Rows; y++ {
		first, last, changed := changedCellSpan(snapshotLine(before, y), snapshotLine(after, y), after.Size.Cols)
		if !changed {
			continue
		}
		fmt.Fprintf(&b, "\x1b[%d;%dH", y+1, first+1)
		writeCellSpan(&b, snapshotLine(after, y), first, last)
	}
	if b.Len() != 0 {
		b.WriteString("\x1b[0m")
	}
	return b.Bytes()
}

func snapshotLine(s vt.Snapshot, row int) vt.Line {
	if row < 0 || row >= len(s.Lines) {
		return vt.Line{}
	}
	return s.Lines[row]
}

func snapshotCell(line vt.Line, col int) vt.Cell {
	if col < 0 || col >= len(line.Cells) {
		return vt.Cell{Width: 1}
	}
	return line.Cells[col]
}

func changedCellSpan(before, after vt.Line, cols int) (int, int, bool) {
	first, last := -1, -1
	for col := 0; col < cols; col++ {
		if cellsVisuallyEqual(snapshotCell(before, col), snapshotCell(after, col)) {
			continue
		}
		if first == -1 {
			first = col
		}
		last = col
	}
	if first == -1 {
		return 0, 0, false
	}

	// Never begin at the spacer tail of a wide glyph. Repainting its leading
	// cell is required both when introducing and when erasing the glyph.
	for first > 0 && (snapshotCell(before, first).Width == 0 || snapshotCell(after, first).Width == 0) {
		first--
	}
	// Include a spacer tail when a changed leading cell becomes wide. This is
	// mostly redundant because the tail normally differs too, but protects
	// against backends which retain an identical tail cell across the update.
	if last+1 < cols && (snapshotCell(before, last).Width == 2 || snapshotCell(after, last).Width == 2) {
		last++
	}
	return first, last, true
}

func cellsVisuallyEqual(a, b vt.Cell) bool {
	if a.Width == 1 && (a.Text == "" || a.Text == " ") {
		a.Text = ""
	}
	if b.Width == 1 && (b.Text == "" || b.Text == " ") {
		b.Text = ""
	}
	return a == b
}

func writeCellSpan(b *bytes.Buffer, line vt.Line, first, last int) {
	lastSGR := ""
	for col := first; col <= last; col++ {
		c := snapshotCell(line, col)
		if c.Width == 0 {
			continue
		}
		sgr := cellSGR(c)
		if sgr != lastSGR {
			b.WriteString(sgr)
			lastSGR = sgr
		}
		writeCellText(b, c.Text)
		if c.Text == "" {
			b.WriteByte(' ')
		}
	}
}

func cursorStyleSequence(style vt.CursorStyle) string {
	switch style {
	case vt.CursorStyleBlock, vt.CursorStyleHollow:
		return "\x1b[2 q"
	case vt.CursorStyleUnderline:
		return "\x1b[4 q"
	case vt.CursorStyleBar:
		return "\x1b[6 q"
	default:
		return "\x1b[0 q"
	}
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
