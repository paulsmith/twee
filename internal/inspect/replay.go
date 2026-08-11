package inspect

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/paulsmith/twee/internal/rpc"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/tracebundle"
	"github.com/paulsmith/twee/internal/tracepolicy"
	"github.com/paulsmith/twee/internal/vt"
)

// ReplaySummary is the semantic terminal state reconstructed from trace events.
type ReplaySummary struct {
	InitialModes    rpc.ModeData     `json:"initial_modes"`
	Final           ReplayFinal      `json:"final"`
	ModeTransitions []ModeTransition `json:"mode_transitions"`
}

// ReplayFinal is the terminal state after every output and resize event.
type ReplayFinal struct {
	EventIndex  *int           `json:"event_index"`
	TMS         *int64         `json:"t_ms"`
	Size        rpc.SizeData   `json:"size"`
	VisibleText string         `json:"visible_text"`
	Cursor      rpc.CursorData `json:"cursor"`
	AltScreen   bool           `json:"alt_screen"`
	Modes       rpc.ModeData   `json:"modes"`
	StyledCells int            `json:"styled_cells"`
	Lines       []ReplayLine   `json:"lines"`
}

// ReplayLine is one physical row in the final semantic snapshot. Runs retain
// every physical cell while compacting adjacent identical cells.
type ReplayLine struct {
	Runs []ReplayCellRun `json:"runs"`
}

// ReplayCellRun is a run of identical adjacent physical cells.
type ReplayCellRun struct {
	Count int          `json:"count"`
	Cell  rpc.CellData `json:"cell"`
}

// ModeTransition is one observed change after a completed control sequence.
type ModeTransition struct {
	TMS        int64        `json:"t_ms"`
	EventIndex int          `json:"event_index"`
	ByteOffset int          `json:"byte_offset"`
	Changed    []string     `json:"changed"`
	Modes      rpc.ModeData `json:"modes"`
}

// LimitError reports a valid trace whose semantic replay would exceed a
// documented resource bound.
type LimitError struct {
	Message string
}

func (e *LimitError) Error() string { return e.Message }

// Replay reconstructs the final semantic terminal state and observed mode
// transitions. Input and exit events do not affect the replayed terminal.
func Replay(bundle tracebundle.Bundle) (ReplaySummary, error) {
	model := vt.New(bundle.Manifest.Cols, bundle.Manifest.Rows)
	if closer, ok := model.(vt.Closer); ok {
		defer closer.Close()
	}
	modeSource, ok := model.(vt.ModeStateSource)
	if !ok {
		return ReplaySummary{}, fmt.Errorf("inspect replay: terminal model does not expose mode state")
	}

	initialState, err := modeSource.ModeState()
	if err != nil {
		return ReplaySummary{}, fmt.Errorf("inspect replay: initial modes: %w", err)
	}
	previousModes := replayModeData(initialState)
	result := ReplaySummary{
		InitialModes:    previousModes,
		ModeTransitions: []ModeTransition{},
	}

	scanner := controlSequenceScanner{}
	var finalEvent *int
	var finalTMS *int64
	for eventIndex, event := range bundle.Events {
		switch event.Type {
		case trace.EventTypeOutput:
			if len(event.Bytes) == 0 {
				continue
			}
			start := 0
			for byteOffset, b := range event.Bytes {
				if !scanner.step(b) {
					continue
				}
				if err := feedReplay(model, event.Bytes[start:byteOffset+1]); err != nil {
					return ReplaySummary{}, fmt.Errorf("inspect replay: output event %d byte %d: %w", eventIndex, byteOffset, err)
				}
				start = byteOffset + 1
				state, err := modeSource.ModeState()
				if err != nil {
					return ReplaySummary{}, fmt.Errorf("inspect replay: modes after event %d byte %d: %w", eventIndex, byteOffset, err)
				}
				modes := replayModeData(state)
				if changed := changedModes(previousModes, modes); len(changed) > 0 {
					if len(result.ModeTransitions) >= tracepolicy.MaxModeTransitions {
						return ReplaySummary{}, &LimitError{Message: fmt.Sprintf("inspect replay: mode transition count exceeds %d", tracepolicy.MaxModeTransitions)}
					}
					result.ModeTransitions = append(result.ModeTransitions, ModeTransition{
						TMS: event.TMS, EventIndex: eventIndex, ByteOffset: byteOffset,
						Changed: changed, Modes: modes,
					})
				}
				previousModes = modes
			}
			if start < len(event.Bytes) {
				if err := feedReplay(model, event.Bytes[start:]); err != nil {
					return ReplaySummary{}, fmt.Errorf("inspect replay: output event %d: %w", eventIndex, err)
				}
			}
			index, tms := eventIndex, event.TMS
			finalEvent, finalTMS = &index, &tms
		case trace.EventTypeResize:
			if err := model.Resize(event.Cols, event.Rows); err != nil {
				return ReplaySummary{}, fmt.Errorf("inspect replay: resize event %d to %dx%d: %w", eventIndex, event.Cols, event.Rows, err)
			}
			index, tms := eventIndex, event.TMS
			finalEvent, finalTMS = &index, &tms
		}
	}

	finalState, err := modeSource.ModeState()
	if err != nil {
		return ReplaySummary{}, fmt.Errorf("inspect replay: final modes: %w", err)
	}
	finalModes := replayModeData(finalState)
	if changed := changedModes(previousModes, finalModes); len(changed) > 0 {
		return ReplaySummary{}, fmt.Errorf("inspect replay: terminal modes changed outside a completed control sequence: %s", strings.Join(changed, ", "))
	}

	snapshot := model.Snapshot()
	result.Final = replayFinal(snapshot, finalModes, finalEvent, finalTMS)
	encoded, err := json.Marshal(result)
	if err != nil {
		return ReplaySummary{}, fmt.Errorf("inspect replay: encode size check: %w", err)
	}
	if len(encoded) > tracepolicy.MaxInspectReplayBytes {
		return ReplaySummary{}, &LimitError{Message: fmt.Sprintf("inspect replay: encoded replay exceeds %d bytes", tracepolicy.MaxInspectReplayBytes)}
	}
	return result, nil
}

