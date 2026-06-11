// Package tuitest is a Playwright-style test harness for terminal UIs.
//
// A Term wraps a child process running under a PTY. Output is parsed by
// an internal VT model; tests inject input via Type/Key/Paste and assert
// on visible state via WaitForText/ExpectText/Snapshot.
//
// All Wait* methods have a default 5s timeout, overridable per call with
// WithTimeout. Expect* helpers wrap the corresponding Wait* and call
// t.Fatalf on timeout.
package tuitest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/paulsmith/twee/internal/engine"
)

// Term is a running TUI under test.
type Term struct {
	*engine.Term
	t testing.TB // optional; required for Expect*
}

// Run constructs a Term bound to a *testing.T running the given command.
func Run(t testing.TB, command string, opts ...Option) *Term {
	t.Helper()
	cfg := newConfig()
	for _, o := range opts {
		o(cfg)
	}
	if len(cfg.cmd) == 0 {
		cfg.cmd = append([]string{command}, cfg.extraArgs...)
	}
	if cfg.tracePath == "" && os.Getenv("TUITEST_RECORD") != "0" {
		cfg.tracePath = filepath.Join(t.TempDir(), "session.twee")
	}
	eng, err := engine.Start(context.Background(), cfg.toEngine())
	if err != nil {
		t.Fatalf("tuitest.Run: %v", err)
	}
	te := &Term{Term: eng, t: t}
	t.Cleanup(func() {
		tracePath := te.TracePath()
		if err := te.Close(); err != nil {
			if tracePath != "" {
				t.Errorf("tuitest close: %v", err)
			} else {
				t.Logf("tuitest close: %v", err)
			}
		}
		if t.Failed() {
			if tracePath != "" {
				t.Logf("tuitest trace: %s", tracePath)
			}
		}
	})
	return te
}

// Start is the lower-level constructor. Caller is responsible for Close.
func Start(ctx context.Context, opts ...Option) (*Term, error) {
	cfg := newConfig()
	for _, o := range opts {
		o(cfg)
	}
	if len(cfg.cmd) == 0 {
		return nil, errors.New("tuitest.Start: no command (use Command option)")
	}
	eng, err := engine.Start(ctx, cfg.toEngine())
	if err != nil {
		return nil, err
	}
	return &Term{Term: eng}, nil
}

// Cursor returns the cursor position. Wrapper that gives the historic
// method name (engine exposes CursorPos to avoid embedding shadow with
// the engine.Cursor type).
func (te *Term) Cursor() Cursor { return te.Term.CursorPos() }
