package pump

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
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

type chunkReader struct {
	chunks <-chan []byte
}

func (r chunkReader) Read(p []byte) (int, error) {
	chunk, ok := <-r.chunks
	if !ok {
		return 0, io.EOF
	}
	copy(p, chunk)
	return len(chunk), nil
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

	go func() { _ = p.Run() }()

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
		{"wrapped eof", fmt.Errorf("read: %w", io.EOF), true},
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

type resizeOrderModel struct {
	cols, rows int
	events     []string
}

func (m *resizeOrderModel) Feed([]byte) error {
	m.events = append(m.events, fmt.Sprintf("feed@%dx%d", m.cols, m.rows))
	return nil
}

func (m *resizeOrderModel) Resize(cols, rows int) error {
	m.cols, m.rows = cols, rows
	m.events = append(m.events, "model")
	return nil
}

func (m *resizeOrderModel) Snapshot() vt.Snapshot { return vt.Snapshot{} }

func TestCommitResizeOrdersSignalOutputAfterModelCommit(t *testing.T) {
	chunks := make(chan []byte)
	model := &resizeOrderModel{cols: 5, rows: 2}
	p := New(model, chunkReader{chunks: chunks})
	runDone := make(chan error, 1)
	go func() { runDone <- p.Run() }()

	if err := p.CommitResize(8, 3, func() error {
		chunks <- []byte("signal output")
		return nil
	}, func() {
		model.events = append(model.events, "committed")
	}); err != nil {
		t.Fatalf("CommitResize: %v", err)
	}
	close(chunks)
	if err := <-runDone; err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"model", "committed", "feed@8x3"}
	if !slices.Equal(model.events, want) {
		t.Fatalf("events = %v, want %v", model.events, want)
	}
}

func TestCommitResizeOrdersOutputHookBeforeResizeBookkeeping(t *testing.T) {
	chunks := make(chan []byte)
	p := New(vt.New(5, 2), chunkReader{chunks: chunks})
	hookEntered := make(chan struct{})
	hookRelease := make(chan struct{})
	outputDone := make(chan struct{})
	p.SetOutputHook(func([]byte, time.Time) {
		close(hookEntered)
		<-hookRelease
		close(outputDone)
	})
	runDone := make(chan error, 1)
	go func() { runDone <- p.Run() }()

	chunks <- []byte("before resize")
	<-hookEntered
	resizeStarted := make(chan struct{})
	resizeCommitted := make(chan struct{})
	resizeDone := make(chan error, 1)
	go func() {
		close(resizeStarted)
		resizeDone <- p.CommitResize(8, 3, nil, func() { close(resizeCommitted) })
	}()
	<-resizeStarted
	select {
	case <-resizeCommitted:
		t.Fatal("resize bookkeeping overtook the preceding output hook")
	case <-time.After(100 * time.Millisecond):
	}

	close(hookRelease)
	<-outputDone
	if err := <-resizeDone; err != nil {
		t.Fatalf("CommitResize: %v", err)
	}
	close(chunks)
	if err := <-runDone; err != nil {
		t.Fatalf("Run: %v", err)
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

func TestEncodeMouseSnapshotRepaintSerializationStress(t *testing.T) {
	p, chunks := startedChunkPump(t)
	sendChunk(t, chunks, []byte("\x1b[?1006h"))
	for i := range 50 {
		oldText, newText := fmt.Sprintf("OLD%02d", i), fmt.Sprintf("NEW%02d", i)
		sendChunk(t, chunks, []byte("\r\x1b[?1003h"+oldText))
		waitForText(t, p, oldText)

		entered := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			_, err := p.EncodeMouseSnapshot(func(s vt.Snapshot) ([]input.MouseEvent, error) {
				got := vt.VisibleText(s)
				if !strings.Contains(got, oldText) || strings.Contains(got, newText) {
					return nil, fmt.Errorf("callback snapshot = %q", got)
				}
				close(entered)
				<-release
				return input.NewClick(0, 0, input.ButtonLeft, nil).Expand()
			})
			done <- err
		}()
		<-entered
		// This repaint also disables tracking. Encoding must still use the
		// enabled state observed with OLD because the repaint is queued on mu.
		sendChunk(t, chunks, []byte("\r\x1b[?1003l"+newText))
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		waitForText(t, p, newText)
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

func TestWaitStableExceptIgnoresExcludedChanges(t *testing.T) {
	p, chunks := startedChunkPump(t)
	sendChunk(t, chunks, []byte("\x1b[1;1Hready\x1b[2;1H-"))
	waitForText(t, p, "ready")

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- p.WaitStableExcept(context.Background(), 50*time.Millisecond, 500*time.Millisecond, []Rect{{X: 0, Y: 1, W: 1, H: 1}})
	}()
	for _, spinner := range []byte("1234") {
		time.Sleep(15 * time.Millisecond)
		sendChunk(t, chunks, []byte("\x1b[2;1H"+string(spinner)))
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WaitStableExcept: %v", err)
		}
		if elapsed := time.Since(start); elapsed >= 110*time.Millisecond {
			t.Fatalf("WaitStableExcept returned after %v; excluded updates reset the quiet window", elapsed)
		}
	case <-time.After(110 * time.Millisecond):
		t.Fatal("WaitStableExcept did not ignore excluded updates")
	}
}

func TestWaitStableExceptWaitsForUnexcludedChanges(t *testing.T) {
	p, chunks := startedChunkPump(t)
	sendChunk(t, chunks, []byte("\x1b[1;1Hready\x1b[2;1H-"))
	waitForText(t, p, "ready")

	done := make(chan error, 1)
	go func() {
		done <- p.WaitStableExcept(context.Background(), 50*time.Millisecond, 500*time.Millisecond, []Rect{{X: 0, Y: 1, W: 1, H: 1}})
	}()
	time.Sleep(20 * time.Millisecond)
	sendChunk(t, chunks, []byte("\x1b[1;1Hchanged"))
	select {
	case err := <-done:
		t.Fatalf("WaitStableExcept returned early after unexcluded update: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WaitStableExcept: %v", err)
		}
	case <-time.After(150 * time.Millisecond):
		t.Fatal("WaitStableExcept did not become stable")
	}
}

