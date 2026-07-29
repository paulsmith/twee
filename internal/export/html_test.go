package export

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
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
	if err := os.WriteFile(out, []byte("previous artifact"), 0o600); err != nil {
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
	assertFileMode(t, out, 0o600)
	if matches, err := filepath.Glob(filepath.Join(dir, ".replay.html.*.tmp")); err != nil || len(matches) != 0 {
		t.Fatalf("temporary outputs after commit = %v, err = %v", matches, err)
	}
}

func TestHTMLNewArtifactHonorsRestrictiveUmask(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	if os.Getenv("TWEE_TEST_RESTRICTIVE_UMASK") == "1" {
		syscall.Umask(0o077)
		out := os.Getenv("TWEE_TEST_OUTPUT")
		s, err := newHTMLSink(out)
		if err != nil {
			t.Fatal(err)
		}
		defer s.abort()
		if err := s.add(solidFrame(color.RGBA{G: 255, A: 255}, 4, 4), time.Second); err != nil {
			t.Fatal(err)
		}
		if err := s.close(); err != nil {
			t.Fatal(err)
		}
		return
	}
	out := filepath.Join(t.TempDir(), "new.html")
	cmd := exec.Command(os.Args[0], "-test.run=^TestHTMLNewArtifactHonorsRestrictiveUmask$")
	cmd.Env = append(os.Environ(),
		"TWEE_TEST_RESTRICTIVE_UMASK=1",
		"TWEE_TEST_OUTPUT="+out,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("umask helper: %v\n%s", err, output)
	}
	assertFileMode(t, out, 0o600)
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

func TestHTMLViewerBehavior(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	const executableScript = "<script>\n"
	start := strings.Index(htmlSuffix, executableScript)
	if start < 0 {
		t.Fatal("HTML suffix is missing executable script")
	}
	script := htmlSuffix[start+len(executableScript):]
	end := strings.Index(script, "\n</script>")
	if end < 0 {
		t.Fatal("HTML suffix is missing executable script terminator")
	}
	script = script[:end]

	cmd := exec.Command(node)
	cmd.Stdin = strings.NewReader(nodeViewerHarness + "\n" + script + "\n" + nodeViewerAssertions)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("viewer behavior: %v\n%s", err, output)
	}
}

const nodeViewerHarness = `
'use strict';
const frameData = [
  {src: 'frame-0', duration_ns: 100000000},
  {src: 'frame-1', duration_ns: 200000000},
  {src: 'frame-2', duration_ns: 300000000},
];
const drawn = [];
function makeElement(id) {
  const element = {
    id,
    value: id === 'speed' ? '1' : '0',
    textContent: id === 'play' ? 'Play' : '',
    listeners: {},
    attributes: {},
    addEventListener(type, listener) { this.listeners[type] = listener; },
    dispatch(type, event = {}) { this.listeners[type](event); },
    click() { this.dispatch('click'); },
    setAttribute(name, value) { this.attributes[name] = value; },
  };
  return element;
}
const elements = {};
for (const id of ['twee-frames', 'frame', 'play', 'timeline', 'time', 'speed', 'restart', 'previous', 'next']) {
  elements[id] = makeElement(id);
}
elements['twee-frames'].textContent = JSON.stringify(frameData);
elements.frame.getContext = () => ({drawImage(picture) { drawn.push(picture.src); }});
const documentListeners = {};
globalThis.document = {
  getElementById(id) { return elements[id]; },
  addEventListener(type, listener) { documentListeners[type] = listener; },
};
globalThis.Image = class {
  constructor() {
    this.complete = false;
    this.naturalWidth = 80;
    this.naturalHeight = 24;
    this.listeners = {};
  }
  addEventListener(type, listener) { this.listeners[type] = listener; }
  set src(value) {
    this._src = value;
    this.complete = true;
    if (this.listeners.load) this.listeners.load();
  }
  get src() { return this._src; }
};
let now = 0;
let animationCallback = null;
globalThis.performance = {now() { return now; }};
globalThis.requestAnimationFrame = callback => { animationCallback = callback; return 1; };
globalThis.cancelAnimationFrame = () => { animationCallback = null; };
`