func feedReplay(model vt.Model, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if err := model.Feed(data); err != nil {
		return err
	}
	if replies, ok := model.(vt.PTYReplySource); ok {
		replies.DrainPTYReplies()
	}
	return nil
}

func replayFinal(snapshot vt.Snapshot, modes rpc.ModeData, eventIndex *int, tms *int64) ReplayFinal {
	lines := make([]ReplayLine, len(snapshot.Lines))
	styledCells := 0
	for y, line := range snapshot.Lines {
		runs := make([]ReplayCellRun, 0)
		var previous vt.Cell
		for x, cell := range line.Cells {
			if replayCellStyled(cell) {
				styledCells++
			}
			if x > 0 && cell == previous {
				runs[len(runs)-1].Count++
			} else {
				runs = append(runs, ReplayCellRun{Count: 1, Cell: replayCellData(cell)})
			}
			previous = cell
		}
		lines[y] = ReplayLine{Runs: runs}
	}
	return ReplayFinal{
		EventIndex:  eventIndex,
		TMS:         tms,
		Size:        rpc.SizeData{Cols: snapshot.Size.Cols, Rows: snapshot.Size.Rows},
		VisibleText: vt.VisibleText(snapshot),
		Cursor: rpc.CursorData{
			X: snapshot.Cursor.Col, Y: snapshot.Cursor.Row,
			Visible: snapshot.Cursor.Visible, Shape: replayCursorShape(snapshot.Cursor.Style),
		},
		AltScreen:   snapshot.AltScreen,
		Modes:       modes,
		StyledCells: styledCells,
		Lines:       lines,
	}
}

func replayCellData(cell vt.Cell) rpc.CellData {
	return rpc.CellData{
		Text: cell.Text, Width: cell.Width,
		Fg: replayColorData(cell.Fg), Bg: replayColorData(cell.Bg),
		Bold: cell.Bold, Dim: cell.Dim, Italic: cell.Italic,
		Underline: cell.Underline, Inverse: cell.Inverse,
		Strikethrough: cell.Strikethrough,
	}
}

func replayColorData(color vt.Color) rpc.ColorData {
	switch color.Kind {
	case vt.ColorPalette:
		index := color.Index
		return rpc.ColorData{Kind: rpc.ColorKindPalette, Index: &index}
	case vt.ColorRGB:
		return rpc.ColorData{Kind: rpc.ColorKindRGB, R: color.R, G: color.G, B: color.B}
	default:
		return rpc.ColorData{Kind: rpc.ColorKindDefault}
	}
}

func replayCellStyled(cell vt.Cell) bool {
	return cell.Fg.Kind != vt.ColorDefault || cell.Bg.Kind != vt.ColorDefault ||
		cell.Bold || cell.Dim || cell.Italic || cell.Underline || cell.Inverse || cell.Strikethrough
}

