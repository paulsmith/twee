TURN 1
TOOL CALL: bash
INPUT:
{"command": "find . -type f -name \"*.go\" | sort | head -80"}
TOOL RESULT: bash
OUTPUT:
./cmd/libghostty-smoke/main.go
./cmd/twee/cli_e2e_test.go
./cmd/twee/client.go
./cmd/twee/cmd_diff.go
./cmd/twee/cmd_input.go
./cmd/twee/cmd_ls.go
./cmd/twee/cmd_query.go
./cmd/twee/cmd_record.go
./cmd/twee/cmd_run.go
./cmd/twee/cmd_start.go
./cmd/twee/cmd_state.go
./cmd/twee/cmd_status.go
./cmd/twee/cmd_stop.go
./cmd/twee/cmd_wait.go
./cmd/twee/daemonize.go
./cmd/twee/emit.go
./cmd/twee/flake_test.go
./cmd/twee/main.go
./cmd/twee/main_test.go
./cmd/twee/paths.go
./cmd/twee/paths_test.go
./fixtures/menu/main.go
./internal/daemon/dispatch.go
./internal/daemon/handlers_diff.go
./internal/daemon/handlers_input.go
./internal/daemon/handlers_lifecycle.go
./internal/daemon/handlers_query.go
./internal/daemon/handlers_record.go
./internal/daemon/handlers_screenshot.go
./internal/daemon/handlers_wait.go
./internal/daemon/server.go
./internal/daemon/server_test.go
./internal/engine/config.go
./internal/engine/input.go
./internal/engine/query.go
./internal/engine/term.go
./internal/engine/types.go
./internal/engine/wait.go
./internal/input/keys.go
./internal/ptyrunner/runner.go
./internal/pump/pump.go
./internal/pump/pump_test.go
./internal/recording/recording.go
./internal/recording/recording_test.go
./internal/recording/replay.go
./internal/render/font.go
./internal/render/font_data.go
./internal/render/render.go
./internal/render/render_test.go
./internal/rpc/codec.go
./internal/rpc/codec_test.go
./internal/rpc/errors.go
./internal/rpc/ops.go
./internal/rpc/types.go
./internal/snapshot/diff.go
./internal/snapshot/text.go
./internal/vt/ghostty.go
./internal/vt/model.go
./internal/vt/term_test.go
./internal/vt/types.go
./internal/vt/visible.go
./tuitest/diagnostic_test.go
./tuitest/expect.go
./tuitest/input.go
./tuitest/menu_e2e_test.go
./tuitest/options.go
./tuitest/runner_smoke_test.go
./tuitest/snapshot.go
./tuitest/snapshot_assert.go
./tuitest/term.go
./tuitest/wait.go
./tuitest/wait_test.go

TOOL CALL: bash
INPUT:
{"command": "cat internal/daemon/handlers_record.go 2>/dev/null || echo \"FILE NOT FOUND\""}
TOOL RESULT: bash
OUTPUT:
package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	optionalRegistrations = append(optionalRegistrations, func(d *Dispatcher) {
		d.Register(rpc.OpRecordStart, handleRecordStart)
		d.Register(rpc.OpRecordStop, handleRecordStop)
	})
}

func handleRecordStart(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.RecordStartArgs
	if err := json.Unmarshal(raw, &a); err != nil && len(raw) > 0 {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	if a.Out == "" {
		dir, err := os.MkdirTemp("", "twee-record-")
		if err != nil {
			return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
		}
		a.Out = filepath.Join(dir, fmt.Sprintf("session-%d.jsonl", time.Now().UnixNano()))
	}
	if err := t.EnableRecording(a.Out); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
	}
	return map[string]string{"out": a.Out}, nil
}

func handleRecordStop(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	path := t.RecordPath()
	if err := t.DisableRecording(); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
	}
	return map[string]string{"path": path}, nil
}

TURN 2
TOOL CALL: bash
INPUT:
{"command": "cat internal/recording/recording.go"}
TOOL RESULT: bash
OUTPUT:
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

TOOL CALL: bash
INPUT:
{"command": "cat internal/recording/recording_test.go"}
TOOL RESULT: bash
OUTPUT:
package recording

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/paulsmith/research/twee/internal/vt"
)

func TestRecordAndReplay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	r, err := New(path, Header{Command: []string{"fixture"}, Cols: 20, Rows: 3})
	if err != nil {
		t.Fatal(err)
	}
	r.WriteOutput([]byte("hello"), time.Now())
	r.WriteOutput([]byte("\r\nworld"), time.Now())
	r.WriteResize(30, 5)
	r.WriteOutput([]byte("\r\n!"), time.Now())
	r.WriteExit(0)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	m := vt.New(20, 3)
	if _, err := ReplayInto(path, m); err != nil {
		t.Fatal(err)
	}
	got := vt.VisibleLines(m.Snapshot())
	want := []string{"hello", "world", "!", "", ""}
	if len(got) < len(want) {
		t.Fatalf("got %d lines, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("line %d = %q, want %q", i, got[i], w)
		}
	}
	s := m.Snapshot()
	if s.Size.Cols != 30 || s.Size.Rows != 5 {
		t.Errorf("size after replay = %+v", s.Size)
	}
}

TURN 3
TOOL CALL: bash
INPUT:
{"command": "cat internal/recording/replay.go"}
TOOL RESULT: bash
OUTPUT:
package recording

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/paulsmith/research/twee/internal/vt"
)

// ReplayInto reads a recording from path and feeds output and resize
// events into model. Input events are skipped (they are diagnostic
// metadata).
//
// On success returns the number of output bytes fed.
func ReplayInto(path string, model vt.Model) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	// Header
	if !sc.Scan() {
		return 0, errors.New("recording: empty")
	}
	var hdr Header
	if err := json.Unmarshal(sc.Bytes(), &hdr); err != nil {
		return 0, err
	}
	if hdr.Cols > 0 && hdr.Rows > 0 {
		_ = model.Resize(hdr.Cols, hdr.Rows)
	}
	total := 0
	for sc.Scan() {
		var ev Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			return total, err
		}
		switch ev.Type {
		case "output":
			b, err := base64.StdEncoding.DecodeString(ev.Bytes)
			if err != nil {
				return total, err
			}
			if err := model.Feed(b); err != nil {
				return total, err
			}
			total += len(b)
		case "resize":
			_ = model.Resize(ev.Cols, ev.Rows)
		}
	}
	if err := sc.Err(); err != nil && err != io.EOF {
		return total, err
	}
	return total, nil
}

TOOL CALL: bash
INPUT:
{"command": "cat cmd/twee/cmd_record.go"}
TOOL RESULT: bash
OUTPUT:
package main

import (
	"flag"

	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	register("record", runRecord)

	registerUsage("record", `twee record start [-out <path.jsonl>] [-name <name>]
twee record stop [-name <name>]
Toggle JSONL recording on the running session.`)
	registerUsage("record start", `twee record start [-out <path.jsonl>] [-name <name>]
Start JSONL recording on the running session.`)
	registerUsage("record stop", `twee record stop [-name <name>]
Stop JSONL recording on the running session.`)
}

func runRecord(args []string) {
	if len(args) == 0 {
		fatalUsage("record: missing subverb (start|stop)")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "start":
		fs := flag.NewFlagSet("record start", flag.ExitOnError)
		name := fs.String("name", "default", "session name")
		out := fs.String("out", "", "output path")
		if err := fs.Parse(rest); err != nil {
			fatalUsage("record start: %v", err)
		}
		callAndEmit(*name, rpc.OpRecordStart, rpc.RecordStartArgs{Out: *out})
	case "stop":
		fs := flag.NewFlagSet("record stop", flag.ExitOnError)
		name := fs.String("name", "default", "session name")
		if err := fs.Parse(rest); err != nil {
			fatalUsage("record stop: %v", err)
		}
		callAndEmit(*name, rpc.OpRecordStop, nil)
	default:
		fatalUsage("record: unknown subverb %q", sub)
	}
}

TURN 4
TOOL CALL: bash
INPUT:
{"command": "cat internal/rpc/ops.go && cat internal/rpc/types.go"}
TOOL RESULT: bash
OUTPUT:
package rpc

// Op names. Multi-word verbs join with underscore; sub-verbs (record start /
// wait text) become a single op name.
const (
	// Input
	OpType   = "type"
	OpKey    = "key"
	OpPaste  = "paste"
	OpSignal = "signal"

	// Queries
	OpText       = "text"
	OpLines      = "lines"
	OpCell       = "cell"
	OpRegion     = "region"
	OpCursor     = "cursor"
	OpFind       = "find"
	OpSize       = "size"
	OpTitle      = "title"
	OpMode       = "mode"
	OpScrollback = "scrollback"
	OpSnapshot   = "snapshot"

	// State changes
	OpResize      = "resize"
	OpScreenshot  = "screenshot"
	OpRecordStart = "record_start"
	OpRecordStop  = "record_stop"
	OpDiff        = "diff"

	// Waits
	OpWaitText   = "wait_text"
	OpWaitNoText = "wait_no_text"
	OpWaitStable = "wait_stable"
	OpWaitCursor = "wait_cursor"
	OpWaitExit   = "wait_exit"

	// Misc
	OpSleep = "sleep"

	// Lifecycle
	OpStop   = "stop"
	OpStatus = "status"
)
// Package rpc defines the wire-format types and codec for twee's
// daemon protocol. Requests and responses are length-prefixed JSON:
//
//	<u32 big-endian length><JSON bytes>
//
// One request, one response, per connection.
package rpc

import (
	"encoding/json"
	"time"
)

