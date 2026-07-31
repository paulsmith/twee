package vt

import (
	"fmt"
	"runtime"
	"strings"

	libghostty "github.com/mitchellh/go-libghostty"
	"github.com/paulsmith/twee/internal/input"
)

// ghosttyTerm wraps a libghostty Terminal plus reusable render-state
// scaffolding. libghostty's terminal is not safe for concurrent use; the
// pump's mutex is the single serialization point.
type ghosttyTerm struct {
	t  *libghostty.Terminal
	rs *libghostty.RenderState
	ri *libghostty.RenderStateRowIterator
	rc *libghostty.RenderStateRowCells

	mouseEncoder *libghostty.MouseEncoder
	mouseEvent   *libghostty.MouseEvent
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
	mouseEncoder, err := libghostty.NewMouseEncoder()
	if err != nil {
		rc.Close()
		ri.Close()
		rs.Close()
		t.Close()
		panic(fmt.Errorf("vt: NewMouseEncoder: %w", err))
	}
	mouseEvent, err := libghostty.NewMouseEvent()
	if err != nil {
		mouseEncoder.Close()
		rc.Close()
		ri.Close()
		rs.Close()
		t.Close()
		panic(fmt.Errorf("vt: NewMouseEvent: %w", err))
	}
	g := &ghosttyTerm{
		t: t, rs: rs, ri: ri, rc: rc,
		mouseEncoder: mouseEncoder, mouseEvent: mouseEvent,
	}
	// libghostty owns C-side allocations; release them on GC. Tests create
	// many short-lived models — without a finalizer the cgo footprint grows
	// for the duration of the process.
	runtime.SetFinalizer(g, (*ghosttyTerm).finalize)
	return g
}

