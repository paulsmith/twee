package pump

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/input"
	"github.com/paulsmith/twee/internal/vt"
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

func TestExpectedEOFDetection(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"eof", io.EOF, true},
		{"unexpected eof", io.ErrUnexpectedEOF, true},
		{"eio text", errors.New("read /dev/ptmx: input/output error"), true},
		{"closed pty text", errors.New("i/o error on closed pty"), true},
		{"file closed text", errors.New("file already closed"), true},
		{"other", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isExpectedEOF(tt.err); got != tt.want {
				t.Fatalf("isExpectedEOF(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestRecentBytesAreCappedAndCopied(t *testing.T) {
	p := New(vt.New(5, 2), bytes.NewReader(nil))
	p.appendRecent(bytes.Repeat([]byte("a"), 70*1024))

	got := p.RecentBytes()
	if len(got) != 64*1024 {
		t.Fatalf("RecentBytes len = %d, want 64KiB", len(got))
	}
	got[0] = 'z'
	again := p.RecentBytes()
	if again[0] != 'a' {
		t.Fatalf("RecentBytes returned mutable internal buffer")
	}

	p.appendRecent([]byte("tail"))
	if !bytes.HasSuffix(p.RecentBytes(), []byte("tail")) {
		t.Fatalf("RecentBytes missing appended tail")
	}
}

func TestResizeAndSnapshot(t *testing.T) {
	p := New(vt.New(5, 2), bytes.NewReader(nil))
	if err := p.Resize(8, 3); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	s := p.Snapshot()
	if s.Size.Cols != 8 || s.Size.Rows != 3 {
		t.Fatalf("snapshot size = %dx%d, want 8x3", s.Size.Cols, s.Size.Rows)
	}
}

func TestWaitImmediateTimeoutContextAndClosed(t *testing.T) {
	p := New(vt.New(5, 2), bytes.NewReader(nil))
	if err := p.Wait(context.Background(), time.Second, func(vt.Snapshot) bool { return true }); err != nil {
		t.Fatalf("immediate Wait: %v", err)
	}
	if err := p.Wait(context.Background(), time.Millisecond, func(vt.Snapshot) bool { return false }); !errors.Is(err, ErrTimeout) {
		t.Fatalf("timeout Wait = %v, want ErrTimeout", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Wait(ctx, time.Second, func(vt.Snapshot) bool { return false }); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Wait = %v, want context canceled", err)
	}

	closed := New(vt.New(5, 2), bytes.NewReader(nil))
	if err := closed.Run(); err != nil {
		t.Fatalf("Run closed: %v", err)
	}
	if err := closed.Wait(context.Background(), time.Second, func(vt.Snapshot) bool { return false }); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed Wait = %v, want ErrClosed", err)
	}
}

func TestWaitStableTimeoutContextAndClosed(t *testing.T) {
	p := New(vt.New(5, 2), bytes.NewReader(nil))
	if err := p.WaitStable(context.Background(), time.Millisecond, time.Millisecond); !errors.Is(err, ErrTimeout) {
		t.Fatalf("WaitStable timeout = %v, want ErrTimeout", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.WaitStable(ctx, time.Second, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitStable canceled = %v, want context canceled", err)
	}

	closed := New(vt.New(5, 2), bytes.NewReader(nil))
	if err := closed.Run(); err != nil {
		t.Fatalf("Run closed: %v", err)
	}
	if err := closed.WaitStable(context.Background(), time.Second, time.Second); err != nil {
		t.Fatalf("WaitStable closed: %v", err)
	}
}

func TestRunUnexpectedError(t *testing.T) {
	want := errors.New("boom")
	p := New(vt.New(5, 2), errReader{err: want})
	if err := p.Run(); !errors.Is(err, want) {
		t.Fatalf("Run = %v, want %v", err, want)
	}
}

func TestMouseCapabilityForwardingAndUnsupported(t *testing.T) {
	events, err := input.NewClick(2, 3, input.ButtonLeft, nil).Expand()
	if err != nil {
		t.Fatal(err)
	}
	wantResult := vt.MouseEncodingResult{
		Bytes: []byte("encoded"), ReportCount: 2,
		State: vt.MouseState{
			Enabled: true, Tracking: vt.MouseTrackingNormal, TrackingKnown: true,
			Format: vt.MouseFormatSGR, FormatKnown: true,
		},
		Size: vt.Size{Cols: 10, Rows: 5},
	}
	model := &fakeMouseModel{result: wantResult, state: wantResult.State}
	p := New(model, bytes.NewReader(nil))

	got, err := p.EncodeMouse(events)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes, wantResult.Bytes) || got.ReportCount != 2 ||
		got.Size != wantResult.Size {
		t.Fatalf("result = %#v", got)
	}
	if len(model.events) != len(events) || model.events[0] != events[0] {
		t.Fatalf("forwarded events = %#v", model.events)
	}
	state, err := p.MouseState()
	if err != nil {
		t.Fatal(err)
	}
	if state != wantResult.State {
		t.Fatalf("state = %#v, want %#v", state, wantResult.State)
	}

	unsupported := New(&noMouseModel{}, bytes.NewReader(nil))
	if _, err := unsupported.EncodeMouse(events); !errors.Is(err, vt.ErrMouseUnsupportedBackend) {
		t.Fatalf("EncodeMouse unsupported error = %v", err)
	}
	if _, err := unsupported.MouseState(); !errors.Is(err, vt.ErrMouseUnsupportedBackend) {
		t.Fatalf("MouseState unsupported error = %v", err)
	}
}

func TestEncodeMouseSerializedWithModelMutation(t *testing.T) {
	model := &fakeMouseModel{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	p := New(model, bytes.NewReader(nil))
	events, err := input.NewClick(1, 1, input.ButtonLeft, nil).Expand()
	if err != nil {
		t.Fatal(err)
	}

	encodeDone := make(chan error, 1)
	go func() {
		_, encodeErr := p.EncodeMouse(events)
		encodeDone <- encodeErr
	}()
	select {
	case <-model.entered:
	case <-time.After(time.Second):
		t.Fatal("EncodeMouse did not enter model")
	}

	resizeDone := make(chan error, 1)
	go func() { resizeDone <- p.Resize(20, 10) }()
	select {
	case err := <-resizeDone:
		t.Fatalf("Resize completed while EncodeMouse held pump mutex: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(model.release)
	if err := <-encodeDone; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-resizeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Resize did not continue after EncodeMouse")
	}
}

type errReader struct {
	err error
}

func (r errReader) Read([]byte) (int, error) {
	return 0, r.err
}

type noMouseModel struct{}

func (*noMouseModel) Feed([]byte) error     { return nil }
func (*noMouseModel) Resize(int, int) error { return nil }
func (*noMouseModel) Snapshot() vt.Snapshot { return vt.Snapshot{} }

type fakeMouseModel struct {
	events  []input.MouseEvent
	result  vt.MouseEncodingResult
	state   vt.MouseState
	entered chan struct{}
	release chan struct{}
}

func (*fakeMouseModel) Feed([]byte) error     { return nil }
func (*fakeMouseModel) Resize(int, int) error { return nil }
func (*fakeMouseModel) Snapshot() vt.Snapshot {
	return vt.Snapshot{}
}
func (m *fakeMouseModel) EncodeMouse(events []input.MouseEvent) (vt.MouseEncodingResult, error) {
	m.events = append([]input.MouseEvent(nil), events...)
	if m.entered != nil {
		close(m.entered)
		<-m.release
	}
	return m.result, nil
}
func (m *fakeMouseModel) MouseState() (vt.MouseState, error) {
	return m.state, nil
}
