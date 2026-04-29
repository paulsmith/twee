package engine

import "github.com/paulsmith/research/twee/internal/vt"

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
	Text      string
	Width     int
	Fg, Bg    Color
	Bold      bool
	Dim       bool
	Underline bool
	Inverse   bool
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
				Bold: c.Bold, Dim: c.Dim,
				Underline: c.Underline, Inverse: c.Inverse,
			}
		}
		out.Lines[i] = Line{Cells: cells}
	}
	return out
}

func fromVTColor(c vt.Color) Color {
	return Color{
		Kind:  ColorKind(c.Kind),
		Index: c.Index,
		R:     c.R, G: c.G, B: c.B,
	}
}