func (g *ghosttyTerm) finalize() {
	g.mouseEvent.Close()
	g.mouseEncoder.Close()
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

func (g *ghosttyTerm) MouseState() (MouseState, error) {
	enabled, err := g.t.MouseTracking()
	if err != nil {
		return MouseState{}, &MouseEncodeError{Reason: MouseErrorState, Err: err}
	}

	raw := MouseRawModes{}
	for _, entry := range []struct {
		mode libghostty.Mode
		dst  *bool
	}{
		{libghostty.ModeX10Mouse, &raw.TrackingX10},
		{libghostty.ModeNormalMouse, &raw.TrackingNormal},
		{libghostty.ModeButtonMouse, &raw.TrackingButton},
		{libghostty.ModeAnyMouse, &raw.TrackingAny},
		{libghostty.ModeUTF8Mouse, &raw.FormatUTF8},
		{libghostty.ModeSGRMouse, &raw.FormatSGR},
		{libghostty.ModeURxvtMouse, &raw.FormatURxvt},
		{libghostty.ModeSGRPixelsMouse, &raw.FormatSGRPixels},
	} {
		value, modeErr := g.t.ModeGet(entry.mode)
		if modeErr != nil {
			return MouseState{}, &MouseEncodeError{Reason: MouseErrorState, Err: modeErr}
		}
		*entry.dst = value
	}

	state := MouseState{Enabled: enabled, Raw: raw}
	tracking, trackingCount := trackingCandidateFromRaw(raw)
	switch trackingCount {
	case 0:
		state.Tracking = MouseTrackingNone
		state.TrackingKnown = true
		state.TrackingCandidate = MouseTrackingNone
	case 1:
		state.TrackingCandidate = tracking
	}
	format, formatCount := formatCandidateFromRaw(raw)
	switch formatCount {
	case 0:
		state.Format = MouseFormatX10
		state.FormatKnown = true
		state.FormatCandidate = MouseFormatX10
	case 1:
		state.FormatCandidate = format
	}
	return state, nil
}

func trackingCandidateFromRaw(raw MouseRawModes) (MouseTracking, int) {
	var tracking MouseTracking
	count := 0
	for _, candidate := range []struct {
		enabled  bool
		tracking MouseTracking
	}{
		{raw.TrackingX10, MouseTrackingX10},
		{raw.TrackingNormal, MouseTrackingNormal},
		{raw.TrackingButton, MouseTrackingButton},
		{raw.TrackingAny, MouseTrackingAny},
	} {
		if candidate.enabled {
			tracking = candidate.tracking
			count++
		}
	}
	if count == 0 {
		return MouseTrackingNone, 0
	}
	if count > 1 {
		return "", count
	}
	return tracking, 1
}

func formatCandidateFromRaw(raw MouseRawModes) (MouseFormat, int) {
	var format MouseFormat
	count := 0
	for _, candidate := range []struct {
		enabled bool
		format  MouseFormat
	}{
		{raw.FormatUTF8, MouseFormatUTF8},
		{raw.FormatSGR, MouseFormatSGR},
		{raw.FormatURxvt, MouseFormatURxvt},
		{raw.FormatSGRPixels, MouseFormatSGRPixels},
	} {
		if candidate.enabled {
			format = candidate.format
			count++
		}
	}
	if count == 0 {
		return MouseFormatX10, 0
	}
	if count > 1 {
		return "", count
	}
	return format, 1
}

func (g *ghosttyTerm) EncodeMouse(events []input.MouseEvent) (MouseEncodingResult, error) {
	cols, err := g.t.Cols()
	if err != nil {
		return MouseEncodingResult{}, &MouseEncodeError{Reason: MouseErrorState, Err: err}
	}
	rows, err := g.t.Rows()
	if err != nil {
		return MouseEncodingResult{}, &MouseEncodeError{Reason: MouseErrorState, Err: err}
	}
	size := Size{Cols: int(cols), Rows: int(rows)}

	gesture, batchErr := validateMouseBatch(events, size)
	if batchErr != nil {
		return MouseEncodingResult{}, batchErr
	}

	state, err := g.MouseState()
	if err != nil {
		return MouseEncodingResult{}, err
	}
	tracking, trackingCount := trackingCandidateFromRaw(state.Raw)
	format, formatCount := formatCandidateFromRaw(state.Raw)
	if state.Raw.FormatSGRPixels {
		return MouseEncodingResult{}, &MouseEncodeError{
			Reason: MouseErrorSGRPixels, Gesture: gesture,
			Tracking: state.Tracking, Format: state.Format,
			TrackingCandidate: tracking, FormatCandidate: MouseFormatSGRPixels,
		}
	}
	if trackingCount == 0 {
		return MouseEncodingResult{}, &MouseEncodeError{
			Reason: MouseErrorTrackingDisabled, Gesture: gesture,
			Tracking: MouseTrackingNone, Format: state.Format,
			FormatCandidate: format,
		}
	}

	// ModeGet exposes retained DECSET bits, not Ghostty's effective scalar
	// tracking mode. Applications such as Herdr deliberately enable several
	// compatible tracking bits at once, so a raw-bit priority would either lie
	// or reject a valid session. Probe the configured encoder entirely in
	// memory instead: the four tracking modes have distinct filtering behavior
	// for press, release, pressed motion, and buttonless motion. The observation
	// is command-local and is never published by MouseState as authoritative.
	observedTracking, probeErr := g.probeMouseTracking(size)
	if probeErr != nil {
		return MouseEncodingResult{}, &MouseEncodeError{
			Reason: MouseErrorEncoding, Gesture: gesture, Event: -1,
			Tracking: state.Tracking, Format: state.Format,
			TrackingCandidate: tracking, FormatCandidate: format, Err: probeErr,
		}
	}
	if observedTracking == MouseTrackingNone {
		return MouseEncodingResult{}, &MouseEncodeError{
			Reason: MouseErrorTrackingDisabled, Gesture: gesture,
			Tracking: observedTracking, Format: state.Format,
			TrackingCandidate: tracking, FormatCandidate: format,
		}
	}

	required := requiredTracking(gesture)
	if !containsTracking(required, observedTracking) {
		return MouseEncodingResult{}, &MouseEncodeError{
			Reason: MouseErrorIncompatible, Gesture: gesture,
			Tracking: observedTracking, Format: state.Format, Required: required,
			TrackingCandidate: tracking, FormatCandidate: format,
		}
	}
	if observedTracking == MouseTrackingX10 && batchHasModifiers(events) {
		return MouseEncodingResult{}, &MouseEncodeError{
			Reason: MouseErrorX10Modifiers, Gesture: gesture,
			Tracking: observedTracking, Format: state.Format,
			TrackingCandidate: tracking, FormatCandidate: format,
		}
	}
	if formatCount == 0 {
		for i, event := range events {
			if event.X > 222 || event.Y > 222 {
				return MouseEncodingResult{}, &MouseEncodeError{
					Reason: MouseErrorLegacyCoordinate, Gesture: gesture, Event: i,
					X: event.X, Y: event.Y, Cols: size.Cols, Rows: size.Rows,
					Tracking: observedTracking, Format: state.Format,
					TrackingCandidate: tracking, FormatCandidate: format,
				}
			}
		}
	}

	encoder := g.mouseEncoder
	eventHandle := g.mouseEvent
	encoder.Reset()
	encoder.SetOptFromTerminal(g.t)
	encoder.SetOptSize(libghostty.MouseEncoderSize{
		ScreenWidth: uint32(size.Cols), ScreenHeight: uint32(size.Rows),
		CellWidth: 1, CellHeight: 1,
	})
	encoder.SetOptTrackLastCell(true)
	encoder.SetOptAnyButtonPressed(false)
	eventHandle.ClearButton()
	defer func() {
		encoder.SetOptAnyButtonPressed(false)
		encoder.Reset()
		eventHandle.ClearButton()
	}()

	result := MouseEncodingResult{
		Events: make([]MouseEventEncoding, len(events)),
		State:  state,
		Size:   size,
	}
	for i, event := range events {
		eventHandle.SetAction(libghosttyAction(event.Action))
		if event.Button == input.ButtonNone {
			eventHandle.ClearButton()
		} else {
			eventHandle.SetButton(libghosttyButton(event.Button))
		}
		eventHandle.SetMods(libghosttyModifiers(event.Modifiers))
		eventHandle.SetPosition(libghostty.MousePosition{
			X: float32(event.X) + 0.5,
			Y: float32(event.Y) + 0.5,
		})

		pressedDrag := gesture == input.MouseGestureDrag &&
			(event.Action == input.MouseActionPress || event.Action == input.MouseActionMotion)
		encoder.SetOptAnyButtonPressed(pressedDrag)

		report, encodeErr := encoder.Encode(eventHandle)
		if encodeErr != nil {
			return MouseEncodingResult{}, &MouseEncodeError{
				Reason: MouseErrorEncoding, Gesture: gesture, Event: i,
				Tracking: observedTracking, Format: state.Format,
				TrackingCandidate: tracking, FormatCandidate: format, Err: encodeErr,
			}
		}
		produced := len(report) != 0
		expected := expectedReport(observedTracking, event)
		if expected && !produced {
			return MouseEncodingResult{}, &MouseEncodeError{
				Reason: MouseErrorMissingReport, Gesture: gesture, Event: i,
				Tracking: observedTracking, Format: state.Format,
				TrackingCandidate: tracking, FormatCandidate: format,
			}
		}
		if !expected && produced {
			return MouseEncodingResult{}, &MouseEncodeError{
				Reason: MouseErrorUnexpectedReport, Gesture: gesture, Event: i,
				Tracking: observedTracking, Format: state.Format,
				TrackingCandidate: tracking, FormatCandidate: format,
			}
		}
		result.Events[i] = MouseEventEncoding{Produced: produced, Bytes: report}
		if produced {
			result.ReportCount++
			result.Bytes = append(result.Bytes, report...)
		}
	}
	return result, nil
}

// probeMouseTracking observes the effective tracking mode copied by
// SetOptFromTerminal without exposing it as terminal state or producing PTY
// input. Ghostty's modes have distinct report filters:
//
//   - none: even a press is filtered;
//   - x10: release and motion are filtered;
//   - normal: release is reported, motion is filtered;
//   - button: pressed motion is reported, buttonless motion is filtered;
//   - any: buttonless motion is reported.
//
// Each probe starts from a fresh encoder state so last-cell deduplication and
// button state cannot influence the observation.
func (g *ghosttyTerm) probeMouseTracking(size Size) (MouseTracking, error) {
	probe := func(
		action libghostty.MouseAction,
		button *libghostty.MouseButton,
		anyButtonPressed bool,
	) (bool, error) {
		encoder := g.mouseEncoder
		event := g.mouseEvent
		encoder.Reset()
		encoder.SetOptFromTerminal(g.t)
		encoder.SetOptSize(libghostty.MouseEncoderSize{
			ScreenWidth: uint32(size.Cols), ScreenHeight: uint32(size.Rows),
			CellWidth: 1, CellHeight: 1,
		})
		encoder.SetOptTrackLastCell(false)
		encoder.SetOptAnyButtonPressed(anyButtonPressed)
		event.SetAction(action)
		if button == nil {
			event.ClearButton()
		} else {
			event.SetButton(*button)
		}
		event.SetMods(0)
		event.SetPosition(libghostty.MousePosition{X: 0.5, Y: 0.5})
		report, err := encoder.Encode(event)
		encoder.SetOptAnyButtonPressed(false)
		encoder.Reset()
		event.ClearButton()
		return len(report) != 0, err
	}

	left := libghostty.MouseButtonLeft
	press, err := probe(libghostty.MouseActionPress, &left, false)
	if err != nil {
		return "", err
	}
	if !press {
		return MouseTrackingNone, nil
	}
	buttonlessMotion, err := probe(libghostty.MouseActionMotion, nil, false)
	if err != nil {
		return "", err
	}
	if buttonlessMotion {
		return MouseTrackingAny, nil
	}
	pressedMotion, err := probe(libghostty.MouseActionMotion, &left, true)
	if err != nil {
		return "", err
	}
	if pressedMotion {
		return MouseTrackingButton, nil
	}
	release, err := probe(libghostty.MouseActionRelease, &left, false)
	if err != nil {
		return "", err
	}
	if release {
		return MouseTrackingNormal, nil
	}
	return MouseTrackingX10, nil
}

func validateMouseBatch(events []input.MouseEvent, size Size) (input.MouseGestureKind, *MouseEncodeError) {
	if len(events) == 0 {
		return input.MouseGestureNone, &MouseEncodeError{
			Reason: MouseErrorInvalidBatch, Event: -1,
		}
	}
	gesture := events[0].Gesture
	for i, event := range events {
		if event.Gesture != gesture || !event.Modifiers.Valid() {
			return gesture, invalidBatch(gesture, i)
		}
		if event.X < 0 || event.Y < 0 || event.X >= size.Cols || event.Y >= size.Rows {
			return gesture, &MouseEncodeError{
				Reason: MouseErrorInvalidCoordinate, Gesture: gesture, Event: i,
				X: event.X, Y: event.Y, Cols: size.Cols, Rows: size.Rows,
			}
		}
	}

	switch gesture {
	case input.MouseGestureClick:
		if len(events) != 2 ||
			events[0].Action != input.MouseActionPress ||
			events[1].Action != input.MouseActionRelease ||
			!regularButton(events[0].Button) ||
			!sameMouseState(events[0], events[1]) ||
			events[0].X != events[1].X || events[0].Y != events[1].Y {
			return gesture, invalidBatch(gesture, -1)
		}

	case input.MouseGestureHover:
		if len(events) != 1 || events[0].Action != input.MouseActionMotion ||
			events[0].Button != input.ButtonNone {
			return gesture, invalidBatch(gesture, -1)
		}

	case input.MouseGestureScroll:
		if len(events) > input.MaxScrollTicks {
			return gesture, invalidBatch(gesture, -1)
		}
		first := events[0]
		if first.Action != input.MouseActionPress ||
			(first.Button != input.ButtonWheelUp && first.Button != input.ButtonWheelDown) {
			return gesture, invalidBatch(gesture, 0)
		}
		for i, event := range events[1:] {
			if event.Action != input.MouseActionPress || !sameMouseState(first, event) ||
				event.X != first.X || event.Y != first.Y {
				return gesture, invalidBatch(gesture, i+1)
			}
		}

	case input.MouseGestureDrag:
		if len(events) < 3 || events[0].Action != input.MouseActionPress ||
			events[len(events)-1].Action != input.MouseActionRelease ||
			!regularButton(events[0].Button) {
			return gesture, invalidBatch(gesture, -1)
		}
		first := events[0]
		for i, event := range events[1 : len(events)-1] {
			if event.Action != input.MouseActionMotion || !sameMouseState(first, event) {
				return gesture, invalidBatch(gesture, i+1)
			}
		}
		lastMotion := events[len(events)-2]
		release := events[len(events)-1]
		if !sameMouseState(first, release) ||
			lastMotion.X != release.X || lastMotion.Y != release.Y {
			return gesture, invalidBatch(gesture, len(events)-1)
		}

	default:
		return gesture, invalidBatch(gesture, -1)
	}
	return gesture, nil
}

func invalidBatch(gesture input.MouseGestureKind, event int) *MouseEncodeError {
	return &MouseEncodeError{Reason: MouseErrorInvalidBatch, Gesture: gesture, Event: event}
}

func sameMouseState(a, b input.MouseEvent) bool {
	return a.Button == b.Button && a.Modifiers == b.Modifiers
}

func regularButton(button input.MouseButton) bool {
	return button == input.ButtonLeft || button == input.ButtonMiddle || button == input.ButtonRight
}

func requiredTracking(gesture input.MouseGestureKind) []MouseTracking {
	switch gesture {
	case input.MouseGestureClick:
		return []MouseTracking{
			MouseTrackingX10, MouseTrackingNormal, MouseTrackingButton, MouseTrackingAny,
		}
	case input.MouseGestureHover:
		return []MouseTracking{MouseTrackingAny}
	case input.MouseGestureScroll:
		return []MouseTracking{MouseTrackingNormal, MouseTrackingButton, MouseTrackingAny}
	case input.MouseGestureDrag:
		return []MouseTracking{MouseTrackingButton, MouseTrackingAny}
	default:
		return nil
	}
}

func containsTracking(modes []MouseTracking, mode MouseTracking) bool {
	for _, candidate := range modes {
		if candidate == mode {
			return true
		}
	}
	return false
}

func batchHasModifiers(events []input.MouseEvent) bool {
	for _, event := range events {
		if event.Modifiers != input.MouseModifiersNone {
			return true
		}
	}
	return false
}

func expectedReport(tracking MouseTracking, event input.MouseEvent) bool {
	return tracking != MouseTrackingX10 || event.Action == input.MouseActionPress
}

func libghosttyAction(action input.MouseAction) libghostty.MouseAction {
	switch action {
	case input.MouseActionRelease:
		return libghostty.MouseActionRelease
	case input.MouseActionMotion:
		return libghostty.MouseActionMotion
	default:
		return libghostty.MouseActionPress
	}
}

func libghosttyButton(button input.MouseButton) libghostty.MouseButton {
	switch button {
	case input.ButtonMiddle:
		return libghostty.MouseButtonMiddle
	case input.ButtonRight:
		return libghostty.MouseButtonRight
	case input.ButtonWheelUp:
		return libghostty.MouseButtonFour
	case input.ButtonWheelDown:
		return libghostty.MouseButtonFive
	default:
		return libghostty.MouseButtonLeft
	}
}

func libghosttyModifiers(modifiers input.MouseModifiers) libghostty.Mods {
	var result libghostty.Mods
	if modifiers&input.MouseModifiersShift != 0 {
		result |= libghostty.ModShift
	}
	if modifiers&input.MouseModifiersAlt != 0 {
		result |= libghostty.ModAlt
	}
	if modifiers&input.MouseModifiersCtrl != 0 {
		result |= libghostty.ModCtrl
	}
	return result
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
