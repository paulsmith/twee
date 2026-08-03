//go:build linux

package ptyrunner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestLeaderExitRetainsProcessGroupIdentityForDescendantSignal(t *testing.T) {
	// Have the descendant signal readiness only after installing its traps. This
	// prevents the leader's terminal hangup from racing trap installation.
	script := `trap 'exit 7' USR1; /bin/sh -c 'trap "" HUP TERM; echo $$; kill -USR1 $PPID; while :; do sleep 1; done' & wait`
	r, err := Start(context.Background(), Config{Command: []string{"/bin/sh", "-c", script}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = r.CloseWithGrace(0) })
	descendant, err := strconv.Atoi(strings.TrimSpace(readPTY(t, r, "\n")))
	if err != nil {
		t.Fatalf("descendant PID: %v", err)
	}
	waitExited(t, r)
	if got := r.ExitCode(); got != 7 {
		t.Fatalf("ExitCode = %d, want 7", got)
	}

	leaderStat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", r.Pid()))
	if err != nil || !strings.Contains(string(leaderStat), ") Z ") {
		t.Fatalf("leader is not retained waitable: stat=%q err=%v", leaderStat, err)
	}
	if got := processGroupFromProc(t, descendant); got != r.Pid() {
		t.Fatalf("descendant process group = %d, want PTY group %d", got, r.Pid())
	}
	if err := r.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("Signal: %v", err)
	}
	waitProcessGone(t, descendant)
	if err := r.CloseWithGrace(0); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", r.Pid())); !os.IsNotExist(err) {
		t.Fatalf("leader was not reaped after Close: %v", err)
	}
}

func TestCancellationRacingCloseAndRepeatedOperations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r, descendant := startRunnerWithDescendant(t, ctx, `trap "" HUP TERM; sleep 30`)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			switch i % 3 {
			case 0:
				_ = r.Signal(syscall.SIGWINCH)
			case 1:
				_ = r.Resize(80+i, 24+i)
			case 2:
				_ = r.CloseWithGrace(0)
			}
		}(i)
	}
	close(start)
	cancel()
	wg.Wait()
	_ = r.CloseWithGrace(0)
	waitExited(t, r)
	waitProcessGone(t, descendant)
}

func processGroupFromProc(t *testing.T, pid int) int {
	t.Helper()
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		t.Fatal(err)
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		t.Fatalf("malformed stat %q", b)
	}
	fields := strings.Fields(string(b[end+1:]))
	if len(fields) < 3 {
		t.Fatalf("short stat %q", b)
	}
	pgid, err := strconv.Atoi(fields[2])
	if err != nil {
		t.Fatal(err)
	}
	return pgid
}

func TestImmediateContextCancellationDoesNotMissProcessGroup(t *testing.T) {
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r, err := Start(ctx, Config{Command: []string{"/bin/sh", "-c", "sleep 30"}})
		if err != nil {
			continue
		}
		_ = r.CloseWithGrace(0)
		select {
		case <-r.ExitedCh():
		case <-time.After(2 * time.Second):
			t.Fatal("cancelled child survived")
		}
	}
}

func TestWaitidFallbackDisablesSignalingBeforePublishingExit(t *testing.T) {
	originalWaitid := waitid
	waitid = func(int, int, *unix.Siginfo, int, *unix.Rusage) error {
		return syscall.ENOSYS
	}
	t.Cleanup(func() { waitid = originalWaitid })

	r, err := Start(context.Background(), Config{Command: []string{"/bin/sh", "-c", "exit 0"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitExited(t, r)
	if err := r.Signal(syscall.SIGTERM); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("Signal after fallback reap = %v, want os.ErrProcessDone", err)
	}
	if err := r.CloseWithGrace(0); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("CloseWithGrace: %v", err)
	}
}
