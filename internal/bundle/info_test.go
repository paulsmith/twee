package bundle

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/trace"
)

// writeSyntheticTrace builds a real .twee bundle via internal/trace's
// writer (the same helper style internal/trace's own tests use), rather
// than hand-rolling a zip.
func writeSyntheticTrace(t *testing.T) string {
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
	tr.WriteOutput([]byte("more"), time.Now())
	tr.WriteExit(0)
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeRawZip builds a .twee bundle from literal file bodies, for tests
// that need to control manifest/events content precisely (mirrors the
// writeTestBundle helper pattern used in internal/play and
// internal/export's own tests).
func writeRawZip(t *testing.T, files map[string]string) string {
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

func TestInspectValidBundle(t *testing.T) {
	path := writeSyntheticTrace(t)

	info, err := Inspect(path)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if info.Version != 1 {
		t.Errorf("version = %d, want 1", info.Version)
	}
	if len(info.Command) != 3 || info.Command[0] != "/bin/sh" {
		t.Errorf("command = %v", info.Command)
	}
	if info.Cols != 80 || info.Rows != 24 {
		t.Errorf("size = %dx%d, want 80x24", info.Cols, info.Rows)
	}
	if info.StartedAt.IsZero() || info.StoppedAt.IsZero() {
		t.Errorf("timestamps: started=%v stopped=%v", info.StartedAt, info.StoppedAt)
	}
	if info.DurationMs < 0 {
		t.Errorf("duration_ms = %d, want >= 0", info.DurationMs)
	}
	if info.SizeBytes <= 0 {
		t.Errorf("size_bytes = %d, want > 0", info.SizeBytes)
	}
	want := map[string]int{"output": 2, "input": 1, "resize": 1, "exit": 1}
	for typ, n := range want {
		if info.Events[typ] != n {
			t.Errorf("events[%q] = %d, want %d (got %#v)", typ, info.Events[typ], n, info.Events)
		}
	}
	if info.NetworkCapture.Present {
		t.Fatalf("network capture = %+v, want absent", info.NetworkCapture)
	}
}

func TestInspectReportsNetworkCaptureMetadata(t *testing.T) {
	dir := t.TempDir()
	pcap := testPCAP()
	pcapPath := filepath.Join(dir, "network.pcap")
	if err := os.WriteFile(pcapPath, pcap, 0o600); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(dir, "session.twee")
	tr, err := trace.New(bundlePath, trace.Manifest{Command: []string{"server"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.AttachNetworkCapture(pcapPath, trace.NetworkCapture{
		Format: trace.NetworkCaptureFormat, Stream: trace.NetworkCaptureStream,
		GVisorVersion: "test-version", PublishTCP: []string{"127.0.0.1:8080=80"},
		ByteLimit: 4096, CapturedBytes: int64(len(pcap)),
		Truncated: true, Status: trace.NetworkCaptureStatusTruncated,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := Inspect(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	network := info.NetworkCapture
	if !network.Present || network.Format != "pcap" || network.Stream != trace.NetworkCaptureStream || network.SizeBytes != int64(len(pcap)) {
		t.Fatalf("network capture = %+v", network)
	}
	if !network.Truncated || network.Status != trace.NetworkCaptureStatusTruncated || network.ByteLimit != 4096 {
		t.Fatalf("network truncation metadata = %+v", network)
	}
	if len(network.PublishTCP) != 1 || network.PublishTCP[0] != "127.0.0.1:8080=80" {
		t.Fatalf("publications = %#v", network.PublishTCP)
	}
}

func TestInspectMissingFileIsIO(t *testing.T) {
	_, err := Inspect(filepath.Join(t.TempDir(), "missing.twee"))
	if err == nil {
		t.Fatal("want error for missing file")
	}
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("error = %v, want *LoadError", err)
	}
	if le.Kind != ErrIO {
		t.Errorf("kind = %v, want ErrIO", le.Kind)
	}
}

func TestInspectDirectoryIsIO(t *testing.T) {
	dir := t.TempDir()
	_, err := Inspect(dir)
	var le *LoadError
	if !errors.As(err, &le) || le.Kind != ErrIO {
		t.Fatalf("error = %v, want *LoadError{Kind: ErrIO}", err)
	}
}

func TestInspectCorruptZipIsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.twee")
	if err := os.WriteFile(path, []byte("not a zip file at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Inspect(path)
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("error = %v, want *LoadError", err)
	}
	if le.Kind != ErrInvalid {
		t.Errorf("kind = %v, want ErrInvalid", le.Kind)
	}
}

func TestInspectBadManifestVersionIsInvalid(t *testing.T) {
	path := writeRawZip(t, map[string]string{
		"manifest.json": `{"version":2,"cols":10,"rows":3}`,
		"events.jsonl":  "",
	})
	_, err := Inspect(path)
	var le *LoadError
	if !errors.As(err, &le) || le.Kind != ErrInvalid {
		t.Fatalf("error = %v, want *LoadError{Kind: ErrInvalid}", err)
	}
}
