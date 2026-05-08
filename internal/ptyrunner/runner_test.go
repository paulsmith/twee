package ptyrunner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStartRejectsEmptyCommand(t *testing.T) {
	if _, err := Start(context.Background(), Config{}); err == nil {
		t.Fatal("Start unexpectedly succeeded")
	}
}

func TestRunnerLifecycle(t *testing.T) {
	r, err := Start(context.Background(), Config{
		Command: []string{"/bin/sh", "-c", "printf 'ready\\r\\n'; sleep 30"},
		Cols:    12,
		Rows:    4,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	if r.Pid() <= 0 {
		t.Fatalf("Pid = %d, want positive", r.Pid())
	}
	if r.Master() == nil {
		t.Fatal("Master is nil")
	}
	if got := readPTY(t, r, "ready"); !strings.Contains(got, "ready") {
		t.Fatalf("output = %q, want ready", got)
	}
	if err := r.Resize(20, 5); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if err := r.Signal(syscall.SIGWINCH); err != nil {
		t.Fatalf("Signal: %v", err)
	}
}

func TestRunnerExitCode(t *testing.T) {
	r, err := Start(context.Background(), Config{
		Command: []string{"/bin/sh", "-c", "exit 6"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	select {
	case <-r.ExitedCh():
	case <-time.After(2 * time.Second):
		t.Fatal("ExitedCh did not close")
	}
	if got := r.ExitCode(); got != 6 {
		t.Fatalf("ExitCode = %d, want 6", got)
	}
}

func TestRunnerCloseTerminatesChild(t *testing.T) {
	r, err := Start(context.Background(), Config{
		Command: []string{"/bin/sh", "-c", "sleep 30"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := r.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-r.ExitedCh():
	case <-time.After(2 * time.Second):
		t.Fatal("ExitedCh did not close after Close")
	}
}

func readPTY(t *testing.T, r *Runner, want string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var out bytes.Buffer
	buf := make([]byte, 128)
	for time.Now().Before(deadline) {
		type readResult struct {
			n   int
			err error
		}
		ch := make(chan readResult, 1)
		go func() {
			n, err := r.master.Read(buf)
			ch <- readResult{n: n, err: err}
		}()
		select {
		case res := <-ch:
			if res.n > 0 {
				out.Write(buf[:res.n])
				if strings.Contains(out.String(), want) {
					return out.String()
				}
			}
			if res.err != nil {
				t.Fatalf("Read: %v", res.err)
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	return out.String()
}
