package tracebundle

import (
	"archive/zip"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/tracepolicy"
)

func TestOpenRoundTrip(t *testing.T) {
	path := writeTestBundle(t, map[string]string{
		"manifest.json": `{"version":1,"command":["vim"],"cols":80,"rows":24}`,
		"events.jsonl": strings.Join([]string{
			`{"t_ms":0,"type":"output","bytes_b64":"` + base64.StdEncoding.EncodeToString([]byte("hi")) + `"}`,
			`{"t_ms":10,"type":"input","kind":"key","key":"Enter","bytes_b64":"DQ=="}`,
			`{"t_ms":20,"type":"resize","cols":100,"rows":40}`,
			`{"t_ms":30,"type":"exit","code":0}`,
		}, "\n"),
	})

	b, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if b.Manifest.Version != 1 || b.Manifest.Cols != 80 || b.Manifest.Rows != 24 {
		t.Fatalf("manifest = %+v", b.Manifest)
	}
	if len(b.Events) != 4 {
		t.Fatalf("events = %d, want 4", len(b.Events))
	}
	if got := string(b.Events[0].Bytes); got != "hi" {
		t.Fatalf("decoded bytes = %q", got)
	}
	if b.MaxCols != 100 || b.MaxRows != 40 {
		t.Fatalf("max size = %dx%d, want 100x40", b.MaxCols, b.MaxRows)
	}
}

func TestOpenEmptyEventsIsLegal(t *testing.T) {
	path := writeTestBundle(t, map[string]string{
		"manifest.json": `{"version":1,"command":[],"cols":10,"rows":3}`,
		"events.jsonl":  "",
	})
	b, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(b.Events) != 0 {
		t.Fatalf("events = %d, want 0", len(b.Events))
	}
}

func TestOpenRejectsUnsafeReplayDimensionsAndNegativeTimestamps(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		events   string
		want     string
	}{
		{"zero initial", `{"version":1,"cols":0,"rows":3}`, "", "terminal size 0x3"},
		{"oversized initial", `{"version":1,"cols":65536,"rows":1}`, "", "outside 1..65535"},
		{"cell budget initial", `{"version":1,"cols":2000,"rows":1000}`, "", "exceeds 100000 cells"},
		{"zero resize", `{"version":1,"cols":10,"rows":3}`, `{"t_ms":0,"type":"resize","cols":10,"rows":0}`, "terminal size 10x0"},
		{"cell budget resize", `{"version":1,"cols":10,"rows":3}`, `{"t_ms":0,"type":"resize","cols":2000,"rows":1000}`, "exceeds 100000 cells"},
		{"negative timestamp", `{"version":1,"cols":10,"rows":3}`, `{"t_ms":-1,"type":"output"}`, "timestamp -1 is negative"},
		{"timestamp overflow", `{"version":1,"cols":10,"rows":3}`, `{"t_ms":9223372036854775807,"type":"output"}`, "exceeds time.Duration range"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeTestBundle(t, map[string]string{
				"manifest.json": test.manifest,
				"events.jsonl":  test.events,
			})
			_, validation, err := OpenValidated(path)
			if err != nil {
				t.Fatalf("OpenValidated: %v", err)
			}
			if validation.Valid || !containsIssue(validation.Issues, test.want) {
				t.Fatalf("validation = %+v, want issue containing %q", validation, test.want)
			}
		})
	}
}

func containsIssue(issues []string, want string) bool {
	for _, issue := range issues {
		if strings.Contains(issue, want) {
			return true
		}
	}
	return false
}

func TestOpenNegativeCases(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name: "missing manifest",
			files: map[string]string{
				"events.jsonl": "",
			},
			want: "missing manifest.json",
		},
		{
			name: "missing events",
			files: map[string]string{
				"manifest.json": `{"version":1,"cols":10,"rows":3}`,
			},
			want: "missing events.jsonl",
		},
		{
			name: "unsupported version",
			files: map[string]string{
				"manifest.json": `{"version":2,"cols":10,"rows":3}`,
				"events.jsonl":  "",
			},
			want: "unsupported bundle version 2",
		},
		{
			name: "malformed json line",
			files: map[string]string{
				"manifest.json": `{"version":1,"cols":10,"rows":3}`,
				"events.jsonl":  "{}\n{bad",
			},
			want: "events.jsonl line 2",
		},
		{
			name: "bad base64",
			files: map[string]string{
				"manifest.json": `{"version":1,"cols":10,"rows":3}`,
				"events.jsonl":  `{"type":"output","bytes_b64":"%%%"}`,
			},
			want: "decode bytes_b64",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Open(writeTestBundle(t, tt.files))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want contains %q", err, tt.want)
			}
		})
	}
}

func TestOpenFailure(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "missing.twee"))
	if err == nil || !strings.Contains(err.Error(), "open") {
		t.Fatalf("error = %v, want open failure", err)
	}
}

func TestOpenValidatedOpensPathOnce(t *testing.T) {
	path := writeTestBundle(t, map[string]string{
		"manifest.json": `{"version":1,"cols":10,"rows":3}`,
		"events.jsonl":  `{"t_ms":0,"type":"output"}`,
	})
	opens := 0
	bundle, validation, err := openValidated(path, func(name string) (*os.File, error) {
		opens++
		return os.Open(name)
	})
	if err != nil || !validation.Valid || len(bundle.Events) != 1 {
		t.Fatalf("OpenValidated = bundle %+v, validation %+v, error %v", bundle, validation, err)
	}
	if opens != 1 {
		t.Fatalf("open calls = %d, want 1", opens)
	}
}

