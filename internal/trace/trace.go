// Package trace writes a .twee trace bundle: a zip archive containing
// manifest.json and events.jsonl.
package trace

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Manifest is the top-level metadata written to manifest.json inside
// the zip bundle.
type Manifest struct {
	Version   int               `json:"version"`
	Command   []string          `json:"command"`
	Env       map[string]string `json:"env,omitempty"`
	Cols      int               `json:"cols"`
	Rows      int               `json:"rows"`
	Pid       int               `json:"pid"`
	Host      HostInfo          `json:"host"`
	StartedAt time.Time         `json:"started_at"`
	StoppedAt time.Time         `json:"stopped_at"`
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

// event is the JSONL shape stored in events.jsonl inside a .twee bundle.
type event struct {
	TMS   int64  `json:"t_ms"`
	Type  string `json:"type"`
	Bytes string `json:"bytes_b64,omitempty"`
	Kind  string `json:"kind,omitempty"`
	Key   string `json:"key,omitempty"`
	Cols  int    `json:"cols,omitempty"`
	Rows  int    `json:"rows,omitempty"`
	Code  int    `json:"code,omitempty"`
}

// Trace streams session artifacts into a temporary work directory and
// writes a .twee zip bundle when Close is called.
type Trace struct {
	mu      sync.Mutex
	path    string
	workDir string
	man     Manifest

	eventsPath string
	eventsFile *os.File
	evEnc      *json.Encoder

	start  time.Time
	closed bool
	err    error
}

// New creates a Trace that will be written to path on Close.
// The manifest's StartedAt is set to time.Now(); Version is forced to 1.
func New(path string, m Manifest) (*Trace, error) {
	if path == "" {
		return nil, errors.New("trace: empty output path")
	}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return nil, fmt.Errorf("trace: output path is a directory: %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	workDir, err := os.MkdirTemp(filepath.Dir(path), ".twee-trace-*")
	if err != nil {
		return nil, err
	}
	eventsPath := filepath.Join(workDir, "events.jsonl")
	eventsFile, err := os.Create(eventsPath)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return nil, err
	}
	now := time.Now()
	m.Version = 1
	m.StartedAt = now
	m.Host = DefaultHostInfo()
	tr := &Trace{
		path:       path,
		workDir:    workDir,
		man:        m,
		eventsPath: eventsPath,
		eventsFile: eventsFile,
		start:      now,
	}
	tr.evEnc = json.NewEncoder(eventsFile)
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
	if tr.closed {
		return
	}
	if err := tr.evEnc.Encode(event{
		TMS:   tr.ms(ts),
		Type:  "output",
		Bytes: base64.StdEncoding.EncodeToString(b),
	}); err != nil && tr.err == nil {
		tr.err = err
	}
}

// WriteInput records an input event (type, key, paste).
func (tr *Trace) WriteInput(kind, key string, b []byte) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.closed {
		return
	}
	if err := tr.evEnc.Encode(event{
		TMS:   tr.ms(time.Now()),
		Type:  "input",
		Kind:  kind,
		Key:   key,
		Bytes: base64.StdEncoding.EncodeToString(b),
	}); err != nil && tr.err == nil {
		tr.err = err
	}
}

// WriteResize records a terminal resize.
func (tr *Trace) WriteResize(cols, rows int) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.closed {
		return
	}
	if err := tr.evEnc.Encode(event{
		TMS:  tr.ms(time.Now()),
		Type: "resize",
		Cols: cols,
		Rows: rows,
	}); err != nil && tr.err == nil {
		tr.err = err
	}
}

// WriteExit records the process exit code.
func (tr *Trace) WriteExit(code int) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.closed {
		return
	}
	if err := tr.evEnc.Encode(event{
		TMS:  tr.ms(time.Now()),
		Type: "exit",
		Code: code,
	}); err != nil && tr.err == nil {
		tr.err = err
	}
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
	defer func() {
		_ = os.RemoveAll(tr.workDir)
	}()
	if err := tr.eventsFile.Close(); err != nil && tr.err == nil {
		tr.err = err
	}
	if tr.err != nil {
		return tr.err
	}
	tr.man.StoppedAt = time.Now()

	zipPath := filepath.Join(tr.workDir, "bundle.twee")
	f, err := os.Create(zipPath)
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
	events, err := os.Open(tr.eventsPath)
	if err != nil {
		_ = f.Close()
		return err
	}
	if _, err := io.Copy(ew, events); err != nil {
		_ = events.Close()
		_ = f.Close()
		return err
	}
	if err := events.Close(); err != nil {
		_ = f.Close()
		return err
	}

	if err := zw.Close(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(zipPath, tr.path)
}