// Request is one client → daemon message.
type Request struct {
	ID   string          `json:"id"`
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Response is one daemon → client message.
type Response struct {
	ID    string          `json:"id"`
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *Error          `json:"error,omitempty"`
}

// Error is the structured error body.
type Error struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

// StatusData is the response shape for the status op (and per-entry in `twee ls`).
type StatusData struct {
	Cmd       []string  `json:"cmd"`
	Cols      int       `json:"cols"`
	Rows      int       `json:"rows"`
	StartedAt time.Time `json:"started_at"`
	Running   bool      `json:"running"`
	ExitCode  *int      `json:"exit_code,omitempty"`
}

// Input args.

type TypeArgs struct {
	Text string `json:"text"`
}

type KeyArgs struct {
	Key string `json:"key"`
}

type PasteArgs struct {
	Text string `json:"text"`
}

type SignalArgs struct {
	Name string `json:"name"`
}

// Query args.

type CellArgs struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type RegionArgs struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type FindArgs struct {
	Text  string `json:"text"`
	Regex bool   `json:"regex,omitempty"`
}

// State args.

type ResizeArgs struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

type ScreenshotArgs struct {
	Out string `json:"out,omitempty"`
}

type RecordStartArgs struct {
	Out string `json:"out,omitempty"`
}

type DiffArgs struct {
	Against string `json:"against"`
}

// Wait args.

type WaitTextArgs struct {
	Text    string `json:"text"`
	Regex   bool   `json:"regex,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}

type WaitNoTextArgs struct {
	Text    string `json:"text"`
	Timeout string `json:"timeout,omitempty"`
}

type WaitStableArgs struct {
	Quiet   string `json:"quiet,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}

type WaitCursorArgs struct {
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Timeout string `json:"timeout,omitempty"`
}

type WaitExitArgs struct {
	Timeout string `json:"timeout,omitempty"`
}

// Misc.

type SleepArgs struct {
	Duration string `json:"duration"`
}

// Data shapes returned by ops with non-trivial responses.

type TextData struct {
	Text string `json:"text"`
}

type LinesData struct {
	Lines []string `json:"lines"`
}

type CursorData struct {
	X       int  `json:"x"`
	Y       int  `json:"y"`
	Visible bool `json:"visible"`
}

type SizeData struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

type TitleData struct {
	Title string `json:"title"`
}

type ModeData struct {
	DECCKM         bool `json:"decckm"`
	BracketedPaste bool `json:"bracketed_paste"`
	AltScreen      bool `json:"alt_screen"`
	Mouse          bool `json:"mouse,omitempty"`
}

type FindMatch struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type DiffData struct {
	Equal    bool   `json:"equal"`
	Unified  string `json:"unified"`
	Current  string `json:"current"`
	Expected string `json:"expected"`
}

type WaitExitData struct {
	ExitCode int `json:"exit_code"`
}

type ScreenshotData struct {
	Out       string `json:"out,omitempty"`
	PNGBase64 string `json:"png_base64,omitempty"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

TOOL CALL: bash
INPUT:
{"command": "cat internal/engine/term.go"}
TOOL RESULT: bash
OUTPUT:
package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/paulsmith/research/twee/internal/ptyrunner"
	"github.com/paulsmith/research/twee/internal/pump"
	"github.com/paulsmith/research/twee/internal/recording"
	"github.com/paulsmith/research/twee/internal/vt"
)

// Term is a running TUI under PTY.
type Term struct {
	cfg Config

	cfgMu  sync.Mutex
	runner *ptyrunner.Runner
	pump   *pump.Pump
	rec    *recording.Recorder

	closeOnce sync.Once
	closeErr  error
	pumpDone  chan struct{}

	startedAt time.Time

	inputsMu sync.Mutex
	inputs   []InputEvent
}

// InputEvent is a single recorded input action for diagnostics.
type InputEvent struct {
	When time.Time
	Desc string
}

// Start spawns the command in cfg under a PTY, attaches the VT model
// and pump, optionally enables recording, and returns a Term ready for
// queries and input.
func Start(ctx context.Context, cfg Config) (*Term, error) {
	cfg.applyDefaults()
	if len(cfg.Cmd) == 0 {
		return nil, errors.New("engine.Start: cfg.Cmd is empty")
	}

	runner, err := ptyrunner.Start(ctx, ptyrunner.Config{
		Command: cfg.Cmd,
		Env:     cfg.BuildEnv(),
		Dir:     cfg.Dir,
		Cols:    cfg.Cols,
		Rows:    cfg.Rows,
	})
	if err != nil {
		return nil, fmt.Errorf("spawn: %w", err)
	}

	model := vt.New(cfg.Cols, cfg.Rows)
	p := pump.New(model, runner.Master())

	var rec *recording.Recorder
	if cfg.RecordPath != "" {
		rec, err = recording.New(cfg.RecordPath, recording.Header{
			Command: cfg.Cmd,
			Cols:    cfg.Cols,
			Rows:    cfg.Rows,
			Env:     cfg.Env,
		})
		if err != nil {
			_ = runner.Close()
			return nil, fmt.Errorf("recording: %w", err)
		}
		p.SetOutputHook(func(b []byte, ts time.Time) { rec.WriteOutput(b, ts) })
	}

	t := &Term{
		cfg:       cfg,
		runner:    runner,
		pump:      p,
		rec:       rec,
		pumpDone:  make(chan struct{}),
		startedAt: time.Now(),
	}
	go func() {
		_ = p.Run()
		close(t.pumpDone)
	}()
	return t, nil
}

// Close terminates the child and the pump.
func (t *Term) Close() error {
	t.closeOnce.Do(func() {
		t.closeErr = t.runner.Close()
		<-t.pumpDone
		t.cfgMu.Lock()
		if t.rec != nil {
			t.rec.WriteExit(t.runner.ExitCode())
			_ = t.rec.Close()
			t.rec = nil
		}
		t.cfgMu.Unlock()
	})
	return t.closeErr
}

// Cmd returns the command line the child was spawned with.
func (t *Term) Cmd() []string { return append([]string(nil), t.cfg.Cmd...) }

// DefaultTimeout returns the configured default timeout for waits.
func (t *Term) DefaultTimeout() time.Duration { return t.cfg.DefaultTimeout }

// StableQuietWindow returns the default quiet window for WaitForStableScreen.
func (t *Term) StableQuietWindow() time.Duration { return t.cfg.StableQuietWindow }

// RecordPath returns the recording path used at Start (or "" if recording
// was not enabled).
func (t *Term) RecordPath() string {
	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	return t.cfg.RecordPath
}

// StartedAt returns the wall-clock time when Start completed.
func (t *Term) StartedAt() time.Time { return t.startedAt }

// EnableRecording starts recording to path. Replaces any current recorder.
func (t *Term) EnableRecording(path string) error {
	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	if t.rec != nil {
		t.rec.WriteExit(t.runner.ExitCode())
		_ = t.rec.Close()
		t.rec = nil
	}
	rec, err := recording.New(path, recording.Header{
		Command: t.cfg.Cmd,
		Cols:    t.cfg.Cols,
		Rows:    t.cfg.Rows,
		Env:     t.cfg.Env,
	})
	if err != nil {
		return err
	}
	t.rec = rec
	t.cfg.RecordPath = path
	t.pump.SetOutputHook(func(b []byte, ts time.Time) { rec.WriteOutput(b, ts) })
	return nil
}

// DisableRecording stops recording and closes the file.
func (t *Term) DisableRecording() error {
	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	if t.rec == nil {
		return nil
	}
	t.rec.WriteExit(t.runner.ExitCode())
	err := t.rec.Close()
	t.rec = nil
	t.cfg.RecordPath = ""
	t.pump.SetOutputHook(nil)
	return err
}

// recordInput appends a description to the bounded ring buffer.
func (t *Term) recordInput(desc string) {
	t.inputsMu.Lock()
	defer t.inputsMu.Unlock()
	const cap = 64
	if len(t.inputs) >= cap {
		t.inputs = append(t.inputs[:0], t.inputs[len(t.inputs)-cap+1:]...)
	}
	t.inputs = append(t.inputs, InputEvent{When: time.Now(), Desc: desc})
}

TURN 5
TOOL CALL: bash
INPUT:
{"command": "cat internal/engine/config.go && cat internal/engine/input.go"}
TOOL RESULT: bash
OUTPUT:
// Package engine owns the shared TUI-under-PTY runtime. It is consumed
// by both the tuitest Go test API and the cmd/twee daemon.
package engine

import (
	"os"
	"time"
)

// Config configures a Term. Callers populate fields directly; defaults
// are applied by Start when zero.
type Config struct {
	Cmd        []string
	Env        map[string]string // overrides on top of defaults
	Dir        string
	Cols, Rows int

	DefaultTimeout    time.Duration
	StableQuietWindow time.Duration

	RecordPath string
}

// applyDefaults fills in zero fields with sensible values.
func (c *Config) applyDefaults() {
	if c.Cols == 0 {
		c.Cols = 80
	}
	if c.Rows == 0 {
		c.Rows = 24
	}
	if c.DefaultTimeout == 0 {
		c.DefaultTimeout = 5 * time.Second
	}
	if c.StableQuietWindow == 0 {
		c.StableQuietWindow = 100 * time.Millisecond
	}
	if c.Env == nil {
		c.Env = map[string]string{}
	}
}

// BuildEnv assembles the final []string env for exec, applying TERM/
// COLORTERM/LANG defaults and inheriting PATH/HOME/USER from the parent
// when not overridden.
func (c *Config) BuildEnv() []string {
	defaults := map[string]string{
		"TERM":      "xterm-256color",
		"COLORTERM": "truecolor",
		"LANG":      "C.UTF-8",
	}
	for k, v := range c.Env {
		defaults[k] = v
	}
	for _, k := range []string{"PATH", "HOME", "USER"} {
		if _, ok := defaults[k]; !ok {
			if v := os.Getenv(k); v != "" {
				defaults[k] = v
			}
		}
	}
	out := make([]string, 0, len(defaults))
	for k, v := range defaults {
		out = append(out, k+"="+v)
	}
	return out
}
package engine

import (
	"fmt"
	"os"

	"github.com/paulsmith/research/twee/internal/input"
)

// Type writes literal text to the PTY.
func (t *Term) Type(s string) error {
	if _, err := t.runner.Master().Write([]byte(s)); err != nil {
		return err
	}
	t.recordInput(fmt.Sprintf("Type %q", s))
	t.cfgMu.Lock()
	rec := t.rec
	t.cfgMu.Unlock()
	if rec != nil {
		rec.WriteInput("type", "", []byte(s))
	}
	return nil
}

// Key sends a named key.
func (t *Term) Key(k input.Key) error {
	b := input.Encode(k)
	if len(b) == 0 {
		return nil
	}
	if _, err := t.runner.Master().Write(b); err != nil {
		return err
	}
	t.recordInput("Key " + input.Name(k))
	t.cfgMu.Lock()
	rec := t.rec
	t.cfgMu.Unlock()
	if rec != nil {
		rec.WriteInput("key", input.Name(k), b)
	}
	return nil
}

// Paste sends text wrapped in bracketed-paste markers.
func (t *Term) Paste(text string) error {
	b := input.EncodePaste(text)
	if _, err := t.runner.Master().Write(b); err != nil {
		return err
	}
	t.recordInput(fmt.Sprintf("Paste %q", text))
	t.cfgMu.Lock()
	rec := t.rec
	t.cfgMu.Unlock()
	if rec != nil {
		rec.WriteInput("paste", "", b)
	}
	return nil
}

// Resize updates the PTY winsize, signals the child with SIGWINCH, and
// resizes the model.
func (t *Term) Resize(cols, rows int) error {
	if err := t.runner.Resize(cols, rows); err != nil {
		return err
	}
	if err := t.pump.Resize(cols, rows); err != nil {
		return err
	}
	t.recordInput(fmt.Sprintf("Resize %dx%d", cols, rows))
	t.cfgMu.Lock()
	rec := t.rec
	t.cfgMu.Unlock()
	if rec != nil {
		rec.WriteResize(cols, rows)
	}
	return nil
}

// Signal forwards a signal to the child process.
func (t *Term) Signal(sig os.Signal) error {
	t.recordInput(fmt.Sprintf("Signal %v", sig))
	return t.runner.Signal(sig)
}

TOOL CALL: bash
INPUT:
{"command": "cat internal/ptyrunner/runner.go && cat internal/pump/pump.go"}
TOOL RESULT: bash
OUTPUT:
// Package ptyrunner spawns a process under a PTY.
package ptyrunner

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// Config configures a Runner.
type Config struct {
	Command []string
	Env     []string // full env, overrides parent
	Dir     string
	Cols    int
	Rows    int
}

// Runner owns the child process and the PTY master.
type Runner struct {
	cfg Config
	cmd *exec.Cmd

	master *os.File
	exited chan struct{}
	exit   exitInfo
}

type exitInfo struct {
	err  error
	code int
}

// Start spawns the process. The PTY master is returned via Master().
func Start(ctx context.Context, cfg Config) (*Runner, error) {
	if len(cfg.Command) == 0 {
		return nil, errors.New("ptyrunner: empty command")
	}
	if cfg.Cols <= 0 {
		cfg.Cols = 80
	}
	if cfg.Rows <= 0 {
		cfg.Rows = 24
	}
	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)
	if cfg.Env != nil {
		cmd.Env = cfg.Env
	}
	if cfg.Dir != "" {
		cmd.Dir = cfg.Dir
	}
	master, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(cfg.Cols),
		Rows: uint16(cfg.Rows),
	})
	if err != nil {
		return nil, err
	}
	r := &Runner{
		cfg:    cfg,
		cmd:    cmd,
		master: master,
		exited: make(chan struct{}),
	}
	go r.wait()
	return r, nil
}

func (r *Runner) wait() {
	err := r.cmd.Wait()
	r.exit.err = err
	if r.cmd.ProcessState != nil {
		r.exit.code = r.cmd.ProcessState.ExitCode()
	}
	close(r.exited)
}

// Master returns the PTY master fd. Reads on it produce app output;
// writes deliver input to the app.
func (r *Runner) Master() io.ReadWriter { return r.master }

// Resize updates the PTY winsize and sends SIGWINCH to the child.
func (r *Runner) Resize(cols, rows int) error {
	if err := pty.Setsize(r.master, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	}); err != nil {
		return err
	}
	if r.cmd.Process != nil {
		_ = r.cmd.Process.Signal(syscall.SIGWINCH)
	}
	return nil
}

// Signal forwards a signal to the child. Returns an error if the
// process has not been started.
func (r *Runner) Signal(sig os.Signal) error {
	if r.cmd == nil || r.cmd.Process == nil {
		return errors.New("ptyrunner: child not started")
	}
	return r.cmd.Process.Signal(sig)
}

// ExitedCh closes when the child has been reaped.
func (r *Runner) ExitedCh() <-chan struct{} { return r.exited }

// ExitCode is valid after ExitedCh fires.
func (r *Runner) ExitCode() int { return r.exit.code }

// Close terminates the child gracefully (SIGTERM, then SIGKILL after a
// grace period) and closes the PTY master. Safe to call multiple times.
func (r *Runner) Close() error {
	if r.cmd.Process != nil {
		_ = r.cmd.Process.Signal(syscall.SIGTERM)
	}
	select {
	case <-r.exited:
	case <-time.After(250 * time.Millisecond):
		if r.cmd.Process != nil {
			_ = r.cmd.Process.Kill()
		}
		select {
		case <-r.exited:
		case <-time.After(2 * time.Second):
			// give up; close the PTY anyway
		}
	}
	return r.master.Close()
}
// Package pump owns the read loop from the PTY and feeds the VT model.
//
// Synchronization model:
//
//   - One goroutine (the pump) owns all reads from the master.
//   - All access to the VT model is guarded by mu.
//   - After every Feed, gen is incremented and cond.Broadcast wakes
//     waiters.
//   - WithSnapshot serializes external readers against the pump.
//   - Wait blocks until either pred(snapshot) is true, the timeout
//     fires, or the model is closed.
package pump

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/paulsmith/research/twee/internal/vt"
)

// Pump drives a vt.Model from an io.Reader.
type Pump struct {
	model  vt.Model
	reader io.Reader

	mu     sync.Mutex
	cond   *sync.Cond
	gen    uint64
	closed bool

	// recent holds the last N output bytes for diagnostics.
	recent []byte

	// last receive time and whether any feed has occurred. lastFeed is
	// only meaningful when gotAnyFeed is true; WaitStable refuses to
	// declare stability before then to avoid first-paint races.
	lastFeed    time.Time
	gotAnyFeed  bool

	// Recorder hook. Called outside mu to avoid blocking the pump on
	// recorder I/O.
	onOutput func(bytes []byte, t time.Time)
}

// New constructs a Pump. The caller must call Run in a goroutine.
func New(model vt.Model, r io.Reader) *Pump {
	p := &Pump{
		model:  model,
		reader: r,
		recent: make([]byte, 0, 64*1024),
	}
	p.cond = sync.NewCond(&p.mu)
	return p
}

// SetOutputHook installs a function called under mu after each Feed.
// Used by the recorder.
func (p *Pump) SetOutputHook(fn func([]byte, time.Time)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onOutput = fn
}

// Run reads until EOF or error. It is intended to run in its own
// goroutine. After Run returns, no further model mutations occur.
func (p *Pump) Run() error {
	buf := make([]byte, 32*1024)
	for {
		n, err := p.reader.Read(buf)
		if n > 0 {
			// Copy the chunk before releasing the lock so the hook can
			// outlive the buffer reuse on the next Read.
			chunk := append([]byte(nil), buf[:n]...)
			p.mu.Lock()
			_ = p.model.Feed(chunk)
			p.appendRecent(chunk)
			now := time.Now()
			p.lastFeed = now
			p.gotAnyFeed = true
			p.gen++
			hook := p.onOutput
			p.cond.Broadcast()
			p.mu.Unlock()
			// Hook runs outside mu so a slow recorder cannot stall
			// snapshots or waiters.
			if hook != nil {
				hook(chunk, now)
			}
		}
		if err != nil {
			p.mu.Lock()
			p.closed = true
			p.gen++
			p.cond.Broadcast()
			p.mu.Unlock()
			if isExpectedEOF(err) {
				return nil
			}
			return err
		}
	}
}

func isExpectedEOF(err error) bool {
	if err == io.EOF {
		return true
	}
	// On Linux, reading from a PTY master after the slave closes
	// returns EIO. Treat that as normal end-of-stream.
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	// syscall.EIO check: do it by string to avoid pulling syscall in here.
	if err != nil && err.Error() != "" && (containsAny(err.Error(),
		"input/output error", "i/o error on closed pty", "file already closed")) {
		return true
	}
	return false
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}

func (p *Pump) appendRecent(chunk []byte) {
	const cap = 64 * 1024
	if len(p.recent)+len(chunk) <= cap {
		p.recent = append(p.recent, chunk...)
		return
	}
	// Drop oldest.
	keep := cap - len(chunk)
	if keep < 0 {
		p.recent = append(p.recent[:0], chunk[len(chunk)-cap:]...)
		return
	}
	p.recent = append(p.recent[:0], p.recent[len(p.recent)-keep:]...)
	p.recent = append(p.recent, chunk...)
}

// WithSnapshot calls fn with a fresh snapshot under the lock. fn must
// not block.
func (p *Pump) WithSnapshot(fn func(vt.Snapshot)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn(p.model.Snapshot())
}

// Snapshot returns a fresh snapshot.
func (p *Pump) Snapshot() vt.Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.model.Snapshot()
}

// RecentBytes returns a copy of the most recent output for diagnostics.
func (p *Pump) RecentBytes() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]byte, len(p.recent))
	copy(out, p.recent)
	return out
}

// Resize forwards a resize to the model under mu.
func (p *Pump) Resize(cols, rows int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	err := p.model.Resize(cols, rows)
	p.gen++
	p.cond.Broadcast()
	return err
}

// Wait blocks until pred(snapshot) returns true, the deadline fires,
// or the pump closes. pred is evaluated under mu after every model
// change.
func (p *Pump) Wait(ctx context.Context, timeout time.Duration, pred func(vt.Snapshot) bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if pred(p.model.Snapshot()) {
		return nil
	}
	if p.closed {
		return ErrClosed
	}

	deadline := time.Now().Add(timeout)
	stop := make(chan struct{})
	defer close(stop)

	// Wake the cond at deadline or on context cancel.
	go func() {
		t := time.NewTimer(timeout)
		defer t.Stop()
		select {
		case <-stop:
		case <-t.C:
			p.mu.Lock()
			p.cond.Broadcast()
			p.mu.Unlock()
		case <-ctx.Done():
			p.mu.Lock()
			p.cond.Broadcast()
			p.mu.Unlock()
		}
	}()

	for {
		p.cond.Wait()
		if pred(p.model.Snapshot()) {
			return nil
		}
		if p.closed {
			if pred(p.model.Snapshot()) {
				return nil
			}
			return ErrClosed
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return ErrTimeout
		}
	}
}

// WaitStable blocks until at least quietFor elapses with no new output,
// or the timeout fires. Refuses to declare stability before the first
// feed has been observed (or the pump has closed) — otherwise a call
// made immediately after Run would return on an empty screen if the
// app's first paint is later than quietFor.
func (p *Pump) WaitStable(ctx context.Context, quietFor, timeout time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	deadline := time.Now().Add(timeout)
	stop := make(chan struct{})
	defer close(stop)

	// Single timer goroutine; we Reset rather than spawn-per-iteration.
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-timer.C:
				p.mu.Lock()
				p.cond.Broadcast()
				p.mu.Unlock()
			case <-ctx.Done():
				p.mu.Lock()
				p.cond.Broadcast()
				p.mu.Unlock()
				return
			}
		}
	}()

	for {
		now := time.Now()
		stable := p.gotAnyFeed && now.Sub(p.lastFeed) >= quietFor
		if stable || p.closed {
			return nil
		}
		// Compute the next interesting wakeup: either when the quiet
		// window completes (relative to the last feed if any, else
		// the deadline) or the deadline itself.
		var wakeIn time.Duration
		if p.gotAnyFeed {
			wakeIn = quietFor - now.Sub(p.lastFeed)
		} else {
			wakeIn = deadline.Sub(now)
		}
		if rem := deadline.Sub(now); rem < wakeIn {
			wakeIn = rem
		}
		if wakeIn <= 0 {
			return ErrTimeout
		}
		timer.Reset(wakeIn)
		p.cond.Wait()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			// Final re-check before declaring timeout.
			if p.gotAnyFeed && time.Since(p.lastFeed) >= quietFor {
				return nil
			}
			return ErrTimeout
		}
	}
}

// Errors.
var (
	ErrTimeout = errors.New("pump: timeout")
	ErrClosed  = errors.New("pump: closed")
)

TURN 6
TOOL CALL: bash
INPUT:
{"command": "cat cmd/twee/cmd_start.go && cat cmd/twee/main.go"}
TOOL RESULT: bash
OUTPUT:
package main

import (
	"flag"
	"fmt"
)

func init() {
	register("start", runStart)
	registerUsage("start", `twee start <cmd> [args...] [flags]
Spawn a TUI in a new daemon and fork into the background. Prints
{"name": ..., "socket": ..., "pid": ...} on success.

Flags:
  -name <name>     session name (default "default")
  -cols <int>      initial columns (default 80)
  -rows <int>      initial rows (default 24)
  -dir <path>      child working directory (default: inherit)
  -env KEY=VALUE   environment override (repeatable)`)
}

func runStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	cols := fs.Int("cols", 80, "initial columns")
	rows := fs.Int("rows", 24, "initial rows")
	dir := fs.String("dir", "", "working directory of child (empty = inherit)")
	var envFlags multiFlag
	fs.Var(&envFlags, "env", "environment override KEY=VALUE (repeatable)")
	if err := fs.Parse(args); err != nil {
		fatalUsage("start: %v", err)
	}
	cmd := fs.Args()
	if len(cmd) == 0 {
		fatalUsage("start: missing command")
	}
	envOverrides := map[string]string{}
	for _, kv := range envFlags {
		k, v, ok := splitKV(kv)
		if !ok {
			fatalUsage("start: bad --env value %q (want KEY=VALUE)", kv)
		}
		envOverrides[k] = v
	}
	msg, err := daemonize(*name, *dir, cmd, *cols, *rows, envOverrides)
	if err != nil {
		emitError("IO", err.Error(), nil, 1)
	}
	emitOK(msg)
}

// multiFlag collects repeated string flags.
type multiFlag []string

func (m *multiFlag) String() string { return fmt.Sprint([]string(*m)) }
func (m *multiFlag) Set(s string) error {
	*m = append(*m, s)
	return nil
}

func splitKV(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}
// Command twee is a CLI for driving terminal UIs. See
// docs/superpowers/specs/2026-04-28-twee-cli-design.md for the design.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Version is overridden at build time via -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	if inDaemonMode() {
		runDaemonChild()
		return // unreachable
	}
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}
	verb := os.Args[1]
	args := os.Args[2:]

	if h := dispatch[verb]; h != nil {
		h(args)
		return
	}

	switch verb {
	case "version":
		fmt.Println(Version)
	case "help", "-h", "--help":
		if len(args) > 0 {
			printVerbHelp(os.Stdout, args)
			return
		}
		printUsage(os.Stdout)
	case "completion":
		runCompletion(args)
	default:
		fmt.Fprintf(os.Stderr, "twee: unknown subcommand %q\n", verb)
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

// dispatch is the verb table. Other files (cmd_*.go) populate it via
// init() so M1 builds without their dependencies.
var dispatch = map[string]func(args []string){}

func register(verb string, fn func(args []string)) { dispatch[verb] = fn }

// usages holds per-verb help text, populated by cmd_*.go init() funcs
// via registerUsage. Multi-word verbs (e.g. "wait text") are keyed by
// their full verb path joined with a single space.
var usages = map[string]string{}

func registerUsage(verb, help string) { usages[verb] = help }

func printVerbHelp(w io.Writer, args []string) {
	key := strings.Join(args, " ")
	if h, ok := usages[key]; ok {
		fmt.Fprintln(w, h)
		return
	}
	// Fall back to the first word — e.g. "help wait" prints the wait
	// overview when no specific subverb was given.
	if h, ok := usages[args[0]]; ok {
		fmt.Fprintln(w, h)
		return
	}
	fmt.Fprintf(os.Stderr, "twee: no help available for %q\n", key)
	os.Exit(2)
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `twee — drive TUIs from the shell.

Usage: twee <verb> [positional args...] [-flag value ...]

Lifecycle:
  start <cmd> [args...]         Spawn a TUI in a daemon
  stop                          Stop the running daemon
  ls                            List running daemons
  status                        Print the status of a daemon
  run <cmd> [args...] -script   Single-shot ephemeral session

Input:    type | key | keys | paste | signal
Queries:  text | lines | cell | region | cursor | find
          size | title | mode | scrollback | snapshot
State:    resize | screenshot | record | diff
Waits:    wait text | wait no-text | wait stable | wait cursor | wait exit
Misc:     sleep | version | help | completion

Common flags (per verb; both -name and --name are accepted):
  -name <name>      Target a named daemon (default: "default")
  -timeout <dur>    Override timeout for wait verbs

Flags must appear AFTER the verb (they're parsed by each verb's flag
set, not globally). "twee --name foo status" fails; write
"twee status -name foo".

For literal characters use "twee type"; "twee key" only accepts named
keys (Enter, Down, Ctrl+C, ...). "twee key i" fails — use "twee type i".

Output is JSON by default:
  {"ok": true, "data": {...}}            on success
  {"ok": false, "error": {...}}          on failure

Run "twee help <verb>" for per-verb usage (e.g. "twee help start",
"twee help wait text"). Spec:
  docs/superpowers/specs/2026-04-28-twee-cli-design.md`)
}

func runCompletion(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "twee completion: missing shell argument (bash|zsh|fish)")
		os.Exit(2)
	}
	switch args[0] {
	case "bash", "zsh", "fish":
		fmt.Println("# twee completion: not yet generated")
	default:
		fmt.Fprintf(os.Stderr, "twee completion: unknown shell %q\n", args[0])
		os.Exit(2)
	}
}

func inDaemonMode() bool { return inDaemonModeReal() }
func runDaemonChild()    { runDaemonChildReal() }

TOOL CALL: bash
INPUT:
{"command": "cat internal/daemon/handlers_lifecycle.go && cat internal/daemon/dispatch.go"}
TOOL RESULT: bash
OUTPUT:
package daemon

import (
	"encoding/json"

	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/rpc"
)

func (d *Dispatcher) registerLifecycle() {
	d.Register(rpc.OpStatus, handleStatus)
	d.Register(rpc.OpStop, handleStop)
}

func handleStatus(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	snap := t.Snapshot()
	data := rpc.StatusData{
		Cmd:       t.Cmd(),
		Cols:      snap.Cols,
		Rows:      snap.Rows,
		StartedAt: t.StartedAt(),
		Running:   true,
	}
	select {
	case <-t.ExitedCh():
		data.Running = false
		c := t.ExitCode()
		data.ExitCode = &c
	default:
	}
	return data, nil
}

func handleStop(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	if err := t.Close(); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
	}
	return nil, nil
}
package daemon

import (
	"encoding/json"
	"fmt"

	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/rpc"
)

// Dispatcher maps op names to handler functions.
type Dispatcher struct {
	term     *engine.Term
	handlers map[string]Handler
}

// Handler processes one request's args and returns either a data
// payload or an error.
type Handler func(t *engine.Term, args json.RawMessage) (any, *rpc.Error)

// NewDispatcher returns a Dispatcher with all built-in handlers
// registered. Later milestones (M3, M5, M6) add registerInput,
// registerQueries, registerWaits, etc.
func NewDispatcher(t *engine.Term) *Dispatcher {
	d := &Dispatcher{term: t, handlers: map[string]Handler{}}
	d.registerLifecycle()
	for _, reg := range optionalRegistrations {
		reg(d)
	}
	return d
}

// optionalRegistrations is appended to from init() in handler files
// added in later milestones; this lets each milestone land independently
// without modifying NewDispatcher.
var optionalRegistrations []func(*Dispatcher)

// Register installs a handler for an op name. Panics on duplicate.
func (d *Dispatcher) Register(op string, h Handler) {
	if _, exists := d.handlers[op]; exists {
		panic(fmt.Sprintf("daemon: duplicate op %q", op))
	}
	d.handlers[op] = h
}

// Dispatch executes one request against the registered handlers.
func (d *Dispatcher) Dispatch(req rpc.Request) rpc.Response {
	resp := rpc.Response{ID: req.ID}
	h, ok := d.handlers[req.Op]
	if !ok {
		resp.OK = false
		resp.Error = &rpc.Error{
			Code:    rpc.CodeInvalidArgument,
			Message: fmt.Sprintf("unknown op %q", req.Op),
		}
		return resp
	}
	data, errResp := h(d.term, req.Args)
	if errResp != nil {
		resp.OK = false
		resp.Error = errResp
		return resp
	}
	resp.OK = true
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			resp.OK = false
			resp.Error = &rpc.Error{
				Code:    rpc.CodeInternal,
				Message: "marshal data: " + err.Error(),
			}
			return resp
		}
		resp.Data = raw
	} else {
		resp.Data = json.RawMessage("null")
	}
	return resp
}

TURN 7
TOOL CALL: bash
INPUT:
{"command": "cat cmd/twee/daemonize.go && cat cmd/twee/client.go"}
TOOL RESULT: bash
OUTPUT:
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/paulsmith/research/twee/internal/daemon"
	"github.com/paulsmith/research/twee/internal/engine"
)

const (
	envDaemonMode    = "TWEE_DAEMON_MODE"
	envDaemonName    = "TWEE_DAEMON_NAME"
	envDaemonReadyFd = "TWEE_DAEMON_READY_FD"
	envDaemonLockFd  = "TWEE_DAEMON_LOCK_FD"
	envDaemonCmd     = "TWEE_DAEMON_CMD"
	envDaemonCols    = "TWEE_DAEMON_COLS"
	envDaemonRows    = "TWEE_DAEMON_ROWS"
	envDaemonDir     = "TWEE_DAEMON_DIR"
	envDaemonEnv     = "TWEE_DAEMON_ENV"
)

// readyMessage is what the child writes to the parent over the pipe.
type readyMessage struct {
	Name   string `json:"name"`
	Socket string `json:"socket"`
	PID    int    `json:"pid"`
	Error  string `json:"error,omitempty"`
}

// inDaemonModeReal returns true when this process was invoked as a daemon child.
// Renamed from inDaemonMode (which is now a thin wrapper in main.go for M1
// compatibility; replaced by this when building the M2 binary).
func inDaemonModeReal() bool {
	return os.Getenv(envDaemonMode) == "1"
}

// runDaemonChildReal is the entry point taken in daemon mode.
func runDaemonChildReal() {
	name := os.Getenv(envDaemonName)
	readyFD, _ := strconv.Atoi(os.Getenv(envDaemonReadyFd))
	lockFD, _ := strconv.Atoi(os.Getenv(envDaemonLockFd))
	cols, _ := strconv.Atoi(os.Getenv(envDaemonCols))
	rows, _ := strconv.Atoi(os.Getenv(envDaemonRows))

	var cmdv []string
	_ = json.Unmarshal([]byte(os.Getenv(envDaemonCmd)), &cmdv)
	var envOverrides map[string]string
	_ = json.Unmarshal([]byte(os.Getenv(envDaemonEnv)), &envOverrides)
	dir := os.Getenv(envDaemonDir)

	readyW := os.NewFile(uintptr(readyFD), "ready-pipe")
	lockFile := os.NewFile(uintptr(lockFD), "lock-file")
	_ = lockFile // hold open to keep flock alive

	sock, err := socketPath(name)
	if err != nil {
		writeReadyErr(readyW, name, err)
		os.Exit(1)
	}
	_ = os.Remove(sock) // stale socket; lock confirmed no live owner

	te, err := engine.Start(context.Background(), engine.Config{
		Cmd:  cmdv,
		Env:  envOverrides,
		Dir:  dir,
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		writeReadyErr(readyW, name, fmt.Errorf("engine.Start: %w", err))
		os.Exit(1)
	}

	l, err := net.Listen("unix", sock)
	if err != nil {
		_ = te.Close()
		writeReadyErr(readyW, name, fmt.Errorf("listen %s: %w", sock, err))
		os.Exit(1)
	}
	if err := os.Chmod(sock, 0o600); err != nil {
		_ = l.Close()
		_ = os.Remove(sock)
		_ = te.Close()
		writeReadyErr(readyW, name, fmt.Errorf("chmod socket: %w", err))
		os.Exit(1)
	}

	// Send ready handshake.
	msg := readyMessage{Name: name, Socket: sock, PID: os.Getpid()}
	_ = json.NewEncoder(readyW).Encode(msg)
	_ = readyW.Close()

	// Detach stdio.
	if devNull, err := os.OpenFile("/dev/null", os.O_RDWR, 0); err == nil {
		_ = syscall.Dup2(int(devNull.Fd()), 0)
		_ = syscall.Dup2(int(devNull.Fd()), 1)
		_ = syscall.Dup2(int(devNull.Fd()), 2)
		_ = devNull.Close()
	}

	srv := daemon.NewServer(te)
	go func() {
		<-te.ExitedCh()
		time.Sleep(100 * time.Millisecond)
		srv.Stop()
		_ = l.Close()
	}()

	_ = srv.Serve(context.Background(), l)
	_ = te.Close()
	_ = os.Remove(sock)
	os.Exit(0)
}

func writeReadyErr(w *os.File, name string, err error) {
	_ = json.NewEncoder(w).Encode(readyMessage{Name: name, Error: err.Error()})
	_ = w.Close()
}

// daemonize re-execs into daemon mode with the given config, holding
// the named lock file. Returns the ready message read back from the
// child.
func daemonize(name, dir string, cmd []string, cols, rows int, envOverrides map[string]string) (readyMessage, error) {
	if err := validateName(name); err != nil {
		return readyMessage{}, err
	}
	lp, err := lockPath(name)
	if err != nil {
		return readyMessage{}, err
	}
	lf, err := os.OpenFile(lp, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return readyMessage{}, fmt.Errorf("open lock: %w", err)
	}
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lf.Close()
		if err == syscall.EWOULDBLOCK {
			return readyMessage{}, fmt.Errorf("daemon %q already running", name)
		}
		return readyMessage{}, fmt.Errorf("flock: %w", err)
	}
	_ = lf.Truncate(0)
	_, _ = lf.Seek(0, 0)
	_, _ = fmt.Fprintf(lf, "%d\n", os.Getpid())

	pr, pw, err := os.Pipe()
	if err != nil {
		_ = lf.Close()
		return readyMessage{}, fmt.Errorf("pipe: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		_ = lf.Close()
		_ = pr.Close()
		_ = pw.Close()
		return readyMessage{}, fmt.Errorf("os.Executable: %w", err)
	}

	cmdJSON, _ := json.Marshal(cmd)
	envJSON, _ := json.Marshal(envOverrides)

	child := exec.Command(exe)
	child.Env = append(os.Environ(),
		envDaemonMode+"=1",
		envDaemonName+"="+name,
		envDaemonReadyFd+"=3", // ExtraFiles[0]
		envDaemonLockFd+"=4",  // ExtraFiles[1]
		envDaemonCols+"="+strconv.Itoa(cols),
		envDaemonRows+"="+strconv.Itoa(rows),
		envDaemonCmd+"="+string(cmdJSON),
		envDaemonEnv+"="+string(envJSON),
		envDaemonDir+"="+dir,
	)
	child.ExtraFiles = []*os.File{pw, lf}
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		_ = lf.Close()
		_ = pr.Close()
		_ = pw.Close()
		return readyMessage{}, fmt.Errorf("start daemon: %w", err)
	}
	_ = pw.Close()
	_ = lf.Close()
	go func() { _ = child.Wait() }()

	dec := json.NewDecoder(pr)
	var msg readyMessage
	// Add a read deadline by pulling bytes via a goroutine + timer.
	type result struct {
		msg readyMessage
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		var m readyMessage
		err := dec.Decode(&m)
		resCh <- result{m, err}
	}()
	select {
	case r := <-resCh:
		_ = pr.Close()
		if r.err != nil {
			return readyMessage{}, fmt.Errorf("ready: %w", r.err)
		}
		msg = r.msg
	case <-time.After(10 * time.Second):
		_ = pr.Close()
		return readyMessage{}, fmt.Errorf("daemon did not signal ready within 10s")
	}
	if msg.Error != "" {
		return msg, fmt.Errorf("daemon failed to start: %s", msg.Error)
	}
	return msg, nil
}
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/paulsmith/research/twee/internal/rpc"
)

// callDaemon dials the named session's socket and runs one op.
func callDaemon(name, op string, args any) (rpc.Response, error) {
	sock, err := socketPath(name)
	if err != nil {
		return rpc.Response{}, err
	}
	c, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return rpc.Response{}, fmt.Errorf("dial %s: %w", sock, err)
	}
	defer c.Close()
	req := rpc.Request{ID: nextID(), Op: op}
	if args != nil {
		raw, err := json.Marshal(args)
		if err != nil {
			return rpc.Response{}, fmt.Errorf("marshal args: %w", err)
		}
		req.Args = raw
	}
	if err := rpc.WriteMessage(c, req); err != nil {
		return rpc.Response{}, fmt.Errorf("write: %w", err)
	}
	var resp rpc.Response
	if err := rpc.ReadMessage(c, &resp); err != nil {
		return rpc.Response{}, fmt.Errorf("read: %w", err)
	}
	return resp, nil
}

var idCounter atomic.Uint64

func nextID() string {
	return fmt.Sprintf("%d", idCounter.Add(1))
}

// callAndEmit calls one op and prints the JSON envelope, exiting on error.
func callAndEmit(name, op string, args any) {
	resp, err := callDaemon(name, op, args)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	if !resp.OK {
		emitError(resp.Error.Code, resp.Error.Message, resp.Error.Details, 1)
	}
	emitOKRaw(resp.Data)
}

// callOnly calls one op, exits on error, but stays silent on success.
func callOnly(name, op string, args any) {
	resp, err := callDaemon(name, op, args)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	if !resp.OK {
		emitError(resp.Error.Code, resp.Error.Message, resp.Error.Details, 1)
	}
}

// emitOKRaw writes {"ok": true, "data": <raw>} where raw is already JSON.
func emitOKRaw(data json.RawMessage) {
	if len(data) == 0 {
		emitOK(nil)
		return
	}
	out := struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}{OK: true, Data: data}
	enc := json.NewEncoder(stdoutFile())
	if err := enc.Encode(out); err != nil {
		fatalUsage("emit: %v", err)
	}
}

TOOL CALL: bash
INPUT:
{"command": "cat cmd/twee/emit.go && cat cmd/twee/paths.go"}
TOOL RESULT: bash
OUTPUT:
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/paulsmith/research/twee/internal/rpc"
)

// stdoutFile returns the output writer for envelope JSON. Indirection
// keeps tests free to redirect.
func stdoutFile() io.Writer { return os.Stdout }

// emitOK writes {"ok": true, "data": data} to stdout and exits 0.
func emitOK(data any) {
	out := struct {
		OK   bool `json:"ok"`
		Data any  `json:"data"`
	}{OK: true, Data: data}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "twee: emit: %v\n", err)
		os.Exit(1)
	}
}

// emitError writes {"ok": false, "error": {...}} to stdout and exits.
func emitError(code, msg string, details json.RawMessage, exitCode int) {
	out := struct {
		OK    bool       `json:"ok"`
		Error *rpc.Error `json:"error"`
	}{OK: false, Error: &rpc.Error{Code: code, Message: msg, Details: details}}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "twee: emit: %v\n", err)
		os.Exit(1)
	}
	os.Exit(exitCode)
}

// fatalUsage prints to stderr and exits with code 2 (POSIX usage error).
func fatalUsage(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "twee: "+format+"\n", args...)
	os.Exit(2)
}
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// stateDir returns the directory in which named-session sockets and
// lock files live. The directory is created with 0700 if missing.
//
// Override resolution order: TWEE_STATE_DIR (used for testing), then
// platform default (XDG_STATE_HOME or macOS Library path), then a
// $TMPDIR/twee-$USER fallback.
func stateDir() (string, error) {
	if v := os.Getenv("TWEE_STATE_DIR"); v != "" {
		if err := os.MkdirAll(v, 0o700); err != nil {
			return "", fmt.Errorf("stateDir: %w", err)
		}
		return v, nil
	}
	var base string
	switch runtime.GOOS {
	case "darwin":
		home := os.Getenv("HOME")
		if home != "" {
			base = filepath.Join(home, "Library", "Application Support")
		}
	default:
		base = os.Getenv("XDG_STATE_HOME")
		if base == "" {
			home := os.Getenv("HOME")
			if home != "" {
				base = filepath.Join(home, ".local", "state")
			}
		}
	}
	if base == "" {
		// Fallback: $TMPDIR/twee-$USER.
		tmp := os.Getenv("TMPDIR")
		if tmp == "" {
			tmp = "/tmp"
		}
		user := os.Getenv("USER")
		if user == "" {
			user = "default"
		}
		base = filepath.Join(tmp, "twee-"+user)
		if err := os.MkdirAll(base, 0o700); err != nil {
			return "", fmt.Errorf("stateDir: %w", err)
		}
		return base, nil
	}
	dir := filepath.Join(base, "twee")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("stateDir: %w", err)
	}
	return dir, nil
}

// socketPath returns the socket path for a named session.
func socketPath(name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".sock"), nil
}

// lockPath returns the lock-file path for a named session.
func lockPath(name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".lock"), nil
}

// validateName rejects names that are empty, contain path separators or
// NUL, or look like traversal.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("name must be non-empty")
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return fmt.Errorf("name must not contain path separators or NUL")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("name %q is reserved", name)
	}
	return nil
}

TURN 8
TOOL CALL: bash
INPUT:
{"command": "cat internal/daemon/handlers_screenshot.go && cat cmd/twee/cmd_stop.go"}
TOOL RESULT: bash
OUTPUT:
package daemon

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"

	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/render"
	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	optionalRegistrations = append(optionalRegistrations, func(d *Dispatcher) {
		d.Register(rpc.OpScreenshot, handleScreenshot)
	})
}

func handleScreenshot(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.ScreenshotArgs
	if err := json.Unmarshal(raw, &a); err != nil && len(raw) > 0 {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	snap := t.Snapshot()
	img, err := render.Render(snap, render.Default())
	if err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInternal, Message: err.Error()}
	}
	if a.Out != "" {
		f, err := os.Create(a.Out)
		if err != nil {
			return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
		}
		defer f.Close()
		if err := render.EncodePNG(f, img); err != nil {
			return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
		}
		return rpc.ScreenshotData{
			Out:    a.Out,
			Width:  img.Bounds().Dx(),
			Height: img.Bounds().Dy(),
		}, nil
	}
	var buf bytes.Buffer
	if err := render.EncodePNG(&buf, img); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInternal, Message: err.Error()}
	}
	return rpc.ScreenshotData{
		PNGBase64: base64.StdEncoding.EncodeToString(buf.Bytes()),
		Width:     img.Bounds().Dx(),
		Height:    img.Bounds().Dy(),
	}, nil
}
package main

import (
	"flag"
	"os"

	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	register("stop", runStop)
	registerUsage("stop", `twee stop [-name <name>]
SIGTERM the child, wait 250ms, escalate to SIGKILL, then remove the
daemon's socket and lock file. Returns {"name": ..., "stopped": true}.`)
}

func runStop(args []string) {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	if err := fs.Parse(args); err != nil {
		fatalUsage("stop: %v", err)
	}
	resp, err := callDaemon(*name, rpc.OpStop, nil)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	if !resp.OK {
		emitError(resp.Error.Code, resp.Error.Message, resp.Error.Details, 1)
	}
	if sp, err := socketPath(*name); err == nil {
		_ = os.Remove(sp)
	}
	if lp, err := lockPath(*name); err == nil {
		_ = os.Remove(lp)
	}
	emitOK(map[string]any{"name": *name, "stopped": true})
}

TOOL CALL: bash
INPUT:
{"command": "cat cmd/twee/cli_e2e_test.go | head -150"}
TOOL RESULT: bash
OUTPUT:
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runCLI executes ./bin/twee with args from the test process' env. It
// must be called only with TWEE_STATE_DIR set in the env, so daemons
// don't collide with the user's real ~/.local/state.
func runCLI(t *testing.T, bin string, env []string, args ...string) (map[string]any, []byte, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.Output()
	if err != nil {
		// Capture stderr if the cmd failed.
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, exitErr.Stderr, err
		}
		return nil, nil, err
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		return nil, out, fmt.Errorf("decode %s: %w", out, err)
	}
	return got, out, nil
}

// testEnv returns the env block for an isolated CLI run.
func testEnv(t *testing.T) []string {
	t.Helper()
	stateDir := t.TempDir()
	ghostty := filepath.Join(repoRoot(t), "build", "_deps", "ghostty-src", "zig-out", "lib")
	return []string{
		"TWEE_STATE_DIR=" + stateDir,
		"DYLD_LIBRARY_PATH=" + ghostty,
		"LD_LIBRARY_PATH=" + ghostty,
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == "/dev/null" {
		t.Fatal("not in a Go module")
	}
	return filepath.Dir(gomod)
}

func TestMenuFixtureViaCLI(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	root := repoRoot(t)
	menuBin := filepath.Join(root, "bin", "menu")
	if _, err := os.Stat(menuBin); err != nil {
		t.Skipf("menu fixture not built (run 'make build'): %v", err)
	}
	defer exec.Command(bin, "stop", "--name", "menu-test").Run()

	mustOK(t, bin, env, "start", "--name", "menu-test", menuBin)
	mustOK(t, bin, env, "wait", "text", "--name", "menu-test", "Choose an option")
	mustOK(t, bin, env, "key", "--name", "menu-test", "Down")
	mustOK(t, bin, env, "wait", "text", "--name", "menu-test", "> second")
	mustOK(t, bin, env, "key", "--name", "menu-test", "Enter")
	mustOK(t, bin, env, "wait", "text", "--name", "menu-test", "selected: second")
	mustOK(t, bin, env, "stop", "--name", "menu-test")
}

func mustOK(t *testing.T, bin string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if resp["ok"] != true {
		t.Fatalf("%v: %s", args, out)
	}
}

func TestStartStatusStopRoundTrip(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	defer exec.Command(bin, "stop", "--name", "rt").Run()

	startOut, raw, err := runCLI(t, bin, env, "start", "--name", "rt", "/bin/sh", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("start: %v\n%s", err, raw)
	}
	if startOut["ok"] != true {
		t.Fatalf("start: %v", startOut)
	}

	time.Sleep(100 * time.Millisecond)

	statusOut, raw2, err := runCLI(t, bin, env, "status", "--name", "rt")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, raw2)
	}
	if statusOut["ok"] != true {
		t.Fatalf("status: %v", statusOut)
	}
	data, _ := statusOut["data"].(map[string]any)
	if data["running"] != true {
		t.Errorf("expected running=true, got %v", data)
	}

	stopOut, raw3, err := runCLI(t, bin, env, "stop", "--name", "rt")
	if err != nil {
		t.Fatalf("stop: %v\n%s", err, raw3)
	}
	if stopOut["ok"] != true {
		t.Fatalf("stop: %v", stopOut)
	}

	// Status after stop should fail.
	cmd := exec.Command(bin, "status", "--name", "rt")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Errorf("expected non-zero exit after stop")
	}
	if !strings.Contains(string(out), "NOT_FOUND") && !strings.Contains(string(out), "no such file") {
		t.Errorf("expected NOT_FOUND after stop, got %s", out)
	}
}

