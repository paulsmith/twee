package trace

// EventType identifies the kind of record stored in events.jsonl.
type EventType string

const (
	EventTypeOutput EventType = "output"
	EventTypeInput  EventType = "input"
	EventTypeResize EventType = "resize"
	EventTypeExit   EventType = "exit"
)

// IsKnown reports whether t is an event type written by twee.
func (t EventType) IsKnown() bool {
	switch t {
	case EventTypeOutput, EventTypeInput, EventTypeResize, EventTypeExit:
		return true
	default:
		return false
	}
}

// InputKind identifies the source of bytes recorded by an input event.
type InputKind string

const (
	InputKindType          InputKind = "type"
	InputKindKey           InputKind = "key"
	InputKindPaste         InputKind = "paste"
	InputKindMouse         InputKind = "mouse"
	InputKindUnknown       InputKind = "unknown"
	InputKindTerminalReply InputKind = "terminal_reply"
)

// EventRecord is the JSONL shape stored in events.jsonl inside a .twee
// bundle. Bytes contains the base64-encoded event payload.
type EventRecord struct {
	TMS   int64       `json:"t_ms"`
	Type  EventType   `json:"type"`
	Bytes string      `json:"bytes_b64,omitempty"`
	Kind  InputKind   `json:"kind,omitempty"`
	Key   string      `json:"key,omitempty"`
	Cols  int         `json:"cols,omitempty"`
	Rows  int         `json:"rows,omitempty"`
	Code  int         `json:"code,omitempty"`
	Mouse *MouseInput `json:"mouse,omitempty"`
}