func TestWaitStableExceptContextAndClosed(t *testing.T) {
	p := New(vt.New(5, 2), bytes.NewReader(nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.WaitStableExcept(ctx, time.Second, time.Second, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitStableExcept canceled = %v, want context canceled", err)
	}

	closed := New(vt.New(5, 2), bytes.NewReader(nil))
	if err := closed.Run(); err != nil {
		t.Fatalf("Run closed: %v", err)
	}
	if err := closed.WaitStableExcept(context.Background(), time.Second, time.Second, nil); err != nil {
		t.Fatalf("WaitStableExcept closed: %v", err)
	}
}

func TestWaitStableExceptResizeResetsQuietWindow(t *testing.T) {
	p, chunks := startedChunkPump(t)
	sendChunk(t, chunks, []byte("ready"))
	waitForText(t, p, "ready")

	done := make(chan error, 1)
	go func() {
		done <- p.WaitStableExcept(context.Background(), 70*time.Millisecond, 500*time.Millisecond, nil)
	}()
	time.Sleep(20 * time.Millisecond)
	if err := p.Resize(21, 3); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("WaitStableExcept returned early after resize: %v", err)
	case <-time.After(40 * time.Millisecond):
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WaitStableExcept: %v", err)
		}
	case <-time.After(150 * time.Millisecond):
		t.Fatal("WaitStableExcept did not become stable after resize")
	}
}

func TestSameOutsideClipsExcludedRectsToViewport(t *testing.T) {
	base := vt.Snapshot{
		Size:  vt.Size{Cols: 2, Rows: 1},
		Lines: []vt.Line{{Cells: []vt.Cell{{Text: "a"}, {Text: "b"}}}},
	}
	changed := base
	changed.Lines = []vt.Line{{Cells: []vt.Cell{{Text: "x"}, {Text: "b"}}}}
	if !sameOutside(base, changed, []Rect{{X: 0, Y: 0, W: 999, H: 999}}) {
		t.Fatal("a rectangle wider and taller than the viewport did not clip")
	}
	if sameOutside(base, changed, []Rect{{X: 2, Y: 0, W: 1, H: 1}}) {
		t.Fatal("a rectangle outside the viewport excluded a visible change")
	}
}

