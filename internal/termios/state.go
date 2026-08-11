// Package termios captures portable summaries and platform-qualified raw
// snapshots of a child pseudo-terminal's terminal attributes.
package termios

import "runtime"

const (
	StatusCaptured    = "captured"
	StatusUnavailable = "unavailable"
	StatusUnsupported = "unsupported"
)

// Record contains the child PTY state at trace start and, when child exit is
// observed before the trace closes, at child exit.
type Record struct {
	SchemaVersion int       `json:"schema_version"`
	Platform      string    `json:"platform"`
	Start         Snapshot  `json:"start"`
	Exit          *Snapshot `json:"exit,omitempty"`
}

// Snapshot is a best-effort termios capture. Capture failures are metadata and
// never make process startup or trace finalization fail.
type Snapshot struct {
	Status string `json:"status"`
	State  *State `json:"state,omitempty"`
	Error  string `json:"error,omitempty"`
}

// State exposes common terminal modes directly and retains the exact native
// values under Raw for platform-specific diagnosis.
type State struct {
	Canonical         bool `json:"canonical"`
	Echo              bool `json:"echo"`
	Signals           bool `json:"signals"`
	ExtendedInput     bool `json:"extended_input"`
	InputFlowControl  bool `json:"input_flow_control"`
	OutputFlowControl bool `json:"output_flow_control"`
	OutputProcessing  bool `json:"output_processing"`
	MapNLToCRNL       bool `json:"map_nl_to_crnl"`
	Raw               Raw  `json:"raw"`
}

// Raw contains the native termios masks, control-character array, and speeds.
// Its interpretation is qualified by Record.Platform.
type Raw struct {
	InputFlags   uint64 `json:"input_flags"`
	OutputFlags  uint64 `json:"output_flags"`
	ControlFlags uint64 `json:"control_flags"`
	LocalFlags   uint64 `json:"local_flags"`
	ControlChars []int  `json:"control_chars"`
	InputSpeed   uint64 `json:"input_speed"`
	OutputSpeed  uint64 `json:"output_speed"`
}

// NewRecord creates trace metadata from a trace-start snapshot.
func NewRecord(start Snapshot) *Record {
	return &Record{SchemaVersion: 1, Platform: runtime.GOOS, Start: CloneSnapshot(start)}
}

// CloneRecord returns a deep copy of record.
func CloneRecord(record *Record) *Record {
	if record == nil {
		return nil
	}
	clone := *record
	clone.Start = CloneSnapshot(record.Start)
	if record.Exit != nil {
		exit := CloneSnapshot(*record.Exit)
		clone.Exit = &exit
	}
	return &clone
}

// CloneSnapshot returns a deep copy suitable for crossing package boundaries.
func CloneSnapshot(snapshot Snapshot) Snapshot {
	if snapshot.State == nil {
		return snapshot
	}
	state := *snapshot.State
	state.Raw.ControlChars = append([]int(nil), snapshot.State.Raw.ControlChars...)
	snapshot.State = &state
	return snapshot
}
