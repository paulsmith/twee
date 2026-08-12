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

type htmlMarkerRecord struct {
	PositionMS float64 `json:"position_ms"`
	TMS        int64   `json:"t_ms"`
	EventIndex int     `json:"event_index"`
	Label      string  `json:"label"`
	Src        string  `json:"src"`
}

func readHTMLJSON[T any](t *testing.T, page []byte, id string) []T {
	t.Helper()
	marker := []byte(`<script type="application/json" id="` + id + `">`)
	start := bytes.Index(page, marker)
	if start < 0 {
		t.Fatalf("page is missing embedded %s data", id)
	}
	start += len(marker)
	end := bytes.Index(page[start:], []byte(`</script>`))
	if end < 0 {
		t.Fatalf("embedded %s data has no closing script tag", id)
	}
	var values []T
	if err := json.Unmarshal(page[start:start+end], &values); err != nil {
		t.Fatalf("decode embedded %s: %v", id, err)
	}
	return values
}

func readHTMLFrames(t *testing.T, page []byte) []htmlFrameRecord {
	return readHTMLJSON[htmlFrameRecord](t, page, "twee-frames")
}

func readHTMLMarkers(t *testing.T, page []byte) []htmlMarkerRecord {
	return readHTMLJSON[htmlMarkerRecord](t, page, "twee-markers")
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
		`type="range"`,
		`id="speed-value"`,
		`#speed-value { display: inline-block; width: 5ch`,
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
	if markers := readHTMLMarkers(t, page); len(markers) != 0 {
		t.Fatalf("markers = %+v, want none in base fixture", markers)
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

func TestExportHTMLEmbedsOrderedMarkers(t *testing.T) {
	bundle := writeCastBundle(t, []string{
		castRecord(0, "output", "hello", ""),
		`{"t_ms":100,"type":"marker","label":"first"}`,
		`{"t_ms":100,"type":"marker","label":"second"}`,
		castRecord(200, "output", "world", ""),
	})
	out := filepath.Join(t.TempDir(), "out.html")
	if err := Export(bundle, out, Options{}); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	markers := readHTMLMarkers(t, page)
	if len(markers) != 2 || markers[0].Label != "first" || markers[1].Label != "second" || markers[0].PositionMS != 100 || markers[1].PositionMS != 100 || markers[0].EventIndex != 1 || markers[1].EventIndex != 2 {
		t.Fatalf("markers = %+v", markers)
	}
}

func TestExportHTMLClampsTailMarkersToPlaybackDuration(t *testing.T) {
	for _, test := range []struct {
		name     string
		opts     Options
		wantTime time.Duration
	}{
		{name: "trailing cap", wantTime: 4 * time.Second},
		{name: "max idle", opts: Options{MaxIdle: 2 * time.Second}, wantTime: 3 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			bundle := writeCastBundle(t, []string{
				castRecord(0, "output", "A", ""),
				castRecord(1000, "output", "B", ""),
				`{"t_ms":10000,"type":"marker","label":"tail"}`,
			})
			out := filepath.Join(t.TempDir(), "out.html")
			if err := Export(bundle, out, test.opts); err != nil {
				t.Fatal(err)
			}
			page, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			frames := readHTMLFrames(t, page)
			var duration time.Duration
			for _, frame := range frames {
				duration += time.Duration(frame.DurationNS)
			}
			if duration != test.wantTime {
				t.Fatalf("playback duration = %v, want %v", duration, test.wantTime)
			}
			markers := readHTMLMarkers(t, page)
			if len(markers) != 1 || markers[0].PositionMS != float64(duration)/float64(time.Millisecond) {
				t.Fatalf("tail markers = %+v, playback duration %v", markers, duration)
			}
		})
	}
}

func TestExportHTMLEmbedsExactMarkerCheckpointsAtEqualTimestamp(t *testing.T) {
	bundle := writeCastBundle(t, []string{
		castRecord(100, "output", "A", ""),
		`{"t_ms":100,"type":"marker","label":"first"}`,
		castRecord(100, "output", "B", ""),
		`{"t_ms":100,"type":"marker","label":"second"}`,
	})
	out := filepath.Join(t.TempDir(), "out.html")
	if err := Export(bundle, out, Options{}); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	markers := readHTMLMarkers(t, page)
	if len(markers) != 2 || markers[0].Src == "" || markers[1].Src == "" {
		t.Fatalf("markers = %+v", markers)
	}
	if markers[0].Src == markers[1].Src {
		t.Fatal("equal-timestamp markers embedded the same terminal checkpoint")
	}
	frames := readHTMLFrames(t, page)
	if len(frames) != 2 {
		t.Fatalf("visual frames = %d, want unchanged replay frame count 2", len(frames))
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
	_, after, ok := strings.Cut(htmlSuffix, executableScript)
	if !ok {
		t.Fatal("HTML suffix is missing executable script")
	}
	script := after
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
const markerData = [
  {position_ms: 100, t_ms: 100, event_index: 1, label: 'ready', src: 'marker-0'},
  {position_ms: 300, t_ms: 300, event_index: 3, label: 'first done', src: 'marker-1'},
  {position_ms: 300, t_ms: 300, event_index: 4, label: 'second done', src: 'marker-2'},
  {position_ms: 600, t_ms: 900, event_index: 5, label: 'tail', src: 'marker-3'},
];
const drawn = [];
function makeElement(id) {
  const element = {
    id,
    value: id === 'speed' ? '0' : '0',
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
for (const id of ['twee-frames', 'twee-markers', 'frame', 'play', 'timeline', 'time', 'speed', 'speed-value', 'restart', 'previous', 'next', 'previous-marker', 'next-marker', 'markers']) {
  elements[id] = makeElement(id);
}
elements['twee-frames'].textContent = JSON.stringify(frameData);
elements['twee-markers'].textContent = JSON.stringify(markerData);
elements.markers.children = [];
elements.markers.appendChild = option => elements.markers.children.push(option);
elements.frame.getContext = () => ({drawImage(picture) { drawn.push(picture.src); }});
const documentListeners = {};
globalThis.document = {
  getElementById(id) { return elements[id]; },
  createElement(id) { return makeElement(id); },
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
function key(value, target = {}) {
  let prevented = false;
  documentListeners.keydown({key: value, target, preventDefault() { prevented = true; }});
  return prevented;
}
assert(elements.timeline.max === '600', 'total duration');
assert(elements.markers.children.length === 4, 'marker option count');
assert(elements.markers.children[0].textContent === '1. ready', 'first marker option');
assert(elements.markers.children[2].textContent === '3. second done', 'equal-position marker option order');
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
assert(key(']'), 'next marker shortcut prevents default');
assert(elements.timeline.value === '100' && elements.markers.value === '0', 'next marker reaches first');
assert(lastDrawn() === 'marker-0', 'first marker paints exact checkpoint');
elements.next.click();
assert(elements.timeline.value === '300' && lastDrawn() === 'frame-2', 'next frame is relative to marker frame');
elements.markers.value = '0';
elements.markers.dispatch('change');
elements.previous.click();
assert(elements.timeline.value === '0' && lastDrawn() === 'frame-0', 'previous frame is relative to marker frame');
elements.markers.value = '0';
elements.markers.dispatch('change');
elements['next-marker'].click();
assert(elements.timeline.value === '300' && elements.markers.value === '1', 'next marker reaches first equal-position marker');
assert(lastDrawn() === 'marker-1', 'first equal-position marker paints exact checkpoint');
assert(key(']'), 'next equal-position marker shortcut prevents default');
assert(elements.timeline.value === '300' && elements.markers.value === '2', 'next marker reaches later equal-position marker');
assert(lastDrawn() === 'marker-2', 'later equal-position marker paints distinct checkpoint');
assert(key('['), 'previous marker shortcut prevents default');
assert(elements.timeline.value === '300' && elements.markers.value === '1', 'previous marker traverses equal-position order');
elements['previous-marker'].click();
assert(elements.timeline.value === '100' && elements.markers.value === '0', 'previous marker reaches earlier position');
elements.markers.value = '2';
elements.markers.dispatch('change');
assert(elements.timeline.value === '300' && elements.markers.value === '2', 'marker selector seeks exact equal-position marker');
elements.markers.value = '3';
elements.markers.dispatch('change');
assert(elements.timeline.value === '600' && elements.markers.value === '3' && lastDrawn() === 'marker-3', 'tail marker remains selected at playback end');
elements.restart.click();
elements.play.click();
assert(elements.play.textContent === 'Pause', 'play transition');
let tick = animationCallback;
animationCallback = null;
now = 50;
tick(now);
assert(elements.timeline.value === '50', 'one-speed playback');
assert(elements['speed-value'].textContent === '1×', 'initial speed label');
assert(key('+'), 'faster shortcut prevents default');
assert(elements.speed.value === '1' && elements['speed-value'].textContent === '2×', 'faster shortcut');
elements.speed.value = '2';
elements.speed.dispatch('input');
assert(elements['speed-value'].textContent === '4×', 'slider speed update');
assert(!key('+', elements.speed), 'focused speed slider keeps native controls');
elements.speed.value = '1';
elements.speed.dispatch('input');
tick = animationCallback;
animationCallback = null;
now = 100;
tick(now);
assert(elements.timeline.value === '150' && lastDrawn() === 'frame-1', 'two-speed playback');
assert(key('-'), 'slower shortcut prevents default');
assert(elements.speed.value === '0' && elements['speed-value'].textContent === '1×', 'slower shortcut');
for (let i = 0; i < 8; i++) key('-');
assert(elements.speed.value === '-2' && elements['speed-value'].textContent === '0.25×', 'minimum speed');
for (let i = 0; i < 8; i++) key('+');
assert(elements.speed.value === '4' && elements['speed-value'].textContent === '16×', 'maximum speed');
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
