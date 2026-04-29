// Package recording writes a JSONL session recording.
//
// Format: one JSON header line, then one JSON event per line.
// Event types: "output", "input", "resize", "exit".
package recording

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Header is the first line of a recording.
type Header struct {
	Version int               `json:"version"`
	Command []string          `json:"command"`
	Cols    int               `json:"cols"`
	Rows    int               `json:"rows"`
	Env     map[string]string `json:"env,omitempty"`
	Started time.Time         `json:"started"`
}

// Event is a recorded event.
type Event struct {
	TMS    int64  `json:"t_ms"`
	Type   string `json:"type"`
	Bytes  string `json:"bytes_b64,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Key    string `json:"key,omitempty"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
	Code   int    `json:"code,omitempty"`
}

// Recorder writes recording events to a file.
type Recorder struct {
	mu    sync.Mutex
	f     *os.File
	enc   *json.Encoder
	start time.Time
}

// New creates a recorder, writing the header immediately.
func New(path string, h Header) (*Recorder, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	r := &Recorder{f: f, enc: json.NewEncoder(f), start: time.Now()}
	h.Version = 1
	h.Started = r.start
	if err := r.enc.Encode(h); err != nil {
		f.Close()
		return nil, err
	}
	return r, nil
}

func (r *Recorder) ms(ts time.Time) int64 {
	if ts.IsZero() {
		ts = time.Now()
	}
	return ts.Sub(r.start).Milliseconds()
}

// WriteOutput records output bytes.
func (r *Recorder) WriteOutput(b []byte, ts time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.enc.Encode(Event{
		TMS:   r.ms(ts),
		Type:  "output",
		Bytes: base64.StdEncoding.EncodeToString(b),
	})
}

// WriteInput records an input event.
func (r *Recorder) WriteInput(kind, key string, b []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.enc.Encode(Event{
		TMS:   r.ms(time.Now()),
		Type:  "input",
		Kind:  kind,
		Key:   key,
		Bytes: base64.StdEncoding.EncodeToString(b),
	})
}

// WriteResize records a resize.
func (r *Recorder) WriteResize(cols, rows int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.enc.Encode(Event{
		TMS:  r.ms(time.Now()),
		Type: "resize",
		Cols: cols,
		Rows: rows,
	})
}

// WriteExit records the process exit.
func (r *Recorder) WriteExit(code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.enc.Encode(Event{
		TMS:  r.ms(time.Now()),
		Type: "exit",
		Code: code,
	})
}

// Close closes the underlying file.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}
