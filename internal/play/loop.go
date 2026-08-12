package play

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"image"
	"math"
	"time"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/render"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/vt"
)

type command int

const (
	cmdPause command = iota
	cmdStep
	cmdFwd1s
	cmdRestart
	cmdToggleStatus
	cmdQuit
)

type frameSink interface {
	Emit(img *image.RGBA, cols, rows int, toast, status string, statusVisible bool) error
}

type terminalResizeSink interface {
	SetTerminalSize(cols, rows int)
}

type loop struct {
	events []Event
	cursor int
	model  vt.Model

	snapHash               []byte
	emittedToast           string
	emittedStatus          string
	emittedStatusVisible   bool
	emittedMouseFrame      int
	emittedMouseGeneration uint64

	playT                   time.Duration
	wallPrev                time.Time
	speed                   float64
	maxIdle                 time.Duration
	paused                  bool
	stepMode                bool
	atEnd                   bool
	statusVisible           bool
	toast                   toast
	mouse                   *activeMouseAnnotation
	mouseGeneration         uint64
	disableMouseAnnotations bool
	cmds                    <-chan command
	sink                    frameSink
	quitNext                bool

	cols, rows         int
	initCols, initRows int
	initStep           bool
	newModel           func(int, int) vt.Model
	renderOptions      render.Options
	displayPixels      displayPixels
	terminalSize       terminalSize
	err                error
}

type displayPixels struct {
	Width  int
	Height int
}

type terminalSize struct {
	Cols int
	Rows int
}

type loopConfig struct {
	Events                  []Event
	Cols                    int
	Rows                    int
	Speed                   float64
	MaxIdle                 time.Duration
	Step                    bool
	HideStatus              bool
	Cmds                    <-chan command
	Sink                    frameSink
	NewModel                func(int, int) vt.Model
	RenderOptions           render.Options
	DisplayPixels           displayPixels
	TerminalSize            terminalSize
	DisableMouseAnnotations bool
}

func newLoop(cfg loopConfig) *loop {
	if cfg.Cols <= 0 {
		cfg.Cols = 80
	}
	if cfg.Rows <= 0 {
		cfg.Rows = 24
	}
	if cfg.Speed == 0 {
		cfg.Speed = 1
	}
	if cfg.NewModel == nil {
		cfg.NewModel = vt.New
	}
	return &loop{
		events:                  append([]Event(nil), cfg.Events...),
		model:                   cfg.NewModel(cfg.Cols, cfg.Rows),
		speed:                   cfg.Speed,
		maxIdle:                 cfg.MaxIdle,
		paused:                  cfg.Step,
		stepMode:                cfg.Step,
		statusVisible:           !cfg.HideStatus,
		cmds:                    cfg.Cmds,
		sink:                    cfg.Sink,
		cols:                    cfg.Cols,
		rows:                    cfg.Rows,
		initCols:                cfg.Cols,
		initRows:                cfg.Rows,
		initStep:                cfg.Step,
		newModel:                cfg.NewModel,
		renderOptions:           cfg.RenderOptions,
		displayPixels:           cfg.DisplayPixels,
		terminalSize:            cfg.TerminalSize,
		disableMouseAnnotations: cfg.DisableMouseAnnotations,
		emittedMouseFrame:       -2,
	}
}

func (l *loop) tick(now time.Time) (done bool) {
	if l.err != nil {
		return true
	}
	if l.wallPrev.IsZero() {
		l.wallPrev = now
	}
	skipAdvance, commandDispatch, done := l.drainCommands(now)
	if done {
		return true
	}

	dispatchReady := commandDispatch
	if !skipAdvance && !l.paused && !l.atEnd {
		dt := max(now.Sub(l.wallPrev), 0)
		l.wallPrev = now
		if l.cursor < len(l.events) && l.maxIdle > 0 {
			gap := l.events[l.cursor].TraceTime() - l.playT
			if gap > l.maxIdle {
				l.playT = l.events[l.cursor].TraceTime() - l.maxIdle
			}
		}
		l.playT += time.Duration(float64(dt) * l.speed)
		dispatchReady = true
	}

	if dispatchReady {
		l.dispatchReady(now)
	}
	if l.cursor == len(l.events) {
		l.atEnd = true
	}
	l.emitFrame(now)
	return l.err != nil || l.quitNext
}

