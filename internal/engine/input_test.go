package engine

import (
	"bytes"
	"errors"
	"io"
	"maps"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/input"
)

func TestWriteAllHandlesShortWrites(t *testing.T) {
	w := &shortWriter{max: 2}
	if err := writeAll(w, []byte("abcdefg")); err != nil {
		t.Fatalf("writeAll: %v", err)
	}
	if got := w.buf.String(); got != "abcdefg" {
		t.Fatalf("written bytes = %q, want %q", got, "abcdefg")
	}
	if w.calls != 4 {
		t.Fatalf("Write calls = %d, want 4", w.calls)
	}
}

func TestWriteAllRejectsNoProgress(t *testing.T) {
	err := writeAll(zeroWriter{}, []byte("x"))
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("writeAll error = %v, want io.ErrShortWrite", err)
	}
}

func TestWriteAllReturnsWriterError(t *testing.T) {
	want := errors.New("write failed")
	err := writeAll(errorWriter{err: want}, []byte("x"))
	if !errors.Is(err, want) {
		t.Fatalf("writeAll error = %v, want %v", err, want)
	}
}

func TestConcurrentInputsAndResize(t *testing.T) {
	term := startEngineTerm(t, []string{
		"/bin/sh", "-c",
		"printf '\\033[?1003h\\033[?1006hREADY'; sleep 30",
	}, 40, 5)
	if err := term.WaitForText("READY"); err != nil {
		t.Fatalf("WaitForText READY: %v", err)
	}

	const iterations = 10
	errs := make(chan error, 8*iterations)
	var wg sync.WaitGroup
	run := func(operation func(int) error) {
		wg.Go(func() {
			for i := range iterations {
				if err := operation(i); err != nil {
					errs <- err
					return
				}
			}
		})
	}
	run(func(int) error { return term.Type("x") })
	run(func(int) error { return term.Key(input.KeyTab) })
	run(func(int) error { return term.Paste("p") })
	run(func(int) error { return term.Click(0, 0, input.ButtonLeft, nil) })
	run(func(int) error { return term.Hover(0, 0, nil) })
	run(func(int) error { return term.Scroll(0, 0, input.ScrollDown, 1, nil) })
	run(func(int) error { return term.Drag(0, 0, 1, 0, input.ButtonRight, nil) })
	run(func(i int) error { return term.Resize(40+i%2, 5) })
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent input: %v", err)
	}
}

