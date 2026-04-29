package vt

import (
	"fmt"
	"runtime"
	"strings"

	libghostty "github.com/mitchellh/go-libghostty"
)

// ghosttyTerm wraps a libghostty Terminal plus reusable render-state
// scaffolding. libghostty's terminal is not safe for concurrent use; the
// pump's mutex is the single serialization point.
type ghosttyTerm struct {
	t  *libghostty.Terminal
	rs *libghostty.RenderState
	ri *libghostty.RenderStateRowIterator
	rc *libghostty.RenderStateRowCells
}

func newGhosttyTerm(cols, rows int) *ghosttyTerm {
	t, err := libghostty.NewTerminal(libghostty.WithSize(uint16(cols), uint16(rows)))
	if err != nil {
		panic(fmt.Errorf("vt: NewTerminal: %w", err))
	}
	rs, err := libghostty.NewRenderState()
	if err != nil {
		t.Close()
		panic(fmt.Errorf("vt: NewRenderState: %w", err))
	}
	ri, err := libghostty.NewRenderStateRowIterator()
	if err != nil {
		rs.Close()
		t.Close()
		panic(fmt.Errorf("vt: NewRenderStateRowIterator: %w", err))
	}
	rc, err := libghostty.NewRenderStateRowCells()
	if err != nil {
		ri.Close()
		rs.Close()
		t.Close()
		panic(fmt.Errorf("vt: NewRenderStateRowCells: %w", err))
	}
	g := &ghosttyTerm{t: t, rs: rs, ri: ri, rc: rc}
	// libghostty owns C-side allocations; release them on GC. Tests create
	// many short-lived models — without a finalizer the cgo footprint grows
	// for the duration of the process.
	runtime.SetFinalizer(g, (*ghosttyTerm).finalize)
	return g
}

func (g *ghosttyTerm) finalize() {
	g.rc.Close()
	g.ri.Close()
	g.rs.Close()
	g.t.Close()
}

func (g *ghosttyTerm) Feed(p []byte) error {
	g.t.VTWrite(p)
	return nil
}

func (g *ghosttyTerm) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	return g.t.Resize(uint16(cols), uint16(rows), 0, 0)
}

func (g *ghosttyTerm) Snapshot() Snapshot {
	if err := g.rs.Update(g.t); err != nil {
		panic(fmt.Errorf("vt: RenderState.Update: %w", err))
	}

	cols, _ := g.t.Cols()
	rows, _ := g.t.Rows()
	cx, _ := g.t.CursorX()
	cy, _ := g.t.CursorY()
	cv, _ := g.t.CursorVisible()
	active, _ := g.t.ActiveScreen()

	out := Snapshot{
		Size:      Size{Cols: int(cols), Rows: int(rows)},
		Cursor:    Cursor{Col: int(cx), Row: int(cy), Visible: cv},
		AltScreen: active == libghostty.ScreenAlternate,
		Lines:     make([]Line, 0, int(rows)),
	}

	if err := g.rs.RowIterator(g.ri); err != nil {
		panic(fmt.Errorf("vt: rs.RowIterator: %w", err))
	}
	for g.ri.Next() {
		if err := g.ri.Cells(g.rc); err != nil {
			panic(fmt.Errorf("vt: ri.Cells: %w", err))
		}
		cells := make([]Cell, 0, int(cols))
		for g.rc.Next() {
			cells = append(cells, readCell(g.rc))
		}
		out.Lines = append(out.Lines, Line{Cells: cells})
	}
	return out
}

func readCell(rc *libghostty.RenderStateRowCells) Cell {
	raw, err := rc.Raw()
	if err != nil {
		panic(fmt.Errorf("vt: rc.Raw: %w", err))
	}

	var c Cell
	wide, _ := raw.Wide()
	switch wide {
	case libghostty.CellWideNarrow:
		c.Width = 1
	case libghostty.CellWideWide:
		c.Width = 2
	case libghostty.CellWideSpacerTail:
		// continuation of the wide cell to the left; carry no text.
		c.Width = 0
	case libghostty.CellWideSpacerHead:
		// soft-wrap padding at end of a row when a wide char wouldn't fit.
		c.Width = 1
	}

	tag, _ := raw.ContentTag()
	switch tag {
	case libghostty.CellContentCodepointGrapheme:
		cps, err := rc.Graphemes()
		if err != nil {
			panic(fmt.Errorf("vt: rc.Graphemes: %w", err))
		}
		var sb strings.Builder
		for _, cp := range cps {
			sb.WriteRune(rune(cp))
		}
		c.Text = sb.String()
	case libghostty.CellContentCodepoint:
		cp, _ := raw.Codepoint()
		if cp != 0 {
			c.Text = string(rune(cp))
		}
	default:
		// Background-only cells (CellContentBgColor*) carry no text.
	}

	if style, err := rc.Style(); err == nil && style != nil {
		c.Bold = style.Bold()
		c.Dim = style.Faint()
		c.Italic = style.Italic()
		c.Underline = style.Underline() != libghostty.UnderlineNone
		c.Inverse = style.Inverse()
		c.Strikethrough = style.Strikethrough()
		c.Fg = styleColor(style.FgColor())
		c.Bg = styleColor(style.BgColor())
	}
	return c
}

func styleColor(sc libghostty.StyleColor) Color {
	switch sc.Tag {
	case libghostty.StyleColorPalette:
		return Color{Kind: ColorPalette, Index: sc.Palette}
	case libghostty.StyleColorRGB:
		return Color{Kind: ColorRGB, R: sc.RGB.R, G: sc.RGB.G, B: sc.RGB.B}
	default:
		return Color{Kind: ColorDefault}
	}
}
