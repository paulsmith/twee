package trace

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/tracepolicy"
)

func TestTraceCloseReportsCleanupFailureAndPreservesPrimaryError(t *testing.T) {
	cleanupErr := errors.New("injected cleanup failure")
	primaryErr := errors.New("injected primary failure")
	tr, err := New(filepath.Join(t.TempDir(), "session.twee"), Manifest{})
	if err != nil {
		t.Fatal(err)
	}
	tr.removeAll = func(string) error { return cleanupErr }
	tr.err = primaryErr

	err = tr.Close()
	if !errors.Is(err, primaryErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("Close error = %v, want joined primary and cleanup errors", err)
	}
	if second := tr.Close(); !errors.Is(second, primaryErr) || !errors.Is(second, cleanupErr) {
		t.Fatalf("second Close error = %v, want same joined errors", second)
	}
	_ = os.RemoveAll(tr.workDir)
}

func TestNewJoinsRollbackCleanupFailures(t *testing.T) {
	primaryErr := errors.New("injected chmod failure")
	cleanupErr := errors.New("injected rollback cleanup failure")
	fsys := defaultTraceFS()
	fsys.chmod = func(string, os.FileMode) error { return primaryErr }
	var workDir string
	originalMkdir := fsys.mkdirTemp
	fsys.mkdirTemp = func(dir, pattern string) (string, error) {
		var err error
		workDir, err = originalMkdir(dir, pattern)
		return workDir, err
	}
	fsys.removeAll = func(string) error { return cleanupErr }

	_, err := newWithFS(filepath.Join(t.TempDir(), "session.twee"), Manifest{}, fsys)
	if !errors.Is(err, primaryErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("New error = %v, want joined primary and rollback cleanup errors", err)
	}
	if workDir != "" {
		_ = os.RemoveAll(workDir)
	}
}

func TestNewJoinsEventsFileCloseAndRollbackFailures(t *testing.T) {
	primaryErr := errors.New("injected file chmod failure")
	closeErr := errors.New("injected file close failure")
	cleanupErr := errors.New("injected rollback cleanup failure")
	fsys := defaultTraceFS()
	fsys.chmodFile = func(*os.File, os.FileMode) error { return primaryErr }
	fsys.closeFile = func(f *os.File) error { _ = f.Close(); return closeErr }
	fsys.removeAll = func(path string) error { _ = os.RemoveAll(path); return cleanupErr }

	_, err := newWithFS(filepath.Join(t.TempDir(), "session.twee"), Manifest{}, fsys)
	if !errors.Is(err, primaryErr) || !errors.Is(err, closeErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("New error = %v, want joined chmod, close, and cleanup errors", err)
	}
}