func TestConcurrentLogicalInputsRemainContiguous(t *testing.T) {
	term := startEngineTerm(t, []string{
		"/bin/sh", "-c",
		"printf '\\033[?1003h\\033[?1006hREADY'; sleep 30",
	}, 40, 5)
	if err := term.WaitForText("READY"); err != nil {
		t.Fatalf("WaitForText READY: %v", err)
	}

	writer := newBlockingShortWriter()
	term.inputWriter = writer

	type operation struct {
		name string
		run  func() error
		want []byte
	}
	first := operation{
		name: "type",
		run:  func() error { return term.Type("TYPE-FIRST") },
		want: []byte("TYPE-FIRST"),
	}
	operations := []operation{
		{
			name: "key",
			run:  func() error { return term.Key(input.KeyTab) },
			want: input.Encode(input.KeyTab),
		},
		{
			name: "paste",
			run:  func() error { return term.Paste("PASTE-BODY") },
			want: input.EncodePaste("PASTE-BODY"),
		},
		{
			name: "click",
			run:  func() error { return term.Click(0, 0, input.ButtonLeft, nil) },
			want: []byte("\x1b[<0;1;1M\x1b[<0;1;1m"),
		},
		{
			name: "hover",
			run:  func() error { return term.Hover(1, 0, nil) },
			want: []byte("\x1b[<35;2;1M"),
		},
		{
			name: "scroll",
			run: func() error {
				return term.Scroll(2, 0, input.ScrollDown, 1, nil)
			},
			want: []byte("\x1b[<65;3;1M"),
		},
		{
			name: "drag",
			run: func() error {
				return term.Drag(0, 1, 1, 1, input.ButtonRight, nil)
			},
			want: []byte("\x1b[<2;1;2M\x1b[<34;2;2M\x1b[<2;2;2m"),
		},
	}

	type result struct {
		name string
		err  error
	}
	results := make(chan result, len(operations)+1)
	go func() { results <- result{first.name, first.run()} }()
	<-writer.entered

	launched := make(chan struct{}, len(operations))
	for _, op := range operations {
		go func() {
			launched <- struct{}{}
			results <- result{op.name, op.run()}
		}()
	}
	for range operations {
		<-launched
	}

	resizeStarted := make(chan struct{})
	resizeDone := make(chan error, 1)
	go func() {
		close(resizeStarted)
		resizeDone <- term.Resize(41, 5)
	}()
	<-resizeStarted

	// The first Type is blocked in a one-byte Write while holding inputMu.
	// A method that bypasses inputMu enters the writer concurrently; Resize
	// instead completes while the input is still only partially written.
	overlappedBeforeRelease := false
	select {
	case <-writer.overlap:
		overlappedBeforeRelease = true
	case <-time.After(100 * time.Millisecond):
	}
	resizedBeforeRelease := false
	var resizeErr error
	select {
	case resizeErr = <-resizeDone:
		resizedBeforeRelease = true
	default:
	}

	close(writer.release)
	for range len(operations) + 1 {
		got := <-results
		if got.err != nil {
			t.Errorf("%s: %v", got.name, got.err)
		}
	}
	if !resizedBeforeRelease {
		resizeErr = <-resizeDone
	}
	if resizeErr != nil {
		t.Errorf("resize: %v", resizeErr)
	}
	if overlappedBeforeRelease {
		t.Error("a second logical input entered the writer before the first completed")
	}
	if resizedBeforeRelease {
		t.Error("Resize completed while a logical input write was incomplete")
	}
	select {
	case <-writer.overlap:
		t.Error("logical input Write calls overlapped")
	default:
	}

	wantWrites := map[string][]byte{first.name: first.want}
	for _, op := range operations {
		wantWrites[op.name] = op.want
	}
	assertContiguousLogicalWrites(t, writer.bytes(), wantWrites)
}

func assertContiguousLogicalWrites(t *testing.T, got []byte, want map[string][]byte) {
	t.Helper()
	remaining := make(map[string][]byte, len(want))
	maps.Copy(remaining, want)
	for len(got) > 0 {
		matched := ""
		for name, b := range remaining {
			if bytes.HasPrefix(got, b) {
				matched = name
				got = got[len(b):]
				break
			}
		}
		if matched == "" {
			t.Fatalf(
				"input stream is not a concatenation of complete logical writes; "+
					"next bytes %q, remaining operations %v",
				got, remaining,
			)
		}
		delete(remaining, matched)
	}
	if len(remaining) != 0 {
		t.Fatalf("input stream omitted logical writes: %v", remaining)
	}
}

type blockingShortWriter struct {
	first       sync.Once
	overlapOnce sync.Once
	active      atomic.Int32
	entered     chan struct{}
	release     chan struct{}
	overlap     chan struct{}

	mu  sync.Mutex
	buf bytes.Buffer
}

func newBlockingShortWriter() *blockingShortWriter {
	return &blockingShortWriter{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		overlap: make(chan struct{}),
	}
}

func (w *blockingShortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if w.active.Add(1) > 1 {
		w.overlapOnce.Do(func() { close(w.overlap) })
	}
	defer w.active.Add(-1)

	w.first.Do(func() {
		close(w.entered)
		<-w.release
	})
	runtime.Gosched()

	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p[:1])
}

func (w *blockingShortWriter) bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return bytes.Clone(w.buf.Bytes())
}

type shortWriter struct {
	max   int
	calls int
	buf   bytes.Buffer
}

func (w *shortWriter) Write(p []byte) (int, error) {
	w.calls++
	if len(p) > w.max {
		p = p[:w.max]
	}
	return w.buf.Write(p)
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }
