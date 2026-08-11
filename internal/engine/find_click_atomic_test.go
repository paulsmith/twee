package engine

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/input"
	"github.com/paulsmith/twee/internal/pump"
	"github.com/paulsmith/twee/internal/vt"
)

func TestFindClickKeepsMatchTargetAndEncodingOnOneRepaintingState(t *testing.T) {
	base := vt.New(20, 2)
	if err := base.Feed([]byte("\x1b[?1003h\x1b[?1006hOLD Submit")); err != nil {
		t.Fatal(err)
	}
	model := &blockingSnapshotMouseModel{
		Model: base, entered: make(chan struct{}), release: make(chan struct{}),
	}
	reader, writer := io.Pipe()
	p := pump.New(model, reader)
	pumpDone := make(chan error, 1)
	go func() { pumpDone <- p.Run() }()
	t.Cleanup(func() {
		select {
		case <-model.release:
		default:
			close(model.release)
		}
		_ = writer.Close()
		<-pumpDone
	})

	var encoded bytes.Buffer
	term := &Term{pump: p, inputWriter: &encoded}
	type outcome struct {
		result FindClickResult
		err    error
	}
	clicked := make(chan outcome, 1)
	go func() {
		result, err := term.FindClick("Submit", false, "", input.ButtonLeft, nil)
		clicked <- outcome{result: result, err: err}
	}()
	<-model.entered

	// Pipe.Write returns after Pump.Run has read the repaint. Pump.Run then
	// blocks on pump.mu, which FindClick still owns through snapshot selection
	// and encoding.
	repaintRead := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte("\r\x1b[2KNEW Cancel"))
		repaintRead <- err
	}()
	if err := <-repaintRead; err != nil {
		t.Fatal(err)
	}
	close(model.release)

	got := <-clicked
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.result.Match.Text != "Submit" || got.result.Match.X != 4 || got.result.Target.X != 6 {
		t.Fatalf("result crossed repaint generations: %+v", got.result)
	}
	if want := "\x1b[<0;7;1M\x1b[<0;7;1m"; encoded.String() != want {
		t.Fatalf("encoded = %q, want old-generation target %q", encoded.String(), want)
	}

	deadline := time.Now().Add(time.Second)
	for !strings.Contains(vt.VisibleText(p.Snapshot()), "NEW Cancel") {
		if time.Now().After(deadline) {
			t.Fatal("queued repaint was not applied after FindClick released pump.mu")
		}
		time.Sleep(time.Millisecond)
	}
}

type blockingSnapshotMouseModel struct {
	vt.Model
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (m *blockingSnapshotMouseModel) Snapshot() vt.Snapshot {
	snapshot := m.Model.Snapshot()
	m.once.Do(func() {
		close(m.entered)
		<-m.release
	})
	return snapshot
}

func (m *blockingSnapshotMouseModel) EncodeMouse(events []input.MouseEvent) (vt.MouseEncodingResult, error) {
	return m.Model.(vt.MouseModel).EncodeMouse(events)
}

func (m *blockingSnapshotMouseModel) MouseState() (vt.MouseState, error) {
	return m.Model.(vt.MouseModel).MouseState()
}