TURN 9
TOOL CALL: bash
INPUT:
{"command": "cat cmd/twee/cli_e2e_test.go | tail -100"}
TOOL RESULT: bash
OUTPUT:
		"DYLD_LIBRARY_PATH=" + ghostty,
		"LD_LIBRARY_PATH=" + ghostty,
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == "/dev/null" {
		t.Fatal("not in a Go module")
	}
	return filepath.Dir(gomod)
}

func TestMenuFixtureViaCLI(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	root := repoRoot(t)
	menuBin := filepath.Join(root, "bin", "menu")
	if _, err := os.Stat(menuBin); err != nil {
		t.Skipf("menu fixture not built (run 'make build'): %v", err)
	}
	defer exec.Command(bin, "stop", "--name", "menu-test").Run()

	mustOK(t, bin, env, "start", "--name", "menu-test", menuBin)
	mustOK(t, bin, env, "wait", "text", "--name", "menu-test", "Choose an option")
	mustOK(t, bin, env, "key", "--name", "menu-test", "Down")
	mustOK(t, bin, env, "wait", "text", "--name", "menu-test", "> second")
	mustOK(t, bin, env, "key", "--name", "menu-test", "Enter")
	mustOK(t, bin, env, "wait", "text", "--name", "menu-test", "selected: second")
	mustOK(t, bin, env, "stop", "--name", "menu-test")
}

