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
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/paulsmith/twee/internal/tracearchive"
	"github.com/paulsmith/twee/internal/tracepolicy"
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
	Network   *NetworkCapture   `json:"network_capture,omitempty"`
}

type NetworkCapture struct {
	Format        string   `json:"format"`
	Stream        string   `json:"stream"`
	GVisorVersion string   `json:"gvisor_version"`
	PublishTCP    []string `json:"publish_tcp,omitempty"`
	ByteLimit     int64    `json:"byte_limit"`
	CapturedBytes int64    `json:"captured_bytes"`
	PacketCount   int64    `json:"packet_count"`
	Truncated     bool     `json:"truncated"`
	Status        string   `json:"status"`
}

const (
	NetworkCaptureFormat = tracepolicy.NetworkCaptureFormat
	NetworkCaptureStream = tracepolicy.NetworkCaptureStream

	NetworkCaptureStatusComplete  = tracepolicy.NetworkCaptureStatusComplete
	NetworkCaptureStatusTruncated = tracepolicy.NetworkCaptureStatusTruncated
)

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
	Gesture   string             `json:"gesture"`
	X         *int               `json:"x,omitempty"`
	Y         *int               `json:"y,omitempty"`
	FromX     *int               `json:"from_x,omitempty"`
	FromY     *int               `json:"from_y,omitempty"`
	ToX       *int               `json:"to_x,omitempty"`
	ToY       *int               `json:"to_y,omitempty"`
	Button    string             `json:"button,omitempty"`
	Modifiers []string           `json:"modifiers"`
	Direction string             `json:"direction,omitempty"`
	Ticks     int                `json:"ticks,omitempty"`
	FindClick *FindClickDecision `json:"find_click,omitempty"`
}

// FindClickDecision records the terminal-state decision behind an atomic
// pattern click. The containing mouse event carries the exact encoded bytes.
type FindClickDecision struct {
	Pattern   string     `json:"pattern"`
	Regex     bool       `json:"regex"`
	Selection string     `json:"selection"`
	Match     TraceMatch `json:"match"`
	Target    TracePoint `json:"target"`
}

type TraceMatch struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type TracePoint struct {
	X int `json:"x"`
	Y int `json:"y"`
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

	start       time.Time
	closed      bool
	err         error
	removeAll   func(string) error
	attachments []attachment
	attached    map[string]struct{}
}

type attachment struct{ source, name string }

// AttachFile adds a staged file to the bundle when Close is called. The three
// format-owned entry names are reserved; callers must use
// AttachNetworkCapture for the network stream.
func (tr *Trace) AttachFile(source, name string) error {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.closed {
		return errors.New("trace: already closed")
	}
	if source == "" || !fs.ValidPath(name) {
		return errors.New("trace: invalid attachment")
	}
	if isReservedEntry(name) {
		return fmt.Errorf("trace: attachment name %q is reserved", name)
	}
	if _, exists := tr.attached[name]; exists {
		return fmt.Errorf("trace: attachment name %q is already attached", name)
	}
	tr.attachments = append(tr.attachments, attachment{source: source, name: name})
	tr.attached[name] = struct{}{}
	return nil
}

