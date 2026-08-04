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
			Exit struct {
				Recorded bool `json:"recorded"`
				Code     *int `json:"code"`
			} `json:"exit"`
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
	if got.Data.Events.Total != 5 || got.Data.Events.ByType["output"] != 1 || got.Data.Events.InputByKind["type"] != 1 {
		t.Fatalf("events = %+v", got.Data.Events)
	}
	if !got.Data.Exit.Recorded || got.Data.Exit.Code == nil || *got.Data.Exit.Code != 7 {
		t.Fatalf("exit = %+v", got.Data.Exit)
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
		"Events: 5 total",
		"Input: key=1, type=1",
		"Exit: code 7",
		"Network capture: none",
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
	path := filepath.Join(t.TempDir(), "session.twee")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	files := map[string]string{
		"manifest.json": `{
			"version": 1,
			"command": ["vim", "file.txt"],
			"cols": 80,
			"rows": 24,
			"started_at": "2026-06-23T12:00:00Z",
			"stopped_at": "2026-06-23T12:00:01.234Z"
		}`,
		"events.jsonl": strings.Join([]string{
			`{"t_ms":0,"type":"output","bytes_b64":"` + base64.StdEncoding.EncodeToString([]byte("hi")) + `"}`,
			`{"t_ms":10,"type":"input","kind":"key","key":"Enter","bytes_b64":"DQ=="}`,
			`{"t_ms":20,"type":"input","kind":"type","bytes_b64":"aQ=="}`,
			`{"t_ms":30,"type":"resize","cols":100,"rows":40}`,
			`{"t_ms":1210,"type":"exit","code":7}`,
		}, "\n"),
	}
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
