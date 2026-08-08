package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/paulsmith/twee/internal/ptyrunner"
	"github.com/paulsmith/twee/internal/pump"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/tracepolicy"
	"github.com/paulsmith/twee/internal/vt"
	"github.com/paulsmith/twee/third_party/netwrap"
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

	cfgMu sync.Mutex
	// inputMu serializes complete logical inputs, including the encode,
	// PTY write, diagnostic, and trace bookkeeping phases. Mouse encoding
	// briefly takes pump.mu underneath this lock; pump code must never try
	// to acquire inputMu.
	inputMu sync.Mutex
	// inputWriter is runner.Master in production. Keeping the writer as the
	// input boundary permits deterministic short-write serialization tests.
	inputWriter io.Writer
	runner      *ptyrunner.Runner
	pump        *pump.Pump
	tr          *trace.Trace
	tracePath   string // guarded by cfgMu

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
	// suppressTombstone lets bulk cleanup request no persistent exit record.
	suppressTombstone atomic.Bool

	startedAt time.Time

	inputsMu sync.Mutex
	inputs   []InputEvent
	network  *networkArtifacts
}

type networkArtifacts struct {
	dir      string
	pcapPath string
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
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	var network *networkArtifacts
	var networkCfg *ptyrunner.NetworkConfig
	if whole := cfg.WholeSessionTrace; whole != nil && whole.Network != nil {
		var err error
		networkDir, err := os.MkdirTemp("", "twee-network-*")
		if err != nil {
			return nil, fmt.Errorf("network capture staging: %w", err)
		}
		network = &networkArtifacts{
			dir: networkDir, pcapPath: filepath.Join(networkDir, "network.pcap"),
		}
		pubs := make([]netwrap.TCPPublication, len(whole.Network.PublishTCP))
		for i, p := range whole.Network.PublishTCP {
			pubs[i] = netwrap.TCPPublication{Listen: p.Listen, Guest: p.Guest}
		}
		networkCfg = &ptyrunner.NetworkConfig{PCAPPath: network.pcapPath, PublishTCP: pubs}
	}

	runner, err := ptyrunner.Start(ctx, ptyrunner.Config{
		Command: cfg.Cmd,
		Env:     cfg.BuildEnv(),
		Dir:     cfg.Dir,
		Cols:    cfg.Cols,
		Rows:    cfg.Rows,
		Network: networkCfg,
	})
	if err != nil {
		if network != nil {
			_ = os.RemoveAll(network.dir)
		}
		return nil, fmt.Errorf("spawn: %w", err)
	}

	model := vt.New(cfg.Cols, cfg.Rows)
	p := pump.New(model, runner.Master())

	t := &Term{
		cfg:           cfg,
		inputWriter:   runner.Master(),
		runner:        runner,
		pump:          p,
		pumpDone:      make(chan struct{}),
		artifactsDone: make(chan struct{}),
		startedAt:     time.Now(),
		network:       network,
	}
	if cfg.WholeSessionTrace != nil {
		if err := t.startTrace(cfg.WholeSessionTrace.Path); err != nil {
			cleanupErr := runner.Close()
			if network != nil {
				cleanupErr = errors.Join(cleanupErr, os.RemoveAll(network.dir))
			}
			if cleanupErr != nil {
				cleanupErr = fmt.Errorf("close PTY after trace setup failure: %w", cleanupErr)
			}
			return nil, fmt.Errorf("trace: %w", errors.Join(err, cleanupErr))
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
		// Do not hold inputMu while draining: closing the PTY is what
		// unblocks an input already stuck in Write. Once draining finishes,
		// take the input boundary before closing the trace so every input
		// whose write succeeded has completed its diagnostic and trace
		// bookkeeping.
		t.inputMu.Lock()
		t.cfgMu.Lock()
		t.finalized = true
		var err error
		if t.tr != nil {
			t.tr.WriteExit(t.runner.ExitCode())
			if t.network != nil {
				err = errors.Join(err, t.attachNetworkCaptureLocked())
			}
			if err != nil {
				_ = t.tr.Abort(err)
			}
			err = t.closeTraceLocked()
		}
		t.cfgMu.Unlock()
		t.inputMu.Unlock()
		t.finalizeErr = err
		if t.network != nil {
			t.finalizeErr = errors.Join(t.finalizeErr, os.RemoveAll(t.network.dir))
		}
		close(t.artifactsDone)
	})
	return t.finalizeErr
}