func mustOK(t *testing.T, bin string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if resp["ok"] != true {
		t.Fatalf("%v: %s", args, out)
	}
}

func TestStartStatusStopRoundTrip(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	defer exec.Command(bin, "stop", "--name", "rt").Run()

	startOut, raw, err := runCLI(t, bin, env, "start", "--name", "rt", "/bin/sh", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("start: %v\n%s", err, raw)
	}
	if startOut["ok"] != true {
		t.Fatalf("start: %v", startOut)
	}

	time.Sleep(100 * time.Millisecond)

	statusOut, raw2, err := runCLI(t, bin, env, "status", "--name", "rt")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, raw2)
	}
	if statusOut["ok"] != true {
		t.Fatalf("status: %v", statusOut)
	}
	data, _ := statusOut["data"].(map[string]any)
	if data["running"] != true {
		t.Errorf("expected running=true, got %v", data)
	}

	stopOut, raw3, err := runCLI(t, bin, env, "stop", "--name", "rt")
	if err != nil {
		t.Fatalf("stop: %v\n%s", err, raw3)
	}
	if stopOut["ok"] != true {
		t.Fatalf("stop: %v", stopOut)
	}

	// Status after stop should fail.
	cmd := exec.Command(bin, "status", "--name", "rt")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Errorf("expected non-zero exit after stop")
	}
	if !strings.Contains(string(out), "NOT_FOUND") && !strings.Contains(string(out), "no such file") {
		t.Errorf("expected NOT_FOUND after stop, got %s", out)
	}
}

TOOL CALL: bash
INPUT:
{"command": "cat internal/rpc/errors.go && cat internal/vt/model.go"}
TOOL RESULT: bash
OUTPUT:
package rpc

// Error codes. Closed set; agents may branch on these.
const (
	CodeTimeout         = "TIMEOUT"
	CodeNotFound        = "NOT_FOUND"
	CodeAlreadyRunning  = "ALREADY_RUNNING"
	CodeChildExited     = "CHILD_EXITED"
	CodeInvalidArgument = "INVALID_ARGUMENT"
	CodeIO              = "IO"
	CodeInternal        = "INTERNAL"
)
package vt

// Model is the backend interface. Implementations must be safe for use
// from a single goroutine; the harness pump serializes access externally.
type Model interface {
	Feed(p []byte) error
	Resize(cols, rows int) error
	Snapshot() Snapshot
}

