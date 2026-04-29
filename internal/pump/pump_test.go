package pump

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/paulsmith/research/twee/internal/vt"
)

// blockingReader emits one chunk then blocks until released.
type blockingReader struct {
	once    sync.Once
	chunk   []byte
	release chan struct{}
}

func (r *blockingReader) Read(p []byte) (int, error) {
	emitted := false
	r.once.Do(func() {
		copy(p, r.chunk)
		emitted = true
	})
	if emitted {
		return len(r.chunk), nil
	}
	<-r.release
	return 0, io.EOF
}

// TestOutputHookOutsideMutex verifies that a slow output hook does not
// block snapshots or waiters on the pump. Regression for the "hook
// called under mu" bug.
func TestOutputHookOutsideMutex(t *testing.T) {
	model := vt.New(20, 3)
	r := &blockingReader{
		chunk:   []byte("ready"),
		release: make(chan struct{}),
	}
	defer close(r.release)
	p := New(model, r)

	hookEntered := make(chan struct{})
	hookRelease := make(chan struct{})
	p.SetOutputHook(func(b []byte, ts time.Time) {
		close(hookEntered)
		<-hookRelease
	})

	go p.Run()

	select {
	case <-hookEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("hook never invoked")
	}

	// Hook is now blocked. Snapshot must still return promptly.
	done := make(chan vt.Snapshot, 1)
	go func() { done <- p.Snapshot() }()
	select {
	case s := <-done:
		if vt.VisibleLines(s)[0] != "ready" {
			t.Errorf("snapshot inconsistent: %q", vt.VisibleLines(s)[0])
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Snapshot blocked while hook held")
	}

	// And so must Wait — hook isn't holding mu, so the broadcast that
	// was issued before Unlock should have already woken any waiter.
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- p.Wait(context.Background(), 500*time.Millisecond, func(s vt.Snapshot) bool {
			return bytes.Contains([]byte(vt.VisibleText(s)), []byte("ready"))
		})
	}()
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("Wait failed: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Wait blocked while hook held")
	}

	close(hookRelease)
}
