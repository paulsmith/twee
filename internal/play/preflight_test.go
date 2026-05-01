package play

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"golang.org/x/term"
)

type fakeTermOps struct {
	isTTY          bool
	width, height  int
	raws, restores int
}

func (f *fakeTermOps) IsTerminal(int) bool { return f.isTTY }
func (f *fakeTermOps) GetSize(int) (int, int, error) {
	return f.width, f.height, nil
}
func (f *fakeTermOps) MakeRaw(int) (*term.State, error) {
	f.raws++
	return nil, nil
}
func (f *fakeTermOps) Restore(int, *term.State) error {
	f.restores++
	return nil
}

func TestPreflightRejectsNonTTY(t *testing.T) {
	err := preflightBundle(Bundle{MaxCols: 10, MaxRows: 3}, preflightOptions{
		Term: &fakeTermOps{isTTY: false, width: 80, height: 24},
		In:   strings.NewReader(""),
		Out:  io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "non-tty") {
		t.Fatalf("error = %v, want non-tty", err)
	}
}

func TestPreflightRejectsSmallTerminal(t *testing.T) {
	err := preflightBundle(Bundle{MaxCols: 100, MaxRows: 40}, preflightOptions{
		Term: &fakeTermOps{isTTY: true, width: 80, height: 24},
		In:   strings.NewReader(""),
		Out:  io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "terminal is 80x24; trace needs at least 100x42") {
		t.Fatalf("error = %v, want size mismatch", err)
	}
}

func TestQueryKittyAcceptsOKReply(t *testing.T) {
	termOps := &fakeTermOps{isTTY: true}
	var out bytes.Buffer
	err := queryKitty(preflightOptions{
		Term:    termOps,
		In:      strings.NewReader("\x1b_Gi=31;OK\x1b\\"),
		Out:     &out,
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("queryKitty: %v", err)
	}
	if out.String() != kittyQuery {
		t.Fatalf("query = %q, want %q", out.String(), kittyQuery)
	}
	if termOps.raws != 1 || termOps.restores != 1 {
		t.Fatalf("raw/restores = %d/%d, want 1/1", termOps.raws, termOps.restores)
	}
}

func TestQueryKittyRejectsGarbledReply(t *testing.T) {
	err := queryKitty(preflightOptions{
		Term:    &fakeTermOps{isTTY: true},
		In:      strings.NewReader("\x1b_Gi=31;NOPE\x1b\\"),
		Out:     io.Discard,
		Timeout: time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "kitty graphics protocol not detected") {
		t.Fatalf("error = %v, want kitty diagnostic", err)
	}
}

func TestQueryKittyTimeout(t *testing.T) {
	r := &blockingReader{ch: make(chan struct{})}
	defer close(r.ch)
	err := queryKitty(preflightOptions{
		Term:    &fakeTermOps{isTTY: true},
		In:      r,
		Out:     io.Discard,
		Timeout: time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "kitty graphics protocol not detected") {
		t.Fatalf("error = %v, want kitty diagnostic", err)
	}
}

type blockingReader struct {
	ch chan struct{}
}

func (r *blockingReader) Read([]byte) (int, error) {
	<-r.ch
	return 0, io.EOF
}
