package tuitest

import "github.com/paulsmith/twee/internal/engine"

// Type aliases — the engine owns the actual definitions.
type (
	Snapshot      = engine.Snapshot
	Cursor        = engine.Cursor
	Line          = engine.Line
	Cell          = engine.Cell
	Color         = engine.Color
	ColorKind     = engine.ColorKind
	CellPredicate = engine.CellPredicate
	Rect          = engine.Rect
	RegionMatch   = engine.RegionMatch
)

// Color-kind constants.
const (
	ColorDefault   = engine.ColorDefault
	ColorIndexed   = engine.ColorIndexed
	ColorPalette   = engine.ColorPalette
	ColorRGB       = engine.ColorRGB
	RegionMatchAny = engine.RegionMatchAny
	RegionMatchAll = engine.RegionMatchAll
)
