package play

import (
	"archive/zip"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenBundleRoundTrip(t *testing.T) {
	path := writeTestBundle(t, map[string]string{
		"manifest.json": `{"version":1,"command":["vim"],"cols":80,"rows":24}`,
		"events.jsonl": strings.Join([]string{
			`{"t_ms":0,"type":"output","bytes_b64":"` + base64.StdEncoding.EncodeToString([]byte("hi")) + `"}`,
			`{"t_ms":10,"type":"input","kind":"key","key":"Enter","bytes_b64":"DQ=="}`,
			`{"t_ms":20,"type":"resize","cols":100,"rows":40}`,
			`{"t_ms":30,"type":"exit","code":0}`,
		}, "\n"),
	})

	b, err := OpenBundle(path)
	if err != nil {
		t.Fatalf("OpenBundle: %v", err)
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

func TestOpenBundleEmptyEventsIsLegal(t *testing.T) {
	path := writeTestBundle(t, map[string]string{
		"manifest.json": `{"version":1,"command":[],"cols":10,"rows":3}`,
		"events.jsonl":  "",
	})
	b, err := OpenBundle(path)
	if err != nil {
		t.Fatalf("OpenBundle: %v", err)
	}
	if len(b.Events) != 0 {
		t.Fatalf("events = %d, want 0", len(b.Events))
	}
}

func TestOpenBundleNegativeCases(t *testing.T) {
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
			_, err := OpenBundle(writeTestBundle(t, tt.files))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want contains %q", err, tt.want)
			}
		})
	}
}

func TestOpenBundleOpenFailure(t *testing.T) {
	_, err := OpenBundle(filepath.Join(t.TempDir(), "missing.twee"))
	if err == nil || !strings.Contains(err.Error(), "twee play: open") {
		t.Fatalf("error = %v, want open failure", err)
	}
}

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
