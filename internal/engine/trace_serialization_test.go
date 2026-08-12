package engine

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/pump"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/tracepolicy"
	"github.com/paulsmith/twee/internal/vt"
)

func TestMarkTraceWaitsForAdmittedOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.twee")
	tr, err := trace.New(path, trace.Manifest{Cols: 20, Rows: 4})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	p := pump.New(vt.New(20, 4), reader)
	hookEntered := make(chan struct{})
	hookRelease := make(chan struct{})
	p.SetOutputHook(func(b []byte, ts time.Time) {
		close(hookEntered)
		<-hookRelease
		tr.WriteOutput(b, ts)
	})
	term := &Term{pump: p, tr: tr, tracePath: path}
	runDone := make(chan error, 1)
	go func() { runDone <- p.Run() }()
	go func() { _, _ = writer.Write([]byte("before")) }()
	<-hookEntered
	markDone := make(chan error, 1)
	go func() { markDone <- term.MarkTrace("checkpoint") }()
	select {
	case err := <-markDone:
		t.Fatalf("MarkTrace overtook admitted output: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(hookRelease)
	if err := <-markDone; err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}

	events := readTraceEvents(t, path)
	if len(events) < 2 || events[0].Type != trace.EventTypeOutput || events[1].Type != trace.EventTypeMarker {
		t.Fatalf("events = %+v, want output before marker", events)
	}
}

func TestMarkTraceClassifiesValidationAndIO(t *testing.T) {
	tr, err := trace.New(filepath.Join(t.TempDir(), "session.twee"), trace.Manifest{Cols: 20, Rows: 4})
	if err != nil {
		t.Fatal(err)
	}
	term := &Term{pump: pump.New(vt.New(20, 4), strings.NewReader("")), tr: tr, tracePath: "session.twee"}
	err = term.MarkTrace(strings.Repeat("\x00", tracepolicy.MaxEventLineBytes/6+1))
	var requestErr *RequestError
	if !errors.As(err, &requestErr) || requestErr.Kind != RequestErrorInvalidArgument {
		t.Fatalf("validation error = %v, want INVALID_ARGUMENT", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	err = term.MarkTrace("checkpoint")
	if !errors.As(err, &requestErr) || requestErr.Kind != RequestErrorIO {
		t.Fatalf("encoder error = %v, want IO", err)
	}
	_ = tr.Abort(err)
}

func readTraceEvents(t *testing.T, path string) []trace.EventRecord {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = zr.Close() })
	f, err := zr.Open("events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	var events []trace.EventRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var event trace.EventRecord
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}

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
