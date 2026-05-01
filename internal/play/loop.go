package play

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"image"
	"time"

	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/render"
	"github.com/paulsmith/research/twee/internal/vt"
)

type command int

const (
	cmdPause command = iota
	cmdStep
	cmdFwd1s
	cmdRestart
	cmdQuit
)

type frameSink interface {
	Emit(img *image.RGBA, cols, rows int, toast, status string) error
}

type loop struct {
	events []Event
	cursor int
	model  vt.Model

	snapHash      []byte
	emittedToast  string
	emittedStatus string

	playT    time.Duration
	wallPrev time.Time
	speed    float64
	maxIdle  time.Duration
	paused   bool
	stepMode bool
	atEnd    bool
	toast    toast
	cmds     <-chan command
	sink     frameSink
	quitNext bool

	cols, rows         int
	initCols, initRows int
	newModel           func(int, int) vt.Model
	renderOptions      render.Options
	err                error
}

type loopConfig struct {
	Events        []Event
	Cols          int
	Rows          int
	Speed         float64
	MaxIdle       time.Duration
	Step          bool
	Cmds          <-chan command
	Sink          frameSink
	NewModel      func(int, int) vt.Model
	RenderOptions render.Options
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
		events:        append([]Event(nil), cfg.Events...),
		model:         cfg.NewModel(cfg.Cols, cfg.Rows),
		speed:         cfg.Speed,
		maxIdle:       cfg.MaxIdle,
		paused:        cfg.Step,
		stepMode:      cfg.Step,
		cmds:          cfg.Cmds,
		sink:          cfg.Sink,
		cols:          cfg.Cols,
		rows:          cfg.Rows,
		initCols:      cfg.Cols,
		initRows:      cfg.Rows,
		newModel:      cfg.NewModel,
		renderOptions: cfg.RenderOptions,
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
		dt := now.Sub(l.wallPrev)
		if dt < 0 {
			dt = 0
		}
		l.wallPrev = now
		if l.cursor < len(l.events) && l.maxIdle > 0 {
			gap := l.events[l.cursor].traceTime() - l.playT
			if gap > l.maxIdle {
				l.playT = l.events[l.cursor].traceTime() - l.maxIdle
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
					l.playT = ev.traceTime()
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
				l.paused = false
				l.stepMode = false
				l.atEnd = false
				l.toast = toast{}
				l.snapHash = nil
				l.cols, l.rows = l.initCols, l.initRows
				skipAdvance = true
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
	for l.cursor < len(l.events) && l.events[l.cursor].traceTime() <= l.playT {
		l.dispatch(l.events[l.cursor], now)
		l.cursor++
	}
}

func (l *loop) dispatch(ev Event, now time.Time) {
	switch ev.Type {
	case "output":
		if err := l.model.Feed(ev.Bytes); err != nil && l.err == nil {
			l.err = err
		}
	case "resize":
		if ev.Cols > 0 && ev.Rows > 0 {
			if err := l.model.Resize(ev.Cols, ev.Rows); err != nil && l.err == nil {
				l.err = err
			}
			l.cols, l.rows = ev.Cols, ev.Rows
		}
		l.setToast(ev)
	case "input":
		l.setToast(ev)
	case "exit":
		// Exit records are metadata in playback v0.
	}
}

func (l *loop) setToast(ev Event) {
	text := formatEventToast(ev)
	if text == "" {
		return
	}
	l.toast = toast{text: text}
}

func (l *loop) emitFrame(time.Time) {
	if l.sink == nil || l.err != nil {
		return
	}
	snap := l.model.Snapshot()
	hash := hashSnapshot(snap)
	toastText := l.toast.text
	status := l.status()
	if bytes.Equal(hash, l.snapHash) && toastText == l.emittedToast && status == l.emittedStatus {
		return
	}
	img, err := render.Render(engineSnapshot(snap), l.renderOptions)
	if err != nil {
		l.err = err
		return
	}
	if err := l.sink.Emit(img, l.cols, l.rows, toastText, status); err != nil {
		l.err = err
		return
	}
	l.snapHash = hash
	l.emittedToast = toastText
	l.emittedStatus = status
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

func engineSnapshot(s vt.Snapshot) engine.Snapshot {
	out := engine.Snapshot{
		Cols:      s.Size.Cols,
		Rows:      s.Size.Rows,
		Cursor:    engine.Cursor{Col: s.Cursor.Col, Row: s.Cursor.Row, Visible: s.Cursor.Visible},
		AltScreen: s.AltScreen,
		Lines:     make([]engine.Line, len(s.Lines)),
	}
	for i, ln := range s.Lines {
		cells := make([]engine.Cell, len(ln.Cells))
		for j, c := range ln.Cells {
			cells[j] = engine.Cell{
				Text: c.Text, Width: c.Width,
				Fg: fromVTColor(c.Fg), Bg: fromVTColor(c.Bg),
				Bold: c.Bold, Dim: c.Dim, Underline: c.Underline, Inverse: c.Inverse,
			}
		}
		out.Lines[i] = engine.Line{Cells: cells}
	}
	return out
}

func fromVTColor(c vt.Color) engine.Color {
	out := engine.Color{Index: c.Index, R: c.R, G: c.G, B: c.B}
	switch c.Kind {
	case vt.ColorPalette:
		out.Kind = engine.ColorPalette
	case vt.ColorRGB:
		out.Kind = engine.ColorRGB
	default:
		out.Kind = engine.ColorDefault
	}
	return out
}
