package daemon

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	optionalRegistrations = append(optionalRegistrations, func(d *Dispatcher) {
		d.Register(rpc.OpText, handleText)
		d.Register(rpc.OpLines, handleLines)
		d.Register(rpc.OpCell, handleCell)
		d.Register(rpc.OpRegion, handleRegion)
		d.Register(rpc.OpCursor, handleCursor)
		d.Register(rpc.OpFind, handleFind)
		d.Register(rpc.OpSize, handleSize)
		d.Register(rpc.OpTitle, handleTitle)
		d.Register(rpc.OpMode, handleMode)
		d.Register(rpc.OpScrollback, handleScrollback)
		d.Register(rpc.OpSnapshot, handleSnapshot)
	})
}

func handleText(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	return rpc.TextData{Text: t.VisibleText()}, nil
}

func handleLines(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	return rpc.LinesData{Lines: t.Lines()}, nil
}

func handleCell(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.CellArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	snap := t.Snapshot()
	if a.Y < 0 || a.Y >= len(snap.Lines) {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: "y out of range"}
	}
	row := snap.Lines[a.Y].Cells
	if a.X < 0 || a.X >= len(row) {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: "x out of range"}
	}
	return row[a.X], nil
}

func handleRegion(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.RegionArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	if a.W <= 0 || a.H <= 0 {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: "w and h must be > 0"}
	}
	snap := t.Snapshot()
	out := make([][]engine.Cell, 0, a.H)
	for y := a.Y; y < a.Y+a.H && y < len(snap.Lines); y++ {
		row := snap.Lines[y].Cells
		end := a.X + a.W
		if end > len(row) {
			end = len(row)
		}
		if a.X < 0 || a.X > end {
			out = append(out, []engine.Cell{})
			continue
		}
		out = append(out, row[a.X:end])
	}
	return out, nil
}

func handleCursor(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	c := t.CursorPos()
	return rpc.CursorData{X: c.Col, Y: c.Row, Visible: c.Visible}, nil
}

func handleFind(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.FindArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	lines := t.Lines()
	matches := make([]rpc.FindMatch, 0)
	if a.Regex {
		re, err := regexp.Compile(a.Text)
		if err != nil {
			return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
		}
		for y, ln := range lines {
			for _, idx := range re.FindAllStringIndex(ln, -1) {
				matches = append(matches, rpc.FindMatch{
					X: idx[0], Y: y, W: idx[1] - idx[0], H: 1,
					Line: y, Text: ln[idx[0]:idx[1]],
				})
			}
		}
	} else {
		for y, ln := range lines {
			start := 0
			for {
				i := strings.Index(ln[start:], a.Text)
				if i < 0 {
					break
				}
				matches = append(matches, rpc.FindMatch{
					X: start + i, Y: y, W: len(a.Text), H: 1,
					Line: y, Text: a.Text,
				})
				start = start + i + len(a.Text)
			}
		}
	}
	return matches, nil
}

func handleSize(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	snap := t.Snapshot()
	return rpc.SizeData{Cols: snap.Cols, Rows: snap.Rows}, nil
}

func handleTitle(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	// Title not currently surfaced via internal/vt; return empty.
	_ = t
	return rpc.TitleData{Title: ""}, nil
}

func handleMode(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	snap := t.Snapshot()
	return rpc.ModeData{
		AltScreen: snap.AltScreen,
		// DECCKM and BracketedPaste are not exposed; default false.
	}, nil
}

func handleScrollback(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	_ = t
	// Scrollback retention is not a v0 feature; viewport-only.
	return struct {
		Lines []string `json:"lines"`
	}{Lines: []string{}}, nil
}

func handleSnapshot(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	return t.Snapshot(), nil
}
