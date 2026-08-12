package export

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestExportCast(t *testing.T) {
	bundle := writeCastBundle(t, []string{
		castRecord(100, "output", "hello", ""),
		`{"t_ms":150,"type":"marker","label":"ready"}`,
		castRecord(200, "resize", "", ""),
		castRecord(300, "input", "typed", "type"),
		castRecord(400, "input", "\x1b[?1;2c", "terminal_reply"),
		castRecord(500, "input", "mouse", "mouse"),
		castRecord(600, "output", "\xff", ""),
		`{"t_ms":700,"type":"exit","code":0}`,
	})
	out := filepath.Join(t.TempDir(), "recording.cast")
	result, err := ExportWithResult(bundle, out, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.OmittedEvents != 5 {
		t.Fatalf("OmittedEvents = %d, want 5", result.OmittedEvents)
	}
	assertCastRecords(t, out, 0.7, [][]any{
		{0.1, "o", "hello"},
		{0.15, "m", "ready"},
		{0.2, "r", "80x24"},
	})
}

func TestExportCastPreservesSameTimestampMarkerOrder(t *testing.T) {
	bundle := writeCastBundle(t, []string{
		`{"t_ms":100,"type":"marker","label":"first"}`,
		`{"t_ms":100,"type":"marker","label":"second"}`,
	})
	out := filepath.Join(t.TempDir(), "recording.cast")
	if _, err := ExportWithResult(bundle, out, Options{}); err != nil {
		t.Fatal(err)
	}
	assertCastRecords(t, out, 0.1, [][]any{{0.1, "m", "first"}, {0.1, "m", "second"}})
}

func TestExportCastIncludesUnambiguousInputOnlyWhenRequested(t *testing.T) {
	bundle := writeCastBundle(t, []string{
		castRecord(100, "input", "typed", "type"),
		castRecord(200, "input", "\r", "key"),
		castRecord(300, "input", "pasted", "paste"),
		castRecord(400, "input", "reply", "terminal_reply"),
	})
	out := filepath.Join(t.TempDir(), "recording.cast")
	result, err := ExportWithResult(bundle, out, Options{IncludeInput: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.OmittedEvents != 1 {
		t.Fatalf("OmittedEvents = %d, want 1", result.OmittedEvents)
	}
	assertCastRecords(t, out, 0.4, [][]any{
		{0.1, "i", "typed"},
		{0.2, "i", "\r"},
		{0.3, "i", "pasted"},
	})
}

func TestExportCastBuffersSplitUTF8Output(t *testing.T) {
	bundle := writeCastBundle(t, []string{
		castRecord(100, "output", "\xe2\x82", ""),
		castRecord(200, "output", "\xac", ""),
	})
	out := filepath.Join(t.TempDir(), "recording.cast")
	result, err := ExportWithResult(bundle, out, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.OmittedEvents != 0 {
		t.Fatalf("OmittedEvents = %d, want 0", result.OmittedEvents)
	}
	assertCastRecords(t, out, 0.2, [][]any{{0.1, "o", "€"}})
}

func TestExportCastBuffersSplitUTF8OutputAcrossMarker(t *testing.T) {
	bundle := writeCastBundle(t, []string{
		castRecord(100, "output", "\xe2\x82", ""),
		`{"t_ms":150,"type":"marker","label":"between"}`,
		castRecord(200, "output", "\xacafter", ""),
	})
	out := filepath.Join(t.TempDir(), "recording.cast")
	result, err := ExportWithResult(bundle, out, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.OmittedEvents != 0 {
		t.Fatalf("OmittedEvents = %d, want 0", result.OmittedEvents)
	}
	assertCastRecords(t, out, 0.2, [][]any{{0.1, "o", "€"}, {0.15, "m", "between"}, {0.2, "o", "after"}})
}

func TestExportCastInvalidatedSplitUTF8KeepsLaterOutputAfterMarker(t *testing.T) {
	bundle := writeCastBundle(t, []string{
		castRecord(100, "output", "\xe2\x82", ""),
		`{"t_ms":150,"type":"marker","label":"between"}`,
		castRecord(200, "output", "after", ""),
	})
	out := filepath.Join(t.TempDir(), "recording.cast")
	result, err := ExportWithResult(bundle, out, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.OmittedEvents != 1 {
		t.Fatalf("OmittedEvents = %d, want 1", result.OmittedEvents)
	}
	assertCastRecords(t, out, 0.2, [][]any{{0.15, "m", "between"}, {0.2, "o", "after"}})
}

func TestExportCastPreservesDurationAfterOmittedEvents(t *testing.T) {
	bundle := writeCastBundle(t, []string{
		castRecord(100, "output", "hello", ""),
		`{"t_ms":700,"type":"exit","code":0}`,
	})
	out := filepath.Join(t.TempDir(), "recording.cast")
	if _, err := ExportWithResult(bundle, out, Options{}); err != nil {
		t.Fatal(err)
	}
	assertCastRecords(t, out, 0.7, [][]any{{0.1, "o", "hello"}})
}
func TestExportCastInvalidBundleDoesNotCommitOutput(t *testing.T) {
	out := filepath.Join(t.TempDir(), "recording.cast")
	if err := os.WriteFile(out, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Export(filepath.Join(t.TempDir(), "invalid.twee"), out, Options{}); err == nil {
		t.Fatal("Export succeeded for invalid bundle")
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "existing" {
		t.Fatalf("output = %q, want existing destination unchanged", got)
	}
}

func castRecord(tms int, typ, payload, kind string) string {
	if typ == "resize" {
		return fmt.Sprintf(`{"t_ms":%d,"type":"resize","cols":80,"rows":24}`, tms)
	}
	record := fmt.Sprintf(`{"t_ms":%d,"type":%q,"bytes_b64":%q`, tms, typ, base64.StdEncoding.EncodeToString([]byte(payload)))
	if kind != "" {
		record += fmt.Sprintf(`,"kind":%q`, kind)
	}
	return record + "}"
}

func writeCastBundle(t *testing.T, events []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.twee")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	manifest, err := zw.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(manifest, `{"version":1,"cols":80,"rows":24}`); err != nil {
		t.Fatal(err)
	}
	eventFile, err := zw.Create("events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eventFile.Write(bytes.Join(stringBytes(events), []byte("\n"))); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func stringBytes(values []string) [][]byte {
	out := make([][]byte, len(values))
	for i, value := range values {
		out[i] = []byte(value)
	}
	return out
}

func assertCastRecords(t *testing.T, path string, duration float64, want [][]any) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(body), []byte("\n"))
	if len(lines) != len(want)+1 {
		t.Fatalf("cast has %d records, want %d:\n%s", len(lines), len(want)+1, body)
	}
	var header struct {
		Version  int     `json:"version"`
		Width    int     `json:"width"`
		Height   int     `json:"height"`
		Duration float64 `json:"duration"`
	}
	if err := json.Unmarshal(lines[0], &header); err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if header.Version != 2 || header.Width != 80 || header.Height != 24 || header.Duration != duration {
		t.Fatalf("header = %+v", header)
	}
	for i := 1; i < len(lines); i++ {
		var got []any
		if err := json.Unmarshal(lines[i], &got); err != nil {
			t.Fatalf("decode event %d: %v", i, err)
		}
		if fmt.Sprint(got) != fmt.Sprint(want[i-1]) {
			t.Errorf("event %d = %#v, want %#v", i, got, want[i-1])
		}
	}
}
