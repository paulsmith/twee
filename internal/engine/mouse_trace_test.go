package engine

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/paulsmith/twee/internal/input"
	"github.com/paulsmith/twee/internal/trace"
)

func TestActiveTraceRecordsOneExactEventPerMouseGesture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mouse.twee")
	term := startTracedMouseTerm(t, path)

	if err := term.Click(
		0, 1,
		input.ButtonRight,
		[]input.MouseModifier{input.ModifierCtrl, input.ModifierShift},
	); err != nil {
		t.Fatalf("Click: %v", err)
	}
	if err := term.Hover(2, 0, []input.MouseModifier{input.ModifierAlt}); err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if err := term.Scroll(
		3, 1,
		input.ScrollDown,
		2,
		[]input.MouseModifier{input.ModifierShift},
	); err != nil {
		t.Fatalf("Scroll: %v", err)
	}
	if err := term.Drag(
		0, 0, 2, 0,
		input.ButtonRight,
		[]input.MouseModifier{input.ModifierCtrl},
	); err != nil {
		t.Fatalf("Drag: %v", err)
	}
	if err := term.DisableTrace(); err != nil {
		t.Fatalf("DisableTrace: %v", err)
	}

	events := readMouseTraceEvents(t, path)
	want := []mouseTraceEvent{
		{
			Bytes: []byte("\x1b[<22;1;2M\x1b[<22;1;2m"),
			Mouse: trace.MouseInput{
				Gesture:   "click",
				X:         new(0),
				Y:         new(1),
				Button:    "right",
				Modifiers: []string{"shift", "ctrl"},
			},
		},
		{
			Bytes: []byte("\x1b[<43;3;1M"),
			Mouse: trace.MouseInput{
				Gesture:   "hover",
				X:         new(2),
				Y:         new(0),
				Modifiers: []string{"alt"},
			},
		},
		{
			Bytes: []byte("\x1b[<69;4;2M\x1b[<69;4;2M"),
			Mouse: trace.MouseInput{
				Gesture:   "scroll",
				X:         new(3),
				Y:         new(1),
				Modifiers: []string{"shift"},
				Direction: "down",
				Ticks:     2,
			},
		},
		{
			Bytes: []byte(
				"\x1b[<18;1;1M\x1b[<50;2;1M\x1b[<50;3;1M\x1b[<18;3;1m",
			),
			Mouse: trace.MouseInput{
				Gesture:   "drag",
				FromX:     new(0),
				FromY:     new(0),
				ToX:       new(2),
				ToY:       new(0),
				Button:    "right",
				Modifiers: []string{"ctrl"},
			},
		},
	}
	if len(events) != len(want) {
		t.Fatalf("mouse trace events = %d, want %d: %#v", len(events), len(want), events)
	}
	for i, wantEvent := range want {
		if !reflect.DeepEqual(events[i].Mouse, wantEvent.Mouse) {
			t.Errorf("event %d mouse = %#v, want %#v", i, events[i].Mouse, wantEvent.Mouse)
		}
		if !bytes.Equal(events[i].Bytes, wantEvent.Bytes) {
			t.Errorf("event %d bytes = %q, want %q", i, events[i].Bytes, wantEvent.Bytes)
		}
	}
	if got := len(term.RecentInputs()); got != len(want) {
		t.Fatalf("mouse diagnostics = %d, want %d", got, len(want))
	}
}

func TestFindClickTraceRecordsDecisionAndExactBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "find-click.twee")
	term := startTracedTerm(t, path, "printf '\\033[?1003h\\033[?1006hREADY Submit'; sleep 30")
	result, err := term.FindClick("Submit", false, "", input.ButtonRight, []input.MouseModifier{input.ModifierCtrl})
	if err != nil {
		t.Fatal(err)
	}
	if err := term.DisableTrace(); err != nil {
		t.Fatal(err)
	}
	events := readMouseTraceEvents(t, path)
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	got := events[0]
	if !bytes.Equal(got.Bytes, []byte("\x1b[<18;9;1M\x1b[<18;9;1m")) {
		t.Fatalf("bytes = %q", got.Bytes)
	}
	decision := got.Mouse.FindClick
	if decision == nil || decision.Pattern != "Submit" || decision.Regex || decision.Selection != "exactly_one" ||
		decision.Match.X != result.Match.X || decision.Target.X != result.Target.X {
		t.Fatalf("decision = %+v; result = %+v", decision, result)
	}
}