// New returns the libghostty-vt-backed Model. The underlying type is
// unexported to keep the backend swappable.
func New(cols, rows int) Model {
	return newGhosttyTerm(cols, rows)
}

TURN 10
TOOL CALL: bash
INPUT:
{"command": "cat internal/vt/types.go && cat internal/vt/visible.go"}
TOOL RESULT: bash
OUTPUT:
// Package vt is the internal terminal model. It is hidden behind a narrow
// interface so the backend (libghostty-vt) can evolve without touching the
// public API.
package vt

// Size is a terminal dimension in cells.
type Size struct {
	Cols, Rows int
}

// Cursor is the cursor position and visibility.
type Cursor struct {
	Col, Row int
	Visible  bool
}

// Color identifies a foreground or background color. Kind selects the
// representation; the other fields are interpreted according to Kind.
type Color struct {
	Kind    ColorKind
	Index   uint8 // for Palette
	R, G, B uint8 // for RGB
}

type ColorKind uint8

const (
	ColorDefault ColorKind = iota
	ColorPalette           // 0..255 (xterm 256-palette; named SGR 0–15 use the low entries)
	ColorRGB
)

// Cell is one display cell. For wide characters the second cell has
// Width=0 and Text="" — the leading cell carries the grapheme.
type Cell struct {
	Text          string
	Width         int
	Fg, Bg        Color
	Bold          bool
	Dim           bool
	Italic        bool
	Underline     bool
	Inverse       bool
	Strikethrough bool
}

// Line is a row of cells.
type Line struct {
	Cells []Cell
}

// Snapshot is an immutable view of the terminal state. Snapshots are full
// copies — callers may retain them indefinitely without affecting the
// live model.
type Snapshot struct {
	Size   Size
	Cursor Cursor
	Lines  []Line
	// AltScreen reports whether the alternate screen is active.
	AltScreen bool
}
package vt

import "strings"

// VisibleText returns the visible viewport as plain text. Continuation
// cells (the second cell of a wide character) are skipped. Trailing
// spaces on each line are stripped. Lines are joined with "\n".
func VisibleText(s Snapshot) string {
	lines := VisibleLines(s)
	return strings.Join(lines, "\n")
}

// VisibleLines returns one string per row, trailing spaces stripped.
func VisibleLines(s Snapshot) []string {
	out := make([]string, len(s.Lines))
	for i, line := range s.Lines {
		var sb strings.Builder
		for _, c := range line.Cells {
			if c.Width == 0 {
				continue
			}
			if c.Text == "" {
				sb.WriteByte(' ')
				continue
			}
			sb.WriteString(c.Text)
		}
		out[i] = strings.TrimRight(sb.String(), " ")
	}
	return out
}

TOOL CALL: bash
INPUT:
{"command": "cat internal/render/render.go | head -60 && cat internal/daemon/handlers_query.go | head -80"}
TOOL RESULT: bash
OUTPUT:
package render

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/paulsmith/research/twee/internal/engine"
)

// Options controls a render pass.
type Options struct {
	SizePt float64 // font size in points; default 14
}

// Default returns sensible options.
func Default() Options { return Options{SizePt: 14} }

// Render rasterizes the snapshot and returns the resulting RGBA image.
func Render(snap engine.Snapshot, opts Options) (*image.RGBA, error) {
	if opts.SizePt == 0 {
		opts.SizePt = 14
	}
	face, err := Face(opts.SizePt)
	if err != nil {
		return nil, err
	}
	cw, ch := cellMetrics(face)
	w := cw * snap.Cols
	h := ch * snap.Rows
	if w <= 0 || h <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	// Default background: black.
	bg := color.RGBA{0, 0, 0, 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)

	for y := 0; y < snap.Rows && y < len(snap.Lines); y++ {
		line := snap.Lines[y].Cells
		for x := 0; x < snap.Cols && x < len(line); x++ {
			drawCell(img, x, y, cw, ch, face, line[x])
		}
	}
	return img, nil
}

// EncodePNG writes the image as PNG to w.
func EncodePNG(w io.Writer, img image.Image) error {
	return png.Encode(w, img)
}

func cellMetrics(face font.Face) (cw, ch int) {
	adv, _ := face.GlyphAdvance('M')
package daemon

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	optionalRegistrations = append(optionalRegistrations, func(d *Dispatcher) {
		d.Register(rpc.OpText, handleText)
		d.Register(rpc.OpLines, handleLines)
		d.Register(rpc.OpCell, handleCell)
		d.Register(rpc.OpRegion, handleRegion)
		d.Register(rpc.OpCursor, handleCursor)
		d.Register(rpc.OpFind, handleFind)
		d.Register(rpc.OpSize, handleSize)
		d.Register(rpc.OpTitle, handleTitle)
		d.Register(rpc.OpMode, handleMode)
		d.Register(rpc.OpScrollback, handleScrollback)
		d.Register(rpc.OpSnapshot, handleSnapshot)
	})
}

func handleText(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	return rpc.TextData{Text: t.VisibleText()}, nil
}

func handleLines(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	return rpc.LinesData{Lines: t.Lines()}, nil
}

func handleCell(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.CellArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	snap := t.Snapshot()
	if a.Y < 0 || a.Y >= len(snap.Lines) {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: "y out of range"}
	}
	row := snap.Lines[a.Y].Cells
	if a.X < 0 || a.X >= len(row) {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: "x out of range"}
	}
	return row[a.X], nil
}

func handleRegion(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.RegionArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	if a.W <= 0 || a.H <= 0 {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: "w and h must be > 0"}
	}
	snap := t.Snapshot()
	out := make([][]engine.Cell, 0, a.H)
	for y := a.Y; y < a.Y+a.H && y < len(snap.Lines); y++ {
		row := snap.Lines[y].Cells
		end := a.X + a.W
		if end > len(row) {
			end = len(row)
		}
		if a.X < 0 || a.X > end {
			out = append(out, []engine.Cell{})
			continue
		}
		out = append(out, row[a.X:end])
	}
	return out, nil
}

func handleCursor(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	c := t.CursorPos()
	return rpc.CursorData{X: c.Col, Y: c.Row, Visible: c.Visible}, nil
}

TURN 11
TOOL CALL: bash
INPUT:
{"command": "cat internal/engine/query.go && cat internal/engine/types.go"}
TOOL RESULT: bash
OUTPUT:
package engine

import (
	"fmt"

	"github.com/paulsmith/research/twee/internal/vt"
)

// Snapshot returns the current terminal state.
func (t *Term) Snapshot() Snapshot {
	return fromVT(t.pump.Snapshot())
}

// VisibleText returns the visible viewport as plain text, trailing
// spaces stripped, lines joined with "\n".
func (t *Term) VisibleText() string {
	return vt.VisibleText(t.pump.Snapshot())
}

// Lines returns one string per row, trailing spaces stripped.
func (t *Term) Lines() []string {
	return vt.VisibleLines(t.pump.Snapshot())
}

// CursorPos returns the cursor position. (Distinct name from the type
// `Cursor` to avoid the embedding shadow when tuitest.Term embeds *Term.)
func (t *Term) CursorPos() Cursor {
	c := t.pump.Snapshot().Cursor
	return Cursor{Col: c.Col, Row: c.Row, Visible: c.Visible}
}

// ExitCode is valid after the child has exited.
func (t *Term) ExitCode() int { return t.runner.ExitCode() }

// ExitedCh returns a channel that closes when the child exits.
func (t *Term) ExitedCh() <-chan struct{} { return t.runner.ExitedCh() }

// RecentBytes returns up to N bytes of recent PTY output, oldest first.
func (t *Term) RecentBytes() []byte { return t.pump.RecentBytes() }

// RecentInputs returns a copy of the input-events ring buffer.
func (t *Term) RecentInputs() []InputEvent {
	t.inputsMu.Lock()
	defer t.inputsMu.Unlock()
	return append([]InputEvent(nil), t.inputs...)
}

// Diagnostic returns a multi-line failure block describing current state.
func (t *Term) Diagnostic() string {
	snap := t.pump.Snapshot()
	lines := vt.VisibleLines(snap)
	var sb diagBuf
	sb.printf("command: %v\n", t.cfg.Cmd)
	sb.printf("size: %dx%d\n", snap.Size.Cols, snap.Size.Rows)
	sb.printf("cursor: (%d,%d)\n", snap.Cursor.Col, snap.Cursor.Row)
	sb.printf("alt screen: %v\n", snap.AltScreen)
	select {
	case <-t.runner.ExitedCh():
		sb.printf("exit status: %d\n", t.runner.ExitCode())
	default:
		sb.printf("exit status: (still running)\n")
	}
	sb.printf("--- visible screen ---\n")
	for _, ln := range lines {
		sb.printf("%s\n", ln)
	}
	sb.printf("--- recent input events (last 16) ---\n")
	evs := t.RecentInputs()
	if n := len(evs); n > 16 {
		evs = evs[n-16:]
	}
	if len(evs) == 0 {
		sb.printf("(none)\n")
	}
	for _, ev := range evs {
		sb.printf("  %s\n", ev.Desc)
	}
	sb.printf("--- recent bytes (escaped, last 1KB) ---\n")
	r := t.pump.RecentBytes()
	if len(r) > 1024 {
		r = r[len(r)-1024:]
	}
	sb.printf("%q\n", string(r))
	if t.cfg.RecordPath != "" {
		sb.printf("recording: %s\n", t.cfg.RecordPath)
	}
	return sb.String()
}

type diagBuf struct{ b []byte }

func (s *diagBuf) printf(format string, a ...any) {
	s.b = append(s.b, fmt.Sprintf(format, a...)...)
}
func (s *diagBuf) String() string { return string(s.b) }
package engine

import "github.com/paulsmith/research/twee/internal/vt"

// Snapshot is an immutable view of terminal state at a moment in time.
type Snapshot struct {
	Cols, Rows int
	Cursor     Cursor
	Lines      []Line
	AltScreen  bool
}

// Cursor position and visibility.
type Cursor struct {
	Col, Row int
	Visible  bool
}

// Line is a row of cells.
type Line struct{ Cells []Cell }

// Cell is one display cell. The second cell of a wide character has
// Width=0 and Text="".
type Cell struct {
	Text      string
	Width     int
	Fg, Bg    Color
	Bold      bool
	Dim       bool
	Underline bool
	Inverse   bool
}

// Color identifies a cell color.
type Color struct {
	Kind    ColorKind
	Index   uint8
	R, G, B uint8
}

// ColorKind selects how a Color is interpreted.
type ColorKind uint8

const (
	ColorDefault ColorKind = iota
	ColorIndexed
	ColorPalette
	ColorRGB
)

func fromVT(s vt.Snapshot) Snapshot {
	out := Snapshot{
		Cols:      s.Size.Cols,
		Rows:      s.Size.Rows,
		Cursor:    Cursor{Col: s.Cursor.Col, Row: s.Cursor.Row, Visible: s.Cursor.Visible},
		AltScreen: s.AltScreen,
		Lines:     make([]Line, len(s.Lines)),
	}
	for i, ln := range s.Lines {
		cells := make([]Cell, len(ln.Cells))
		for j, c := range ln.Cells {
			cells[j] = Cell{
				Text: c.Text, Width: c.Width,
				Fg: fromVTColor(c.Fg), Bg: fromVTColor(c.Bg),
				Bold: c.Bold, Dim: c.Dim,
				Underline: c.Underline, Inverse: c.Inverse,
			}
		}
		out.Lines[i] = Line{Cells: cells}
	}
	return out
}

func fromVTColor(c vt.Color) Color {
	return Color{
		Kind:  ColorKind(c.Kind),
		Index: c.Index,
		R:     c.R, G: c.G, B: c.B,
	}
}

TOOL CALL: bash
INPUT:
{"command": "cat go.mod && cat Makefile 2>/dev/null || ls -la"}
TOOL RESULT: bash
OUTPUT:
module github.com/paulsmith/research/twee

go 1.26.0

require (
	github.com/creack/pty v1.1.24
	github.com/mitchellh/go-libghostty v0.0.0-20260428141358-29fdb3130d7c
	golang.org/x/image v0.39.0
	golang.org/x/term v0.42.0
)

require (
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
)
BUILD_DIR := build

# CMake's FetchContent populates ghostty source under build/_deps/ghostty-src.
# Its zig-out is what go-libghostty's pkgconfig points at.
GHOSTTY_ZIG_OUT := $(CURDIR)/$(BUILD_DIR)/_deps/ghostty-src/zig-out
PKG_CONFIG_PATH := $(GHOSTTY_ZIG_OUT)/share/pkgconfig
DYLD_LIBRARY_PATH := $(GHOSTTY_ZIG_OUT)/lib
LD_LIBRARY_PATH := $(GHOSTTY_ZIG_OUT)/lib

STAMP := $(BUILD_DIR)/.ghostty-built

VERSION := $(shell jj log -r @ -T 'change_id.short()' --no-graph 2>/dev/null || echo dev)

.PHONY: all build test smoke clean twee

all: build

$(STAMP):
	cmake -B $(BUILD_DIR) -DCMAKE_BUILD_TYPE=Release
	cmake --build $(BUILD_DIR)
	@touch $(STAMP)

twee: $(STAMP)
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) go build -o ./bin/twee \
		-ldflags "-X main.Version=$(VERSION)" ./cmd/twee

build: $(STAMP) twee
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) go build ./...

test: $(STAMP)
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) \
	DYLD_LIBRARY_PATH=$(DYLD_LIBRARY_PATH) \
	LD_LIBRARY_PATH=$(LD_LIBRARY_PATH) \
	go test ./...

smoke: $(STAMP)
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) \
	DYLD_LIBRARY_PATH=$(DYLD_LIBRARY_PATH) \
	LD_LIBRARY_PATH=$(LD_LIBRARY_PATH) \
	go run ./cmd/libghostty-smoke

clean:
	rm -rf $(BUILD_DIR)

TURN 12
TOOL CALL: bash
INPUT:
{"command": "cat cmd/twee/cmd_run.go"}
TOOL RESULT: bash
OUTPUT:
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

	"github.com/paulsmith/research/twee/internal/daemon"
	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	register("run", runRun)
	registerUsage("run", `twee run <cmd> [args...] [flags]
Single-shot: spin up an ephemeral daemon, execute a JSON script of
RPC ops, exit. The daemon's socket is removed on exit.

Flags:
  -script <path>   path to script JSON; "-" or empty reads stdin
  -cols <int>      initial cols (default 80)
  -rows <int>      initial rows (default 24)
  -dir <path>      child working directory
  -emit results    stream NDJSON op responses instead of one summary

The script is a JSON array of RPC bodies (op + args). Use the wire
op names (e.g. "wait_text", not "wait text"). See:
  docs/superpowers/specs/2026-04-28-twee-cli-design.md`)
}

func runRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	scriptPath := fs.String("script", "", "path to script JSON; '-' or empty reads stdin")
	cols := fs.Int("cols", 80, "initial cols")
	rows := fs.Int("rows", 24, "initial rows")
	dir := fs.String("dir", "", "child working dir")
	emit := fs.String("emit", "", "if 'results', stream NDJSON op responses")
	if err := fs.Parse(args); err != nil {
		fatalUsage("run: %v", err)
	}
	cmd := fs.Args()
	if len(cmd) == 0 {
		fatalUsage("run: missing command")
	}

	scriptBytes, err := readScript(*scriptPath)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	var ops []rpc.Request
	if err := json.Unmarshal(scriptBytes, &ops); err != nil {
		emitError(rpc.CodeInvalidArgument, "script: "+err.Error(), nil, 1)
	}

	te, err := engine.Start(context.Background(), engine.Config{
		Cmd: cmd, Cols: *cols, Rows: *rows, Dir: *dir,
	})
	if err != nil {
		emitError(rpc.CodeIO, "engine.Start: "+err.Error(), nil, 1)
	}
	defer te.Close()

	tmpDir, _ := os.MkdirTemp("", "twee-run-")
	defer os.RemoveAll(tmpDir)
	sock := filepath.Join(tmpDir, "twee.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	defer l.Close()

	srv := daemon.NewServer(te)
	go srv.Serve(context.Background(), l)
	defer srv.Stop()

	emitResults := *emit == "results"
	for i, op := range ops {
		op.ID = fmt.Sprintf("%d", i)
		c, err := net.Dial("unix", sock)
		if err != nil {
			emitError(rpc.CodeIO, err.Error(), nil, 1)
		}
		if err := rpc.WriteMessage(c, op); err != nil {
			c.Close()
			emitError(rpc.CodeIO, err.Error(), nil, 1)
		}
		var resp rpc.Response
		if err := rpc.ReadMessage(c, &resp); err != nil {
			c.Close()
			emitError(rpc.CodeIO, err.Error(), nil, 1)
		}
		c.Close()
		if emitResults {
			_ = json.NewEncoder(os.Stdout).Encode(resp)
		}
		if !resp.OK {
			if !emitResults {
				emitError(resp.Error.Code, resp.Error.Message, resp.Error.Details, 1)
			}
			os.Exit(1)
		}
	}
	if !emitResults {
		emitOK(map[string]any{"ops": len(ops)})
	}
}

func readScript(path string) ([]byte, error) {
	if path == "" || path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

TOOL CALL: bash
INPUT:
{"command": "cat tuitest/runner_smoke_test.go && cat tuitest/term.go"}
TOOL RESULT: bash
OUTPUT:
package tuitest

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunHelloWorld(t *testing.T) {
	term := Run(t, "/bin/sh", Args("-c", "printf 'hello\\r\\nworld\\r\\n'"), Size(40, 5))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		text := term.VisibleText()
		if strings.Contains(text, "hello") && strings.Contains(text, "world") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("did not see expected output:\n%s", term.Diagnostic())
}

func TestCloseTerminatesProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	term, err := Start(ctx, Command("/bin/sh", "-c", "sleep 30"), Size(40, 5))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := term.Close(); err != nil {
		t.Logf("Close: %v", err) // some EIO is expected
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Close took %v, expected <3s", elapsed)
	}
}
// Package tuitest is a Playwright-style test harness for terminal UIs.
//
// A Term wraps a child process running under a PTY. Output is parsed by
// an internal VT model; tests inject input via Type/Key/Paste and assert
// on visible state via WaitForText/ExpectText/Snapshot.
//
// All Wait* methods have a default 5s timeout, overridable per call with
// WithTimeout. Expect* helpers wrap the corresponding Wait* and call
// t.Fatalf on timeout.
package tuitest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/paulsmith/research/twee/internal/engine"
)

// Term is a running TUI under test.
//
// recordPath is not stored here; it lives on the embedded engine and
// is reachable via the promoted RecordPath() method.
type Term struct {
	*engine.Term
	t testing.TB // optional; required for Expect*
}

// Run constructs a Term bound to a *testing.T running the given command.
func Run(t testing.TB, command string, opts ...Option) *Term {
	t.Helper()
	cfg := newConfig()
	for _, o := range opts {
		o(cfg)
	}
	if len(cfg.cmd) == 0 {
		cfg.cmd = append([]string{command}, cfg.extraArgs...)
	}
	if cfg.recordPath == "" && os.Getenv("TUITEST_RECORD") != "0" {
		cfg.recordPath = filepath.Join(t.TempDir(), "session.jsonl")
	}
	eng, err := engine.Start(context.Background(), cfg.toEngine())
	if err != nil {
		t.Fatalf("tuitest.Run: %v", err)
	}
	te := &Term{Term: eng, t: t}
	t.Cleanup(func() {
		_ = te.Close()
		if t.Failed() && te.RecordPath() != "" {
			t.Logf("tuitest recording: %s", te.RecordPath())
		}
	})
	return te
}

// Start is the lower-level constructor. Caller is responsible for Close.
func Start(ctx context.Context, opts ...Option) (*Term, error) {
	cfg := newConfig()
	for _, o := range opts {
		o(cfg)
	}
	if len(cfg.cmd) == 0 {
		return nil, errors.New("tuitest.Start: no command (use Command option)")
	}
	eng, err := engine.Start(ctx, cfg.toEngine())
	if err != nil {
		return nil, err
	}
	return &Term{Term: eng}, nil
}

// Cursor returns the cursor position. Wrapper that gives the historic
// method name (engine exposes CursorPos to avoid embedding shadow with
// the engine.Cursor type).
func (te *Term) Cursor() Cursor { return te.Term.CursorPos() }

TURN 13
TOOL CALL: bash
INPUT:
{"command": "cat tuitest/options.go && cat tuitest/snapshot.go"}
TOOL RESULT: bash
OUTPUT:
package tuitest

import (
	"time"

	"github.com/paulsmith/research/twee/internal/engine"
)

// Option configures a Term.
type Option func(*config)

type config struct {
	cmd        []string
	extraArgs  []string
	env        map[string]string
	dir        string
	cols, rows int

	defaultTimeout    time.Duration
	stableQuietWindow time.Duration

	recordPath string
}

func newConfig() *config {
	return &config{
		cols:              80,
		rows:              24,
		defaultTimeout:    5 * time.Second,
		stableQuietWindow: 100 * time.Millisecond,
		env:               map[string]string{},
	}
}

func (c *config) toEngine() engine.Config {
	return engine.Config{
		Cmd:               c.cmd,
		Env:               c.env,
		Dir:               c.dir,
		Cols:              c.cols,
		Rows:              c.rows,
		DefaultTimeout:    c.defaultTimeout,
		StableQuietWindow: c.stableQuietWindow,
		RecordPath:        c.recordPath,
	}
}

// Command sets the command to run.
func Command(args ...string) Option {
	return func(c *config) { c.cmd = append([]string{}, args...) }
}

// Args appends arguments after the command.
func Args(args ...string) Option {
	return func(c *config) { c.extraArgs = append(c.extraArgs, args...) }
}

// Size sets the initial terminal size.
func Size(cols, rows int) Option {
	return func(c *config) { c.cols, c.rows = cols, rows }
}

// Env sets a single environment variable.
func Env(key, value string) Option {
	return func(c *config) { c.env[key] = value }
}

// Dir sets the working directory of the child.
func Dir(dir string) Option {
	return func(c *config) { c.dir = dir }
}

// DefaultTimeout sets the default timeout for WaitFor* and Expect*.
func DefaultTimeout(d time.Duration) Option {
	return func(c *config) { c.defaultTimeout = d }
}

// Record enables session recording to the given path.
func Record(path string) Option {
	return func(c *config) { c.recordPath = path }
}
package tuitest

import "github.com/paulsmith/research/twee/internal/engine"

// Type aliases — the engine owns the actual definitions.
type (
	Snapshot  = engine.Snapshot
	Cursor    = engine.Cursor
	Line      = engine.Line
	Cell      = engine.Cell
	Color     = engine.Color
	ColorKind = engine.ColorKind
)

// Color-kind constants.
const (
	ColorDefault = engine.ColorDefault
	ColorIndexed = engine.ColorIndexed
	ColorPalette = engine.ColorPalette
	ColorRGB     = engine.ColorRGB
)

TOOL CALL: bash
INPUT:
{"command": "cat internal/daemon/server.go && cat internal/daemon/server_test.go"}
TOOL RESULT: bash
OUTPUT:
// Package daemon serves twee RPC ops over a Unix socket against a
// single engine.Term.
package daemon

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/rpc"
)

// Server owns one engine.Term and serves RPC over a net.Listener.
type Server struct {
	term *engine.Term
	d    *Dispatcher

	wg     sync.WaitGroup
	stopCh chan struct{}
	once   sync.Once
}

// NewServer constructs a Server wrapping the given Term.
func NewServer(t *engine.Term) *Server {
	return &Server{
		term:   t,
		d:      NewDispatcher(t),
		stopCh: make(chan struct{}),
	}
}

// Serve accepts connections on l until either the listener errors, the
// context cancels, or Stop is called.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	go func() {
		select {
		case <-ctx.Done():
			_ = l.Close()
		case <-s.stopCh:
			_ = l.Close()
		}
	}()
	for {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				s.wg.Wait()
				return nil
			}
			return err
		}
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			s.handleConn(c)
		}(conn)
	}
}

// Stop signals the accept loop to exit. Existing connections drain.
func (s *Server) Stop() {
	s.once.Do(func() { close(s.stopCh) })
}

// Term returns the wrapped Term.
func (s *Server) Term() *engine.Term { return s.term }

func (s *Server) handleConn(c net.Conn) {
	defer c.Close()
	var req rpc.Request
	if err := rpc.ReadMessage(c, &req); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return
		}
		_ = rpc.WriteMessage(c, rpc.Response{
			OK: false,
			Error: &rpc.Error{
				Code:    rpc.CodeIO,
				Message: "rpc read: " + err.Error(),
			},
		})
		return
	}
	resp := s.d.Dispatch(req)
	_ = rpc.WriteMessage(c, resp)
}
package daemon

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/rpc"
)

// startTestTerm spawns /bin/sh that prints a known string and waits
// until killed. Returns the running engine.Term.
func startTestTerm(t *testing.T) *engine.Term {
	t.Helper()
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd:  []string{"/bin/sh", "-c", "printf 'hello\\r\\n'; sleep 30"},
		Cols: 40, Rows: 5,
	})
	if err != nil {
		t.Fatalf("engine.Start: %v", err)
	}
	if err := te.WaitForText("hello"); err != nil {
		_ = te.Close()
		t.Fatalf("WaitForText: %v", err)
	}
	t.Cleanup(func() { _ = te.Close() })
	return te
}

func startTestServer(t *testing.T, te *engine.Term) (string, *Server) {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := NewServer(te)
	go func() { _ = s.Serve(context.Background(), l) }()
	t.Cleanup(func() {
		s.Stop()
		_ = l.Close()
		_ = os.Remove(sock)
	})
	return sock, s
}

func dialAndCall(t *testing.T, sock string, req rpc.Request) rpc.Response {
	t.Helper()
	c, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if err := rpc.WriteMessage(c, req); err != nil {
		t.Fatalf("write: %v", err)
	}
	var resp rpc.Response
	if err := rpc.ReadMessage(c, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	return resp
}

func TestStatusOp(t *testing.T) {
	te := startTestTerm(t)
	sock, _ := startTestServer(t, te)

	resp := dialAndCall(t, sock, rpc.Request{ID: "1", Op: rpc.OpStatus})
	if !resp.OK {
		t.Fatalf("status: %+v", resp.Error)
	}
}

func TestUnknownOp(t *testing.T) {
	te := startTestTerm(t)
	sock, _ := startTestServer(t, te)

	resp := dialAndCall(t, sock, rpc.Request{ID: "1", Op: "no-such-op"})
	if resp.OK {
		t.Fatalf("expected error response")
	}
	if resp.Error.Code != rpc.CodeInvalidArgument {
		t.Errorf("error code = %q, want %q", resp.Error.Code, rpc.CodeInvalidArgument)
	}
}

TURN 14
TOOL CALL: bash
INPUT:
{"command": "cat cmd/twee/main_test.go && cat cmd/twee/flake_test.go"}
TOOL RESULT: bash
OUTPUT:
package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildBinary compiles cmd/twee into a temp dir once per test process.
func buildBinary(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "twee")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func TestVersion(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(string(out)) == "" {
		t.Errorf("expected non-empty version, got %q", out)
	}
}

func TestHelp(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "help").Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !bytes.Contains(out, []byte("Usage: twee")) {
		t.Errorf("help output missing usage banner:\n%s", out)
	}
}

func TestUnknownVerbExits2(t *testing.T) {
	bin := buildBinary(t)
	cmd := exec.Command(bin, "bogus")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exit.ExitCode() != 2 {
		t.Errorf("exit code %d, want 2", exit.ExitCode())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("unknown subcommand")) {
		t.Errorf("stderr missing 'unknown subcommand':\n%s", stderr.String())
	}
}
//go:build flake
// +build flake

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFlake200Menu(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	root := repoRoot(t)
	menuBin := filepath.Join(root, "bin", "menu")
	if _, err := os.Stat(menuBin); err != nil {
		t.Skipf("menu fixture not built: %v", err)
	}

	dir := t.TempDir()
	script := []map[string]any{
		{"op": "wait_text", "args": map[string]any{"text": "Choose an option", "timeout": "5s"}},
		{"op": "key", "args": map[string]any{"key": "Down"}},
		{"op": "wait_text", "args": map[string]any{"text": "> second", "timeout": "5s"}},
		{"op": "key", "args": map[string]any{"key": "Enter"}},
		{"op": "wait_text", "args": map[string]any{"text": "selected: second", "timeout": "5s"}},
	}
	b, _ := json.Marshal(script)
	scriptPath := filepath.Join(dir, "ops.json")
	if err := os.WriteFile(scriptPath, b, 0o600); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 200; i++ {
		cmd := exec.Command(bin, "run", "--script", scriptPath, menuBin)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("iter %d: %v\n%s", i, err, out)
		}
	}
}

TOOL CALL: bash
INPUT:
{"command": "cat design.md 2>/dev/null | head -150 || echo \"No design.md\""}
TOOL RESULT: bash
OUTPUT:
# Design: Go TUI Test Harness Using `libghostty-vt`

## Status

Draft for implementation.

## Context

`libghostty-vt` is the virtual terminal core extracted from Ghostty. It parses terminal control sequences and maintains terminal state: screen cells, cursor, styles, wrapping, scrollback, and related VT behavior. A Go binding exists (`go-libghostty`) using CGO, but the Go API should be treated as unstable and hidden behind our own interface.

This design targets deterministic TUI tests for Go projects and other terminal applications. The goal is to assert against structured terminal state, not screenshots, OCR, `tmux capture-pane`, or ad hoc ANSI parsing.

## Critique of the previous design

The previous version had the right core idea but was too optimistic in a few places:

1. It treated terminal input encoding as a small detail. It is not. Keys depend on terminal mode, modifiers, application cursor mode, bracketed paste, mouse reporting, and sometimes TERM/terminfo behavior.
2. It suggested `TERM=xterm-256color` as a reasonable default. That may work for many apps, but it hides whether we are emulating Ghostty, xterm, or a reduced test terminal. We need an explicit compatibility profile.
3. It under-specified the event loop. A good harness must continuously drain the PTY, feed the terminal model, track quiescence, and avoid deadlocks.
4. It exposed style assertions too early. Visible text and cell layout matter first; styles should be a second-order feature.
5. It treated snapshots as straightforward. Full cell snapshots are useful but brittle. Normalized text snapshots should be the default; full cell snapshots should be opt-in.
6. It omitted recording/replay from v0. That was a mistake. Recording byte streams is cheap and invaluable for debugging.
7. It did not clearly separate public API, backend API, and test-runner lifecycle.
8. It did not address golden terminal byte-stream tests for the harness itself.
9. It implied mouse support in v0. That should be deferred unless there is a concrete app that needs it.
10. It did not discuss terminal-size determinism, locale, Unicode width, or process cleanup deeply enough.

## Goals

Build a Go library that can:

- Spawn a command under a PTY.
- Feed PTY output into `libghostty-vt`.
- Maintain a structured terminal screen model.
- Inject common keyboard input.
- Wait for stable observable states.
- Assert on visible text, cursor position, and selected cells.
- Record terminal sessions for debugging and replay.
- Work well in `go test`.

## Non-goals for v0

- Driving the Ghostty GUI.
- Screenshot comparison.
- OCR.
- `tmux` integration.
- Pixel-perfect rendering.
- Full mouse protocol support.
- Full terminfo emulation.
- Cross-platform support beyond Unix-like systems with PTYs.
- Guaranteeing identical behavior to every real terminal.
- Public exposure of `libghostty` or `go-libghostty` types.

