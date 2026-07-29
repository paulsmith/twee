package play

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"golang.org/x/term"
)

type fakeTermOps struct {
	isTTY          bool
	width, height  int
	raws, restores int
}

func (f *fakeTermOps) IsTerminal(int) bool { return f.isTTY }
func (f *fakeTermOps) GetSize(int) (int, int, error) {
	return f.width, f.height, nil
}
func (f *fakeTermOps) MakeRaw(int) (*term.State, error) {
	f.raws++
	return nil, nil
}
func (f *fakeTermOps) Restore(int, *term.State) error {
	f.restores++
	return nil
}

func TestPreflightRejectsNonTTY(t *testing.T) {
	err := preflightBundle(Bundle{MaxCols: 10, MaxRows: 3}, preflightOptions{
		Term: &fakeTermOps{isTTY: false, width: 80, height: 24},
		In:   strings.NewReader(""),
		Out:  io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "non-tty") {
		t.Fatalf("error = %v, want non-tty", err)
	}
}

func TestPreflightRejectsSmallTerminal(t *testing.T) {
	err := preflightBundle(Bundle{MaxCols: 100, MaxRows: 40}, preflightOptions{
		Term: &fakeTermOps{isTTY: true, width: 80, height: 24},
		In:   strings.NewReader(""),
		Out:  io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "terminal is 80x24; trace needs at least 100x42") {
		t.Fatalf("error = %v, want size mismatch", err)
	}
}

func TestQueryKittyAcceptsOKReply(t *testing.T) {
	termOps := &fakeTermOps{isTTY: true}
	var out bytes.Buffer
	err := queryKitty(preflightOptions{
		Term:    termOps,
		In:      strings.NewReader("\x1b_Gi=31;OK\x1b\\"),
		Out:     &out,
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("queryKitty: %v", err)
	}
	if out.String() != kittyQuery {
		t.Fatalf("query = %q, want %q", out.String(), kittyQuery)
	}
	if termOps.raws != 1 || termOps.restores != 1 {
		t.Fatalf("raw/restores = %d/%d, want 1/1", termOps.raws, termOps.restores)
	}
}

func TestQueryKittyRejectsGarbledReply(t *testing.T) {
	err := queryKitty(preflightOptions{
		Term:    &fakeTermOps{isTTY: true},
		In:      strings.NewReader("\x1b_Gi=31;NOPE\x1b\\"),
		Out:     io.Discard,
		Timeout: time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "kitty graphics protocol not detected") {
		t.Fatalf("error = %v, want kitty diagnostic", err)
	}
}

func TestQueryKittyTimeout(t *testing.T) {
	r := &blockingReader{ch: make(chan struct{})}
	defer close(r.ch)
	err := queryKitty(preflightOptions{
		Term:    &fakeTermOps{isTTY: true},
		In:      r,
		Out:     io.Discard,
		Timeout: time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "kitty graphics protocol not detected") {
		t.Fatalf("error = %v, want kitty diagnostic", err)
	}
}

func TestSelectBackendAutoPreference(t *testing.T) {
	tests := []struct {
		name  string
		reply string
		want  Backend
	}{
		{
			name:  "kitty before all others",
			reply: "noise\x1b_Gi=31;OK\x1b\\\x1b]1337;Capabilities=FSx\x1b\\\x1b[?1;4c",
			want:  BackendKitty,
		},
		{
			name:  "iterm2 before sixel",
			reply: "noise\x1b]1337;Capabilities=FSx\x1b\\more\x1b[?1;4c",
			want:  BackendITerm2,
		},
		{
			name:  "sixel primary DA",
			reply: "noise\x1b[?1;2;4;6c",
			want:  BackendSixel,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			termOps := &fakeTermOps{isTTY: true}
			var query bytes.Buffer
			got, err := selectBackend(preflightOptions{
				Term: termOps, In: strings.NewReader(tt.reply), Out: &query,
				Timeout: time.Second, Pixels: displayPixels{Width: 800, Height: 600},
			}, BackendAuto)
			if err != nil {
				t.Fatalf("selectBackend: %v", err)
			}
			if got != tt.want {
				t.Fatalf("backend = %q, want %q", got, tt.want)
			}
			if query.String() != kittyQuery+iterm2Query+primaryDAQuery {
				t.Fatalf("query = %q", query.String())
			}
			if termOps.raws != 1 || termOps.restores != 1 {
				t.Fatalf("raw/restores = %d/%d, want 1/1", termOps.raws, termOps.restores)
			}
		})
	}
}

func TestSelectBackendExplicitDiagnostics(t *testing.T) {
	tests := []struct {
		backend Backend
		want    string
	}{
		{BackendKitty, "kitty backend unavailable"},
		{BackendITerm2, "iterm2 backend unavailable"},
		{BackendSixel, "sixel backend unavailable"},
	}
	for _, tt := range tests {
		t.Run(string(tt.backend), func(t *testing.T) {
			_, err := selectBackend(preflightOptions{
				Term: &fakeTermOps{isTTY: true}, In: strings.NewReader(""), Out: io.Discard,
				Timeout: time.Second, Pixels: displayPixels{Width: 800, Height: 600},
			}, tt.backend)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestSelectBackendUsesPublishedEnvironmentFallbacks(t *testing.T) {
	for _, tt := range []struct {
		name, key, value string
		backend          Backend
	}{
		{"iterm FILE", "TERM_FEATURES", "TF", BackendITerm2},
		{"sixel", "TERM_FEATURES", "TSx", BackendSixel},
		{"old iterm", "TERM_PROGRAM", "iTerm.app", BackendITerm2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			backend, err := selectBackend(preflightOptions{
				Term: &fakeTermOps{isTTY: true}, In: strings.NewReader(""), Out: io.Discard,
				Timeout: time.Second, Pixels: displayPixels{Width: 800, Height: 600},
				Getenv: func(key string) string {
					if key == tt.key {
						return tt.value
					}
					return ""
				},
			}, BackendAuto)
			if err != nil || backend != tt.backend {
				t.Fatalf("backend/error = %q/%v, want %q/nil", backend, err, tt.backend)
			}
		})
	}
}

func TestSelectBackendAutoSkipsSixelWithoutPixelGeometry(t *testing.T) {
	_, err := selectBackend(preflightOptions{
		Term: &fakeTermOps{isTTY: true},
		In:   strings.NewReader("\x1b[?1;4c"), Out: io.Discard,
		Timeout: time.Second,
	}, BackendAuto)
	if err == nil {
		t.Fatal("selectBackend succeeded without pixel geometry")
	}
	for _, want := range []string{
		"kitty: protocol not detected", "iterm2: FILE capability not detected",
		"sixel: terminal pixel geometry unavailable",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestSelectBackendExplicitSixelChecksGeometryBeforeProbe(t *testing.T) {
	termOps := &fakeTermOps{isTTY: true}
	_, err := selectBackend(preflightOptions{
		Term: termOps, In: strings.NewReader("\x1b[?1;4c"), Out: io.Discard,
	}, BackendSixel)
	if err == nil || !strings.Contains(err.Error(), "pixel geometry") {
		t.Fatalf("error = %v, want pixel-geometry diagnostic", err)
	}
	if termOps.raws != 0 {
		t.Fatalf("raw calls = %d, want validation before probe", termOps.raws)
	}
}

func TestSelectBackendRejectsMultiplexerBeforeProbe(t *testing.T) {
	termOps := &fakeTermOps{isTTY: true}
	_, err := selectBackend(preflightOptions{
		Term: termOps, In: strings.NewReader(""), Out: io.Discard,
		Getenv: func(key string) string {
			if key == "TMUX" {
				return "/tmp/tmux"
			}
			return ""
		},
	}, BackendAuto)
	if err == nil || !strings.Contains(err.Error(), "direct terminal") {
		t.Fatalf("error = %v, want direct-terminal diagnostic", err)
	}
	if termOps.raws != 0 {
		t.Fatalf("raw calls = %d, want 0", termOps.raws)
	}
}

func TestCapabilityReplyParsingNoiseAndTerminators(t *testing.T) {
	replies := capabilityReplies([]byte("junk\x1b]1337;Capabilities=TF\x1b\\noise" +
		"\x1b]1337;Capabilities=TSx\ajunk"))
	if got, want := strings.Join(replies, ","), "TF,TSx"; got != want {
		t.Fatalf("replies = %q, want %q", got, want)
	}
	if !parseFeatureString(replies[0])["F"] || !parseFeatureString(replies[1])["Sx"] {
		t.Fatalf("features not parsed from %#v", replies)
	}
	if parseFeatureString("TF.invalid")["Sx"] {
		t.Fatal("parsed feature after invalid suffix")
	}
}

func TestCapabilityRepliesConsecutiveSTSequences(t *testing.T) {
	replies := capabilityReplies([]byte("\x1b]1337;Capabilities=F\x1b\\" +
		"\x1b]1337;Capabilities=Sx\x1b\\"))
	if got, want := strings.Join(replies, ","), "F,Sx"; got != want {
		t.Fatalf("replies = %q, want %q", got, want)
	}
}

func TestParseBackendSupportRejectsPartialAndNoisyLookalikes(t *testing.T) {
	for _, reply := range []string{
		"\x1b_Gi=31;OK",                       // missing ST
		"\x1b]1337;Capabilities=F",            // missing terminator
		"\x1b[?1;40c",                         // 40 is not parameter 4
		"noise Gi=31;OK Capabilities=F ?1;4c", // missing controls
	} {
		if got := parseBackendSupport([]byte(reply)); got != (backendSupport{}) {
			t.Fatalf("parseBackendSupport(%q) = %+v, want no support", reply, got)
		}
	}
}

func TestSelectBackendAutoTimeout(t *testing.T) {
	r := &blockingReader{ch: make(chan struct{})}
	defer close(r.ch)
	_, err := selectBackend(preflightOptions{
		Term: &fakeTermOps{isTTY: true}, In: r, Out: io.Discard,
		Timeout: time.Millisecond, Pixels: displayPixels{Width: 800, Height: 600},
	}, BackendAuto)
	if err == nil || !strings.Contains(err.Error(), "no usable graphics backend") {
		t.Fatalf("error = %v, want auto backend diagnostic", err)
	}
}

func TestSelectBackendKeepsCapabilityReplyWhenDAFenceTimesOut(t *testing.T) {
	r := &blockingAfterReader{
		data: []byte("noise\x1b]1337;Capabilities=FSx\x1b\\"),
		ch:   make(chan struct{}),
	}
	defer close(r.ch)
	backend, err := selectBackend(preflightOptions{
		Term: &fakeTermOps{isTTY: true}, In: r, Out: io.Discard,
		Timeout: time.Millisecond, Pixels: displayPixels{Width: 800, Height: 600},
	}, BackendAuto)
	if err != nil {
		t.Fatalf("selectBackend: %v", err)
	}
	if backend != BackendITerm2 {
		t.Fatalf("backend = %q, want iterm2", backend)
	}
}

func TestReadProbeStreamStopsAtPrimaryDAFence(t *testing.T) {
	r := &countingReader{data: []byte("noise\x1b[?1;2cignored")}
	reply, err := readProbeStream(r, 4096, BackendAuto)
	if err != nil {
		t.Fatalf("readProbeStream: %v", err)
	}
	if got, want := string(reply), "noise\x1b[?1;2c"; got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
	if r.read != len(reply) {
		t.Fatalf("read bytes = %d, want early stop at %d", r.read, len(reply))
	}
}

func TestReadProbeStreamBoundsNoise(t *testing.T) {
	_, err := readProbeStream(strings.NewReader(strings.Repeat("x", 65)), 64, BackendAuto)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("error = %v, want bounded-read error", err)
	}
}

type countingReader struct {
	data []byte
	read int
}

func FuzzBackendReplyParsers(f *testing.F) {
	for _, seed := range [][]byte{
		{},
		[]byte("\x1b_Gi=31;OK\x1b\\"),
		[]byte("noise\x1b]1337;Capabilities=FSx\x1b\\\x1b[?1;4c"),
		[]byte(strings.Repeat("x", maxProbeReplySize+1)),
		[]byte("\x1b]1337;Capabilities="),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, reply []byte) {
		_ = parseBackendSupport(reply)
		_ = capabilityReplies(reply)
		_ = primaryDAHasSixel(reply)
		_ = hasPrimaryDAReply(reply)
		for _, backend := range []Backend{BackendAuto, BackendKitty, BackendITerm2, BackendSixel} {
			_ = probeRepliesComplete(reply, backend)
		}
	})
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.read == len(r.data) {
		return 0, io.EOF
	}
	p[0] = r.data[r.read]
	r.read++
	return 1, nil
}

type blockingReader struct {
	ch chan struct{}
}

func (r *blockingReader) Read([]byte) (int, error) {
	<-r.ch
	return 0, io.EOF
}

type blockingAfterReader struct {
	data []byte
	ch   chan struct{}
}

func (r *blockingAfterReader) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	<-r.ch
	return 0, io.EOF
}
