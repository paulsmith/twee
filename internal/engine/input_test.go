package engine

import (
	"bytes"
	"errors"
	"io"
	"testing"
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
