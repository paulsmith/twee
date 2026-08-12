package daemon

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	optionalRegistrations = append(optionalRegistrations, func(d *Dispatcher) {
		d.Register(rpc.OpWaitCell, handleWaitCell)
		d.Register(rpc.OpAssertCell, handleAssertCell)
		d.Register(rpc.OpAssertRegion, handleAssertRegion)
	})
}

func handleWaitCell(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeCanonicalArgs[rpc.WaitCellArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	x, y, predicate, errResp := validateCellPredicateArgs(a.X, a.Y, a.Predicate)
	if errResp != nil {
		return nil, errResp
	}
	to, err := parseTimeout(a.Timeout, t.DefaultTimeout())
	if err != nil {
		return nil, invalidArgument(err)
	}
	snapshot, err := t.WaitForCellAtSnapshot(x, y, predicate, engine.WithTimeout(to))
	if err != nil {
		return nil, waitErrDetails(t, err, cellFailureDetails(snapshot, x, y, a.Predicate))
	}
	return nil, nil
}

func handleAssertCell(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeCanonicalArgs[rpc.AssertCellArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	x, y, predicate, errResp := validateCellPredicateArgs(a.X, a.Y, a.Predicate)
	if errResp != nil {
		return nil, errResp
	}
	diagnostic := t.CaptureDiagnostic()
	cell, ok := engine.CellAt(diagnostic.Snapshot, x, y)
	if !ok || !predicate.Matches(cell) {
		return nil, assertionFailed("cell predicate did not match", diagnostic, cellFailureDetails(diagnostic.Snapshot, x, y, a.Predicate))
	}
	return nil, nil
}

func handleAssertRegion(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeCanonicalArgs[rpc.AssertRegionArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	predicate, errResp := cellPredicateFromRPC(a.Predicate)
	if errResp != nil {
		return nil, errResp
	}
	rect, errResp := regionFromRPC(a.X, a.Y, a.W, a.H)
	if errResp != nil {
		return nil, errResp
	}
	mode := engine.RegionMatchAny
	if a.Match != "" {
		mode = engine.RegionMatch(a.Match)
	}
	if mode != engine.RegionMatchAny && mode != engine.RegionMatchAll {
		return nil, invalidArgumentMessage("match must be any or all")
	}
	diagnostic := t.CaptureDiagnostic()
	evaluation := engine.EvaluateRegion(diagnostic.Snapshot, rect, mode, predicate)
	if !evaluation.Matches {
		details := map[string]any{
			"predicate":      a.Predicate,
			"match":          mode,
			"region_summary": regionSummaryDetails(evaluation.Summary),
		}
		if rect == nil {
			details["region"] = "viewport"
		} else {
			details["region"] = map[string]int{"x": rect.X, "y": rect.Y, "w": rect.W, "h": rect.H}
		}
		return nil, assertionFailed("region predicate did not match", diagnostic, details)
	}
	return nil, nil
}

func validateCellPredicateArgs(xArg, yArg *int, wire rpc.CellPredicate) (int, int, engine.CellPredicate, *rpc.Error) {
	if xArg == nil || yArg == nil {
		return 0, 0, engine.CellPredicate{}, invalidArgumentMessage("x and y are required")
	}
	if *xArg < 0 || *yArg < 0 {
		return 0, 0, engine.CellPredicate{}, invalidArgumentMessage("x and y must be >= 0")
	}
	predicate, errResp := cellPredicateFromRPC(wire)
	return *xArg, *yArg, predicate, errResp
}

func cellPredicateFromRPC(wire rpc.CellPredicate) (engine.CellPredicate, *rpc.Error) {
	predicate := engine.CellPredicate{
		Text: wire.Text, Width: wire.Width,
		Bold: wire.Bold, Dim: wire.Dim, Italic: wire.Italic,
		Underline: wire.Underline, Inverse: wire.Inverse,
		Strikethrough: wire.Strikethrough,
	}
	if wire.Width != nil && (*wire.Width < 0 || *wire.Width > 2) {
		return engine.CellPredicate{}, invalidArgumentMessage("width must be 0, 1, or 2")
	}
	var errResp *rpc.Error
	predicate.Fg, errResp = colorPredicateFromRPC("fg", wire.Fg)
	if errResp != nil {
		return engine.CellPredicate{}, errResp
	}
	predicate.Bg, errResp = colorPredicateFromRPC("bg", wire.Bg)
	if errResp != nil {
		return engine.CellPredicate{}, errResp
	}
	if predicate.Empty() {
		return engine.CellPredicate{}, invalidArgumentMessage("at least one cell predicate is required")
	}
	return predicate, nil
}

func colorPredicateFromRPC(name string, wire *rpc.ColorPredicate) (*engine.Color, *rpc.Error) {
	if wire == nil {
		return nil, nil
	}
	switch wire.Kind {
	case rpc.ColorKindDefault:
		if wire.Index != nil || wire.R != nil || wire.G != nil || wire.B != nil {
			return nil, invalidArgumentMessage(name + " default color accepts no components")
		}
		color := engine.Color{Kind: engine.ColorDefault}
		return &color, nil
	case rpc.ColorKindPalette:
		if wire.Index == nil || wire.R != nil || wire.G != nil || wire.B != nil {
			return nil, invalidArgumentMessage(name + " palette color requires only index")
		}
		color := engine.Color{Kind: engine.ColorPalette, Index: *wire.Index}
		return &color, nil
	case rpc.ColorKindRGB:
		if wire.Index != nil || wire.R == nil || wire.G == nil || wire.B == nil {
			return nil, invalidArgumentMessage(name + " rgb color requires r, g, and b")
		}
		color := engine.Color{Kind: engine.ColorRGB, R: *wire.R, G: *wire.G, B: *wire.B}
		return &color, nil
	default:
		return nil, invalidArgumentMessage(fmt.Sprintf("%s color kind must be default, palette, or rgb", name))
	}
}

func regionFromRPC(x, y, w, h *int) (*engine.Rect, *rpc.Error) {
	present := 0
	for _, value := range []*int{x, y, w, h} {
		if value != nil {
			present++
		}
	}
	if present == 0 {
		return nil, nil
	}
	if present != 4 {
		return nil, invalidArgumentMessage("x, y, w, and h must be provided together")
	}
	if *x < 0 || *y < 0 || *w <= 0 || *h <= 0 {
		return nil, invalidArgumentMessage("x/y must be >= 0 and w/h must be > 0")
	}
	return &engine.Rect{X: *x, Y: *y, W: *w, H: *h}, nil
}

func cellFailureDetails(snapshot engine.Snapshot, x, y int, predicate rpc.CellPredicate) map[string]any {
	details := map[string]any{
		"x": x, "y": y, "predicate": predicate,
	}
	if cell, ok := engine.CellAt(snapshot, x, y); ok {
		details["actual"] = cellData(cell)
	} else {
		details["actual"] = nil
	}
	return details
}

func regionSummaryDetails(summary engine.RegionSummary) map[string]any {
	details := map[string]any{
		"total_cells":        summary.TotalCells,
		"matching_cells":     summary.MatchingCells,
		"empty_intersection": summary.TotalCells == 0,
	}
	if summary.Requested != nil {
		details["requested"] = map[string]int{"x": summary.Requested.X, "y": summary.Requested.Y, "w": summary.Requested.W, "h": summary.Requested.H}
	} else {
		details["requested"] = "viewport"
	}
	if summary.Clipped != nil {
		details["clipped"] = map[string]int{"x": summary.Clipped.X, "y": summary.Clipped.Y, "w": summary.Clipped.W, "h": summary.Clipped.H}
	} else {
		details["clipped"] = nil
	}
	if summary.FirstMismatch != nil {
		details["first_mismatch"] = map[string]any{
			"x":      summary.FirstMismatch.X,
			"y":      summary.FirstMismatch.Y,
			"actual": cellData(summary.FirstMismatch.Cell),
		}
	}
	return details
}

func assertionFailed(message string, diagnostic engine.Diagnostic, extra map[string]any) *rpc.Error {
	detailValues := commonFailureDetails(diagnostic)
	maps.Copy(detailValues, extra)
	details, _ := json.Marshal(detailValues)
	return &rpc.Error{Code: rpc.CodeAssertionFailed, Message: message, Details: details}
}