## Architecture

```text
Go test
  |
  v
Public harness API
  |
  +-- Runner: process + PTY lifecycle
  +-- Pump: PTY output drain + terminal model feed
  +-- Input: semantic key/paste/resize API
  +-- Model: internal libghostty-vt wrapper
  +-- Query: text, line, region, cell, cursor
  +-- Assert/wait: polling over model snapshots
  +-- Recorder: raw byte stream + input events + resize events
  |
  v
Application under test
```

## Key design principle

The PTY byte stream is the source of truth. The terminal model is a derived state.

The harness should be able to save and replay:

```text
initial size
environment
output bytes
input events
resize events
process exit
timing metadata
```

This makes flakes diagnosable and lets us test the harness independently of live applications.

## Package shape

Suggested module layout:

```text
/tuitest
  public API

/internal/ptyrunner
  exec, PTY setup, process lifecycle

/internal/vt
  libghostty-vt wrapper

/internal/input
  key encoding and terminal mode handling

/internal/pump
  read loop, model feed, synchronization

/internal/snapshot
  text and cell snapshot serialization

/internal/recording
  session recording and replay
```

## Public API sketch

```go
func TestCreateProject(t *testing.T) {
    term := tuitest.Run(t, "./myapp",
        tuitest.Size(100, 30),
        tuitest.Env("NO_COLOR", ""),
    )

    term.WaitForText("Projects")

    term.Key(tuitest.CtrlN)
    term.Type("pushup")
    term.Key(tuitest.Enter)

    term.WaitForText("Saved")
    term.ExpectText("pushup")
    term.ExpectNoText("panic")
}
```

Prefer `Run(t, ...)` so the library can register cleanup with `t.Cleanup`.

Also support lower-level construction:

```go
term, err := tuitest.Start(ctx, tuitest.Command("./myapp"))
if err != nil {
    return err
}
defer term.Close()
```

TURN 15
TOOL CALL: bash
INPUT:
{"command": "cat plan.md 2>/dev/null | head -100 || echo \"No plan.md\""}
TOOL RESULT: bash
OUTPUT:
# Implementation Plan: twee (Go TUI Test Harness)

> **Note:** the VT-backend portion of this plan is superseded by
> [`docs/plan-libghostty.md`](docs/plan-libghostty.md), which swaps the
> hand-rolled pure-Go emulator for `libghostty-vt`.

This plan operationalizes `design.md`. It also resolves or sequences the
critical issues flagged in the design review so they don't block work
mid-milestone.

Module path: `github.com/paulsmith/research/twee`. Public package: `tuitest`.
The directory `twee/` is the repo location; `tuitest` is the import name.

## Up-front decisions

These are committed now so milestones don't stall on them.

- **Default terminal profile**: `xterm-256color`. No DECCKM (application
  cursor mode) awareness in v0 — cursor keys always emit the CSI form
  (`ESC [ A` etc.). Document the limitation; revisit in v1 if a fixture
  needs it.
- **Default timeouts**: `WaitFor*` = 5s. `WaitForStableScreen` = 100ms
  quiescence window with 5s overall cap. Both overridable globally
  (`tuitest.SetDefaultTimeout`) and per call (`WithTimeout(d)`).
- **`Expect*` vs `WaitFor*`**: `Expect*` is `WaitFor*` with the default
  timeout, calling `t.Fatalf` on timeout. There is no synchronous
  snapshot assertion in the public API. Tests that want sync semantics
  call `Snapshot()` and inspect the returned struct directly.
- **`Type` / `Key` concurrency**: synchronous write to the PTY master,
  no waiting for any model effect. Ordering guarantee: bytes written by
  `Type` are flushed before the call returns, so a subsequent
  `WaitForText` will observe their effects once the pump processes them.
- **`WaitForText` matching**: against the visible viewport only, not
  scrollback. Match is a substring search against each line's collapsed
  visible text (continuation cells removed, trailing spaces stripped).
  Cross-line matching is not supported in v0; users who need it call
  `WaitForTextRegex` or assert on `VisibleText()` directly.
- **stderr**: merged into the PTY by default (matches how a real
  terminal works). No separate capture in v0.
- **Scrollback in `VisibleText()`**: viewport only. Scrollback access is
  a separate method, `Scrollback() []string`, gated by an option that
  enables retention.
- **PTY library**: `github.com/creack/pty`.

## Milestone 0: preflight (½ day)

Goal: a repo skeleton that builds and runs a trivial test in CI.

- Create `twee/` Go module: `go mod init github.com/paulsmith/research/twee`.
- Add directory layout from design doc: `tuitest/` (public),
  `internal/{vt,ptyrunner,pump,input,snapshot,recording}/`.
- Add `go-libghostty` as a dependency. Vendor it. Pin the C
  `libghostty-vt` build to a known commit; commit a build script under
  `scripts/build-libghostty.sh`.
- CI: a GitHub Actions workflow that installs the C toolchain, builds
  libghostty-vt, runs `go test ./...`. Linux only.
- Smoke test: a single passing test in `tuitest/` that imports nothing
  from libghostty yet.

Exit criterion: `go test ./...` green in CI.

## Milestone 1: VT model wrapper (2-3 days)

Goal: feed bytes to a hidden libghostty model and read structured state
back out, with no PTY involved.

Files:

- `internal/vt/model.go` — `Model` interface, libghostty-backed impl.
- `internal/vt/snapshot.go` — `Snapshot`, `Line`, `Cell`, `Cursor`,
  `Size`, `Color` types, all Go-native.
- `internal/vt/visible.go` — `VisibleText(snap)` extraction.
- `internal/vt/model_test.go` — golden byte-stream tests.

Concrete tasks:

1. Define the `Model` interface from the design doc (`Feed`, `Resize`,
   `Snapshot`).
2. Implement it against `go-libghostty`. Each `Snapshot()` call returns
   a fully copied, immutable Go struct — no shared memory with the C
   side. Snapshots are cheap because tests don't run a hot loop.
3. Implement `VisibleText`: walk lines, skip continuation cells, strip
   trailing spaces per line, join with `\n`. No scrollback.
4. Golden tests for: plain ASCII, CR/LF handling, cursor movement
   (`CUP`, `CUU`, `CUD`, `CUF`, `CUB`), erase (`ED`, `EL`), line wrap
   at right margin, SGR styles (bold, underline, fg/bg color),
   alternate screen enter/exit, and a wide-character + combining-mark
   string. Each test is a `[]byte` literal → `Feed` → assertion on
   `VisibleText`/`Snapshot`.

Exit criterion: ~15 golden byte-stream tests passing. No PTY code yet.

## Milestone 2: PTY runner + pump (3-4 days)

Goal: spawn a process under a PTY, drain its output into the model
continuously, and clean up reliably.

Files:

- `internal/ptyrunner/runner.go` — `Runner` type wrapping `exec.Cmd` +

TOOL CALL: bash
INPUT:
{"command": "cat tuitest/testdata/*.jsonl 2>/dev/null | head -20; ls tuitest/testdata/ 2>/dev/null"}
TOOL RESULT: bash
OUTPUT:
snapshots

TURN 16
TOOL CALL: bash
INPUT:
{"command": "ls tuitest/testdata/snapshots/ 2>/dev/null && cat tuitest/snapshot_assert.go"}
TOOL RESULT: bash
OUTPUT:
TestMenuSnapshotInitialScreen
package tuitest

import (
	"flag"
	"path/filepath"

	"github.com/paulsmith/research/twee/internal/snapshot"
)

var updateSnapshots = flag.Bool("tuitest-update", false,
	"update tuitest snapshots instead of comparing")

// ExpectTextSnapshot compares the current visible text against
// testdata/snapshots/<test>/<name>.txt. With -tuitest-update the file
// is overwritten.
func (te *Term) ExpectTextSnapshot(name string) {
	if te.t == nil {
		panic("tuitest: ExpectTextSnapshot requires Run(t, ...) construction")
	}
	te.t.Helper()
	testName := filepath.Base(te.t.Name())
	path := filepath.Join("testdata", "snapshots", testName, name+".txt")
	actual := te.VisibleText()
	res, err := snapshot.CompareText(path, actual, *updateSnapshots)
	if err != nil {
		te.t.Fatalf("%v", err)
	}
	if res.Updated {
		te.t.Logf("snapshot updated: %s", res.Path)
	}
}

TOOL CALL: bash
INPUT:
{"command": "cat internal/snapshot/diff.go && cat internal/snapshot/text.go"}
TOOL RESULT: bash
OUTPUT:
package snapshot

import (
	"fmt"
	"strings"
)

// UnifiedDiff returns a human-readable line-by-line diff of expected
// vs actual. Returns "" when equal. Output uses leading-space context
// lines and `-`/`+` for changes (similar to unified diff but without
// minimal hunks — sufficient for v0).
func UnifiedDiff(expected, actual string) string {
	if expected == actual {
		return ""
	}
	wl := strings.Split(expected, "\n")
	gl := strings.Split(actual, "\n")
	var sb strings.Builder
	sb.WriteString("--- expected\n+++ actual\n@@ @@\n")
	maxLen := len(wl)
	if len(gl) > maxLen {
		maxLen = len(gl)
	}
	for i := 0; i < maxLen; i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		switch {
		case w == g:
			fmt.Fprintf(&sb, " %s\n", w)
		case w == "":
			fmt.Fprintf(&sb, "+%s\n", g)
		case g == "":
			fmt.Fprintf(&sb, "-%s\n", w)
		default:
			fmt.Fprintf(&sb, "-%s\n+%s\n", w, g)
		}
	}
	return sb.String()
}
// Package snapshot writes and compares text and cell snapshots.
package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// updateMode is true when the user runs go test with -tuitest-update.
// Implemented as a flag in the public package; this internal helper
// just receives the bool.
type Result struct {
	Updated bool // file was created or updated
	Path    string
}

// CompareText compares actual against the file at path. If the file
// doesn't exist, or update is true, it is written and Result.Updated
// is true. Otherwise, returns a non-nil error if the contents differ.
func CompareText(path, actual string, update bool) (Result, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, err
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return Result{}, err
	}
	if os.IsNotExist(err) || update {
		if err := os.WriteFile(path, []byte(actual), 0o644); err != nil {
			return Result{}, err
		}
		return Result{Updated: true, Path: path}, nil
	}
	if string(existing) == actual {
		return Result{Path: path}, nil
	}
	return Result{Path: path}, fmt.Errorf("snapshot mismatch in %s\n%s",
		path, unifiedDiff(string(existing), actual))
}

// unifiedDiff prints a minimal diff: per-line ± markers.
func unifiedDiff(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	var sb strings.Builder
	max := len(wl)
	if len(gl) > max {
		max = len(gl)
	}
	for i := 0; i < max; i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w == g {
			fmt.Fprintf(&sb, "  %s\n", w)
		} else {
			if i < len(wl) {
				fmt.Fprintf(&sb, "- %s\n", w)
			}
			if i < len(gl) {
				fmt.Fprintf(&sb, "+ %s\n", g)
			}
		}
	}
	return sb.String()
}

TURN 17
TOOL CALL: bash
INPUT:
{"command": "grep -n \"Trace\\|trace\\|zip\\|bundle\\|manifest\" internal/recording/recording.go internal/recording/replay.go cmd/twee/cmd_record.go 2>/dev/null || true"}
TOOL RESULT: bash
OUTPUT:
(no output)
TOOL CALL: bash
INPUT:
{"command": "go doc archive/zip 2>/dev/null | head -20 && go version"}
TOOL RESULT: bash
OUTPUT:
package zip // import "archive/zip"

Package zip provides support for reading and writing ZIP archives.

See the ZIP specification for details.

This package does not support disk spanning.

A note about ZIP64:

To be backwards compatible the FileHeader has both 32 and 64 bit Size fields.
The 64 bit fields will always contain the correct value and for normal archives
both fields will be the same. For files requiring the ZIP64 format the 32 bit
fields will be 0xffffffff and the 64 bit fields must be used instead.

[ZIP specification]: https://support.pkware.com/pkzip/appnote

const Store uint16 = 0 ...
var ErrFormat = errors.New("zip: not a valid zip file") ...
func RegisterCompressor(method uint16, comp Compressor)
go version go1.26.2 darwin/arm64

TURN 18
TOOL CALL: bash
INPUT:
{"command": "# Check what runtime info is accessible\ngo doc runtime GOOS 2>/dev/null | head -5\necho \"---\"\n# Check what's available for host info\ngrep -rn \"runtime\\.\" internal/ cmd/ | grep -E \"GOOS|GOARCH|Hostname\" | head -10"}
TOOL RESULT: bash
OUTPUT:
package runtime // import "runtime"

const GOOS string = goos.GOOS
    GOOS is the running program's operating system target: one of darwin,
    freebsd, linux, and so on. To view possible combinations of GOOS and GOARCH,