func TestSameOutsideCursorExclusionBoundaries(t *testing.T) {
	base := vt.Snapshot{Size: vt.Size{Cols: 2, Rows: 1}, Cursor: vt.Cursor{Col: 0, Row: 0, Visible: true}}
	inside := base
	inside.Cursor.Visible = false
	if !sameOutside(base, inside, []Rect{{X: 0, Y: 0, W: 1, H: 1}}) {
		t.Fatal("cursor change inside excluded rectangle was visible")
	}
	outside := base
	outside.Cursor.Col = 1
	if sameOutside(base, outside, []Rect{{X: 0, Y: 0, W: 1, H: 1}}) {
		t.Fatal("cursor change across excluded rectangle boundary was hidden")
	}
}

func startedChunkPump(t *testing.T) (*Pump, chan<- []byte) {
	t.Helper()
	chunks := make(chan []byte)
	p := New(vt.New(20, 3), chunkReader{chunks: chunks})
	t.Cleanup(func() { close(chunks) })
	go func() { _ = p.Run() }()
	return p, chunks
}

func sendChunk(t *testing.T, chunks chan<- []byte, chunk []byte) {
	t.Helper()
	select {
	case chunks <- chunk:
	case <-time.After(time.Second):
		t.Fatal("pump did not read chunk")
	}
}

func waitForText(t *testing.T, p *Pump, text string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(vt.VisibleText(p.Snapshot()), text) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("pump never rendered %q", text)
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

func TestPresentationAndKeyEncodingShareModelState(t *testing.T) {
	model := &fakePresentationModel{presentation: vt.Presentation{Input: vt.InputModes{KittyKeyboardKnown: true}}}
	p := New(model, bytes.NewReader(nil))

	got, err := p.EncodeKey(input.KeyUp)
	if err != nil || !bytes.Equal(got, []byte("\x1b[A")) {
		t.Fatalf("normal Up = %q, %v; want CSI", got, err)
	}
	model.presentation.Input.ApplicationCursor = true
	got, err = p.EncodeKey(input.KeyUp)
	if err != nil || !bytes.Equal(got, []byte("\x1bOA")) {
		t.Fatalf("application Up = %q, %v; want SS3", got, err)
	}
	presentation, err := p.Presentation()
	if err != nil || !presentation.Input.ApplicationCursor {
		t.Fatalf("Presentation = %+v, %v; want DECCKM enabled", presentation, err)
	}

	unsupported := New(&noMouseModel{}, bytes.NewReader(nil))
	if _, err := unsupported.Presentation(); !errors.Is(err, ErrPresentationUnavailable) {
		t.Fatalf("unsupported Presentation error = %v, want %v", err, ErrPresentationUnavailable)
	}
	modeErr := errors.New("mode unavailable")
	model.err = modeErr
	if _, err := p.Presentation(); !errors.Is(err, modeErr) {
		t.Fatalf("Presentation mode error = %v, want %v", err, modeErr)
	}
	if _, err := p.EncodeKey(input.KeyUp); !errors.Is(err, modeErr) {
		t.Fatalf("EncodeKey mode error = %v, want %v", err, modeErr)
	}
	model.err = nil
	model.presentation.Input.KittyKeyboardFlags = 1
	if _, err := p.EncodeKey(input.KeyUp); !errors.Is(err, ErrKittyKeyboardUnsupported) {
		t.Fatalf("active Kitty EncodeKey error = %v, want %v", err, ErrKittyKeyboardUnsupported)
	}
	model.presentation.Input.KittyKeyboardKnown = false
	if _, err := p.EncodeKey(input.KeyUp); !errors.Is(err, ErrKittyKeyboardStateUnknown) {
		t.Fatalf("unknown Kitty EncodeKey error = %v, want %v", err, ErrKittyKeyboardStateUnknown)
	}
}

type fakePresentationModel struct {
	presentation vt.Presentation
	err          error
}

func (*fakePresentationModel) Feed([]byte) error     { return nil }
func (*fakePresentationModel) Resize(int, int) error { return nil }
func (*fakePresentationModel) Snapshot() vt.Snapshot { return vt.Snapshot{} }
func (m *fakePresentationModel) Presentation() (vt.Presentation, error) {
	return m.presentation, m.err
}

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