func TestFailedMouseGestureRecordsNoTraceOrDiagnostic(t *testing.T) {
	t.Run("invalid argument", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "invalid-mouse.twee")
		term := startTracedMouseTerm(t, path)
		if err := term.Drag(
			0, 0, int(^uint(0)>>1), 0,
			input.ButtonLeft, nil,
		); err == nil {
			t.Fatal("out-of-range drag unexpectedly succeeded")
		}
		assertNoMouseBookkeeping(t, term, path)
	})

	t.Run("failed precondition", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "disabled-mouse.twee")
		term := startTracedTerm(t, path, "printf READY; sleep 30")
		if err := term.Click(0, 0, input.ButtonLeft, nil); err == nil {
			t.Fatal("click with tracking disabled unexpectedly succeeded")
		}
		assertNoMouseBookkeeping(t, term, path)
	})
}

func assertNoMouseBookkeeping(t *testing.T, term *Term, path string) {
	t.Helper()
	if got := len(term.RecentInputs()); got != 0 {
		t.Fatalf("failed mouse diagnostics = %d, want 0", got)
	}
	if err := term.DisableTrace(); err != nil {
		t.Fatalf("DisableTrace: %v", err)
	}
	if events := readMouseTraceEvents(t, path); len(events) != 0 {
		t.Fatalf("failed mouse trace events = %#v, want none", events)
	}
}

func startTracedMouseTerm(t *testing.T, path string) *Term {
	t.Helper()
	return startTracedTerm(
		t, path,
		"printf '\\033[?1003h\\033[?1006hREADY'; sleep 30",
	)
}

func startTracedTerm(t *testing.T, path, script string) *Term {
	t.Helper()
	term, err := Start(context.Background(), Config{
		Cmd:  []string{"/bin/sh", "-c", script},
		Cols: 40,
		Rows: 5,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })
	if err := term.EnableTrace(path); err != nil {
		t.Fatalf("EnableTrace: %v", err)
	}
	if err := term.WaitForText("READY"); err != nil {
		t.Fatalf("WaitForText READY: %v", err)
	}
	return term
}

type mouseTraceEvent struct {
	Bytes []byte
	Mouse trace.MouseInput
}

func readMouseTraceEvents(t *testing.T, path string) []mouseTraceEvent {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open trace: %v", err)
	}
	defer func() { _ = zr.Close() }()
	eventsFile, err := zr.Open("events.jsonl")
	if err != nil {
		t.Fatalf("open events.jsonl: %v", err)
	}
	defer func() { _ = eventsFile.Close() }()

	var events []mouseTraceEvent
	scanner := bufio.NewScanner(eventsFile)
	for scanner.Scan() {
		var event struct {
			Type  string            `json:"type"`
			Kind  string            `json:"kind"`
			Bytes string            `json:"bytes_b64"`
			Mouse *trace.MouseInput `json:"mouse"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode trace event: %v", err)
		}
		if event.Type != "input" || event.Kind != "mouse" {
			continue
		}
		if event.Mouse == nil {
			t.Fatal("mouse trace event has no metadata")
		}
		bytes, err := base64.StdEncoding.DecodeString(event.Bytes)
		if err != nil {
			t.Fatalf("decode mouse bytes: %v", err)
		}
		events = append(events, mouseTraceEvent{Bytes: bytes, Mouse: *event.Mouse})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan trace events: %v", err)
	}
	return events
}
