package daemon

import (
	"archive/zip"
	"bufio"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paulsmith/research/twee/internal/rpc"
)

func TestTraceStartStopOps(t *testing.T) {
	te := startTestTerm(t)
	sock, _ := startTestServer(t, te)
	tracePath := filepath.Join(t.TempDir(), "session.twee")

	resp := dialAndCall(t, sock, rpc.Request{
		ID:   "1",
		Op:   rpc.OpTraceStart,
		Args: mustJSON(t, rpc.TraceStartArgs{Out: tracePath}),
	})
	if !resp.OK {
		t.Fatalf("trace_start: %+v", resp.Error)
	}
	if got := te.TracePath(); got != tracePath {
		t.Fatalf("TracePath = %q, want %q", got, tracePath)
	}

	resp = dialAndCall(t, sock, rpc.Request{
		ID:   "2",
		Op:   rpc.OpType,
		Args: mustJSON(t, rpc.TypeArgs{Text: "x"}),
	})
	if !resp.OK {
		t.Fatalf("type: %+v", resp.Error)
	}

	resp = dialAndCall(t, sock, rpc.Request{ID: "3", Op: rpc.OpTraceStop})
	if !resp.OK {
		t.Fatalf("trace_stop: %+v", resp.Error)
	}
	if got := te.TracePath(); got != "" {
		t.Fatalf("TracePath after stop = %q, want empty", got)
	}

	zr, err := zip.OpenReader(tracePath)
	if err != nil {
		t.Fatalf("open trace zip: %v", err)
	}
	defer zr.Close()

	mf, err := zr.Open("manifest.json")
	if err != nil {
		t.Fatalf("manifest.json: %v", err)
	}
	var man struct {
		Screenshots []string `json:"screenshots"`
	}
	if err := json.NewDecoder(mf).Decode(&man); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	_ = mf.Close()
	if len(man.Screenshots) != 2 {
		t.Fatalf("screenshots = %v, want start and stop screenshots", man.Screenshots)
	}
	for _, path := range man.Screenshots {
		sf, err := zr.Open(path)
		if err != nil {
			t.Fatalf("screenshot %q: %v", path, err)
		}
		_ = sf.Close()
	}
	if !traceHasInput(t, &zr.Reader, "x") {
		t.Fatal("trace events missing typed input")
	}
}

func TestTraceStartInvalidPathFails(t *testing.T) {
	te := startTestTerm(t)
	sock, _ := startTestServer(t, te)
	tracePath := filepath.Join(t.TempDir(), "missing", "session.twee")

	resp := dialAndCall(t, sock, rpc.Request{
		ID:   "1",
		Op:   rpc.OpTraceStart,
		Args: mustJSON(t, rpc.TraceStartArgs{Out: tracePath}),
	})
	if resp.OK {
		t.Fatalf("trace_start unexpectedly succeeded")
	}
	if resp.Error.Code != rpc.CodeIO {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, rpc.CodeIO)
	}
	if got := te.TracePath(); got != "" {
		t.Fatalf("TracePath = %q, want empty", got)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func traceHasInput(t *testing.T, zr *zip.Reader, want string) bool {
	t.Helper()
	ef, err := zr.Open("events.jsonl")
	if err != nil {
		t.Fatalf("events.jsonl: %v", err)
	}
	defer ef.Close()
	sc := bufio.NewScanner(ef)
	for sc.Scan() {
		var ev struct {
			Type  string `json:"type"`
			Bytes string `json:"bytes_b64"`
		}
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if ev.Type != "input" {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(ev.Bytes)
		if err != nil {
			t.Fatalf("decode input bytes: %v", err)
		}
		if strings.Contains(string(b), want) {
			return true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan events: %v", err)
	}
	return false
}