func (l *loop) drainCommands(now time.Time) (skipAdvance, dispatchReady, done bool) {
	for {
		select {
		case cmd, ok := <-l.cmds:
			if !ok {
				l.cmds = nil
				l.quitNext = true
				return skipAdvance, dispatchReady, false
			}
			switch cmd {
			case cmdPause:
				l.paused = !l.paused
				l.stepMode = false
				l.wallPrev = now
			case cmdStep:
				l.paused = true
				l.stepMode = true
				if l.cursor < len(l.events) {
					ev := l.events[l.cursor]
					l.playT = ev.TraceTime()
					l.dispatch(ev, now)
					l.cursor++
				}
			case cmdFwd1s:
				if !l.atEnd {
					l.playT += time.Second
					dispatchReady = true
				}
			case cmdRestart:
				l.model = l.newModel(l.initCols, l.initRows)
				l.cursor = 0
				l.playT = 0
				l.wallPrev = now
				l.paused = l.initStep
				l.stepMode = l.initStep
				l.atEnd = false
				l.toast = toast{}
				l.snapHash = nil
				l.mouse = nil
				l.emittedMouseFrame = -2
				l.cols, l.rows = l.initCols, l.initRows
				skipAdvance = true
			case cmdToggleStatus:
				l.statusVisible = !l.statusVisible
				l.snapHash = nil
			case cmdQuit:
				return skipAdvance, dispatchReady, true
			}
			if l.cursor == len(l.events) {
				l.atEnd = true
			}
		default:
			return skipAdvance, dispatchReady, false
		}
	}
}

func (l *loop) dispatchReady(now time.Time) {
	for l.cursor < len(l.events) && l.events[l.cursor].TraceTime() <= l.playT {
		l.dispatch(l.events[l.cursor], now)
		l.cursor++
	}
}

func (l *loop) dispatch(ev Event, now time.Time) {
	screenEvent, err := ApplyScreenEvent(l.model, ev)
	if err != nil && l.err == nil {
		l.err = err
	}

	switch ev.Type {
	case trace.EventTypeResize:
		if screenEvent {
			l.cols, l.rows = ev.Cols, ev.Rows
			// A coordinate belongs to the viewport that existed when it
			// was recorded. Do not carry it across a later resize.
			l.mouse = nil
		}
		l.setToast(ev)
	case trace.EventTypeInput:
		l.setToast(ev)
		if ev.Kind == trace.InputKindMouse && !l.disableMouseAnnotations && validMouseAnnotation(ev.Mouse, l.cols, l.rows) {
			l.mouse = &activeMouseAnnotation{mouse: cloneMouseInput(ev.Mouse), started: now}
			l.mouseGeneration++
		}
	case trace.EventTypeExit:
		// Exit records are metadata in playback v0.
	}
}

func (l *loop) setToast(ev Event) {
	text := FormatEventToast(ev)
	if text == "" {
		return
	}
	l.toast = toast{text: text}
}

func (l *loop) emitFrame(now time.Time) {
	if l.sink == nil || l.err != nil {
		return
	}
	snap := l.model.Snapshot()
	hash := hashSnapshot(snap)
	toastText := l.toast.text
	status := l.status()
	mouse, phase, mouseFrame, mouseGeneration := l.mouseFrame(now)
	if bytes.Equal(hash, l.snapHash) && toastText == l.emittedToast && status == l.emittedStatus &&
		l.statusVisible == l.emittedStatusVisible && mouseFrame == l.emittedMouseFrame &&
		mouseGeneration == l.emittedMouseGeneration {
		return
	}
	placement := l.placementForSnapshot(snap)
	es := EngineSnapshot(snap)
	if l.atEnd {
		overlayEndScreen(&es)
	}
	img, err := render.Render(es, l.renderOptionsForPlacement(placement))
	if err != nil {
		l.err = err
		return
	}
	if mouse != nil {
		// Metadata was validated when dispatched; retaining this guard makes
		// frame emission safe if a future event source bypasses that path.
		_ = drawMouseAnnotation(img, mouse, snap.Size.Cols, snap.Size.Rows, phase)
	}
	if err := l.sink.Emit(img, placement.Cols, placement.Rows, toastText, status, l.statusVisible); err != nil {
		l.err = err
		return
	}
	l.snapHash = hash
	l.emittedToast = toastText
	l.emittedStatus = status
	l.emittedStatusVisible = l.statusVisible
	l.emittedMouseFrame = mouseFrame
	l.emittedMouseGeneration = mouseGeneration
}

func (l *loop) resizeViewport(size terminalSize, pixels displayPixels) {
	if size.Cols < 1 || size.Rows < 2 {
		return
	}
	if pixels.Width <= 0 || pixels.Height <= 0 {
		pixels = scaleDisplayPixels(l.displayPixels, l.terminalSize, size)
	}
	if size == l.terminalSize && pixels == l.displayPixels {
		return
	}
	l.terminalSize = size
	l.displayPixels = pixels
	l.snapHash = nil
	if sink, ok := l.sink.(terminalResizeSink); ok {
		sink.SetTerminalSize(size.Cols, size.Rows)
	}
}

