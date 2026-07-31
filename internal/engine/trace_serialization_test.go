package engine

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestTraceLifecycleWaitsForInputBoundary(t *testing.T) {
	term, err := Start(context.Background(), Config{
		Cmd:  []string{"/bin/sh", "-c", "sleep 30"},
		Cols: 20,
		Rows: 4,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })

	firstPath := filepath.Join(t.TempDir(), "first.twee")
	term.inputMu.Lock()
	enableStarted := make(chan struct{})
	enableDone := make(chan error, 1)
	go func() {
		close(enableStarted)
		enableDone <- term.EnableTrace(firstPath)
	}()
	<-enableStarted
	enableErr, enableCompleted := traceLifecycleCompleted(enableDone)
	term.inputMu.Unlock()
	if enableCompleted {
		t.Fatalf("EnableTrace crossed a held input boundary: %v", enableErr)
	}
	if err := <-enableDone; err != nil {
		t.Fatalf("EnableTrace: %v", err)
	}

	term.inputMu.Lock()
	disableStarted := make(chan struct{})
	disableDone := make(chan error, 1)
	go func() {
		close(disableStarted)
		disableDone <- term.DisableTrace()
	}()
	<-disableStarted
	disableErr, disableCompleted := traceLifecycleCompleted(disableDone)
	term.inputMu.Unlock()
	if disableCompleted {
		t.Fatalf("DisableTrace crossed a held input boundary: %v", disableErr)
	}
	if err := <-disableDone; err != nil {
		t.Fatalf("DisableTrace: %v", err)
	}

	secondPath := filepath.Join(t.TempDir(), "second.twee")
	if err := term.EnableTrace(secondPath); err != nil {
		t.Fatalf("second EnableTrace: %v", err)
	}
	term.inputMu.Lock()
	finalizeDone := make(chan error, 1)
	go func() {
		finalizeDone <- term.FinalizeArtifactsWithGrace(0)
	}()
	select {
	case <-term.pumpDone:
	case <-time.After(5 * time.Second):
		term.inputMu.Unlock()
		t.Fatal("finalization did not finish draining output")
	}
	finalizeErr, finalizeCompleted := traceLifecycleCompleted(finalizeDone)
	term.inputMu.Unlock()
	if finalizeCompleted {
		t.Fatalf("FinalizeArtifacts crossed a held input boundary: %v", finalizeErr)
	}
	if err := <-finalizeDone; err != nil {
		t.Fatalf("FinalizeArtifacts: %v", err)
	}
}

func traceLifecycleCompleted(done <-chan error) (error, bool) {
	select {
	case err := <-done:
		return err, true
	case <-time.After(50 * time.Millisecond):
		return nil, false
	}
}
