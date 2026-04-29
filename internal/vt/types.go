// Package vt is the internal terminal model. It is hidden behind a narrow
// interface so the backend (libghostty-vt) can evolve without touching the
// public API.
package vt

// Size is a terminal dimension in cells.
type Size struct {
	Cols, Rows int
}

// Cursor is the cursor position and visibility.
type Cursor struct {
	Col, Row int
	Visible  bool
}

// Color identifies a foreground or background color. Kind selects the
// representation; the other fields are interpreted according to Kind.
type Color struct {
	Kind    ColorKind
	Index   uint8 // for Palette
	R, G, B uint8 // for RGB
}

type ColorKind uint8

const (
	ColorDefault ColorKind = iota
	ColorPalette           // 0..255 (xterm 256-palette; named SGR 0–15 use the low entries)
	ColorRGB
)

// Cell is one display cell. For wide characters the second cell has
// Width=0 and Text="" — the leading cell carries the grapheme.
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

// Line is a row of cells.
type Line struct {
	Cells []Cell
}

// Snapshot is an immutable view of the terminal state. Snapshots are full
// copies — callers may retain them indefinitely without affecting the
// live model.
type Snapshot struct {
	Size   Size
	Cursor Cursor
	Lines  []Line
	// AltScreen reports whether the alternate screen is active.
	AltScreen bool
}
