package codegen

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/vt"
)

type closeFailingTrace struct{}

func (closeFailingTrace) Close() error          { return errors.New("close failed") }
func (closeFailingTrace) Abort(err error) error { return err }
func (closeFailingTrace) AttachNetworkCapture(string, trace.NetworkCapture) error {
	return nil
}
func (closeFailingTrace) WriteOutput([]byte, time.Time)              {}
func (closeFailingTrace) WriteInput(trace.InputKind, string, []byte) {}
func (closeFailingTrace) WriteExit(int)                              {}
func (closeFailingTrace) WriteResize(int, int)                       {}

func TestNextRecorderPathReservesCollisions(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 5, 14, 3, 9, 0, time.UTC)
	a, err := nextRecorderPathInDir(dir, "twee-trace", ".twee", now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := nextRecorderPathInDir(dir, "twee-trace", ".twee", now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(a) != "twee-trace-20260505-140309.twee" || filepath.Base(b) != "twee-trace-20260505-140309-02.twee" {
		t.Fatalf("%q %q", a, b)
	}
	if _, err := os.Stat(a); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(b); err != nil {
		t.Fatal(err)
	}
}

func TestNextRecorderPathConcurrentReservations(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 5, 14, 3, 9, 0, time.UTC)
	const n = 16
	paths := make(chan string, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			p, e := nextRecorderPathInDir(dir, "twee-script", ".json", now)
			if e != nil {
				errs <- e
				return
			}
			paths <- p
		})
	}
	wg.Wait()
	close(paths)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	for p := range paths {
		if seen[p] {
			t.Fatalf("duplicate %q", p)
		}
		seen[p] = true
		if _, e := os.Stat(p); e != nil {
			t.Fatal(e)
		}
	}
	if len(seen) != n {
		t.Fatalf("reserved %d, want %d", len(seen), n)
	}
}

func TestTraceControllerOneShot(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.twee")
	c := newTraceController(Options{Command: []string{"/bin/cat"}}, 80, 24, 1)
	if err := c.start(p, vt.New(80, 24).Snapshot()); err != nil {
		t.Fatal(err)
	}
	if err := c.close(); err != nil {
		t.Fatal(err)
	}
	if err := c.start(p, vt.New(80, 24).Snapshot()); err == nil {
		t.Fatal("restart succeeded")
	}
}

func TestNetworkTraceControllerIsWholeSession(t *testing.T) {
	c := newTraceController(Options{NetworkCapture: true}, 80, 24, 1)
	if !c.wholeSession {
		t.Fatal("network trace controller is stoppable")
	}
}

func TestTraceOpenFailureIsPermanent(t *testing.T) {
	c := newTraceController(Options{Command: []string{"/bin/cat"}}, 80, 24, 1)
	bad := t.TempDir()
	if err := c.start(bad, vt.New(80, 24).Snapshot()); err == nil {
		t.Fatal("directory target succeeded")
	}
	if c.state != recorderFailed {
		t.Fatalf("state=%v", c.state)
	}
	if err := c.start(filepath.Join(t.TempDir(), "ok.twee"), vt.New(80, 24).Snapshot()); err == nil {
		t.Fatal("retry succeeded")
	}
}

func TestTraceCloseFailureCleansOnlyGeneratedReservation(t *testing.T) {
	dir := t.TempDir()
	path, reservation, err := reserveRecorderPathInDir(dir, "twee-trace", ".twee", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c := &traceController{tr: closeFailingTrace{}, state: recorderRecording, path: path, reservation: reservation}
	if err := c.close(); err == nil {
		t.Fatal("close succeeded")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("generated reservation remains: %v", err)
	}

	explicit := filepath.Join(dir, "explicit.twee")
	if err := os.WriteFile(explicit, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c = &traceController{tr: closeFailingTrace{}, state: recorderRecording, path: explicit}
	if err := c.close(); err == nil {
		t.Fatal("close succeeded")
	}
	if _, err := os.Stat(explicit); err != nil {
		t.Fatalf("explicit path removed: %v", err)
	}
}

func TestCleanupReservedPathLeavesReplacement(t *testing.T) {
	path, reservation, err := reserveRecorderPathInDir(t.TempDir(), "twee-trace", ".twee", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanupReservedPath(path, reservation)
	if got, err := os.ReadFile(path); err != nil || string(got) != "replacement" {
		t.Fatalf("replacement removed or changed: %q %v", got, err)
	}
}
