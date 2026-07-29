package engine

import "github.com/paulsmith/twee/internal/vt"

// Snapshot is an immutable view of terminal state at a moment in time.
type Snapshot struct {
	Cols, Rows int
	Cursor     Cursor
	Lines      []Line
	AltScreen  bool
}

// Cursor position and visibility.
type Cursor struct {
	Col, Row int
	Visible  bool
}

// Line is a row of cells.
type Line struct{ Cells []Cell }

// Cell is one display cell. The second cell of a wide character has
// Width=0 and Text="".
type Cell struct {
	Text          string
	Width         int
	Fg, Bg        Color
	Bold          bool
	Dim           bool
	Italic        bool
	Underline     bool
	Inverse       bool
	Strikethrough bool
}

// Color identifies a cell color.
type Color struct {
	Kind    ColorKind
	Index   uint8
	R, G, B uint8
}

// ColorKind selects how a Color is interpreted.
type ColorKind uint8

const (
	ColorDefault ColorKind = iota
	ColorIndexed
	ColorPalette
	ColorRGB
)

func fromVT(s vt.Snapshot) Snapshot {
	out := Snapshot{
		Cols:      s.Size.Cols,
		Rows:      s.Size.Rows,
		Cursor:    Cursor{Col: s.Cursor.Col, Row: s.Cursor.Row, Visible: s.Cursor.Visible},
		AltScreen: s.AltScreen,
		Lines:     make([]Line, len(s.Lines)),
	}
	for i, ln := range s.Lines {
		cells := make([]Cell, len(ln.Cells))
		for j, c := range ln.Cells {
			cells[j] = Cell{
				Text: c.Text, Width: c.Width,
				Fg: fromVTColor(c.Fg), Bg: fromVTColor(c.Bg),
				Bold: c.Bold, Dim: c.Dim, Italic: c.Italic,
				Underline: c.Underline, Inverse: c.Inverse,
				Strikethrough: c.Strikethrough,
			}
		}
		out.Lines[i] = Line{Cells: cells}
	}
	return out
}

// fromVTColor converts a vt.Color to the engine's Color. vt.ColorKind and
// ColorKind are deliberately not identical enums (ColorKind additionally
// distinguishes ColorIndexed from ColorPalette for rendering purposes), so
// this must switch on the meaning of each kind rather than cast the
// numeric value directly — a previous version of this function did
// `ColorKind(c.Kind)`, which silently relabeled every real vt.ColorPalette
// value (iota 1) as ColorIndexed (also iota 1), causing screenshot
// rendering to run every palette color through the 16-color ANSI table
// instead of the full 256-color palette.
func fromVTColor(c vt.Color) Color {
	switch c.Kind {
	case vt.ColorPalette:
		return Color{Kind: ColorPalette, Index: c.Index}
	case vt.ColorRGB:
		return Color{Kind: ColorRGB, R: c.R, G: c.G, B: c.B}
	default:
		return Color{Kind: ColorDefault}
	}
}
