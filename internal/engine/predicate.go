package engine

// CellPredicate matches the explicitly populated fields of a physical terminal
// cell. Pointer fields distinguish an omitted constraint from a required zero
// value or false style.
type CellPredicate struct {
	Text          *string
	Width         *int
	Fg, Bg        *Color
	Bold          *bool
	Dim           *bool
	Italic        *bool
	Underline     *bool
	Inverse       *bool
	Strikethrough *bool
}

// Empty reports whether the predicate has no constraints.
func (p CellPredicate) Empty() bool {
	return p.Text == nil && p.Width == nil && p.Fg == nil && p.Bg == nil &&
		p.Bold == nil && p.Dim == nil && p.Italic == nil && p.Underline == nil &&
		p.Inverse == nil && p.Strikethrough == nil
}

// Matches reports whether cell satisfies every populated constraint.
func (p CellPredicate) Matches(cell Cell) bool {
	return matchValue(p.Text, cell.Text) && matchValue(p.Width, cell.Width) &&
		matchColor(p.Fg, cell.Fg) && matchColor(p.Bg, cell.Bg) &&
		matchValue(p.Bold, cell.Bold) && matchValue(p.Dim, cell.Dim) &&
		matchValue(p.Italic, cell.Italic) && matchValue(p.Underline, cell.Underline) &&
		matchValue(p.Inverse, cell.Inverse) && matchValue(p.Strikethrough, cell.Strikethrough)
}

func matchValue[T comparable](want *T, got T) bool {
	return want == nil || *want == got
}

func matchColor(want *Color, got Color) bool {
	if want == nil {
		return true
	}
	if want.Kind == ColorPalette && got.Kind == ColorIndexed {
		return want.Index == got.Index
	}
	if want.Kind == ColorIndexed && got.Kind == ColorPalette {
		return want.Index == got.Index
	}
	return *want == got
}

// CellAt returns one physical cell and whether the coordinate is in bounds.
func CellAt(s Snapshot, x, y int) (Cell, bool) {
	if y < 0 || y >= len(s.Lines) || x < 0 || x >= len(s.Lines[y].Cells) {
		return Cell{}, false
	}
	return s.Lines[y].Cells[x], true
}

// RegionMatch selects how cells inside a region are quantified.
type RegionMatch string

const (
	RegionMatchAny RegionMatch = "any"
	RegionMatchAll RegionMatch = "all"
)

// RegionMatches evaluates predicate over the viewport intersection of rect.
// A nil rect means the whole viewport. Empty intersections never match.
func RegionMatches(s Snapshot, rect *Rect, mode RegionMatch, predicate CellPredicate) bool {
	x0, y0, x1, y1, ok := clippedRegion(s, rect)
	if !ok {
		return false
	}
	matched := 0
	total := 0
	for y := y0; y < y1; y++ {
		rowEnd := min(x1, len(s.Lines[y].Cells))
		for x := x0; x < rowEnd; x++ {
			total++
			if predicate.Matches(s.Lines[y].Cells[x]) {
				matched++
				if mode == RegionMatchAny {
					return true
				}
			} else if mode == RegionMatchAll {
				return false
			}
		}
	}
	return total > 0 && mode == RegionMatchAll && matched == total
}

func clippedRegion(s Snapshot, rect *Rect) (x0, y0, x1, y1 int, ok bool) {
	if rect == nil {
		return 0, 0, max(s.Cols, 0), min(max(s.Rows, 0), len(s.Lines)), s.Cols > 0 && s.Rows > 0 && len(s.Lines) > 0
	}
	x0, y0 = rect.X, rect.Y
	if x0 >= s.Cols || y0 >= s.Rows || x0 < 0 || y0 < 0 || rect.W <= 0 || rect.H <= 0 {
		return 0, 0, 0, 0, false
	}
	x1 = s.Cols
	if rect.W < s.Cols-x0 {
		x1 = x0 + rect.W
	}
	y1 = min(s.Rows, len(s.Lines))
	if rect.H < y1-y0 {
		y1 = y0 + rect.H
	}
	return x0, y0, x1, y1, x0 < x1 && y0 < y1
}