func replayCursorShape(style vt.CursorStyle) string {
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

func replayModeData(state vt.ModeState) rpc.ModeData {
	data := rpc.ModeData{
		DECCKM:             state.Input.ApplicationCursor,
		ApplicationKeypad:  state.Input.ApplicationKeypad,
		BracketedPaste:     state.Input.BracketedPaste,
		FocusEvents:        state.Input.FocusEvents,
		KittyKeyboardKnown: state.Input.KittyKeyboardKnown,
		KittyKeyboardFlags: state.Input.KittyKeyboardFlags,
		AltScreen:          state.AltScreen,
		MouseKnown:         state.Mouse.TrackingKnown,
		MouseRaw:           state.Mouse.Enabled,

		MouseTrackingX10:    state.Mouse.Raw.TrackingX10,
		MouseTrackingNormal: state.Mouse.Raw.TrackingNormal,
		MouseTrackingButton: state.Mouse.Raw.TrackingButton,
		MouseTrackingAny:    state.Mouse.Raw.TrackingAny,

		MouseFormatUTF8:      state.Mouse.Raw.FormatUTF8,
		MouseFormatSGR:       state.Mouse.Raw.FormatSGR,
		MouseFormatURxvt:     state.Mouse.Raw.FormatURxvt,
		MouseFormatSGRPixels: state.Mouse.Raw.FormatSGRPixels,
	}
	if state.Mouse.TrackingKnown {
		data.Mouse = state.Mouse.Tracking != vt.MouseTrackingNone
		data.MouseTracking = string(state.Mouse.Tracking)
	}
	if state.Mouse.FormatKnown {
		data.MouseFormat = string(state.Mouse.Format)
	}
	return data
}

func changedModes(before, after rpc.ModeData) []string {
	changed := make([]string, 0)
	for _, field := range []struct {
		name    string
		changed bool
	}{
		{"alt_screen", before.AltScreen != after.AltScreen},
		{"application_keypad", before.ApplicationKeypad != after.ApplicationKeypad},
		{"bracketed_paste", before.BracketedPaste != after.BracketedPaste},
		{"decckm", before.DECCKM != after.DECCKM},
		{"focus_events", before.FocusEvents != after.FocusEvents},
		{"kitty_keyboard_flags", before.KittyKeyboardFlags != after.KittyKeyboardFlags},
		{"kitty_keyboard_known", before.KittyKeyboardKnown != after.KittyKeyboardKnown},
		{"mouse", before.Mouse != after.Mouse},
		{"mouse_format", before.MouseFormat != after.MouseFormat},
		{"mouse_format_sgr", before.MouseFormatSGR != after.MouseFormatSGR},
		{"mouse_format_sgr_pixels", before.MouseFormatSGRPixels != after.MouseFormatSGRPixels},
		{"mouse_format_urxvt", before.MouseFormatURxvt != after.MouseFormatURxvt},
		{"mouse_format_utf8", before.MouseFormatUTF8 != after.MouseFormatUTF8},
		{"mouse_known", before.MouseKnown != after.MouseKnown},
		{"mouse_raw", before.MouseRaw != after.MouseRaw},
		{"mouse_tracking", before.MouseTracking != after.MouseTracking},
		{"mouse_tracking_any", before.MouseTrackingAny != after.MouseTrackingAny},
		{"mouse_tracking_button", before.MouseTrackingButton != after.MouseTrackingButton},
		{"mouse_tracking_normal", before.MouseTrackingNormal != after.MouseTrackingNormal},
		{"mouse_tracking_x10", before.MouseTrackingX10 != after.MouseTrackingX10},
	} {
		if field.changed {
			changed = append(changed, field.name)
		}
	}
	return changed
}

type controlState uint8

const (
	controlGround controlState = iota
	controlEscape
	controlEscapeIntermediate
	controlCSI
	controlOSC
	controlString
	controlOSCEscape
	controlStringEscape
)

type controlSequenceScanner struct {
	state controlState
}

// step reports whether b completes an ESC, CSI, OSC, DCS, SOS, PM, or APC
// sequence. It tracks framing only; libghostty remains the semantic parser.
func (s *controlSequenceScanner) step(b byte) bool {
	switch s.state {
	case controlGround:
		if b == 0x1b {
			s.state = controlEscape
		}
	case controlEscape:
		switch {
		case b == '[':
			s.state = controlCSI
		case b == ']':
			s.state = controlOSC
		case b == 'P' || b == 'X' || b == '^' || b == '_':
			s.state = controlString
		case b >= 0x20 && b <= 0x2f:
			s.state = controlEscapeIntermediate
		case b >= 0x30 && b <= 0x7e:
			s.state = controlGround
			return true
		case b == 0x1b:
			s.state = controlEscape
		case b == 0x18 || b == 0x1a:
			s.state = controlGround
		}
	case controlEscapeIntermediate:
		switch {
		case b >= 0x20 && b <= 0x2f:
		case b >= 0x30 && b <= 0x7e:
			s.state = controlGround
			return true
		case b == 0x1b:
			s.state = controlEscape
		case b == 0x18 || b == 0x1a:
			s.state = controlGround
		}
	case controlCSI:
		switch {
		case b >= 0x40 && b <= 0x7e:
			s.state = controlGround
			return true
		case b == 0x1b:
			s.state = controlEscape
		case b == 0x18 || b == 0x1a:
			s.state = controlGround
		}
	case controlOSC:
		switch b {
		case 0x07:
			s.state = controlGround
			return true
		case 0x1b:
			s.state = controlOSCEscape
		case 0x18, 0x1a:
			s.state = controlGround
		}
	case controlString:
		switch b {
		case 0x1b:
			s.state = controlStringEscape
		case 0x18, 0x1a:
			s.state = controlGround
		}
	case controlOSCEscape:
		if b == '\\' {
			s.state = controlGround
			return true
		}
		s.state = controlEscape
		return s.step(b)
	case controlStringEscape:
		if b == '\\' {
			s.state = controlGround
			return true
		}
		s.state = controlEscape
		return s.step(b)
	}
	return false
}
