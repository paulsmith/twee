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

// MouseInput describes one high-level mouse gesture. Coordinates are pointers
// because zero is a valid cell while an omitted coordinate belongs to a
// different gesture shape (for example, click uses X/Y while drag uses
// FromX/FromY/ToX/ToY).
type MouseInput struct {
	Gesture   string   `json:"gesture"`
	X         *int     `json:"x,omitempty"`
	Y         *int     `json:"y,omitempty"`
	FromX     *int     `json:"from_x,omitempty"`
	FromY     *int     `json:"from_y,omitempty"`
	ToX       *int     `json:"to_x,omitempty"`
	ToY       *int     `json:"to_y,omitempty"`
	Button    string   `json:"button,omitempty"`
	Modifiers []string `json:"modifiers"`
	Direction string   `json:"direction,omitempty"`
	Ticks     int      `json:"ticks,omitempty"`
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
	TMS   int64       `json:"t_ms"`
	Type  string      `json:"type"`
	Bytes string      `json:"bytes_b64,omitempty"`
	Kind  string      `json:"kind,omitempty"`
	Key   string      `json:"key,omitempty"`
	Cols  int         `json:"cols,omitempty"`
	Rows  int         `json:"rows,omitempty"`
	Code  int         `json:"code,omitempty"`
	Mouse *MouseInput `json:"mouse,omitempty"`
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

	start     time.Time
	closed    bool
	err       error
	removeAll func(string) error
}

// New creates a Trace that will be written to path on Close.
// The manifest's StartedAt is set to time.Now(); Version is forced to 1.
func New(path string, m Manifest) (*Trace, error) {
	return newWithFS(path, m, defaultTraceFS())
}

func defaultTraceFS() traceFS {
	return traceFS{
		mkdirTemp: os.MkdirTemp,
		chmod:     os.Chmod,
		openFile:  os.OpenFile,
		chmodFile: func(f *os.File, mode os.FileMode) error { return f.Chmod(mode) },
		closeFile: func(f *os.File) error { return f.Close() },
		removeAll: os.RemoveAll,
	}
}

type traceFS struct {
	mkdirTemp func(string, string) (string, error)
	chmod     func(string, os.FileMode) error
	openFile  func(string, int, os.FileMode) (*os.File, error)
	chmodFile func(*os.File, os.FileMode) error
	closeFile func(*os.File) error
	removeAll func(string) error
}

func newWithFS(path string, m Manifest, fsys traceFS) (*Trace, error) {
	if path == "" {
		return nil, errors.New("trace: empty output path")
	}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return nil, fmt.Errorf("trace: output path is a directory: %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	workDir, err := fsys.mkdirTemp(filepath.Dir(path), ".twee-trace-*")
	if err != nil {
		return nil, err
	}
	if err := fsys.chmod(workDir, 0o700); err != nil {
		return nil, errors.Join(err, cleanupError(fsys.removeAll(workDir)))
	}
	eventsPath := filepath.Join(workDir, "events.jsonl")
	eventsFile, err := fsys.openFile(eventsPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, errors.Join(err, cleanupError(fsys.removeAll(workDir)))
	}
	if err := fsys.chmodFile(eventsFile, 0o600); err != nil {
		return nil, errors.Join(err,
			cleanupStepError("close private trace events file", fsys.closeFile(eventsFile)),
			cleanupError(fsys.removeAll(workDir)))
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
		removeAll:  os.RemoveAll,
	}
	tr.evEnc = json.NewEncoder(eventsFile)
	return tr, nil
}

func cleanupError(err error) error {
	return cleanupStepError("remove private trace work directory", err)
}

func cleanupStepError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", action, err)
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

// WriteMouseInput records one successfully encoded high-level mouse gesture.
// The encoded reports stay attached to the single semantic event so playback
// and export can annotate the gesture without feeding the bytes to the VT.
func (tr *Trace) WriteMouseInput(mouse MouseInput, b []byte) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.closed {
		return
	}
	mouse.Modifiers = append([]string{}, mouse.Modifiers...)
	if err := tr.evEnc.Encode(event{
		TMS:   tr.ms(time.Now()),
		Type:  "input",
		Kind:  "mouse",
		Bytes: base64.StdEncoding.EncodeToString(b),
		Mouse: &mouse,
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

func (tr *Trace) writeLocked() (err error) {
	defer func() {
		if cleanupErr := tr.removeAll(tr.workDir); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove private trace work directory: %w", cleanupErr))
		}
	}()
	if err := tr.eventsFile.Close(); err != nil && tr.err == nil {
		tr.err = err
	}
	if tr.err != nil {
		return tr.err
	}
	tr.man.StoppedAt = time.Now()

	zipPath := filepath.Join(tr.workDir, "bundle.twee")
	f, err := os.OpenFile(zipPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
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