const nodeViewerAssertions = `
function assert(condition, message) {
  if (!condition) throw new Error(message);
}
function lastDrawn() { return drawn[drawn.length - 1]; }
function scrub(value) {
  elements.timeline.value = String(value);
  elements.timeline.dispatch('input');
}
assert(elements.timeline.max === '600', 'total duration');
assert(elements.time.textContent === '0:00.000 / 0:00.600', 'initial clock');
assert(lastDrawn() === 'frame-0', 'initial frame');
scrub(99.999);
assert(lastDrawn() === 'frame-0', 'frame before first boundary');
scrub(100);
assert(lastDrawn() === 'frame-1', 'frame at first boundary');
scrub(299.999);
assert(lastDrawn() === 'frame-1', 'frame before second boundary');
scrub(300);
assert(lastDrawn() === 'frame-2', 'frame at second boundary');
scrub(600);
assert(lastDrawn() === 'frame-2', 'frame at end');
assert(elements.time.textContent === '0:00.600 / 0:00.600', 'clock at end');
elements.previous.click();
assert(elements.timeline.value === '100' && lastDrawn() === 'frame-1', 'previous frame');
elements.next.click();
assert(elements.timeline.value === '300' && lastDrawn() === 'frame-2', 'next frame');
elements.restart.click();
assert(elements.timeline.value === '0' && lastDrawn() === 'frame-0', 'restart');
elements.play.click();
assert(elements.play.textContent === 'Pause', 'play transition');
let tick = animationCallback;
animationCallback = null;
now = 50;
tick(now);
assert(elements.timeline.value === '50', 'one-speed playback');
elements.speed.value = '2';
elements.speed.dispatch('change');
tick = animationCallback;
animationCallback = null;
now = 100;
tick(now);
assert(elements.timeline.value === '150' && lastDrawn() === 'frame-1', 'two-speed playback');
elements.play.click();
assert(elements.play.textContent === 'Play' && animationCallback === null, 'pause transition');
scrub(600);
elements.play.click();
assert(elements.timeline.value === '0', 'play from end restarts');
tick = animationCallback;
animationCallback = null;
now = 500;
tick(now);
assert(elements.timeline.value === '600', 'playback clamps at end');
assert(elements.play.textContent === 'Play', 'playback stops at end');
`

func FuzzExportHTMLCorruptBundle(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("not a zip bundle"))
	f.Add([]byte("PK\x03\x04truncated"))
	f.Add(htmlFuzzBundle(`{"version":1,"cols":4,"rows":2}`, "not json\n"))
	f.Add(htmlFuzzBundle(`{"version":1,"cols":4,"rows":2`, ""))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 2<<20 {
			t.Skip("keep individual fuzz cases bounded")
		}
		dir := t.TempDir()
		bundle := filepath.Join(dir, "input.twee")
		out := filepath.Join(dir, "out.html")
		const previous = "known good artifact"
		if err := os.WriteFile(bundle, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, []byte(previous), 0o600); err != nil {
			t.Fatal(err)
		}
		err := Export(bundle, out, Options{})
		got, readErr := os.ReadFile(out)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err != nil {
			if string(got) != previous {
				t.Fatalf("failed export replaced destination with %.40q", got)
			}
		} else if !bytes.HasPrefix(got, []byte("<!doctype html>")) {
			t.Fatalf("successful export produced invalid artifact: %.40q", got)
		}
		matches, globErr := filepath.Glob(filepath.Join(dir, ".out.html.*.tmp"))
		if globErr != nil || len(matches) != 0 {
			t.Fatalf("temporary outputs = %v, err = %v", matches, globErr)
		}
	})
}

func htmlFuzzBundle(manifest, events string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mw, _ := zw.Create("manifest.json")
	_, _ = mw.Write([]byte(manifest))
	ew, _ := zw.Create("events.jsonl")
	_, _ = ew.Write([]byte(events))
	_ = zw.Close()
	return buf.Bytes()
}