// ArtifactsDone is closed once FinalizeArtifacts (or Close) has finished
// writing artifacts.
func (t *Term) ArtifactsDone() <-chan struct{} { return t.artifactsDone }

// ArtifactError returns the finalization error after ArtifactsDone closes.
// It returns nil while finalization is still pending.
func (t *Term) ArtifactError() error {
	select {
	case <-t.artifactsDone:
		return t.finalizeErr
	default:
		return nil
	}
}

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
	path := t.tracePath
	err := t.tr.Close()
	t.tr = nil
	t.tracePath = ""
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
	return t.tracePath
}

// EnableTrace starts a trace recording to path.
func (t *Term) EnableTrace(path string) error {
	t.inputMu.Lock()
	defer t.inputMu.Unlock()

	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	if t.cfg.WholeSessionTrace != nil {
		return errors.New("EnableTrace: trace is configured for the whole session")
	}
	if t.finalized {
		return errors.New("EnableTrace: artifacts already finalized")
	}
	if t.tr != nil {
		if err := t.closeTraceLocked(); err != nil {
			return err
		}
	}
	return t.startTraceLocked(path)
}

func (t *Term) startTrace(path string) error {
	t.inputMu.Lock()
	defer t.inputMu.Unlock()
	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	return t.startTraceLocked(path)
}

func (t *Term) startTraceLocked(path string) error {
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
	t.tracePath = path
	t.updateOutputHookLocked()
	return nil
}

func (t *Term) networkManifest() (*trace.NetworkCapture, error) {
	whole := t.cfg.WholeSessionTrace
	if whole == nil || whole.Network == nil {
		return nil, nil
	}
	pubs := make([]string, len(whole.Network.PublishTCP))
	for i, p := range whole.Network.PublishTCP {
		var err error
		pubs[i], err = FormatTCPPublication(p)
		if err != nil {
			return nil, fmt.Errorf("network capture publication metadata: %w", err)
		}
	}
	return &trace.NetworkCapture{
		Format: trace.NetworkCaptureFormat, Stream: trace.NetworkCaptureStream,
		GVisorVersion: netwrap.GVisorVersion, PublishTCP: pubs,
		ByteLimit: tracepolicy.MaxNetworkCaptureBytes,
	}, nil
}

// attachNetworkCaptureLocked bridges completed runner lifecycle state into the
// trace manifest. FinalizeArtifacts drains the runner before calling it, so the
// returned statistics and PCAP are stable.
func (t *Term) attachNetworkCaptureLocked() error {
	if err := t.runner.Err(); err != nil {
		return fmt.Errorf("network capture runtime: %w", err)
	}
	result, ok := t.runner.NetworkCapture()
	if !ok {
		return errors.New("network capture: runner did not provide capture results")
	}
	capture, err := t.networkManifest()
	if err != nil {
		return err
	}
	capture.ByteLimit = result.MaxBytes
	capture.CapturedBytes = result.BytesWritten
	capture.PacketCount = int64(result.PacketCount)
	capture.Truncated = result.Truncated
	capture.Status = trace.NetworkCaptureStatusComplete
	if result.Truncated {
		capture.Status = trace.NetworkCaptureStatusTruncated
	}
	return t.tr.AttachNetworkCapture(t.network.pcapPath, *capture)
}

// DisableTrace stops tracing and writes the zip bundle.
func (t *Term) DisableTrace() error {
	t.inputMu.Lock()
	defer t.inputMu.Unlock()

	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	if t.cfg.WholeSessionTrace != nil {
		return errors.New("DisableTrace: trace is configured for the whole session")
	}
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
