package play

import (
	"bytes"
	"errors"
	"image"
	"os"
	"strings"
	"testing"
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
	defer in.Close()
	outPath := t.TempDir() + "/terminal.out"
	out, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
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