---
cmd/twee/paths.go:25:	switch runtime.GOOS {
cmd/twee/paths_test.go:18:	if runtime.GOOS == "darwin" {

TOOL CALL: bash
INPUT:
{"command": "# Understand current recording.Header to know what we're extending\ngrep -n \"Header\\|Version\\|Command\\|Env\\|Started\" internal/recording/recording.go | head -20\n# Also see if there are any PNG/screenshot references\ngrep -rn \"screenshot\\|Screenshot\\|\\.png\\|PNG\" internal/ cmd/ | grep -v \"_test\\|render\\|handlers_screenshot\" | head -10"}
TOOL RESULT: bash
OUTPUT:
15:// Header is the first line of a recording.
16:type Header struct {
17:	Version int               `json:"version"`
18:	Command []string          `json:"command"`
21:	Env     map[string]string `json:"env,omitempty"`
22:	Started time.Time         `json:"started"`
46:func New(path string, h Header) (*Recorder, error) {
52:	h.Version = 1
53:	h.Started = r.start
internal/rpc/types.go:90:type ScreenshotArgs struct {
internal/rpc/types.go:188:type ScreenshotData struct {
internal/rpc/types.go:190:	PNGBase64 string `json:"png_base64,omitempty"`
internal/rpc/ops.go:27:	OpScreenshot  = "screenshot"
cmd/twee/cmd_state.go:14:	register("screenshot", runScreenshot)
cmd/twee/cmd_state.go:20:	registerUsage("screenshot", `twee screenshot [-out <path.png>] [-name <name>]
cmd/twee/cmd_state.go:21:Render the current screen to PNG. Without -out, the response includes
cmd/twee/cmd_state.go:26:func runScreenshot(args []string) {
cmd/twee/cmd_state.go:27:	fs := flag.NewFlagSet("screenshot", flag.ExitOnError)
cmd/twee/cmd_state.go:29:	out := fs.String("out", "", "output PNG path; if empty, response includes png_base64")

TURN 19
TOOL CALL: bash
INPUT:
{"command": "# Check what existing recording infrastructure hooks into but doesn't yet write\ngrep -n \"WriteOutput\\|WriteInput\\|WriteResize\\|WriteExit\\|SetOutputHook\" internal/engine/term.go internal/engine/input.go internal/pump/pump.go | head -30"}
TOOL RESULT: bash
OUTPUT:
internal/engine/term.go:76:		p.SetOutputHook(func(b []byte, ts time.Time) { rec.WriteOutput(b, ts) })
internal/engine/term.go:101:			t.rec.WriteExit(t.runner.ExitCode())
internal/engine/term.go:135:		t.rec.WriteExit(t.runner.ExitCode())
internal/engine/term.go:150:	t.pump.SetOutputHook(func(b []byte, ts time.Time) { rec.WriteOutput(b, ts) })
internal/engine/term.go:161:	t.rec.WriteExit(t.runner.ExitCode())
internal/engine/term.go:165:	t.pump.SetOutputHook(nil)
internal/engine/input.go:20:		rec.WriteInput("type", "", []byte(s))
internal/engine/input.go:39:		rec.WriteInput("key", input.Name(k), b)
internal/engine/input.go:55:		rec.WriteInput("paste", "", b)
internal/engine/input.go:74:		rec.WriteResize(cols, rows)
internal/pump/pump.go:59:// SetOutputHook installs a function called under mu after each Feed.
internal/pump/pump.go:61:func (p *Pump) SetOutputHook(fn func([]byte, time.Time)) {

TOOL CALL: bash
INPUT:
{"command": "# Look for any existing trace/zip references\ngrep -rn \"trace\\|Trace\\|\\.zip\\|bundle\\|manifest\" . --include=\"*.go\" | grep -v \"_test\\|vendor\\|build\\|\\.git\" | head -20"}
TOOL RESULT: bash
OUTPUT:
(no output)
TURN 20
TOOL CALL: bash
INPUT:
{"command": "# Check ptyrunner for pid access\ngrep -n \"Pid\\|PID\\|pid\\|Process\\b\" internal/ptyrunner/runner.go"}
TOOL RESULT: bash
OUTPUT:
96:	if r.cmd.Process != nil {
97:		_ = r.cmd.Process.Signal(syscall.SIGWINCH)
105:	if r.cmd == nil || r.cmd.Process == nil {
108:	return r.cmd.Process.Signal(sig)
120:	if r.cmd.Process != nil {
121:		_ = r.cmd.Process.Signal(syscall.SIGTERM)
126:		if r.cmd.Process != nil {
127:			_ = r.cmd.Process.Kill()

TOOL CALL: bash
INPUT:
{"command": "# Check what the daemon start path knows about pid, args, etc.\ngrep -n \"Pid\\|PID\\|pid\\|os.Getpid\\|Process\\.\" cmd/twee/daemonize.go internal/engine/term.go internal/ptyrunner/runner.go | head -20"}
TOOL RESULT: bash
OUTPUT:
cmd/twee/daemonize.go:34:	PID    int    `json:"pid"`
cmd/twee/daemonize.go:97:	msg := readyMessage{Name: name, Socket: sock, PID: os.Getpid()}
cmd/twee/daemonize.go:152:	_, _ = fmt.Fprintf(lf, "%d\n", os.Getpid())
internal/ptyrunner/runner.go:97:		_ = r.cmd.Process.Signal(syscall.SIGWINCH)
internal/ptyrunner/runner.go:108:	return r.cmd.Process.Signal(sig)
internal/ptyrunner/runner.go:121:		_ = r.cmd.Process.Signal(syscall.SIGTERM)
internal/ptyrunner/runner.go:127:			_ = r.cmd.Process.Kill()

TURN 21
TOOL CALL: bash
INPUT:
{"command": "# Check if runner exposes PID\ngrep -n \"func.*Runner\" internal/ptyrunner/runner.go | head -20"}
TOOL RESULT: bash
OUTPUT:
41:func Start(ctx context.Context, cfg Config) (*Runner, error) {
75:func (r *Runner) wait() {
86:func (r *Runner) Master() io.ReadWriter { return r.master }
89:func (r *Runner) Resize(cols, rows int) error {
104:func (r *Runner) Signal(sig os.Signal) error {
112:func (r *Runner) ExitedCh() <-chan struct{} { return r.exited }
115:func (r *Runner) ExitCode() int { return r.exit.code }
119:func (r *Runner) Close() error {

TOOL CALL: bash
INPUT:
{"command": "# Check test patterns for recording\ngrep -rn \"recording\\|Record\\|\\.jsonl\" tuitest/ internal/ cmd/ --include=\"*.go\" | grep -v \"_test\\|vendor\" | head -30"}
TOOL RESULT: bash
OUTPUT:
tuitest/options.go:44:		RecordPath:        c.recordPath,
tuitest/options.go:78:// Record enables session recording to the given path.
tuitest/options.go:79:func Record(path string) Option {
tuitest/term.go:25:// is reachable via the promoted RecordPath() method.
tuitest/term.go:42:		cfg.recordPath = filepath.Join(t.TempDir(), "session.jsonl")
tuitest/term.go:51:		if t.Failed() && te.RecordPath() != "" {
tuitest/term.go:52:			t.Logf("tuitest recording: %s", te.RecordPath())
internal/recording/recording.go:1:// Package recording writes a JSONL session recording.
internal/recording/recording.go:5:package recording
internal/recording/recording.go:15:// Header is the first line of a recording.
internal/recording/recording.go:37:// Recorder writes recording events to a file.
internal/recording/recording.go:38:type Recorder struct {
internal/recording/recording.go:46:func New(path string, h Header) (*Recorder, error) {
internal/recording/recording.go:51:	r := &Recorder{f: f, enc: json.NewEncoder(f), start: time.Now()}
internal/recording/recording.go:61:func (r *Recorder) ms(ts time.Time) int64 {
internal/recording/recording.go:69:func (r *Recorder) WriteOutput(b []byte, ts time.Time) {
internal/recording/recording.go:80:func (r *Recorder) WriteInput(kind, key string, b []byte) {
internal/recording/recording.go:93:func (r *Recorder) WriteResize(cols, rows int) {
internal/recording/recording.go:105:func (r *Recorder) WriteExit(code int) {
internal/recording/recording.go:116:func (r *Recorder) Close() error {
internal/recording/replay.go:1:package recording
internal/recording/replay.go:14:// ReplayInto reads a recording from path and feeds output and resize
internal/recording/replay.go:29:		return 0, errors.New("recording: empty")
internal/daemon/handlers_record.go:16:		d.Register(rpc.OpRecordStart, handleRecordStart)
internal/daemon/handlers_record.go:17:		d.Register(rpc.OpRecordStop, handleRecordStop)
internal/daemon/handlers_record.go:21:func handleRecordStart(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
internal/daemon/handlers_record.go:22:	var a rpc.RecordStartArgs
internal/daemon/handlers_record.go:31:		a.Out = filepath.Join(dir, fmt.Sprintf("session-%d.jsonl", time.Now().UnixNano()))
internal/daemon/handlers_record.go:33:	if err := t.EnableRecording(a.Out); err != nil {
internal/daemon/handlers_record.go:39:func handleRecordStop(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {

TURN 22
TEXT:
Now I have a complete and thorough picture of the entire codebase. Here is the structured exploration summary:

---

## Exploration Summary

### Relevant Files

| Path | Role |
|---|---|
| `internal/recording/recording.go` | **Core target.** `Recorder` + `Header` + `Event` types; writes JSONL (version 1). This is what we're replacing/extending with the zip trace format. |
| `internal/recording/replay.go` | Replay: reads JSONL, feeds output/resize events into a `vt.Model`. Will need updating to read from zip. |
| `internal/recording/recording_test.go` | Only test for recording; round-trips JSONL via `ReplayInto`. |
| `internal/engine/term.go` | `Term` owns the `*recording.Recorder`; calls `EnableRecording`/`DisableRecording`; hooks output via `pump.SetOutputHook`; writes input events inline. Central wiring point. |
| `internal/engine/input.go` | `Type`, `Key`, `Paste`, `Resize` — each appends to the recorder via `rec.WriteInput` / `rec.WriteResize`. |
| `internal/engine/config.go` | `Config.RecordPath string`; `Config.Env map[string]string`; `BuildEnv()`. Source of env/cmd metadata. |
| `internal/pump/pump.go` | `SetOutputHook(fn func([]byte, time.Time))` — the tap point for raw PTY output bytes. |
| `internal/ptyrunner/runner.go` | Owns `exec.Cmd`; `cmd.Process.Pid` accessible; `ExitCode()`, `ExitedCh()`. Source of PID. |
| `internal/daemon/handlers_record.go` | RPC handlers for `record_start`/`record_stop`; creates the JSONL path, calls `t.EnableRecording`. **Needs new zip-aware args.** |
| `cmd/twee/cmd_record.go` | CLI `twee record start/stop`; `-out` flag for path. Passes `RecordStartArgs` to daemon. |
| `internal/rpc/types.go` | `RecordStartArgs{Out string}` — needs new fields (or replacement) for zip output path. |
| `internal/rpc/ops.go` | Op name constants `OpRecordStart`, `OpRecordStop`. |
| `tuitest/term.go` | `Run()` auto-enables recording to `t.TempDir()/session.jsonl` unless `TUITEST_RECORD=0`. Logs path on test failure. |
| `tuitest/options.go` | `Record(path string)` option; `config.recordPath`. Will need a `RecordTrace(path string)` option for zip format. |
| `internal/render/render.go` | `Render(snap, opts) (*image.RGBA, error)` + `EncodePNG` — used by screenshot handler; we'll call this to write per-event screenshots into the zip. |
| `internal/engine/query.go` | `Snapshot()`, `VisibleText()`, `Lines()` — for capturing VT state snapshots into the trace. |
| `internal/engine/types.go` | `Snapshot`, `Cell`, `Line`, `Color` types — JSON-serializable for the zip manifest. |
| `cmd/twee/daemonize.go` | Daemon startup: has `os.Getpid()`, inherits `cmd`, `cols`, `rows`, `envOverrides`. Source of session metadata. |

---

### Architecture Constraints

1. **JSONL is the current wire format for recordings.** The `Recorder` streams events line-by-line to a single `.jsonl` file. The zip format will be a new parallel path — we should keep JSONL for the `tuitest` auto-record (lightweight replay/diagnostics) and introduce the zip trace as the "rich" format behind a separate flag/option.

2. **`Recorder` is instantiated at `engine.Start` time or via `EnableRecording`.** All recording state lives on `*engine.Term` under `cfgMu`. The zip recorder must satisfy the same interface contracts: constructor writes metadata immediately, methods are called from multiple goroutines, `Close` finalizes.

3. **Output hook (`pump.SetOutputHook`) is the only tap point for raw bytes.** It is called *outside* the pump's mutex (deliberate — "so a slow recorder cannot stall snapshots or waiters"). The zip recorder's output writer must be fast enough or buffer aggressively; writing to an in-memory `bytes.Buffer` then flushing to the zip at `Close` is the safe pattern.

4. **`engine.Term` uses `cfgMu` to protect `t.rec` and `t.cfg.RecordPath`.** Any new trace recorder field must be similarly guarded.

5. **`EnableRecording` / `DisableRecording` are the public engine API.** The daemon calls them; the tuitest harness sets `RecordPath` in `Config` at start. The zip format needs parallel `EnableTrace` / `DisableTrace` or a unified `EnableRecording` that accepts a mode flag — the cleanest approach is a **separate recorder type** that implements the same internal write calls.

6. **RPC types are a closed set in `rpc/types.go`.** New fields go in `RecordStartArgs`; we should add an `OutZip string` or `Format string` field. The daemon handler is in `handlers_record.go`.

7. **All ops return one JSON object and exit.** The zip path returned from `record stop` will be the `.tweerecord` (or `.zip`) file path, matching the existing `{"path": ...}` shape.

8. **`archive/zip` is stdlib** — no new dependencies needed.

9. **CGO is required** (libghostty-vt); rendering screenshots for the trace will pull in `internal/render`, which requires the CGO VT model. This is fine since we're always inside an engine context.

10. **`runtime.GOOS` / `runtime.GOARCH`** are available for host metadata. `os.Hostname()` is available. PID of the child is `runner.cmd.Process.Pid` — not yet publicly exposed via a method; we need to add a `Pid()` accessor to `Runner` (or pass it through `engine.Term`).

---

### Test Conventions

- **Unit tests**: `package foo` (same package). Files: `*_test.go` in the package dir. Examples: `internal/recording/recording_test.go`, `internal/pump/pump_test.go`, `internal/vt/term_test.go`.
- **Integration/server tests**: `internal/daemon/server_test.go` — spawns a real `/bin/sh`, starts a real Unix socket server, dials it with `dialAndCall`.
- **CLI e2e tests**: `cmd/twee/cli_e2e_test.go` — builds the binary with `buildBinary(t)`, runs it as a subprocess, parses JSON output. Uses `TWEE_STATE_DIR=t.TempDir()` for isolation.
- **Smoke tests**: `tuitest/runner_smoke_test.go` — uses the public `tuitest` API.
- **Build tag for flakes**: `//go:build flake` gates expensive loop tests.
- **Test helpers**: `mustOK(t, bin, env, args...)` checks `{"ok": true}`. `runCLI` returns `map[string]any`.
- **Fixtures**: `fixtures/menu/main.go` compiled to `bin/menu` by `make build`.
- **Build**: `make test` → `PKG_CONFIG_PATH=... go test ./...`. New tests follow the same pattern.

---

### Risks

1. **ZIP write-at-close vs streaming**: `archive/zip` requires writing the central directory at the end. If the process crashes mid-session the zip is corrupt. Mitigation: buffer each artifact in memory and flush to zip only at `Close`/stop. For very long sessions (many MB of output), provide a configurable max-size guard or segment.

2. **Screenshot rendering at every input event is expensive.** The Playwright trace model takes screenshots at each action. We need to make this opt-in or rate-limited. Safest: capture a screenshot only at `record stop` (or at each explicit `RecordFrame` call), not on every keystroke.

3. **PID exposure.** `ptyrunner.Runner` doesn't currently expose `Pid()`. We need to add `func (r *Runner) Pid() int` — trivial but a file change. Similarly `engine.Term` needs to forward it.

4. **Zip path collision with existing JSONL path.** `handlers_record.go` currently auto-generates a `.jsonl` path if none given. We need to not accidentally mix the two formats. Cleanest: separate `OpTraceStart` / `OpTraceStop` ops (new constants) or add a `Format string` field to `RecordStartArgs`.

5. **`internal/render` import in `internal/recording`**: currently `recording` doesn't import `render`. Adding trace screenshots creates a dependency. This is fine architecturally (both are internal) but the zip trace writer must live in a new subpackage (e.g. `internal/trace`) rather than polluting the lean JSONL `recording` package.

6. **Backward compatibility of `ReplayInto`**: existing JSONL replay must continue to work for `tuitest` auto-recordings. The new zip format adds a separate replay path; both coexist.

---

### Build System

```
make build          # builds libghostty-vt via cmake, then go build ./...
make test           # go test ./... with correct PKG_CONFIG_PATH + DYLD_LIBRARY_PATH
make twee           # just the CLI binary → bin/twee
```

New packages follow the same pattern — no Makefile changes needed for pure-Go packages. The `internal/trace` package (new) will be pure Go using `archive/zip`, `encoding/json`, `image/png`, and `internal/render` (CGO-backed), so it builds only when the libghostty stamp exists — same as today.

---

### Design Decision for the Implementation

Given all of the above, here is the clean plan before writing a single line of code:

**New package: `internal/trace`**  
`Trace` type (parallel to `Recorder`) that accumulates artifacts in memory and writes a `.tweerecord` zip at `Close`. The zip contains:

```
manifest.json          # session metadata (cmd, args, env, host, pid, cols, rows, started_at)
events.jsonl           # same event stream as today (output, input, resize, exit) with timestamps  
screenshots/
  0000.png             # screenshot at trace-start (initial screen)
  <n>.png              # one per explicit RecordFrame call (or at stop)
```

**`engine.Term` gains**: `EnableTrace(path string) error` / `DisableTrace() error` / `TraceRecordFrame()` — parallel to `EnableRecording`/`DisableRecording`.

**New RPC ops**: `OpTraceStart` / `OpTraceStop` (or reuse `OpRecordStart`/`OpRecordStop` with a `format` discriminator). New CLI: `twee trace start [-out foo.tweerecord]` / `twee trace stop`.

**`tuitest`**: new `Trace(path string)` option; `RecordFrame()` method on `*Term`.

Session 5cfe20b4 completed in 2m1s
Turns: 22 | Tool calls: 42 (bash: 42)
Tokens: 5742 (in: 24, out: 5718) | Cost: $0.30
Longest turn: 1m0s
