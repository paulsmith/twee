package export

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type htmlFrameRecord struct {
	Src        string `json:"src"`
	DurationNS int64  `json:"duration_ns"`
}

func readHTMLFrames(t *testing.T, page []byte) []htmlFrameRecord {
	t.Helper()
	const marker = `<script type="application/json" id="twee-frames">`
	start := bytes.Index(page, []byte(marker))
	if start < 0 {
		t.Fatalf("page is missing embedded frame data")
	}
	start += len(marker)
	end := bytes.Index(page[start:], []byte(`</script>`))
	if end < 0 {
		t.Fatalf("embedded frame data has no closing script tag")
	}
	var frames []htmlFrameRecord
	if err := json.Unmarshal(page[start:start+end], &frames); err != nil {
		t.Fatalf("decode embedded frames: %v", err)
	}
	return frames
}

func TestExportHTMLEndToEnd(t *testing.T) {
	bundle := writeTestBundle(t)
	out := filepath.Join(t.TempDir(), "out.html")
	if err := Export(bundle, out, Options{}); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"<!doctype html>",
		"Content-Security-Policy",
		`id="restart"`,
		`id="previous"`,
		`id="play"`,
		`id="next"`,
		`id="timeline"`,
		`id="time"`,
		`id="speed"`,
		"<canvas",
		"drawImage",
		"requestAnimationFrame",
	} {
		if !bytes.Contains(page, []byte(want)) {
			t.Errorf("page is missing %q", want)
		}
	}
	for _, unwanted := range []string{
		"http://",
		"https://",
		"super-secret-command",
		"secret-environment-value",
		"secret-input-bytes",
		"secret-hostname",
		"424242",
	} {
		if bytes.Contains(page, []byte(unwanted)) {
			t.Errorf("page unexpectedly contains %q", unwanted)
		}
	}

	frames := readHTMLFrames(t, page)
	if got, want := len(frames), 3; got != want {
		t.Fatalf("frame count = %d, want %d", got, want)
	}
	wantDurations := []time.Duration{100 * time.Millisecond, 500 * time.Millisecond, 400 * time.Millisecond}
	var total time.Duration
	var bounds string
	for i, frame := range frames {
		const dataPrefix = "data:image/png;base64,"
		if !strings.HasPrefix(frame.Src, dataPrefix) {
			t.Fatalf("frame %d src = %.40q, want embedded PNG data URL", i, frame.Src)
		}
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(frame.Src, dataPrefix))
		if err != nil {
			t.Fatalf("frame %d base64: %v", i, err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("frame %d PNG: %v", i, err)
		}
		gotBounds := img.Bounds().String()
		if i == 0 {
			bounds = gotBounds
		} else if gotBounds != bounds {
			t.Errorf("frame %d bounds = %s, want %s", i, gotBounds, bounds)
		}
		if got := time.Duration(frame.DurationNS); got != wantDurations[i] {
			t.Errorf("frame %d duration = %v, want %v", i, got, wantDurations[i])
		}
		total += time.Duration(frame.DurationNS)
	}
	if total != time.Second {
		t.Errorf("total duration = %v, want 1s", total)
	}
}

func TestExportHTMLDoesNotResolveFFmpeg(t *testing.T) {
	bundle := writeTestBundle(t)
	out := filepath.Join(t.TempDir(), "out.HTML")
	if err := Export(bundle, out, Options{FFmpeg: "/definitely/not/ffmpeg"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("HTML output missing: %v", err)
	}
}

func TestHTMLSinkCommitsAtomically(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "replay.html")
	if err := os.WriteFile(out, []byte("previous artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := newHTMLSink(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.add(solidFrame(color.RGBA{R: 255, A: 255}, 4, 4), time.Second); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != "previous artifact" {
		t.Fatalf("destination changed before close: %q", before)
	}
	if err := s.close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(after, []byte("<!doctype html>")) {
		t.Fatalf("committed output does not look like HTML: %.40q", after)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, ".replay.html.*.tmp")); err != nil || len(matches) != 0 {
		t.Fatalf("temporary outputs after commit = %v, err = %v", matches, err)
	}
}

func TestHTMLSinkAbortPreservesDestination(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "replay.html")
	if err := os.WriteFile(out, []byte("previous artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := newHTMLSink(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.add(solidFrame(color.RGBA{B: 255, A: 255}, 4, 4), time.Second); err != nil {
		t.Fatal(err)
	}
	s.abort()
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "previous artifact" {
		t.Fatalf("destination after abort = %q", got)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, ".replay.html.*.tmp")); err != nil || len(matches) != 0 {
		t.Fatalf("temporary outputs after abort = %v, err = %v", matches, err)
	}
}

func TestHTMLSinkCloseFailurePreservesDestinationAndCleansTemp(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "replay.html")
	if err := os.WriteFile(out, []byte("previous artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := newHTMLSink(out)
	if err != nil {
		t.Fatal(err)
	}
	// Closing the underlying file forces the buffered flush in close to fail,
	// simulating an I/O error after frames have already been generated.
	if err := s.file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.add(solidFrame(color.RGBA{G: 255, A: 255}, 4, 4), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := s.close(); err == nil {
		t.Fatal("close succeeded with a closed output file")
	}
	s.abort()
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "previous artifact" {
		t.Fatalf("destination after failed close = %q", got)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, ".replay.html.*.tmp")); err != nil || len(matches) != 0 {
		t.Fatalf("temporary outputs after failed close = %v, err = %v", matches, err)
	}
}

func TestExportHTMLCorruptBundleLeavesNoArtifact(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "corrupt.twee")
	out := filepath.Join(dir, "out.html")
	if err := os.WriteFile(bundle, []byte("not a zip bundle"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Export(bundle, out, Options{}); err == nil {
		t.Fatal("Export succeeded for corrupt input")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("output exists after failed export: %v", err)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, ".out.html.*.tmp")); err != nil || len(matches) != 0 {
		t.Fatalf("temporary outputs after failed export = %v, err = %v", matches, err)
	}
}

func TestExportHTMLCorruptBundlePreservesExistingArtifact(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "corrupt.twee")
	out := filepath.Join(dir, "out.html")
	if err := os.WriteFile(bundle, []byte("not a zip bundle"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("known good artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Export(bundle, out, Options{}); err == nil {
		t.Fatal("Export succeeded for corrupt input")
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "known good artifact" {
		t.Fatalf("existing output after failed export = %q", got)
	}
}
