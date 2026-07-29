package main

import (
	"archive/zip"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/trace"
)

// exitCommand runs bin with args and returns stdout regardless of exit
// code (exec.Cmd.Output still populates it on a non-zero exit, since
// twee's JSON error envelope goes to stdout, not stderr).
func exitCommand(bin string, args ...string) ([]byte, error) {
	return exec.Command(bin, args...).Output()
}

func writeBundleTestTrace(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.twee")
	tr, err := trace.New(path, trace.Manifest{
		Command: []string{"/bin/sh", "-c", "echo hi"},
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatal(err)
	}
	tr.WriteOutput([]byte("hi\r\n"), time.Now())
	tr.WriteInput("key", "Enter", []byte("\r"))
	tr.WriteResize(100, 30)
	tr.WriteExit(0)
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeBundleRawZip(t *testing.T, files map[string]string) string {
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

func TestBundleInfoValidBundle(t *testing.T) {
	bin := buildBinary(t)
	path := writeBundleTestTrace(t)

	out, err := exitCommand(bin, "bundle", "info", path)
	if err != nil {
		t.Fatalf("bundle info: %v\n%s", err, out)
	}
	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			Version int            `json:"version"`
			Cols    int            `json:"cols"`
			Rows    int            `json:"rows"`
			Events  map[string]int `json:"events"`
		} `json:"data"`
	}
	if jerr := json.Unmarshal(out, &resp); jerr != nil {
		t.Fatalf("decode %s: %v", out, jerr)
	}
	if !resp.OK {
		t.Fatalf("ok = false, want true: %s", out)
	}
	if resp.Data.Cols != 80 || resp.Data.Rows != 24 {
		t.Errorf("cols/rows = %d/%d, want 80/24", resp.Data.Cols, resp.Data.Rows)
	}
	if resp.Data.Events["input"] != 1 || resp.Data.Events["resize"] != 1 || resp.Data.Events["exit"] != 1 {
		t.Errorf("events = %#v", resp.Data.Events)
	}
}

func TestBundleInfoMissingFileIsIO(t *testing.T) {
	bin := buildBinary(t)
	out, err := exitCommand(bin, "bundle", "info", filepath.Join(t.TempDir(), "missing.twee"))
	if err == nil {
		t.Fatalf("want non-zero exit, got success: %s", out)
	}
	code := decodeErrorCode(t, out)
	if code != "IO" {
		t.Errorf("code = %q, want IO", code)
	}
}

func TestBundleInfoCorruptManifestIsInvalidArgument(t *testing.T) {
	bin := buildBinary(t)
	path := writeBundleRawZip(t, map[string]string{
		"manifest.json": `{"version":2,"cols":10,"rows":3}`,
		"events.jsonl":  "",
	})
	out, err := exitCommand(bin, "bundle", "info", path)
	if err == nil {
		t.Fatalf("want non-zero exit, got success: %s", out)
	}
	if code := decodeErrorCode(t, out); code != "INVALID_ARGUMENT" {
		t.Errorf("code = %q, want INVALID_ARGUMENT", code)
	}
}

func TestBundleValidateValidBundle(t *testing.T) {
	bin := buildBinary(t)
	path := writeBundleTestTrace(t)

	out, err := exitCommand(bin, "bundle", "validate", path)
	if err != nil {
		t.Fatalf("bundle validate: %v\n%s", err, out)
	}
	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			Valid  bool `json:"valid"`
			Events int  `json:"events"`
		} `json:"data"`
	}
	if jerr := json.Unmarshal(out, &resp); jerr != nil {
		t.Fatalf("decode %s: %v", out, jerr)
	}
	if !resp.OK || !resp.Data.Valid {
		t.Fatalf("resp = %s, want ok:true, valid:true", out)
	}
	if resp.Data.Events != 4 { // output + input + resize + exit
		t.Errorf("events = %d, want 4", resp.Data.Events)
	}
}

func TestBundleValidateTruncatedZip(t *testing.T) {
	bin := buildBinary(t)
	full := writeBundleTestTrace(t)
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	truncated := filepath.Join(t.TempDir(), "truncated.twee")
	if err := os.WriteFile(truncated, raw[:len(raw)/2], 0o644); err != nil {
		t.Fatal(err)
	}

	out, cmdErr := exitCommand(bin, "bundle", "validate", truncated)
	if cmdErr == nil {
		t.Fatalf("want non-zero exit for a truncated bundle, got success: %s", out)
	}
	code := decodeErrorCode(t, out)
	if code != "INVALID_ARGUMENT" {
		t.Errorf("code = %q, want INVALID_ARGUMENT", code)
	}
	if !strings.Contains(string(out), "invalid zip") {
		t.Errorf("output %s missing issue detail", out)
	}
}

func TestBundleValidateGarbageManifest(t *testing.T) {
	bin := buildBinary(t)
	path := writeBundleRawZip(t, map[string]string{
		"manifest.json": `{not valid json`,
		"events.jsonl":  "",
	})
	out, cmdErr := exitCommand(bin, "bundle", "validate", path)
	if cmdErr == nil {
		t.Fatalf("want non-zero exit, got success: %s", out)
	}
	issues := decodeErrorIssues(t, out)
	if !anyContains(issues, "manifest.json") {
		t.Errorf("issues = %v, want one mentioning manifest.json", issues)
	}
}

func TestBundleValidateCorruptEventsLine(t *testing.T) {
	bin := buildBinary(t)
	path := writeBundleRawZip(t, map[string]string{
		"manifest.json": `{"version":1,"cols":10,"rows":3}`,
		"events.jsonl": strings.Join([]string{
			`{"t_ms":0,"type":"output"}`,
			`{not json`,
		}, "\n"),
	})
	out, cmdErr := exitCommand(bin, "bundle", "validate", path)
	if cmdErr == nil {
		t.Fatalf("want non-zero exit, got success: %s", out)
	}
	code := decodeErrorCode(t, out)
	if code != "INVALID_ARGUMENT" {
		t.Errorf("code = %q, want INVALID_ARGUMENT", code)
	}
	issues := decodeErrorIssues(t, out)
	if !anyContains(issues, "events.jsonl line 2") {
		t.Errorf("issues = %v, want one naming line 2", issues)
	}
}

func TestBundleValidateMissingFileIsIO(t *testing.T) {
	bin := buildBinary(t)
	out, cmdErr := exitCommand(bin, "bundle", "validate", filepath.Join(t.TempDir(), "missing.twee"))
	if cmdErr == nil {
		t.Fatalf("want non-zero exit, got success: %s", out)
	}
	if code := decodeErrorCode(t, out); code != "IO" {
		t.Errorf("code = %q, want IO", code)
	}
}

func decodeErrorCode(t *testing.T, out []byte) string {
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

func decodeErrorIssues(t *testing.T, out []byte) []string {
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

func anyContains(items []string, substr string) bool {
	for _, i := range items {
		if strings.Contains(i, substr) {
			return true
		}
	}
	return false
}