// AttachNetworkCapture adds the format-owned network stream and its completed
// capture metadata. It must be called only after the capture writer has
// stopped, so CapturedBytes and Status describe the durable file.
func (tr *Trace) AttachNetworkCapture(source string, capture NetworkCapture) error {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.closed {
		return errors.New("trace: already closed")
	}
	if source == "" {
		return errors.New("trace: network capture source is empty")
	}
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("trace: stat network capture: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("trace: network capture source is not a regular file")
	}
	if capture.Format != NetworkCaptureFormat {
		return fmt.Errorf("trace: network capture format %q is unsupported", capture.Format)
	}
	if capture.Stream != NetworkCaptureStream {
		return fmt.Errorf("trace: network capture stream must be %q", NetworkCaptureStream)
	}
	if capture.GVisorVersion == "" {
		return errors.New("trace: network capture gVisor version is empty")
	}
	if capture.ByteLimit <= 0 || capture.ByteLimit > tracepolicy.MaxNetworkCaptureBytes {
		return fmt.Errorf("trace: network capture byte limit must be in 1..%d", tracepolicy.MaxNetworkCaptureBytes)
	}
	if capture.CapturedBytes < 0 || capture.CapturedBytes > capture.ByteLimit {
		return fmt.Errorf("trace: network capture size %d is outside byte limit %d", capture.CapturedBytes, capture.ByteLimit)
	}
	if capture.CapturedBytes != info.Size() {
		return fmt.Errorf("trace: network capture size %d does not match staged file size %d", capture.CapturedBytes, info.Size())
	}
	pcapInfo, err := tracearchive.ValidatePCAPFile(source)
	if err != nil {
		return fmt.Errorf("trace: invalid network capture: %w", err)
	}
	if capture.PacketCount < 0 || capture.PacketCount != pcapInfo.Packets {
		return fmt.Errorf("trace: network capture packet count %d does not match staged file count %d", capture.PacketCount, pcapInfo.Packets)
	}
	wantStatus := NetworkCaptureStatusComplete
	if capture.Truncated {
		wantStatus = NetworkCaptureStatusTruncated
	}
	if capture.Status != wantStatus {
		return fmt.Errorf("trace: network capture status %q is inconsistent with truncated=%t", capture.Status, capture.Truncated)
	}
	if _, exists := tr.attached[NetworkCaptureStream]; exists {
		return errors.New("trace: network capture is already attached")
	}
	capture.PublishTCP = slices.Clone(capture.PublishTCP)
	tr.man.Network = &capture
	tr.attachments = append(tr.attachments, attachment{source: source, name: NetworkCaptureStream})
	tr.attached[NetworkCaptureStream] = struct{}{}
	return nil
}

// Abort closes and removes trace staging without publishing a bundle. The
// cause is retained as Close's idempotent result.
func (tr *Trace) Abort(cause error) error {
	if cause == nil {
		cause = errors.New("trace: aborted")
	}
	tr.mu.Lock()
	if !tr.closed {
		tr.err = errors.Join(tr.err, cause)
	}
	tr.mu.Unlock()
	return tr.Close()
}

func isReservedEntry(name string) bool {
	switch name {
	case "manifest.json", "events.jsonl", NetworkCaptureStream:
		return true
	default:
		return false
	}
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
	if m.Network != nil {
		return nil, errors.New("trace: network capture metadata must be added with AttachNetworkCapture")
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
		attached:   make(map[string]struct{}),
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
	if err := tr.evEnc.Encode(EventRecord{
		TMS:   tr.ms(ts),
		Type:  EventTypeOutput,
		Bytes: base64.StdEncoding.EncodeToString(b),
	}); err != nil && tr.err == nil {
		tr.err = err
	}
}

// WriteInput records an input event.
func (tr *Trace) WriteInput(kind InputKind, key string, b []byte) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.closed {
		return
	}
	if err := tr.evEnc.Encode(EventRecord{
		TMS:   tr.ms(time.Now()),
		Type:  EventTypeInput,
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
	if err := tr.evEnc.Encode(EventRecord{
		TMS:   tr.ms(time.Now()),
		Type:  EventTypeInput,
		Kind:  InputKindMouse,
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
	if err := tr.evEnc.Encode(EventRecord{
		TMS:  tr.ms(time.Now()),
		Type: EventTypeResize,
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
	if err := tr.evEnc.Encode(EventRecord{
		TMS:  tr.ms(time.Now()),
		Type: EventTypeExit,
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

	for _, item := range tr.attachments {
		aw, err := zw.Create(item.name)
		if err != nil {
			_ = f.Close()
			return err
		}
		af, err := os.Open(item.source)
		if err != nil {
			_ = f.Close()
			return err
		}
		_, copyErr := io.Copy(aw, af)
		closeErr := af.Close()
		if copyErr != nil {
			_ = f.Close()
			return copyErr
		}
		if closeErr != nil {
			_ = f.Close()
			return closeErr
		}
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
