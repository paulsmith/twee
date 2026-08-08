package play

import (
	"errors"
	"testing"

	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/vt"
)

type screenEventModel struct {
	fed         []byte
	feedErr     error
	resizeErr   error
	resizeCalls int
	resizeCols  int
	resizeRows  int
}

func (m *screenEventModel) Feed(p []byte) error {
	m.fed = append(m.fed, p...)
	return m.feedErr
}

func (m *screenEventModel) Resize(cols, rows int) error {
	m.resizeCalls++
	m.resizeCols, m.resizeRows = cols, rows
	return m.resizeErr
}

func (*screenEventModel) Snapshot() vt.Snapshot { return vt.Snapshot{} }

func TestApplyScreenEvent(t *testing.T) {
	t.Run("output", func(t *testing.T) {
		wantErr := errors.New("feed")
		model := &screenEventModel{feedErr: wantErr}
		screen, err := ApplyScreenEvent(model, Event{Type: trace.EventTypeOutput, Bytes: []byte("hello")})
		if !screen || !errors.Is(err, wantErr) || string(model.fed) != "hello" {
			t.Fatalf("ApplyScreenEvent = %v, %v, fed %q; want true, feed error, hello", screen, err, model.fed)
		}
	})

	t.Run("valid resize", func(t *testing.T) {
		wantErr := errors.New("resize")
		model := &screenEventModel{resizeErr: wantErr}
		screen, err := ApplyScreenEvent(model, Event{Type: trace.EventTypeResize, Cols: 100, Rows: 30})
		if !screen || !errors.Is(err, wantErr) || model.resizeCalls != 1 || model.resizeCols != 100 || model.resizeRows != 30 {
			t.Fatalf("ApplyScreenEvent = %v, %v, resize %d times to %dx%d; want true, resize error, once to 100x30", screen, err, model.resizeCalls, model.resizeCols, model.resizeRows)
		}
	})

	for _, tc := range []struct {
		name  string
		event Event
	}{
		{name: "zero columns", event: Event{Type: trace.EventTypeResize, Rows: 30}},
		{name: "zero rows", event: Event{Type: trace.EventTypeResize, Cols: 100}},
		{name: "negative columns", event: Event{Type: trace.EventTypeResize, Cols: -1, Rows: 30}},
		{name: "negative rows", event: Event{Type: trace.EventTypeResize, Cols: 100, Rows: -1}},
		{name: "input", event: Event{Type: trace.EventTypeInput, Bytes: []byte("ignored")}},
		{name: "exit", event: Event{Type: trace.EventTypeExit}},
		{name: "unknown", event: Event{Type: trace.EventType("unknown")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &screenEventModel{}
			screen, err := ApplyScreenEvent(model, tc.event)
			if screen || err != nil || len(model.fed) != 0 || model.resizeCalls != 0 {
				t.Fatalf("ApplyScreenEvent = %v, %v, fed %q, resize calls %d; want no-op", screen, err, model.fed, model.resizeCalls)
			}
		})
	}
}
