package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/termios"
	"github.com/paulsmith/twee/internal/tracebundle"
)

func TestMidSessionTraceCapturesStateAtTraceStartWithoutExit(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("termios capture is unsupported on this platform")
	}
	term, err := Start(context.Background(), Config{
		Cmd:  []string{"/bin/sh", "-c", "stty -echo -icanon -isig; echo ready; sleep 30"},
		Cols: 40,
		Rows: 5,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = term.CloseWithGrace(0) }()
	if err := term.WaitForText("ready", WithTimeout(5*time.Second)); err != nil {
		t.Fatalf("WaitForText: %v", err)
	}

	path := filepath.Join(t.TempDir(), "mid-session.twee")
	if err := term.EnableTrace(path); err != nil {
		t.Fatalf("EnableTrace: %v", err)
	}
	if err := term.DisableTrace(); err != nil {
		t.Fatalf("DisableTrace: %v", err)
	}
	bundle, err := tracebundle.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	record := bundle.Manifest.ChildPTYTermios
	if record == nil || record.Start.Status != termios.StatusCaptured || record.Start.State == nil {
		t.Fatalf("start termios = %+v, want captured", record)
	}
	if record.Start.State.Canonical || record.Start.State.Echo || record.Start.State.Signals {
		t.Fatalf("trace-start modes = %+v, want child changes", record.Start.State)
	}
	if record.Exit != nil {
		t.Fatalf("exit termios = %+v, want omitted before child exit", record.Exit)
	}
}

func TestMidSessionTraceClosedAfterExitRecordsExitAndTermios(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("termios capture is unsupported on this platform")
	}
	term, err := Start(context.Background(), Config{
		Cmd:  []string{"/bin/sh", "-c", "echo ready; read line; stty -echo -icanon -isig; exit 7"},
		Cols: 40,
		Rows: 5,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = term.CloseWithGrace(0) }()
	if err := term.WaitForText("ready", WithTimeout(5*time.Second)); err != nil {
		t.Fatalf("WaitForText: %v", err)
	}

	path := filepath.Join(t.TempDir(), "after-exit.twee")
	if err := term.EnableTrace(path); err != nil {
		t.Fatalf("EnableTrace: %v", err)
	}
	if err := term.Type("go\n"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	if _, err := term.WaitForExit(WithTimeout(5 * time.Second)); err != nil {
		t.Fatalf("WaitForExit: %v", err)
	}
	if err := term.DisableTrace(); err != nil {
		t.Fatalf("DisableTrace: %v", err)
	}
	bundle, err := tracebundle.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if bundle.Manifest.ChildPTYTermios == nil || bundle.Manifest.ChildPTYTermios.Exit == nil {
		t.Fatalf("child PTY termios = %+v, want exit snapshot", bundle.Manifest.ChildPTYTermios)
	}
	if len(bundle.Events) == 0 || bundle.Events[len(bundle.Events)-1].Type != "exit" || bundle.Events[len(bundle.Events)-1].Code != 7 {
		t.Fatalf("events = %+v, want final exit code 7", bundle.Events)
	}
}

func TestEnableTraceRejectsPostExitStart(t *testing.T) {
	term, err := Start(context.Background(), Config{Cmd: []string{"/bin/true"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = term.Close() }()
	if _, err := term.WaitForExit(WithTimeout(5 * time.Second)); err != nil {
		t.Fatalf("WaitForExit: %v", err)
	}
	if err := term.EnableTrace(filepath.Join(t.TempDir(), "late.twee")); err == nil {
		t.Fatal("EnableTrace after child exit succeeded")
	}
}

func TestCloseReturnsTraceCloseError(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "session.twee")
	term, err := Start(context.Background(), Config{
		Cmd:               []string{"/bin/sh", "-c", "sleep 30"},
		Cols:              40,
		Rows:              5,
		WholeSessionTrace: &WholeSessionTraceConfig{Path: tracePath},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := os.Mkdir(tracePath, 0o755); err != nil {
		_ = term.Close()
		t.Fatalf("Mkdir final trace path: %v", err)
	}
	if err := term.Close(); err == nil {
		t.Fatal("Close succeeded despite trace final path being a directory")
	}
}
