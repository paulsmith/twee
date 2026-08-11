package ptyrunner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/paulsmith/twee/internal/termios"
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
	ws, err := pty.GetsizeFull(r.master)
	if err != nil {
		t.Fatalf("GetsizeFull: %v", err)
	}
	if ws.Cols != 20 || ws.Rows != 5 {
		t.Fatalf("PTY size = %dx%d, want 20x5", ws.Cols, ws.Rows)
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
	if err := r.Err(); err != nil {
		t.Fatalf("Err = %v, want nil for an ordinary nonzero exit", err)
	}
}

func TestResizeAfterExitFails(t *testing.T) {
	r, err := Start(context.Background(), Config{Command: []string{"/bin/true"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	select {
	case <-r.ExitedCh():
	case <-time.After(2 * time.Second):
		t.Fatal("ExitedCh did not close")
	}
	if err := r.Resize(20, 5); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("Resize after exit = %v, want os.ErrProcessDone", err)
	}
}

func TestRunnerCapturesInitialAndExitTermios(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("termios capture is unsupported on this platform")
	}
	r, err := Start(context.Background(), Config{
		Command: []string{"/bin/sh", "-c", "stty -echo -icanon -isig ixoff -ixon; exit 0"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	initial := r.InitialTermios()
	if initial.Status != termios.StatusCaptured || initial.State == nil {
		t.Fatalf("initial termios = %+v, want captured state", initial)
	}
	if !initial.State.Canonical || !initial.State.Echo || !initial.State.Signals {
		t.Fatalf("initial modes = %+v, want canonical/echo/signals enabled", initial.State)
	}

	waitExited(t, r)
	exit, ok := r.ExitTermios()
	if !ok || exit.Status != termios.StatusCaptured || exit.State == nil {
		t.Fatalf("exit termios = %+v, %t; want captured state", exit, ok)
	}
	if exit.State.Canonical || exit.State.Echo || exit.State.Signals {
		t.Fatalf("exit modes = %+v, want canonical/echo/signals disabled", exit.State)
	}
	if !exit.State.InputFlowControl || exit.State.OutputFlowControl {
		t.Fatalf("exit flow control = %+v, want IXOFF input=true and IXON output=false", exit.State)
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

func TestSignalTargetsProcessGroup(t *testing.T) {
	r, descendant := startRunnerWithDescendant(t, context.Background(), "sleep 30")
	if err := r.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("Signal: %v", err)
	}
	waitProcessGone(t, descendant)
}

func TestResizeSignalsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "winch")
	armed := filepath.Join(dir, "armed")
	script := fmt.Sprintf("trap 'touch %s' WINCH; touch %s; while :; do sleep 1; done", marker, armed)
	r, _ := startRunnerWithDescendant(t, context.Background(), script)
	t.Cleanup(func() { _ = r.CloseWithGrace(0) })
	waitFile(t, armed)

	if err := r.Resize(100, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	waitFile(t, marker)
}

func TestContextCancellationTerminatesProcessGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r, descendant := startRunnerWithDescendant(t, ctx, "trap '' TERM; sleep 30")
	cancel()
	waitExited(t, r)
	waitProcessGone(t, descendant)
}

func TestCloseWithGraceTerminatesProcessGroupAfterLeaderExits(t *testing.T) {
	r, descendant := startRunnerWithDescendant(t, context.Background(), "trap '' TERM; sleep 30")
	if err := r.CloseWithGrace(50 * time.Millisecond); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("CloseWithGrace: %v", err)
	}
	waitProcessGone(t, descendant)
}

func TestSignalAfterProcessGroupExitReturnsProcessDone(t *testing.T) {
	r, err := Start(context.Background(), Config{Command: []string{"/bin/sh", "-c", "exit 0"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitExited(t, r)
	if err := r.CloseWithGrace(0); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("CloseWithGrace: %v", err)
	}
	if err := r.Signal(syscall.SIGTERM); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("Signal after close = %v, want os.ErrProcessDone", err)
	}
}

func TestCloseWithGraceReturnsWhenLeaderExits(t *testing.T) {
	r, err := Start(context.Background(), Config{
		Command: []string{"/bin/sh", "-c", `trap 'exit 0' TERM; while :; do :; done`},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	start := time.Now()
	if err := r.CloseWithGrace(2 * time.Second); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("CloseWithGrace: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("CloseWithGrace took %s, want cooperative exit before grace", elapsed)
	}
}

func startRunnerWithDescendant(t *testing.T, ctx context.Context, descendantScript string) (*Runner, int) {
	t.Helper()
	script := fmt.Sprintf("/bin/sh -c %s & echo $!; wait", shellQuote(descendantScript))
	r, err := Start(ctx, Config{Command: []string{"/bin/sh", "-c", script}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = r.CloseWithGrace(0) })
	line := strings.TrimSpace(readPTY(t, r, "\n"))
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("descendant PID output %q: %v", line, err)
	}
	return r, pid
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func waitExited(t *testing.T, r *Runner) {
	t.Helper()
	select {
	case <-r.ExitedCh():
	case <-time.After(2 * time.Second):
		t.Fatal("ExitedCh did not close")
	}
}

func waitProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if os.IsNotExist(err) || err == nil && strings.Contains(string(stat), ") Z ") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d survived", pid)
}

func waitFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("file %s was not created", path)
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
