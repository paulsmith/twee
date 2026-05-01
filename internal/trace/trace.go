// Package trace writes a .twee trace bundle — a zip archive containing
// a manifest, JSONL event stream, and PNG screenshots.
package trace

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
)

// Manifest is the top-level metadata written to manifest.json inside
// the zip bundle.
type Manifest struct {
	Version     int               `json:"version"`
	Command     []string          `json:"command"`
	Env         map[string]string `json:"env,omitempty"`
	Cols        int               `json:"cols"`
	Rows        int               `json:"rows"`
	Pid         int               `json:"pid"`
	Host        HostInfo          `json:"host"`
	StartedAt   time.Time         `json:"started_at"`
	StoppedAt   time.Time         `json:"stopped_at"`
	Screenshots []string          `json:"screenshots"`
}

// HostInfo captures details about the machine that recorded the trace.
type HostInfo struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Hostname string `json:"hostname"`
}

// DefaultHostInfo returns HostInfo populated from the current machine.
func DefaultHostInfo() HostInfo {
	h, _ := os.Hostname()
	return HostInfo{
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Hostname: h,
	}
}

// event mirrors recording.Event so that the trace package does not
// import internal/recording.
type event struct {
	TMS   int64  `json:"t_ms"`
	Type  string `json:"type"`
	Bytes string `json:"bytes_b64,omitempty"`
	Kind  string `json:"kind,omitempty"`
	Key   string `json:"key,omitempty"`
	Cols  int    `json:"cols,omitempty"`
	Rows  int    `json:"rows,omitempty"`
}

// Trace accumulates session artifacts in memory and writes a .twee zip
// bundle when Close is called.
type Trace struct {
	mu   sync.Mutex
	path string
	man  Manifest

	events      bytes.Buffer
	evEnc       *json.Encoder
	screenshots [][]byte // PNG-encoded

	start  time.Time
	closed bool
	err    error
}

// New creates a Trace that will be written to path on Close.
// The manifest's StartedAt is set to time.Now(); Version is forced to 1.
func New(path string, m Manifest) (*Trace, error) {
	now := time.Now()
	m.Version = 1
	m.StartedAt = now
	m.Host = DefaultHostInfo()
	tr := &Trace{
		path:  path,
		man:   m,
		start: now,
	}
	tr.evEnc = json.NewEncoder(&tr.events)
	return tr, nil
}

func (tr *Trace) ms(ts time.Time) int64 {
	if ts.IsZero() {
		ts = time.Now()
	}
	return ts.Sub(tr.start).Milliseconds()
}

// WriteOutput records raw PTY output bytes.
func (tr *Trace) WriteOutput(b []byte, ts time.Time) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	_ = tr.evEnc.Encode(event{
		TMS:   tr.ms(ts),
		Type:  "output",
		Bytes: base64.StdEncoding.EncodeToString(b),
	})
}

// WriteInput records an input event (type, key, paste).
func (tr *Trace) WriteInput(kind, key string, b []byte) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	_ = tr.evEnc.Encode(event{
		TMS:   tr.ms(time.Now()),
		Type:  "input",
		Kind:  kind,
		Key:   key,
		Bytes: base64.StdEncoding.EncodeToString(b),
	})
}

// WriteResize records a terminal resize.
func (tr *Trace) WriteResize(cols, rows int) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	_ = tr.evEnc.Encode(event{
		TMS:  tr.ms(time.Now()),
		Type: "resize",
		Cols: cols,
		Rows: rows,
	})
}

// AddScreenshotPNG stores a pre-encoded PNG screenshot. The caller is
// responsible for rendering the snapshot to PNG before calling this.
func (tr *Trace) AddScreenshotPNG(pngData []byte) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.screenshots = append(tr.screenshots, append([]byte(nil), pngData...))
}

// Close finalises the trace, writing the zip bundle to disk. It is
// idempotent — the second and subsequent calls return the error (if
// any) from the first call.
func (tr *Trace) Close() error {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.closed {
		return tr.err
	}
	tr.closed = true
	tr.err = tr.writeLocked()
	return tr.err
}

func (tr *Trace) writeLocked() error {
	tr.man.StoppedAt = time.Now()

	// Build screenshot manifest paths.
	tr.man.Screenshots = make([]string, len(tr.screenshots))
	for i := range tr.screenshots {
		tr.man.Screenshots[i] = fmt.Sprintf("screenshots/%04d.png", i)
	}

	f, err := os.Create(tr.path)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)

	// manifest.json
	mw, err := zw.Create("manifest.json")
	if err != nil {
		_ = f.Close()
		return err
	}
	enc := json.NewEncoder(mw)
	enc.SetIndent("", "  ")
	if err := enc.Encode(tr.man); err != nil {
		_ = f.Close()
		return err
	}

	// events.jsonl
	ew, err := zw.Create("events.jsonl")
	if err != nil {
		_ = f.Close()
		return err
	}
	if _, err := ew.Write(tr.events.Bytes()); err != nil {
		_ = f.Close()
		return err
	}

	// screenshots
	for i, png := range tr.screenshots {
		sw, err := zw.Create(fmt.Sprintf("screenshots/%04d.png", i))
		if err != nil {
			_ = f.Close()
			return err
		}
		if _, err := sw.Write(png); err != nil {
			_ = f.Close()
			return err
		}
	}

	if err := zw.Close(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
