package daemon

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
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
	var man map[string]json.RawMessage
	if err := json.NewDecoder(mf).Decode(&man); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	_ = mf.Close()
	if _, ok := man["screenshots"]; ok {
		t.Fatal("manifest has screenshots key")
	}
	assertNoScreenshotEntries(t, &zr.Reader)
	if !traceHasInput(t, &zr.Reader, "x") {
		t.Fatal("trace events missing typed input")
	}
}

// TestTraceStartWhileActiveFails pins down that a second "trace start"
// while a trace is already active errors instead of silently finalizing
// the first trace and starting a new one over it.
func TestTraceStartWhileActiveFails(t *testing.T) {
	te := startTestTerm(t)
	sock, _ := startTestServer(t, te)
	firstPath := filepath.Join(t.TempDir(), "first.twee")
	secondPath := filepath.Join(t.TempDir(), "second.twee")

	resp := dialAndCall(t, sock, rpc.Request{
		ID:   "1",
		Op:   rpc.OpTraceStart,
		Args: mustJSON(t, rpc.TraceStartArgs{Out: firstPath}),
	})
	if !resp.OK {
		t.Fatalf("first trace_start: %+v", resp.Error)
	}

	resp = dialAndCall(t, sock, rpc.Request{
		ID:   "2",
		Op:   rpc.OpTraceStart,
		Args: mustJSON(t, rpc.TraceStartArgs{Out: secondPath}),
	})
	if resp.OK {
		t.Fatalf("second trace_start unexpectedly succeeded: %+v", resp.Data)
	}
	if resp.Error.Code != rpc.CodeAlreadyRunning {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, rpc.CodeAlreadyRunning)
	}
	var details struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(resp.Error.Details, &details); err != nil {
		t.Fatalf("decode details: %v", err)
	}
	if details.Path != firstPath {
		t.Fatalf("details.path = %q, want active trace path %q", details.Path, firstPath)
	}

	// The first trace must still be the active one, untouched.
	if got := te.TracePath(); got != firstPath {
		t.Fatalf("TracePath after rejected second start = %q, want %q", got, firstPath)
	}

	resp = dialAndCall(t, sock, rpc.Request{ID: "3", Op: rpc.OpTraceStop})
	if !resp.OK {
		t.Fatalf("trace_stop: %+v", resp.Error)
	}
	if _, err := zip.OpenReader(firstPath); err != nil {
		t.Fatalf("first trace bundle missing/invalid: %v", err)
	}
	if _, err := os.Stat(secondPath); !os.IsNotExist(err) {
		t.Fatalf("second trace path should not exist: stat err = %v", err)
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

func TestWaitExitReportsTracePath(t *testing.T) {
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd:  []string{"/bin/sh", "-c", "printf 'hello\\r\\n'; sleep 0.3"},
		Cols: 40, Rows: 5,
	})
	if err != nil {
		t.Fatalf("engine.Start: %v", err)
	}
	t.Cleanup(func() { _ = te.Close() })
	if err := te.WaitForText("hello"); err != nil {
		t.Fatalf("WaitForText: %v", err)
	}
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

	resp = dialAndCall(t, sock, rpc.Request{ID: "2", Op: rpc.OpWaitExit})
	if !resp.OK {
		t.Fatalf("wait_exit: %+v", resp.Error)
	}
	var exitData struct {
		ExitCode  int    `json:"exit_code"`
		TracePath string `json:"trace_path"`
	}
	if err := json.Unmarshal(resp.Data, &exitData); err != nil {
		t.Fatalf("decode wait_exit data: %v", err)
	}
	if exitData.TracePath != tracePath {
		t.Fatalf("wait_exit trace_path = %q, want %q", exitData.TracePath, tracePath)
	}
	if _, err := zip.OpenReader(tracePath); err != nil {
		t.Fatalf("bundle not durable when wait_exit answered: %v", err)
	}

	// A trace stop arriving after auto-finalization reports the bundle
	// instead of silently re-answering with a stale or empty path.
	resp = dialAndCall(t, sock, rpc.Request{ID: "3", Op: rpc.OpTraceStop})
	if !resp.OK {
		t.Fatalf("trace_stop after finalize: %+v", resp.Error)
	}
	var stopData struct {
		Path             string `json:"path"`
		AlreadyFinalized bool   `json:"already_finalized"`
	}
	if err := json.Unmarshal(resp.Data, &stopData); err != nil {
		t.Fatalf("decode trace_stop data: %v", err)
	}
	if stopData.Path != tracePath || !stopData.AlreadyFinalized {
		t.Fatalf("trace_stop after finalize = %+v, want path %q and already_finalized", stopData, tracePath)
	}
}

func TestWaitExitReportsTraceFinalizationFailure(t *testing.T) {
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd: []string{"/bin/sh", "-c", "exit 0"}, Cols: 40, Rows: 5,
	})
	if err != nil {
		t.Fatalf("engine.Start: %v", err)
	}
	t.Cleanup(func() { _ = te.Close() })
	tracePath := filepath.Join(t.TempDir(), "blocked.twee")
	if err := te.EnableTrace(tracePath); err != nil {
		t.Fatalf("EnableTrace: %v", err)
	}
	if err := os.Mkdir(tracePath, 0o700); err != nil {
		t.Fatalf("Mkdir blocking output: %v", err)
	}

	_, rpcErr := handleWaitExit(te, mustJSON(t, rpc.WaitExitArgs{Timeout: "2s"}))
	if rpcErr == nil || rpcErr.Code != rpc.CodeIO {
		t.Fatalf("handleWaitExit error = %+v, want IO finalization failure", rpcErr)
	}
	if !strings.Contains(rpcErr.Message, "finalize trace") || !strings.Contains(rpcErr.Message, "rename") {
		t.Fatalf("handleWaitExit message = %q, want finalization cause", rpcErr.Message)
	}
	if got := te.FinalizedTracePath(); got != "" {
		t.Fatalf("FinalizedTracePath = %q after failure", got)
	}
	if err := te.ArtifactError(); err == nil || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("ArtifactError = %v, want retained finalization failure", err)
	}
}

func TestTraceStopNoTrace(t *testing.T) {
	te := startTestTerm(t)
	sock, _ := startTestServer(t, te)
	resp := dialAndCall(t, sock, rpc.Request{ID: "1", Op: rpc.OpTraceStop})
	if resp.OK {
		t.Fatalf("trace_stop with no trace succeeded: %+v", resp)
	}
	if resp.Error.Code != rpc.CodeNotFound {
		t.Fatalf("trace_stop error code = %q, want %q", resp.Error.Code, rpc.CodeNotFound)
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

func assertNoScreenshotEntries(t *testing.T, zr *zip.Reader) {
	t.Helper()
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "screenshots/") {
			t.Fatalf("unexpected screenshot entry %q", f.Name)
		}
	}
}
