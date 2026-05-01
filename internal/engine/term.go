package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/paulsmith/research/twee/internal/ptyrunner"
	"github.com/paulsmith/research/twee/internal/pump"
	"github.com/paulsmith/research/twee/internal/recording"
	"github.com/paulsmith/research/twee/internal/trace"
	"github.com/paulsmith/research/twee/internal/vt"
)

// Term is a running TUI under PTY.
type Term struct {
	cfg Config

	cfgMu  sync.Mutex
	runner *ptyrunner.Runner
	pump   *pump.Pump
	rec    *recording.Recorder
	tr     *trace.Trace

	closeOnce sync.Once
	closeErr  error
	pumpDone  chan struct{}

	startedAt time.Time

	inputsMu sync.Mutex
	inputs   []InputEvent
}

// InputEvent is a single recorded input action for diagnostics.
type InputEvent struct {
	When time.Time
	Desc string
}

// Start spawns the command in cfg under a PTY, attaches the VT model
// and pump, optionally enables recording, and returns a Term ready for
// queries and input.
func Start(ctx context.Context, cfg Config) (*Term, error) {
	cfg.applyDefaults()
	if len(cfg.Cmd) == 0 {
		return nil, errors.New("engine.Start: cfg.Cmd is empty")
	}

	runner, err := ptyrunner.Start(ctx, ptyrunner.Config{
		Command: cfg.Cmd,
		Env:     cfg.BuildEnv(),
		Dir:     cfg.Dir,
		Cols:    cfg.Cols,
		Rows:    cfg.Rows,
	})
	if err != nil {
		return nil, fmt.Errorf("spawn: %w", err)
	}

	model := vt.New(cfg.Cols, cfg.Rows)
	p := pump.New(model, runner.Master())

	var rec *recording.Recorder
	if cfg.RecordPath != "" {
		rec, err = recording.New(cfg.RecordPath, recording.Header{
			Command: cfg.Cmd,
			Cols:    cfg.Cols,
			Rows:    cfg.Rows,
			Env:     cfg.Env,
		})
		if err != nil {
			_ = runner.Close()
			return nil, fmt.Errorf("recording: %w", err)
		}
	}

	t := &Term{
		cfg:       cfg,
		runner:    runner,
		pump:      p,
		rec:       rec,
		pumpDone:  make(chan struct{}),
		startedAt: time.Now(),
	}
	if cfg.TracePath != "" {
		if err := t.EnableTrace(cfg.TracePath); err != nil {
			if rec != nil {
				_ = rec.Close()
			}
			_ = runner.Close()
			return nil, fmt.Errorf("trace: %w", err)
		}
	} else if rec != nil {
		t.updateOutputHookLocked() // safe: no other goroutines access t yet
	}
	go func() {
		_ = p.Run()
		close(t.pumpDone)
	}()
	return t, nil
}

// Close terminates the child and the pump.
func (t *Term) Close() error {
	t.closeOnce.Do(func() {
		err := t.runner.Close()
		<-t.pumpDone
		t.cfgMu.Lock()
		if t.tr != nil {
			err = errors.Join(err, t.tr.Close())
			t.tr = nil
		}
		if t.rec != nil {
			t.rec.WriteExit(t.runner.ExitCode())
			err = errors.Join(err, t.rec.Close())
			t.rec = nil
		}
		t.cfgMu.Unlock()
		t.closeErr = err
	})
	return t.closeErr
}

// Cmd returns the command line the child was spawned with.
func (t *Term) Cmd() []string { return append([]string(nil), t.cfg.Cmd...) }

// DefaultTimeout returns the configured default timeout for waits.
func (t *Term) DefaultTimeout() time.Duration { return t.cfg.DefaultTimeout }

// StableQuietWindow returns the default quiet window for WaitForStableScreen.
func (t *Term) StableQuietWindow() time.Duration { return t.cfg.StableQuietWindow }

// RecordPath returns the recording path used at Start (or "" if recording
// was not enabled).
func (t *Term) RecordPath() string {
	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	return t.cfg.RecordPath
}