func TestTraceArtifactsHavePrivateModes(t *testing.T) {
	tr, err := New(filepath.Join(t.TempDir(), "session.twee"), Manifest{})
	if err != nil {
		t.Fatal(err)
	}
	for name, artifact := range map[string]struct {
		path string
		want os.FileMode
	}{
		"work directory": {tr.workDir, 0o700},
		"events file":    {tr.eventsPath, 0o600},
	} {
		info, err := os.Stat(artifact.path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != artifact.want {
			t.Errorf("%s mode = %o, want %o", name, got, artifact.want)
		}
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(tr.path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("final trace mode = %o, want 600", got)
	}
}

func TestTraceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.twee")

	tr, err := New(path, Manifest{
		Command: []string{"/bin/sh", "-c", "echo hello"},
		Env:     map[string]string{"TERM": "xterm-256color"},
		Cols:    10,
		Rows:    3,
		Pid:     12345,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Write some events.
	tr.WriteOutput([]byte("hello\r\n"), time.Now())
	tr.WriteInput("type", "", []byte("h"))
	tr.WriteInput("key", "Enter", []byte("\r"))
	x, y := 0, 2
	tr.WriteMouseInput(MouseInput{
		Gesture: "click", X: &x, Y: &y, Button: "left",
		Modifiers: []string{},
	}, []byte("\x1b[<0;1;3M\x1b[<0;1;3m"))
	tr.WriteResize(20, 5)
	tr.WriteOutput([]byte("world"), time.Now())
	tr.WriteExit(7)

	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}

	// Open the zip and verify contents.
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()

	// Check manifest.json
	mf, err := zr.Open("manifest.json")
	if err != nil {
		t.Fatal("manifest.json not found:", err)
	}
	var rawManifest map[string]json.RawMessage
	if err := json.NewDecoder(mf).Decode(&rawManifest); err != nil {
		t.Fatal("decode manifest:", err)
	}
	_ = mf.Close()
	if _, ok := rawManifest["screenshots"]; ok {
		t.Fatal("manifest has screenshots key")
	}

	var man Manifest
	manifestBytes, err := json.Marshal(rawManifest)
	if err != nil {
		t.Fatal("re-encode manifest:", err)
	}
	if err := json.Unmarshal(manifestBytes, &man); err != nil {
		t.Fatal("decode manifest struct:", err)
	}

	if man.Version != 1 {
		t.Errorf("version = %d, want 1", man.Version)
	}
	if len(man.Command) != 3 || man.Command[0] != "/bin/sh" {
		t.Errorf("command = %v", man.Command)
	}
	if man.Pid != 12345 {
		t.Errorf("pid = %d, want 12345", man.Pid)
	}
	if man.Cols != 10 || man.Rows != 3 {
		t.Errorf("size = %dx%d, want 10x3", man.Cols, man.Rows)
	}
	if man.Host.OS == "" || man.Host.Arch == "" {
		t.Errorf("host info empty: %+v", man.Host)
	}
	if man.StartedAt.IsZero() || man.StoppedAt.IsZero() {
		t.Errorf("timestamps: started=%v stopped=%v", man.StartedAt, man.StoppedAt)
	}
	if !man.StoppedAt.After(man.StartedAt) && !man.StoppedAt.Equal(man.StartedAt) {
		t.Errorf("stopped_at (%v) should be >= started_at (%v)", man.StoppedAt, man.StartedAt)
	}
	assertNoScreenshotEntries(t, &zr.Reader)

	// Check events.jsonl
	ef, err := zr.Open("events.jsonl")
	if err != nil {
		t.Fatal("events.jsonl not found:", err)
	}
	sc := bufio.NewScanner(ef)
	nEvents := 0
	sawExit := false
	sawMouse := false
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev EventRecord
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatalf("event line %d: %v\nraw: %s", nEvents, err, line)
		}
		if ev.Type == EventTypeExit {
			sawExit = true
			if ev.Code != 7 {
				t.Errorf("exit code = %d, want 7", ev.Code)
			}
		}
		if ev.Kind == InputKindMouse {
			sawMouse = true
			if ev.Mouse == nil || ev.Mouse.Gesture != "click" || ev.Mouse.X == nil || *ev.Mouse.X != 0 {
				t.Errorf("mouse event = %+v, want click at x=0", ev.Mouse)
			}
			if ev.Mouse.Modifiers == nil {
				t.Errorf("mouse modifiers = nil, want explicit empty array")
			}
		}
		nEvents++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if err := ef.Close(); err != nil {
		t.Fatal(err)
	}
	if nEvents != 7 { // 2 output + 3 input + 1 resize + 1 exit
		t.Errorf("events count = %d, want 7", nEvents)
	}
	if !sawExit {
		t.Error("events missing exit event")
	}
	if !sawMouse {
		t.Error("events missing mouse event")
	}
}

func TestTraceIncludesNetworkCapture(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "capture.pcap")
	want := makeTestPCAP()
	if err := os.WriteFile(source, want, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "session.twee")
	tr, err := New(path, Manifest{})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.AttachNetworkCapture(source, NetworkCapture{
		Format: NetworkCaptureFormat, Stream: NetworkCaptureStream,
		GVisorVersion: "test", PublishTCP: []string{"127.0.0.1:8080=80"},
		ByteLimit: 1024, CapturedBytes: int64(len(want)), Status: NetworkCaptureStatusComplete,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	r, err := zr.Open("streams/network.pcap")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("capture = %x, want %x", got, want)
	}
}

func TestTraceRejectsReservedAndDuplicateAttachments(t *testing.T) {
	tr, err := New(filepath.Join(t.TempDir(), "session.twee"), Manifest{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	for _, name := range []string{"manifest.json", "events.jsonl", NetworkCaptureStream} {
		if err := tr.AttachFile("source", name); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("AttachFile(%q) error = %v, want reserved-name error", name, err)
		}
	}
	if err := tr.AttachFile("first", "extra/data.bin"); err != nil {
		t.Fatal(err)
	}
	if err := tr.AttachFile("second", "extra/data.bin"); err == nil || !strings.Contains(err.Error(), "already attached") {
		t.Fatalf("duplicate AttachFile error = %v, want duplicate error", err)
	}
}

func TestTraceRejectsInvalidNetworkCaptureMetadata(t *testing.T) {
	valid := makeTestPCAP()
	base := NetworkCapture{
		Format: NetworkCaptureFormat, Stream: NetworkCaptureStream,
		GVisorVersion: "test", ByteLimit: 1024,
		CapturedBytes: int64(len(valid)),
		Status:        NetworkCaptureStatusComplete,
	}
	tests := []struct {
		name    string
		prepare func(t *testing.T, tr *Trace, source string)
		edit    func(*NetworkCapture)
		want    string
	}{
		{
			name: "wrong format",
			edit: func(c *NetworkCapture) { c.Format = "pcapng" },
			want: `format "pcapng" is unsupported`,
		},
		{
			name: "wrong stream",
			edit: func(c *NetworkCapture) { c.Stream = "capture.pcap" },
			want: "capture stream must be",
		},
		{
			name: "empty gVisor version",
			edit: func(c *NetworkCapture) { c.GVisorVersion = "" },
			want: "gVisor version is empty",
		},
		{
			name: "byte limit zero",
			edit: func(c *NetworkCapture) { c.ByteLimit = 0 },
			want: "byte limit must be in 1..",
		},
		{
			name: "byte limit above policy maximum",
			edit: func(c *NetworkCapture) { c.ByteLimit = tracepolicy.MaxNetworkCaptureBytes + 1 },
			want: "byte limit must be in 1..",
		},
		{
			name: "captured bytes negative",
			edit: func(c *NetworkCapture) { c.CapturedBytes = -1 },
			want: "size -1 is outside byte limit 1024",
		},
		{
			name: "captured bytes above byte limit",
			edit: func(c *NetworkCapture) { c.CapturedBytes = c.ByteLimit + 1 },
			want: "size 1025 is outside byte limit 1024",
		},
		{
			name: "captured bytes mismatch staged file size",
			edit: func(c *NetworkCapture) { c.CapturedBytes++ },
			want: "does not match staged file size",
		},
		{
			name: "packet count negative",
			edit: func(c *NetworkCapture) { c.PacketCount = -1 },
			want: "packet count -1 does not match staged file count 0",
		},
		{
			name: "packet count mismatch",
			edit: func(c *NetworkCapture) { c.PacketCount = 1 },
			want: "packet count 1 does not match staged file count 0",
		},
		{
			name: "complete status with truncated flag",
			edit: func(c *NetworkCapture) { c.Truncated = true },
			want: `status "complete" is inconsistent with truncated=true`,
		},
		{
			name: "truncated status without truncated flag",
			edit: func(c *NetworkCapture) { c.Status = NetworkCaptureStatusTruncated },
			want: `status "truncated" is inconsistent with truncated=false`,
		},
		{
			name: "double attach",
			prepare: func(t *testing.T, tr *Trace, source string) {
				t.Helper()
				if err := tr.AttachNetworkCapture(source, base); err != nil {
					t.Fatalf("first AttachNetworkCapture: %v", err)
				}
			},
			edit: func(*NetworkCapture) {},
			want: "already attached",
		},
		{
			name: "attach after close",
			prepare: func(t *testing.T, tr *Trace, _ string) {
				t.Helper()
				if err := tr.Close(); err != nil {
					t.Fatalf("Close: %v", err)
				}
			},
			edit: func(*NetworkCapture) {},
			want: "already closed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "capture.pcap")
			if err := os.WriteFile(source, valid, 0o600); err != nil {
				t.Fatal(err)
			}
			tr, err := New(filepath.Join(dir, "session.twee"), Manifest{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = tr.Close() })
			if tt.prepare != nil {
				tt.prepare(t, tr, source)
			}
			capture := base
			tt.edit(&capture)
			err = tr.AttachNetworkCapture(source, capture)
			if err == nil {
				t.Fatal("AttachNetworkCapture succeeded with invalid metadata")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("AttachNetworkCapture error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestTraceRejectsInvalidNetworkCaptureSource(t *testing.T) {
	valid := makeTestPCAP()
	base := NetworkCapture{
		Format: NetworkCaptureFormat, Stream: NetworkCaptureStream,
		GVisorVersion: "test", ByteLimit: 1024,
		CapturedBytes: int64(len(valid)),
		Status:        NetworkCaptureStatusComplete,
	}
	tests := []struct {
		name   string
		source func(t *testing.T, dir string) string
		want   string
	}{
		{
			name:   "empty path",
			source: func(*testing.T, string) string { return "" },
			want:   "source is empty",
		},
		{
			name: "missing file",
			source: func(_ *testing.T, dir string) string {
				return filepath.Join(dir, "missing.pcap")
			},
			want: "stat network capture",
		},
		{
			name:   "directory",
			source: func(_ *testing.T, dir string) string { return dir },
			want:   "not a regular file",
		},
		{
			name: "invalid PCAP content",
			source: func(t *testing.T, dir string) string {
				t.Helper()
				path := filepath.Join(dir, "zeros.pcap")
				if err := os.WriteFile(path, make([]byte, len(makeTestPCAP())), 0o600); err != nil {
					t.Fatal(err)
				}
				return path
			},
			want: "invalid PCAP magic",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tr, err := New(filepath.Join(dir, "session.twee"), Manifest{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = tr.Close() })
			err = tr.AttachNetworkCapture(tt.source(t, dir), base)
			if err == nil {
				t.Fatal("AttachNetworkCapture succeeded with invalid source")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("AttachNetworkCapture error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func makeTestPCAP() []byte {
	return []byte{
		0xd4, 0xc3, 0xb2, 0xa1, 0x02, 0x00, 0x04, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xff, 0xff, 0x00, 0x00, 0x65, 0x00, 0x00, 0x00,
	}
}

func assertNoScreenshotEntries(t *testing.T, zr *zip.Reader) {
	t.Helper()
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "screenshots/") {
			t.Fatalf("unexpected screenshot entry %q", f.Name)
		}
	}
}

func TestTraceIdempotentClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.twee")

	tr, err := New(path, Manifest{
		Command: []string{"echo"},
		Cols:    10,
		Rows:    3,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := tr.Close(); err != nil {
		t.Fatal("first close:", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatal("second close should succeed:", err)
	}
}

func TestTraceAbortDoesNotPublishBundle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.twee")
	tr, err := New(path, Manifest{})
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("capture did not close")
	if err := tr.Abort(cause); !errors.Is(err, cause) {
		t.Fatalf("Abort error = %v, want cause", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("published path after Abort: %v", err)
	}
	if err := tr.Close(); !errors.Is(err, cause) {
		t.Fatalf("Close after Abort = %v, want cause", err)
	}
}

func TestTraceNewValidatesOutputPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "session.twee")
	if _, err := New(path, Manifest{Command: []string{"echo"}, Cols: 10, Rows: 3}); err == nil {
		t.Fatal("New succeeded with missing output directory")
	}

	dir := t.TempDir()
	if _, err := New(dir, Manifest{Command: []string{"echo"}, Cols: 10, Rows: 3}); err == nil {
		t.Fatal("New succeeded with output path pointing at directory")
	}

	validPath := filepath.Join(t.TempDir(), "session.twee")
	tr, err := New(validPath, Manifest{Command: []string{"echo"}, Cols: 10, Rows: 3})
	if err != nil {
		t.Fatalf("New valid path: %v", err)
	}
	if _, err := os.Stat(validPath); !os.IsNotExist(err) {
		t.Fatalf("final path exists before Close: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(validPath); err != nil {
		t.Fatalf("final path after Close: %v", err)
	}
}

func TestTraceNewRejectsPredeclaredNetworkCapture(t *testing.T) {
	_, err := New(filepath.Join(t.TempDir(), "session.twee"), Manifest{Network: &NetworkCapture{}})
	if err == nil || !strings.Contains(err.Error(), "AttachNetworkCapture") {
		t.Fatalf("New error = %v, want typed network attachment guidance", err)
	}
}

func TestTraceTimestampsNeverDecreaseAcrossEventTypes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.twee")
	tr, err := New(path, Manifest{Command: []string{"echo"}, Cols: 10, Rows: 3})
	if err != nil {
		t.Fatal(err)
	}
	tr.WriteInput(InputKindKey, "x", []byte("x"))
	tr.WriteOutput([]byte("data"), tr.start.Add(-time.Second))
	tr.WriteMouseInput(MouseInput{}, []byte("mouse"))
	tr.WriteResize(20, 4)
	tr.WriteExit(0)
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	ef, err := zr.Open("events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer ef.Close()
	var last int64
	first := true
	scanner := bufio.NewScanner(ef)
	for scanner.Scan() {
		var event EventRecord
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		if !first && event.TMS < last {
			t.Fatalf("event timestamp decreased from %d to %d", last, event.TMS)
		}
		if event.TMS < 0 {
			t.Fatalf("event timestamp is negative: %d", event.TMS)
		}
		last, first = event.TMS, false
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestTraceMSClampsPreStartAndMillisecondTruncation(t *testing.T) {
	tr, err := New(filepath.Join(t.TempDir(), "session.twee"), Manifest{})
	if err != nil {
		t.Fatal(err)
	}
	if got := tr.ms(tr.start.Add(-time.Second)); got != 0 {
		t.Fatalf("pre-start timestamp = %d, want 0", got)
	}
	if got := tr.ms(tr.start.Add(1500 * time.Microsecond)); got != 1 {
		t.Fatalf("sub-millisecond timestamp = %d, want 1", got)
	}
	if got := tr.ms(tr.start.Add(2*time.Millisecond + 500*time.Microsecond)); got != 2 {
		t.Fatalf("millisecond timestamp = %d, want 2", got)
	}
	_ = tr.Abort(nil)
}

func TestTraceConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.twee")

	tr, err := New(path, Manifest{
		Command: []string{"echo"},
		Cols:    10,
		Rows:    3,
	})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for range 50 {
				tr.WriteOutput([]byte("data"), time.Now())
				tr.WriteInput("type", "", []byte("x"))
			}
		}(i)
	}
	wg.Wait()

	if err := tr.Close(); err != nil {
		t.Fatal("close:", err)
	}

	// Verify the zip is well-formed.
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = zr.Close()
}
