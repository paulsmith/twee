package daemon

import (
	"encoding/json"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
	"github.com/paulsmith/twee/internal/vt"
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
		end := min(a.X+a.W, len(row))
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
	return cursorData(t.CursorPos()), nil
}

func cursorData(c engine.Cursor) rpc.CursorData {
	return rpc.CursorData{X: c.Col, Y: c.Row, Visible: c.Visible, Shape: cursorShape(c.Style)}
}

func cursorShape(style vt.CursorStyle) string {
	switch style {
	case vt.CursorStyleBlock:
		return "block"
	case vt.CursorStyleUnderline:
		return "underline"
	case vt.CursorStyleBar:
		return "bar"
	case vt.CursorStyleHollow:
		return "hollow"
	default:
		return "default"
	}
}

func handleFind(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeArgs[rpc.FindArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	matches, err := engine.FindMatches(t.Snapshot(), a.Text, a.Regex)
	if err != nil {
		return nil, invalidArgument(err)
	}
	out := make([]rpc.FindMatch, len(matches))
	for i, match := range matches {
		out[i] = findMatchData(match)
	}
	return out, nil
}

func findMatchData(match engine.FindMatch) rpc.FindMatch {
	return rpc.FindMatch{X: match.X, Y: match.Y, W: match.W, H: match.H, Line: match.Line, Text: match.Text}
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
	diagnostic := t.CaptureDiagnostic()
	if diagnostic.PresentationErr != nil {
		return nil, internalFailure(diagnostic.PresentationErr)
	}
	if diagnostic.MouseErr != nil {
		return nil, internalFailure(diagnostic.MouseErr)
	}
	return modeData(diagnostic), nil
}

func modeData(diagnostic engine.Diagnostic) rpc.ModeData {
	presentation := diagnostic.Presentation
	mouse := diagnostic.Mouse
	data := rpc.ModeData{
		DECCKM:             presentation.Input.ApplicationCursor,
		ApplicationKeypad:  presentation.Input.ApplicationKeypad,
		BracketedPaste:     presentation.Input.BracketedPaste,
		FocusEvents:        presentation.Input.FocusEvents,
		KittyKeyboardKnown: presentation.Input.KittyKeyboardKnown,
		KittyKeyboardFlags: presentation.Input.KittyKeyboardFlags,
		AltScreen:          diagnostic.Snapshot.AltScreen,
		MouseKnown:         mouse.TrackingKnown,
		MouseRaw:           mouse.Enabled,

		MouseTrackingX10:    mouse.Raw.TrackingX10,
		MouseTrackingNormal: mouse.Raw.TrackingNormal,
		MouseTrackingButton: mouse.Raw.TrackingButton,
		MouseTrackingAny:    mouse.Raw.TrackingAny,

		MouseFormatUTF8:      mouse.Raw.FormatUTF8,
		MouseFormatSGR:       mouse.Raw.FormatSGR,
		MouseFormatURxvt:     mouse.Raw.FormatURxvt,
		MouseFormatSGRPixels: mouse.Raw.FormatSGRPixels,
	}
	if mouse.TrackingKnown {
		data.Mouse = mouse.Tracking != vt.MouseTrackingNone
		data.MouseTracking = string(mouse.Tracking)
	}
	if mouse.FormatKnown {
		data.MouseFormat = string(mouse.Format)
	}
	return data
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
