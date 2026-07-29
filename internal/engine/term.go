package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/paulsmith/twee/internal/ptyrunner"
	"github.com/paulsmith/twee/internal/pump"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/vt"
)

// DefaultCloseGrace is the SIGTERM-to-SIGKILL escalation window Close
// uses when no explicit grace is given. Mirrors ptyrunner.DefaultGrace
// (the two are kept in sync deliberately, not merged into one constant,
// so each package documents its own default independently of the other's
// internals).
const DefaultCloseGrace = ptyrunner.DefaultGrace

// Term is a running TUI under PTY.
type Term struct {
	cfg Config

	cfgMu  sync.Mutex
	runner *ptyrunner.Runner
	pump   *pump.Pump
	tr     *trace.Trace

	closeOnce sync.Once
	closeErr  error
	pumpDone  chan struct{}

	drainOnce sync.Once
	drainErr  error

	finalizeOnce  sync.Once
	finalizeErr   error
	artifactsDone chan struct{}

	finalized          bool   // guarded by cfgMu; set once FinalizeArtifacts ran
	finalizedTracePath string // guarded by cfgMu

	// stopRequested records that an explicit "twee stop" (as opposed to
	// the child exiting on its own) asked this session to end. Set by the
	// daemon's stop handler before it calls CloseWithGrace; read at
	// teardown to fill the session's tombstone. The handler and the
	// exit-watching goroutine that eventually tears the daemon down run
	// concurrently and neither side needs to block on the other, so an
	// atomic is a simpler fit than a mutex-guarded bool.
	stopRequested atomic.Bool

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
// and pump, optionally enables tracing, and returns a Term ready for
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

	t := &Term{
		cfg:           cfg,
		runner:        runner,
		pump:          p,
		pumpDone:      make(chan struct{}),
		artifactsDone: make(chan struct{}),
		startedAt:     time.Now(),
	}
	if cfg.TracePath != "" {
		if err := t.EnableTrace(cfg.TracePath); err != nil {
			_ = runner.Close()
			return nil, fmt.Errorf("trace: %w", err)
		}
	}
	go func() {
		_ = p.Run()
		close(t.pumpDone)
	}()
	return t, nil
}

// Close terminates the child and the pump, then finalizes artifacts,
// using DefaultCloseGrace as the SIGTERM-to-SIGKILL escalation window.
func (t *Term) Close() error {
	return t.CloseWithGrace(DefaultCloseGrace)
}

// CloseWithGrace is Close with an overridable escalation window; see
// ptyrunner.Runner.CloseWithGrace for how grace is interpreted (<= 0
// means SIGKILL immediately). Idempotent like Close: the grace passed by
// whichever caller wins the race to run first is the one that applies.
func (t *Term) CloseWithGrace(grace time.Duration) error {
	t.closeOnce.Do(func() {
		ferr := t.FinalizeArtifactsWithGrace(grace)
		t.closeErr = errors.Join(t.drainErr, ferr)
	})
	return t.closeErr
}

// DrainOutput terminates the child if it is still running, closes the
// PTY, and waits for the output pump to deliver everything it read.
// Idempotent. After it returns, Snapshot reflects the final terminal
// state. The runner shutdown error is reported by Close. Uses
// DefaultCloseGrace; see DrainOutputWithGrace to override it.
func (t *Term) DrainOutput() {
	t.DrainOutputWithGrace(DefaultCloseGrace)
}

// DrainOutputWithGrace is DrainOutput with an overridable
// SIGTERM-to-SIGKILL escalation window.
func (t *Term) DrainOutputWithGrace(grace time.Duration) {
	t.drainOnce.Do(func() {
		t.drainErr = t.runner.CloseWithGrace(grace)
		<-t.pumpDone
	})
}

// FinalizeArtifacts drains output, then closes the active trace so its
// file is durable on disk. Idempotent; ArtifactsDone
// is closed once finalization completes. The returned error covers only
// artifact closing; runner shutdown errors are reported by Close. Uses
// DefaultCloseGrace; see FinalizeArtifactsWithGrace to override it.
func (t *Term) FinalizeArtifacts() error {
	return t.FinalizeArtifactsWithGrace(DefaultCloseGrace)
}

// FinalizeArtifactsWithGrace is FinalizeArtifacts with an overridable
// SIGTERM-to-SIGKILL escalation window, passed through to DrainOutput.
func (t *Term) FinalizeArtifactsWithGrace(grace time.Duration) error {
	t.finalizeOnce.Do(func() {
		t.DrainOutputWithGrace(grace)
		t.cfgMu.Lock()
		t.finalized = true
		var err error
		if t.tr != nil {
			t.tr.WriteExit(t.runner.ExitCode())
			err = t.closeTraceLocked()
		}
		t.cfgMu.Unlock()
		t.finalizeErr = err
		close(t.artifactsDone)
	})
	return t.finalizeErr
}

// ArtifactsDone is closed once FinalizeArtifacts (or Close) has finished
// writing artifacts.
func (t *Term) ArtifactsDone() <-chan struct{} { return t.artifactsDone }

// FinalizedTracePath returns the path of the last trace bundle written
// (by DisableTrace or FinalizeArtifacts), or "" if none was written.
func (t *Term) FinalizedTracePath() string {
	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	return t.finalizedTracePath
}

// closeTraceLocked closes the active trace, records the finalized bundle
// path on success, and clears trace state. Caller must hold cfgMu and
// have checked t.tr != nil.
func (t *Term) closeTraceLocked() error {
	path := t.cfg.TracePath
	err := t.tr.Close()
	t.tr = nil
	t.cfg.TracePath = ""
	t.updateOutputHookLocked()
	if err == nil {
		t.finalizedTracePath = path
	}
	return err
}

// Cmd returns the command line the child was spawned with.
func (t *Term) Cmd() []string { return append([]string(nil), t.cfg.Cmd...) }

// DefaultTimeout returns the configured default timeout for waits.
func (t *Term) DefaultTimeout() time.Duration { return t.cfg.DefaultTimeout }

// StableQuietWindow returns the default quiet window for WaitForStableScreen.
func (t *Term) StableQuietWindow() time.Duration { return t.cfg.StableQuietWindow }

// StartedAt returns the wall-clock time when Start completed.
func (t *Term) StartedAt() time.Time { return t.startedAt }

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
	if t.finalized {
		return errors.New("EnableTrace: artifacts already finalized")
	}
	if t.tr != nil {
		if err := t.closeTraceLocked(); err != nil {
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
	if seed := TraceSeedOutput(t.pump.Snapshot()); len(seed) > 0 {
		tr.WriteOutput(seed, time.Now())
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
	return t.closeTraceLocked()
}

// updateOutputHookLocked sets the pump's output hook to the active trace.
// Must be called with cfgMu held.
func (t *Term) updateOutputHookLocked() {
	tr := t.tr
	if tr != nil {
		t.pump.SetOutputHook(func(b []byte, ts time.Time) {
			tr.WriteOutput(b, ts)
		})
		return
	}
	t.pump.SetOutputHook(nil)
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
