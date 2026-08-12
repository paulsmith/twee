package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/inspect"
)

func TestInspectDefaultJSON(t *testing.T) {
	bin := buildBinary(t)
	bundle := writeInspectBundle(t)
	out, err := exec.Command(bin, "inspect", bundle).Output()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if bytes.Contains(bytes.ToLower(out), []byte("screenshots")) {
		t.Fatalf("inspect output should not mention screenshots:\n%s", out)
	}

	var got struct {
		OK   bool `json:"ok"`
		Data struct {
			Path        string   `json:"path"`
			Version     int      `json:"version"`
			Command     []string `json:"command"`
			Duration    string   `json:"duration"`
			DurationMS  int64    `json:"duration_ms"`
			EventSpanMS int64    `json:"event_span_ms"`
			Terminal    struct {
				Cols    int `json:"cols"`
				Rows    int `json:"rows"`
				MaxCols int `json:"max_cols"`
				MaxRows int `json:"max_rows"`
			} `json:"terminal"`
			Events struct {
				Total       int            `json:"total"`
				ByType      map[string]int `json:"by_type"`
				InputByKind map[string]int `json:"input_by_kind"`
			} `json:"events"`
			Markers []inspect.MarkerSummary `json:"markers"`
			Exit    struct {
				Recorded bool `json:"recorded"`
				Code     *int `json:"code"`
			} `json:"exit"`
			ChildPTYTermios inspect.ChildPTYTermiosSummary `json:"child_pty_termios"`
			Replay          struct {
				InitialModes struct {
					KittyKeyboardKnown bool `json:"kitty_keyboard_known"`
				} `json:"initial_modes"`
				Final struct {
					EventIndex  *int   `json:"event_index"`
					TMS         *int64 `json:"t_ms"`
					VisibleText string `json:"visible_text"`
					Size        struct {
						Cols int `json:"cols"`
						Rows int `json:"rows"`
					} `json:"size"`
					Cursor struct {
						X       int    `json:"x"`
						Y       int    `json:"y"`
						Visible bool   `json:"visible"`
						Shape   string `json:"shape"`
					} `json:"cursor"`
					Lines []struct {
						Runs []struct {
							Count int `json:"count"`
						} `json:"runs"`
					} `json:"lines"`
				} `json:"final"`
				ModeTransitions []json.RawMessage `json:"mode_transitions"`
			} `json:"replay"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if !got.OK {
		t.Fatalf("ok = false, output:\n%s", out)
	}
	if got.Data.Path != bundle || got.Data.Version != 1 {
		t.Fatalf("identity = %q/%d, want %q/1", got.Data.Path, got.Data.Version, bundle)
	}
	if got.Data.Duration != "1.234s" || got.Data.DurationMS != 1234 || got.Data.EventSpanMS != 1210 {
		t.Fatalf("duration = %q/%d span %d", got.Data.Duration, got.Data.DurationMS, got.Data.EventSpanMS)
	}
	if got.Data.Terminal.Cols != 80 || got.Data.Terminal.Rows != 24 || got.Data.Terminal.MaxCols != 100 || got.Data.Terminal.MaxRows != 40 {
		t.Fatalf("terminal = %+v", got.Data.Terminal)
	}
	if got.Data.Events.Total != 6 || got.Data.Events.ByType["output"] != 1 || got.Data.Events.ByType["marker"] != 1 || got.Data.Events.InputByKind["type"] != 1 {
		t.Fatalf("events = %+v", got.Data.Events)
	}
	if len(got.Data.Markers) != 1 || got.Data.Markers[0].EventIndex != 4 || got.Data.Markers[0].TMS != 30 || got.Data.Markers[0].Label != "ready" {
		t.Fatalf("markers = %+v", got.Data.Markers)
	}
	if !got.Data.Exit.Recorded || got.Data.Exit.Code == nil || *got.Data.Exit.Code != 7 {
		t.Fatalf("exit = %+v", got.Data.Exit)
	}
	if !got.Data.ChildPTYTermios.Present || got.Data.ChildPTYTermios.Start == nil || got.Data.ChildPTYTermios.Exit == nil || got.Data.ChildPTYTermios.Start.State == nil || !got.Data.ChildPTYTermios.Start.State.Canonical || got.Data.ChildPTYTermios.Exit.State == nil || got.Data.ChildPTYTermios.Exit.State.Canonical {
		t.Fatalf("child PTY termios = %+v", got.Data.ChildPTYTermios)
	}
	if !got.Data.Replay.InitialModes.KittyKeyboardKnown {
		t.Fatalf("initial modes = %+v", got.Data.Replay.InitialModes)
	}
	final := got.Data.Replay.Final
	if final.EventIndex == nil || *final.EventIndex != 3 || final.TMS == nil || *final.TMS != 30 || final.Size.Cols != 100 || final.Size.Rows != 40 || !strings.Contains(final.VisibleText, "hi") {
		t.Fatalf("replay final = %+v", final)
	}
	firstLineWidth := 0
	if len(final.Lines) > 0 {
		for _, run := range final.Lines[0].Runs {
			firstLineWidth += run.Count
		}
	}
	if len(final.Lines) != 40 || firstLineWidth != 100 || got.Data.Replay.ModeTransitions == nil {
		t.Fatalf("replay lines/transitions = %d/%d %#v", len(final.Lines), firstLineWidth, got.Data.Replay.ModeTransitions)
	}
}

func TestInspectTextOutput(t *testing.T) {
	bin := buildBinary(t)
	bundle := writeInspectBundle(t)
	out, err := exec.Command(bin, "inspect", "--format", "text", bundle).Output()
	if err != nil {
		t.Fatalf("inspect text: %v", err)
	}
	for _, want := range []string{
		"Path: " + bundle,
		"Version: 1",
		"Command: vim file.txt",
		"Duration: 1.234s (1234 ms)",
		"Event span: 1210 ms",
		"Terminal: 80x24 (max 100x40)",
		"Events: 6 total",
		"Input: key=1, type=1",
		"Markers: 1",
		"  1. 30 ms event=4: ready",
		"Exit: code 7",
		"Child PTY termios (linux): start canonical=true echo=true signals=true; exit canonical=false echo=false signals=false",
		"Network capture: none",
		"Replay final: 100x40 at 30 ms (event 3)",
		"Cursor: x=2 y=0 visible=true",
		"Mode transitions: 0",
		"Final viewport:",
		"   0 | hi",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("inspect text missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(strings.ToLower(string(out)), "screenshots") {
		t.Fatalf("inspect text should not mention screenshots:\n%s", out)
	}
}

func TestPrintInspectTextReportsNetworkCapture(t *testing.T) {
	var out bytes.Buffer
	printInspectText(&out, inspect.Summary{
		Network: inspect.NetworkSummary{
			Present: true, Format: "pcap", PacketCount: 12, SizeBytes: 4096,
			Status: "truncated", Truncated: true,
		},
	})
	if want := "Network capture: pcap, 12 packets, 4096 bytes, status truncated (truncated)"; !strings.Contains(out.String(), want) {
		t.Fatalf("inspect text missing %q:\n%s", want, &out)
	}
}

func TestInspectInvalidFormatUsageError(t *testing.T) {
	bin := buildBinary(t)
	bundle := writeInspectBundle(t)
	cmd := exec.Command(bin, "inspect", "--format", "yaml", bundle)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exit.ExitCode() != 2 {
		t.Fatalf("exit code = %d, want 2", exit.ExitCode())
	}
	if !strings.Contains(stderr.String(), "invalid --format") {
		t.Fatalf("stderr missing invalid format message:\n%s", stderr.String())
	}
}

func TestInspectReadErrorJSON(t *testing.T) {
	bin := buildBinary(t)
	cmd := exec.Command(bin, "inspect", filepath.Join(t.TempDir(), "missing.twee"))
	out, err := cmd.Output()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exit.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1", exit.ExitCode())
	}

	var got struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode error envelope: %v\n%s", err, out)
	}
	if got.OK || got.Error.Code != "IO" {
		t.Fatalf("error envelope = %+v, want ok=false code IO", got)
	}
}

func TestInspectDirectoryIsIO(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "inspect", t.TempDir()).Output()
	if err == nil {
		t.Fatalf("want non-zero exit, got success: %s", out)
	}
	if code := inspectErrorCode(t, out); code != "IO" {
		t.Fatalf("code = %q, want IO", code)
	}
}

func TestInspectReportsAllValidationIssues(t *testing.T) {
	bin := buildBinary(t)
	path := writeInspectRawBundle(t, map[string]string{
		"manifest.json": `{"version":2,"cols":10,"rows":3}`,
		"events.jsonl": strings.Join([]string{
			`{"t_ms":500,"type":"teleport"}`,
			`{not json`,
			`{"t_ms":120,"type":"output"}`,
		}, "\n"),
	})

	out, err := exec.Command(bin, "inspect", path).Output()
	if err == nil {
		t.Fatalf("want non-zero exit, got success: %s", out)
	}
	if code := inspectErrorCode(t, out); code != "INVALID_ARGUMENT" {
		t.Fatalf("code = %q, want INVALID_ARGUMENT", code)
	}
	issues := inspectErrorIssues(t, out)
	for _, want := range []string{
		"unsupported bundle version 2",
		`unknown event type "teleport"`,
		"events.jsonl line 2",
		"timestamp 120 before previous 500",
	} {
		if !containsInspectIssue(issues, want) {
			t.Errorf("issues = %v, want one containing %q", issues, want)
		}
	}
	if len(issues) != 4 {
		t.Errorf("issues = %v, want exactly four exhaustive issues", issues)
	}
}

func TestInspectReportsMultipleDecoderOnlyIssues(t *testing.T) {
	bin := buildBinary(t)
	path := writeInspectRawBundle(t, map[string]string{
		"manifest.json": `{"version":1,"cols":10,"rows":3}`,
		"events.jsonl": strings.Join([]string{
			`{"t_ms":0,"type":"output","bytes_b64":"%%%"}`,
			`{"t_ms":1,"type":"input","bytes_b64":"!!!"}`,
			`{"t_ms":2,"type":"resize","cols":"wide","rows":3}`,
		}, "\n"),
	})

	out, err := exec.Command(bin, "inspect", path).Output()
	if err == nil {
		t.Fatalf("want non-zero exit, got success: %s", out)
	}
	if code := inspectErrorCode(t, out); code != "INVALID_ARGUMENT" {
		t.Fatalf("code = %q, want INVALID_ARGUMENT", code)
	}
	issues := inspectErrorIssues(t, out)
	if len(issues) != 3 ||
		!containsInspectIssue(issues, "line 1: decode bytes_b64") ||
		!containsInspectIssue(issues, "line 2: decode bytes_b64") ||
		!containsInspectIssue(issues, "line 3: json: cannot unmarshal string") {
		t.Fatalf("issues = %v, want both payload failures and typed-field failure", issues)
	}
}

func TestInspectHelp(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "help", "inspect").Output()
	if err != nil {
		t.Fatalf("help inspect: %v", err)
	}
	for _, want := range []string{
		"twee inspect [--format json|text] <bundle.twee>",
		"--format json|text",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("inspect help missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(strings.ToLower(string(out)), "screenshots") {
		t.Fatalf("inspect help should not mention screenshots:\n%s", out)
	}
}

func writeInspectBundle(t *testing.T) string {
	t.Helper()
	return writeInspectRawBundle(t, map[string]string{
		"manifest.json": `{
			"version": 1,
			"command": ["vim", "file.txt"],
			"cols": 80,
			"rows": 24,
			"started_at": "2026-06-23T12:00:00Z",
			"stopped_at": "2026-06-23T12:00:01.234Z",
			"child_pty_termios": {
				"schema_version": 1,
				"platform": "linux",
				"start": {"status":"captured","state":{"canonical":true,"echo":true,"signals":true,"extended_input":true,"input_flow_control":true,"output_flow_control":false,"output_processing":true,"map_nl_to_crnl":true,"raw":{"input_flags":1,"output_flags":2,"control_flags":3,"local_flags":4,"control_chars":[3,28,127,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"input_speed":15,"output_speed":15}}},
				"exit": {"status":"captured","state":{"canonical":false,"echo":false,"signals":false,"extended_input":false,"input_flow_control":false,"output_flow_control":false,"output_processing":true,"map_nl_to_crnl":true,"raw":{"input_flags":1,"output_flags":2,"control_flags":3,"local_flags":0,"control_chars":[3,28,127,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"input_speed":15,"output_speed":15}}}
			}
		}`,
		"events.jsonl": strings.Join([]string{
			`{"t_ms":0,"type":"output","bytes_b64":"` + base64.StdEncoding.EncodeToString([]byte("hi")) + `"}`,
			`{"t_ms":10,"type":"input","kind":"key","key":"Enter","bytes_b64":"DQ=="}`,
			`{"t_ms":20,"type":"input","kind":"type","bytes_b64":"aQ=="}`,
			`{"t_ms":30,"type":"resize","cols":100,"rows":40}`,
			`{"t_ms":30,"type":"marker","label":"ready"}`,
			`{"t_ms":1210,"type":"exit","code":7}`,
		}, "\n"),
	})
}

func writeInspectRawBundle(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.twee")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func inspectErrorCode(t *testing.T, out []byte) string {
	t.Helper()
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode error envelope %s: %v", out, err)
	}
	return resp.Error.Code
}

func inspectErrorIssues(t *testing.T, out []byte) []string {
	t.Helper()
	var resp struct {
		Error struct {
			Details struct {
				Issues []string `json:"issues"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode error envelope %s: %v", out, err)
	}
	return resp.Error.Details.Issues
}

func containsInspectIssue(issues []string, want string) bool {
	for _, issue := range issues {
		if strings.Contains(issue, want) {
			return true
		}
	}
	return false
}
