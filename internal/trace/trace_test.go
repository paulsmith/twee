package trace

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// makeTinyPNG creates a small valid PNG in memory for testing.
func makeTinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{200, 200, 200, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
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
	tr.WriteResize(20, 5)
	tr.WriteOutput([]byte("world"), time.Now())

	// Add a screenshot.
	tr.AddScreenshotPNG(makeTinyPNG(t))

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
	var man Manifest
	if err := json.NewDecoder(mf).Decode(&man); err != nil {
		t.Fatal("decode manifest:", err)
	}
	mf.Close()

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
	if len(man.Screenshots) != 1 {
		t.Fatalf("screenshots = %v, want 1 entry", man.Screenshots)
	}
	if man.Screenshots[0] != "screenshots/0000.png" {
		t.Errorf("screenshot[0] = %q", man.Screenshots[0])
	}

	// Check events.jsonl
	ef, err := zr.Open("events.jsonl")
	if err != nil {
		t.Fatal("events.jsonl not found:", err)
	}
	sc := bufio.NewScanner(ef)
	nEvents := 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev event
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatalf("event line %d: %v\nraw: %s", nEvents, err, line)
		}
		nEvents++
	}
	ef.Close()
	if nEvents != 5 { // 2 output + 2 input + 1 resize
		t.Errorf("events count = %d, want 5", nEvents)
	}

	// Check screenshot is a valid PNG.
	sf, err := zr.Open("screenshots/0000.png")
	if err != nil {
		t.Fatal("screenshot not found:", err)
	}
	if _, err := png.Decode(sf); err != nil {
		t.Fatal("screenshot is not valid PNG:", err)
	}
	sf.Close()
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