func scaleDisplayPixels(pixels displayPixels, oldSize, newSize terminalSize) displayPixels {
	if pixels.Width <= 0 || pixels.Height <= 0 ||
		oldSize.Cols <= 0 || oldSize.Rows <= 0 ||
		newSize.Cols <= 0 || newSize.Rows <= 0 {
		return pixels
	}
	return displayPixels{
		Width:  scaledPixels(pixels.Width, newSize.Cols, oldSize.Cols),
		Height: scaledPixels(pixels.Height, newSize.Rows, oldSize.Rows),
	}
}

func (l *loop) mouseFrame(now time.Time) (*trace.MouseInput, float64, int, uint64) {
	if l.mouse == nil {
		return nil, 0, -1, l.mouseGeneration
	}
	elapsed := max(now.Sub(l.mouse.started), 0)
	if elapsed >= mouseAnnotationDuration {
		l.mouse = nil
		return nil, 0, -1, l.mouseGeneration
	}
	return l.mouse.mouse, float64(elapsed) / float64(mouseAnnotationDuration), int(elapsed / mouseAnnotationFramePeriod), l.mouseGeneration
}

func (l *loop) placementForSnapshot(snap vt.Snapshot) terminalSize {
	frame := terminalSize{Cols: snap.Size.Cols, Rows: snap.Size.Rows}
	if frame.Cols <= 0 {
		frame.Cols = l.cols
	}
	if frame.Rows <= 0 {
		frame.Rows = l.rows
	}
	if frame.Cols <= 0 || frame.Rows <= 0 ||
		l.terminalSize.Cols <= 0 || l.terminalSize.Rows <= 0 {
		return frame
	}
	availableRows := l.terminalSize.Rows
	if l.statusVisible {
		availableRows--
	}
	return fitFrameCells(frame, terminalSize{
		Cols: l.terminalSize.Cols,
		Rows: availableRows,
	})
}

func fitFrameCells(frame, available terminalSize) terminalSize {
	if frame.Cols <= 0 || frame.Rows <= 0 ||
		available.Cols <= 0 || available.Rows <= 0 {
		return frame
	}
	scaleW := float64(available.Cols) / float64(frame.Cols)
	scaleH := float64(available.Rows) / float64(frame.Rows)
	scale := scaleW
	if scaleH < scale {
		scale = scaleH
	}
	cols := int(math.Round(float64(frame.Cols) * scale))
	rows := int(math.Round(float64(frame.Rows) * scale))
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	if cols > available.Cols {
		cols = available.Cols
	}
	if rows > available.Rows {
		rows = available.Rows
	}
	return terminalSize{Cols: cols, Rows: rows}
}

func (l *loop) renderOptionsForPlacement(placement terminalSize) render.Options {
	if l.displayPixels.Width <= 0 || l.displayPixels.Height <= 0 ||
		l.terminalSize.Cols <= 0 || l.terminalSize.Rows <= 0 {
		return l.renderOptions
	}
	if placement.Cols <= 0 || placement.Rows <= 0 {
		return l.renderOptions
	}
	return render.Options{
		PixelWidth:  scaledPixels(l.displayPixels.Width, placement.Cols, l.terminalSize.Cols),
		PixelHeight: scaledPixels(l.displayPixels.Height, placement.Rows, l.terminalSize.Rows),
	}
}

func scaledPixels(displayPixels, placementCells, terminalCells int) int {
	if displayPixels <= 0 || placementCells <= 0 || terminalCells <= 0 {
		return 0
	}
	n := int(math.Round(float64(displayPixels) * float64(placementCells) / float64(terminalCells)))
	if n < 1 {
		return 1
	}
	return n
}

func (l *loop) status() string {
	mode := "playing"
	switch {
	case l.atEnd:
		mode = "at end"
	case l.paused && l.stepMode:
		mode = "step"
	case l.paused:
		mode = "paused"
	}
	return formatStatus(mode, l.speed, l.cursor, len(l.events))
}

func hashSnapshot(s vt.Snapshot) []byte {
	b, _ := json.Marshal(s)
	sum := sha256.Sum256(b)
	return sum[:]
}

// EngineSnapshot converts a VT snapshot to the renderer's input type,
// used by both "twee play" (this package) and "twee export"
// (internal/export/canvas.go calls this function directly). It used to
// carry its own copy of this conversion, independent from engine's
// live-session equivalent (engine.Term.Snapshot), and had fallen behind:
// it cast vt's ColorKind numerically instead of switching on it —
// vt.ColorPalette and engine.ColorIndexed are both iota 1, so every
// real 256-color palette entry silently rendered through the 16-color
// ANSI table instead — and it dropped the Italic and Strikethrough cell
// attributes entirely. engine.FromVT (internal/engine/types.go) got the
// same fix first for the live-session path (commit de4ff93); this now
// just delegates to it so the two paths can't drift apart again.
func EngineSnapshot(s vt.Snapshot) engine.Snapshot {
	return engine.FromVT(s)
}
