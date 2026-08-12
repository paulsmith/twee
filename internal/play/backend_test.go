package play

import (
	"bytes"
	"errors"
	"image"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestValidBackend(t *testing.T) {
	for _, backend := range []Backend{BackendAuto, BackendKitty, BackendITerm2, BackendSixel} {
		if !ValidBackend(backend) {
			t.Fatalf("ValidBackend(%q) = false", backend)
		}
	}
	if ValidBackend("bogus") {
		t.Fatal("ValidBackend(bogus) = true")
	}
}

func TestNewFrameSinkRequiresSixelPixelGeometry(t *testing.T) {
	_, err := newFrameSink(BackendSixel, ioDiscard{}, 80, displayPixels{})
	if err == nil || !strings.Contains(err.Error(), "pixel geometry") {
		t.Fatalf("error = %v, want pixel-geometry diagnostic", err)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

type lifecycleSink struct {
	emitErr error
	panic   bool
	closed  bool
}

type capturePlaybackSink struct {
	fakeSink
	closed bool
}

type resizeCaptureSink struct {
	frames chan frameRecord
	sizes  chan terminalSize
	closed bool
}

type pixelResizeTermOps struct {
	fakeTermOps
	pixelWidth, pixelHeight int
}

func (o *pixelResizeTermOps) GetPixelSize(*os.File) (int, int, error) {
	return o.pixelWidth, o.pixelHeight, nil
}

func (s *resizeCaptureSink) Emit(img *image.RGBA, cols, rows int, toast, status string) error {
	s.frames <- frameRecord{
		cols: cols, rows: rows, toast: toast, status: status, size: img.Bounds(), img: img,
	}
	return nil
}

func (s *resizeCaptureSink) SetTerminalSize(cols, rows int) {
	s.sizes <- terminalSize{Cols: cols, Rows: rows}
}

func (s *resizeCaptureSink) Close() error {
	s.closed = true
	return nil
}

func (s *capturePlaybackSink) Close() error {
	s.closed = true
	return nil
}

func (s *lifecycleSink) Emit(*image.RGBA, int, int, string, string) error {
	if s.panic {
		panic("sink panic")
	}
	return s.emitErr
}

func (s *lifecycleSink) Close() error {
	s.closed = true
	return nil
}

func TestRunRestoresTerminalAndClosesSinkOnEmitError(t *testing.T) {
	sink := &lifecycleSink{emitErr: errors.New("emit failed")}
	outPath := runWithLifecycleSink(t, sink, false)
	if !sink.closed {
		t.Fatal("sink was not closed")
	}
	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("\x1b[?25h\x1b[?1049l")) {
		t.Fatalf("terminal restore missing from %q", out)
	}
}

func TestRunRestoresTerminalAndClosesSinkOnPanic(t *testing.T) {
	sink := &lifecycleSink{panic: true}
	outPath := runWithLifecycleSink(t, sink, true)
	if !sink.closed {
		t.Fatal("sink was not closed")
	}
	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("\x1b[?25h\x1b[?1049l")) {
		t.Fatalf("terminal restore missing from %q", out)
	}
}

func TestRunUsesPreflightTerminalSizeToFitRecording(t *testing.T) {
	path := writeTestBundle(t, map[string]string{
		"manifest.json": `{"version":1,"cols":100,"rows":40}`,
		"events.jsonl":  "",
	})
	in, reply, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	if _, err := io.WriteString(reply, "\x1b_Gi=31;OK\x1b\\"); err != nil {
		t.Fatal(err)
	}
	if err := reply.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := os.CreateTemp(t.TempDir(), "terminal-*.out")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Close() }()

	termOps := &fakeTermOps{isTTY: true, width: 80, height: 24}
	sink := &capturePlaybackSink{}
	err = Run(path, Options{
		Backend:           BackendKitty,
		DisplayPixelWidth: 800, DisplayPixelHeight: 480,
		Stdin: in, Stdout: out, Stderr: io.Discard,
		SkipRaw: true, sink: sink, termOps: termOps,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if termOps.sizeCalls != 1 {
		t.Fatalf("terminal size calls = %d, want 1", termOps.sizeCalls)
	}
	if len(sink.frames) == 0 {
		t.Fatal("Run emitted no frames")
	}
	frame := sink.frames[len(sink.frames)-1]
	if frame.cols != 55 || frame.rows != 22 {
		t.Fatalf("placement = %dx%d, want 55x22", frame.cols, frame.rows)
	}
	if frame.size.Dx() != 550 || frame.size.Dy() != 440 {
		t.Fatalf("frame size = %dx%d, want 550x440", frame.size.Dx(), frame.size.Dy())
	}
	if !sink.closed {
		t.Fatal("sink was not closed")
	}
}

func TestRunRescalesPlaybackOnSIGWINCH(t *testing.T) {
	path := writeTestBundle(t, map[string]string{
		"manifest.json": `{"version":1,"cols":100,"rows":40}`,
		"events.jsonl":  "",
	})
	in, input, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.CreateTemp(t.TempDir(), "terminal-*.out")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Close() }()

	sink := &resizeCaptureSink{
		frames: make(chan frameRecord, 2),
		sizes:  make(chan terminalSize, 1),
	}
	termOps := &pixelResizeTermOps{
		fakeTermOps: fakeTermOps{width: 80, height: 24},
		pixelWidth:  640, pixelHeight: 360,
	}
	resizeSignals := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(path, Options{
			Backend:           BackendKitty,
			DisplayPixelWidth: 1000, DisplayPixelHeight: 840,
			Stdin: in, Stdout: out, Stderr: io.Discard,
			SkipPreflight: true, SkipRaw: true, sink: sink,
			termOps: termOps, resizeSignals: resizeSignals,
		})
	}()

	initial := receiveFrame(t, sink.frames)
	if initial.cols != 100 || initial.rows != 40 {
		t.Fatalf("initial placement = %dx%d, want 100x40", initial.cols, initial.rows)
	}
	resizeSignals <- syscall.SIGWINCH
	select {
	case size := <-sink.sizes:
		if size != (terminalSize{Cols: 80, Rows: 24}) {
			t.Fatalf("sink terminal size = %+v, want 80x24", size)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sink resize")
	}
	frame := receiveFrame(t, sink.frames)
	if frame.cols != 55 || frame.rows != 22 {
		t.Fatalf("resized placement = %dx%d, want 55x22", frame.cols, frame.rows)
	}
	if got := frame.size; got.Dx() != 440 || got.Dy() != 330 {
		t.Fatalf("resized frame = %dx%d, want 440x330", got.Dx(), got.Dy())
	}

	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for playback to stop")
	}
	if !sink.closed {
		t.Fatal("sink was not closed")
	}
}

func receiveFrame(t *testing.T, frames <-chan frameRecord) frameRecord {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for playback frame")
		return frameRecord{}
	}
}

func runWithLifecycleSink(t *testing.T, sink *lifecycleSink, wantPanic bool) string {
	t.Helper()
	path := writeTestBundle(t, map[string]string{
		"manifest.json": `{"version":1,"cols":2,"rows":1}`,
		"events.jsonl":  "",
	})
	in, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	outPath := t.TempDir() + "/terminal.out"
	out, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Close() }()
	panicked := false
	func() {
		defer func() { panicked = recover() != nil }()
		err = Run(path, Options{
			Stdin: in, Stdout: out, Stderr: ioDiscard{},
			SkipPreflight: true, SkipRaw: true, sink: sink,
		})
	}()
	if panicked != wantPanic {
		t.Fatalf("panicked = %v, want %v", panicked, wantPanic)
	}
	if !wantPanic && (err == nil || !strings.Contains(err.Error(), "emit failed")) {
		t.Fatalf("Run error = %v, want emit failure", err)
	}
	if err := out.Sync(); err != nil {
		t.Fatal(err)
	}
	return outPath
}