func TestOpenValidatedAccumulatesTypedAndPayloadIssues(t *testing.T) {
	path := writeTestBundle(t, map[string]string{
		"manifest.json": `{"version":1,"cols":10,"rows":3}`,
		"events.jsonl": strings.Join([]string{
			`{"t_ms":5,"type":"output","bytes_b64":"%%%"}`,
			`{"t_ms":4,"type":"input","bytes_b64":"!!!"}`,
			`{"t_ms":6,"type":"resize","cols":"wide","rows":3}`,
			`{"t_ms":7,"type":"teleport"}`,
		}, "\n"),
	})
	bundle, validation, err := OpenValidated(path)
	if err != nil {
		t.Fatalf("OpenValidated: %v", err)
	}
	if validation.Valid || bundle.Manifest.Version != 0 || len(bundle.Events) != 0 {
		t.Fatalf("bundle = %+v, validation = %+v; want invalid and no decoded bundle", bundle, validation)
	}
	for _, want := range []string{
		"line 1: decode bytes_b64",
		"line 2: decode bytes_b64",
		"timestamp 4 before previous 5",
		"line 3: json: cannot unmarshal string",
		`unknown event type "teleport"`,
	} {
		if !issuesContain(validation.Issues, want) {
			t.Errorf("issues = %v, want one containing %q", validation.Issues, want)
		}
	}
	if validation.Events != 4 {
		t.Errorf("events = %d, want 4", validation.Events)
	}
}

func TestOpenValidatedContinuesAcrossIndependentArchiveParts(t *testing.T) {
	path := writeTestBundle(t, map[string]string{
		"manifest.json":                    `{not json`,
		"events.jsonl":                     `{"t_ms":0,"type":"output","bytes_b64":"%%%"}`,
		tracepolicy.NetworkCaptureStream:   "not a pcap",
		"extra/../non-canonical-entry.txt": "unsafe",
	})
	_, validation, err := OpenValidated(path)
	if err != nil {
		t.Fatalf("OpenValidated: %v", err)
	}
	for _, want := range []string{
		"unsafe non-canonical zip entry path",
		"manifest.json:",
		"decode bytes_b64",
		tracepolicy.NetworkCaptureStream + ":",
	} {
		if !issuesContain(validation.Issues, want) {
			t.Errorf("issues = %v, want one containing %q", validation.Issues, want)
		}
	}
}

func TestOpenRejectsDuplicateRequiredEntriesWithoutChoosingOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duplicate.twee")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, entry := range []struct{ name, body string }{
		{"manifest.json", `{"version":1,"command":["first"]}`},
		{"manifest.json", `{"version":1,"command":["second"]}`},
		{"events.jsonl", ""},
	} {
		w, err := zw.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(path)
	if err == nil || !strings.Contains(err.Error(), "duplicate required zip entry manifest.json") {
		t.Fatalf("Open error = %v, want duplicate-entry rejection", err)
	}
}

func TestOpenRejectsUnsafeEntryPath(t *testing.T) {
	path := writeTestBundle(t, map[string]string{
		"manifest.json":     `{"version":1}`,
		"events.jsonl":      "",
		"../outside-marker": "not extracted, but unsafe structure",
	})
	_, err := Open(path)
	if err == nil || !strings.Contains(err.Error(), "unsafe non-canonical zip entry path") {
		t.Fatalf("Open error = %v, want unsafe-path rejection", err)
	}
}

func TestOpenRejectsCompressedEventsBomb(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bomb.twee")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	mw, _ := zw.Create("manifest.json")
	_, _ = io.WriteString(mw, `{"version":1}`)
	ew, _ := zw.Create("events.jsonl")
	if _, err := io.CopyN(ew, zeroReader{}, tracepolicy.MaxEventsBytes+1); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "uncompressed size") {
		t.Fatalf("Open error = %v, want compressed-bomb size rejection", err)
	}
}

func TestDecodeEventsEnforcesCountAndDecodedPayload(t *testing.T) {
	event := `{"type":"output"}` + "\n"
	if _, err := decodeEventsWithLimits(strings.NewReader(strings.Repeat(event, 4)), 3, 1024); err == nil || !strings.Contains(err.Error(), "event count exceeds 3") {
		t.Fatalf("count error = %v", err)
	}
	payload := base64.StdEncoding.EncodeToString([]byte("abc"))
	body := fmt.Sprintf("{\"type\":\"output\",\"bytes_b64\":%q}\n{\"type\":\"output\",\"bytes_b64\":%q}\n", payload, payload)
	if _, err := decodeEventsWithLimits(strings.NewReader(body), 10, 5); err == nil || !strings.Contains(err.Error(), "decoded payload exceeds 5") {
		t.Fatalf("payload error = %v", err)
	}
}

func TestDecodeEventsEnforcesLineSize(t *testing.T) {
	line := strings.Repeat("x", tracepolicy.MaxEventLineBytes+1)
	if _, err := decodeEvents(strings.NewReader(line)); err == nil || !strings.Contains(err.Error(), "token too long") {
		t.Fatalf("line-size error = %v", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func writeTestBundle(t *testing.T, files map[string]string) string {
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

func issuesContain(issues []string, want string) bool {
	for _, issue := range issues {
		if strings.Contains(issue, want) {
			return true
		}
	}
	return false
}
