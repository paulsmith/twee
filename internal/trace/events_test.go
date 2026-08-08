package trace

import (
	"encoding/json"
	"testing"
)

func TestEventTypeVocabulary(t *testing.T) {
	types := []EventType{EventTypeOutput, EventTypeInput, EventTypeResize, EventTypeExit}
	want := []string{"output", "input", "resize", "exit"}
	for i, typ := range types {
		if string(typ) != want[i] {
			t.Errorf("event type %d = %q, want %q", i, typ, want[i])
		}
		if !typ.IsKnown() {
			t.Errorf("event type %q is not known", typ)
		}
	}
	if EventType("teleport").IsKnown() {
		t.Fatal("unknown event type is known")
	}
}

func TestInputKindVocabulary(t *testing.T) {
	kinds := []InputKind{
		InputKindType,
		InputKindKey,
		InputKindPaste,
		InputKindMouse,
		InputKindUnknown,
		InputKindTerminalReply,
	}
	want := []string{"type", "key", "paste", "mouse", "unknown", "terminal_reply"}
	for i, kind := range kinds {
		if string(kind) != want[i] {
			t.Errorf("input kind %d = %q, want %q", i, kind, want[i])
		}
	}
}

func TestEventRecordJSONShape(t *testing.T) {
	b, err := json.Marshal(EventRecord{
		TMS:   12,
		Type:  EventTypeInput,
		Bytes: "eA==",
		Kind:  InputKindKey,
		Key:   "Enter",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"t_ms":12,"type":"input","bytes_b64":"eA==","kind":"key","key":"Enter"}`
	if string(b) != want {
		t.Fatalf("event record = %s, want %s", b, want)
	}
}