// StartedAt returns the wall-clock time when Start completed.
func (t *Term) StartedAt() time.Time { return t.startedAt }

// EnableRecording starts recording to path. Replaces any current recorder.
func (t *Term) EnableRecording(path string) error {
	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	if t.rec != nil {
		t.rec.WriteExit(t.runner.ExitCode())
		_ = t.rec.Close()
		t.rec = nil
	}
	rec, err := recording.New(path, recording.Header{
		Command: t.cfg.Cmd,
		Cols:    t.cfg.Cols,
		Rows:    t.cfg.Rows,
		Env:     t.cfg.Env,
	})
	if err != nil {
		return err
	}
	t.rec = rec
	t.cfg.RecordPath = path
	t.updateOutputHookLocked()
	return nil
}

// DisableRecording stops recording and closes the file.
func (t *Term) DisableRecording() error {
	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	if t.rec == nil {
		return nil
	}
	t.rec.WriteExit(t.runner.ExitCode())
	err := t.rec.Close()
	t.rec = nil
	t.cfg.RecordPath = ""
	t.updateOutputHookLocked()
	return err
}

// TracePath returns the trace path (or "" if not tracing).
func (t *Term) TracePath() string {
	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	return t.cfg.TracePath
}

// EnableTrace starts a trace recording to path.
func (t *Term) EnableTrace(path string) error {
	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	if t.tr != nil {
		err := t.tr.Close()
		t.tr = nil
		t.cfg.TracePath = ""
		t.updateOutputHookLocked()
		if err != nil {
			return err
		}
	}
	tr, err := trace.New(path, trace.Manifest{
		Command: t.cfg.Cmd,
		Env:     t.cfg.Env,
		Cols:    t.cfg.Cols,
		Rows:    t.cfg.Rows,
		Pid:     t.runner.Pid(),
	})
	if err != nil {
		return err
	}
	t.tr = tr
	t.cfg.TracePath = path
	t.updateOutputHookLocked()
	return nil
}

// DisableTrace stops tracing and writes the zip bundle.
func (t *Term) DisableTrace() error {
	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	if t.tr == nil {
		return nil
	}
	err := t.tr.Close()
	t.tr = nil
	t.cfg.TracePath = ""
	t.updateOutputHookLocked()
	return err
}

// TraceAddScreenshot adds a pre-encoded PNG screenshot to the active trace.
// Returns nil if no trace is active.
func (t *Term) TraceAddScreenshot(pngData []byte) {
	t.cfgMu.Lock()
	tr := t.tr
	t.cfgMu.Unlock()
	if tr != nil {
		tr.AddScreenshotPNG(pngData)
	}
}

// updateOutputHookLocked sets the pump's output hook to fan out to
// whichever recorders are active. Must be called with cfgMu held.
func (t *Term) updateOutputHookLocked() {
	rec := t.rec
	tr := t.tr
	switch {
	case rec != nil && tr != nil:
		t.pump.SetOutputHook(func(b []byte, ts time.Time) {
			rec.WriteOutput(b, ts)
			tr.WriteOutput(b, ts)
		})
	case rec != nil:
		t.pump.SetOutputHook(func(b []byte, ts time.Time) {
			rec.WriteOutput(b, ts)
		})
	case tr != nil:
		t.pump.SetOutputHook(func(b []byte, ts time.Time) {
			tr.WriteOutput(b, ts)
		})
	default:
		t.pump.SetOutputHook(nil)
	}
}

// recordInput appends a description to the bounded ring buffer.
func (t *Term) recordInput(desc string) {
	t.inputsMu.Lock()
	defer t.inputsMu.Unlock()
	const cap = 64
	if len(t.inputs) >= cap {
		t.inputs = append(t.inputs[:0], t.inputs[len(t.inputs)-cap+1:]...)
	}
	t.inputs = append(t.inputs, InputEvent{When: time.Now(), Desc: desc})
}
