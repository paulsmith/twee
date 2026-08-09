//go:build linux

package ptyrunner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/paulsmith/twee/third_party/netwrap"
	"golang.org/x/sys/unix"
)

func TestNetworkStartReportsExecFailureSynchronously(t *testing.T) {
	requireNetwrap(t)
	_, err := Start(context.Background(), Config{
		Command: []string{"/definitely/missing/twee-network-test"},
		Network: &NetworkConfig{PCAPPath: filepath.Join(t.TempDir(), "capture.pcap")},
	})
	if err == nil || !strings.Contains(err.Error(), "command setup failed") {
		t.Fatalf("Start error = %v; want synchronous command setup error", err)
	}
}

func TestNetworkRunnerHasControllingTTYAndPID(t *testing.T) {
	r := startNetworkRunner(t, context.Background(), `/bin/sh -c 'test -t 0 && test -t 1 && : > /dev/tty && exit 17'`)
	if r.Pid() <= 0 {
		t.Fatalf("Pid = %d; want positive", r.Pid())
	}
	waitExited(t, r)
	if err := r.Err(); err != nil {
		t.Fatalf("Err = %v", err)
	}
	if got := r.ExitCode(); got != 17 {
		t.Fatalf("ExitCode = %d; want natural exit 17", got)
	}
}

func TestNetworkRunnerForwardsSignalsAndResize(t *testing.T) {
	dir := t.TempDir()
	winch := filepath.Join(dir, "winch")
	usr1 := filepath.Join(dir, "usr1")
	script := fmt.Sprintf(`trap ': > %s' WINCH; trap ': > %s; exit 23' USR1; echo ready; while :; do :; done`, shellQuote(winch), shellQuote(usr1))
	r := startNetworkRunner(t, context.Background(), script)
	if got := readPTY(t, r, "ready"); !strings.Contains(got, "ready") {
		t.Fatalf("output = %q; want ready", got)
	}
	if err := r.Resize(101, 41); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	waitFile(t, winch)
	if err := r.Signal(syscall.SIGUSR1); err != nil {
		t.Fatalf("Signal(SIGUSR1): %v", err)
	}
	waitFile(t, usr1)
	waitExited(t, r)
	if got := r.ExitCode(); got != 23 {
		t.Fatalf("ExitCode = %d; want 23", got)
	}
}

func TestNetworkRunnerCloseUsesCallerGraceAndIsRepeatedlySafe(t *testing.T) {
	r := startNetworkRunner(t, context.Background(), `trap '' TERM; echo ready; while :; do :; done`)
	_ = readPTY(t, r, "ready")
	started := time.Now()
	if err := r.CloseWithGrace(120 * time.Millisecond); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("CloseWithGrace: %v", err)
	}
	elapsed := time.Since(started)
	if elapsed < 90*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("CloseWithGrace took %s; want caller-selected grace", elapsed)
	}
	if err := r.CloseWithGrace(time.Second); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("repeated CloseWithGrace: %v", err)
	}
	if signal, ok := r.ExitSignal(); !ok || signal != "SIGKILL" {
		t.Fatalf("ExitSignal = %q, %v; want SIGKILL", signal, ok)
	}
}

func TestNetworkRunnerCloseReapsTermIgnoringDescendant(t *testing.T) {
	r := startNetworkRunner(t, context.Background(), `/bin/sh -c 'trap "" HUP TERM; exec sleep 30' & echo $!; wait`)
	line := strings.TrimSpace(readPTY(t, r, "\n"))
	descendant, err := strconv.Atoi(line)
	if err != nil {
		t.Fatalf("descendant PID output %q: %v", line, err)
	}
	if err := r.CloseWithGrace(80 * time.Millisecond); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("CloseWithGrace: %v", err)
	}
	waitProcessGone(t, descendant)
}

func TestNetworkRunnerCancellationAndCaptureCompletion(t *testing.T) {
	requireNetwrap(t)
	ctx, cancel := context.WithCancel(context.Background())
	pcap := filepath.Join(t.TempDir(), "capture.pcap")
	r, err := Start(ctx, Config{
		Command: []string{"/bin/sh", "-c", "echo ready; while :; do :; done"},
		Network: &NetworkConfig{PCAPPath: pcap},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = readPTY(t, r, "ready")
	cancel()
	waitExited(t, r)
	if !errors.Is(r.Err(), context.Canceled) {
		t.Fatalf("Err = %v; want context.Canceled", r.Err())
	}
	capture, ok := r.NetworkCapture()
	if !ok || capture.MaxBytes == 0 || capture.BytesWritten < 24 {
		t.Fatalf("NetworkCapture = %+v, %v", capture, ok)
	}
	info, err := os.Stat(pcap)
	if err != nil {
		t.Fatalf("stat completed PCAP: %v", err)
	}
	if info.Size() != capture.BytesWritten {
		t.Fatalf("PCAP size = %d; capture bytes = %d", info.Size(), capture.BytesWritten)
	}
}

func TestSignaledChildReportsSameExitCodeOnBothBackends(t *testing.T) {
	const script = `kill -TERM $$; while :; do :; done`
	local, err := Start(context.Background(), Config{Command: []string{"/bin/sh", "-c", script}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = local.CloseWithGrace(0) })
	waitExited(t, local)
	if signal, ok := local.ExitSignal(); !ok || signal != "SIGTERM" {
		t.Fatalf("local ExitSignal = %q, %v; want SIGTERM", signal, ok)
	}
	if got := local.ExitCode(); got != 143 {
		t.Fatalf("local ExitCode = %d; want 128+SIGTERM = 143", got)
	}

	network := startNetworkRunner(t, context.Background(), script)
	waitExited(t, network)
	if signal, ok := network.ExitSignal(); !ok || signal != "SIGTERM" {
		t.Fatalf("network ExitSignal = %q, %v; want SIGTERM", signal, ok)
	}
	if got := network.ExitCode(); got != 143 {
		t.Fatalf("network ExitCode = %d; want 128+SIGTERM = 143", got)
	}
}

func requireNetwrap(t *testing.T) {
	t.Helper()
	if err := netwrap.Preflight(); err != nil {
		t.Skipf("netwrap prerequisites unavailable: %v", err)
	}
}

func startNetworkRunner(t *testing.T, ctx context.Context, script string) *Runner {
	t.Helper()
	requireNetwrap(t)
	r, err := Start(ctx, Config{
		Command: []string{"/bin/sh", "-c", script},
		Network: &NetworkConfig{PCAPPath: filepath.Join(t.TempDir(), "capture.pcap")},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = r.CloseWithGrace(0) })
	return r
}

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
	for i := range 8 {
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
	for range 20 {
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
