package trace

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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
	defer zr.Close()

	// Check manifest.json
	mf, err := zr.Open("manifest.json")
	if err != nil {
		t.Fatal("manifest.json not found:", err)
	}
	var rawManifest map[string]json.RawMessage
	if err := json.NewDecoder(mf).Decode(&rawManifest); err != nil {
		t.Fatal("decode manifest:", err)
	}
	mf.Close()
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
		var ev event
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatalf("event line %d: %v\nraw: %s", nEvents, err, line)
		}
		if ev.Type == "exit" {
			sawExit = true
			if ev.Code != 7 {
				t.Errorf("exit code = %d, want 7", ev.Code)
			}
		}
		if ev.Kind == "mouse" {
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
	ef.Close()
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
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
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
	zr.Close()
}
