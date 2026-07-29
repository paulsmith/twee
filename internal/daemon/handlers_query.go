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
	a, errResp := decodeArgs[rpc.CellArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	snap := t.Snapshot()
	if a.Y < 0 || a.Y >= len(snap.Lines) {
		return nil, invalidArgumentMessage("y out of range")
	}
	row := snap.Lines[a.Y].Cells
	if a.X < 0 || a.X >= len(row) {
		return nil, invalidArgumentMessage("x out of range")
	}
	return cellData(row[a.X]), nil
}

func handleRegion(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeArgs[rpc.RegionArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	if a.W <= 0 || a.H <= 0 {
		return nil, invalidArgumentMessage("w and h must be > 0")
	}
	snap := t.Snapshot()
	out := make([][]rpc.CellData, 0, a.H)
	for y := a.Y; y < a.Y+a.H && y < len(snap.Lines); y++ {
		row := snap.Lines[y].Cells
		end := a.X + a.W
		if end > len(row) {
			end = len(row)
		}
		if a.X < 0 || a.X > end {
			out = append(out, []rpc.CellData{})
			continue
		}
		cells := make([]rpc.CellData, len(row[a.X:end]))
		for i, c := range row[a.X:end] {
			cells[i] = cellData(c)
		}
		out = append(out, cells)
	}
	return out, nil
}

// cellData converts an internal engine.Cell into its stable, snake_case
// wire shape for the "cell" and "region" ops.
func cellData(c engine.Cell) rpc.CellData {
	return rpc.CellData{
		Text: c.Text, Width: c.Width,
		Fg: colorData(c.Fg), Bg: colorData(c.Bg),
		Bold: c.Bold, Dim: c.Dim, Italic: c.Italic,
		Underline: c.Underline, Inverse: c.Inverse,
		Strikethrough: c.Strikethrough,
	}
}

// colorData converts an internal engine.Color into its wire shape. Kind is
// a string enum rather than the bare int engine uses internally.
// engine.ColorIndexed is folded into "palette": both represent an
// xterm-palette index, just rendered at different granularity (16 vs 256
// colors) by the screenshot renderer — a distinction that isn't part of
// the terminal-state model this op reports on.
func colorData(c engine.Color) rpc.ColorData {
	switch c.Kind {
	case engine.ColorPalette, engine.ColorIndexed:
		idx := c.Index
		return rpc.ColorData{Kind: rpc.ColorKindPalette, Index: &idx}
	case engine.ColorRGB:
		return rpc.ColorData{Kind: rpc.ColorKindRGB, R: c.R, G: c.G, B: c.B}
	default:
		return rpc.ColorData{Kind: rpc.ColorKindDefault}
	}
}

func handleCursor(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	c := t.CursorPos()
	return rpc.CursorData{X: c.Col, Y: c.Row, Visible: c.Visible}, nil
}

func handleFind(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeArgs[rpc.FindArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	if a.Text == "" && !a.Regex {
		return nil, invalidArgumentMessage("text or regex required")
	}
	lines := t.Lines()
	matches := make([]rpc.FindMatch, 0)
	if a.Regex {
		re, err := regexp.Compile(a.Text)
		if err != nil {
			return nil, invalidArgument(err)
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
