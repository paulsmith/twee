package daemon

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
)

// startTestTerm spawns /bin/sh that prints a known string and waits
// until killed. Returns the running engine.Term.
func startTestTerm(t *testing.T) *engine.Term {
	t.Helper()
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd:  []string{"/bin/sh", "-c", "printf 'hello\\r\\n'; sleep 30"},
		Cols: 40, Rows: 5,
	})
	if err != nil {
		t.Fatalf("engine.Start: %v", err)
	}
	if err := te.WaitForText("hello"); err != nil {
		_ = te.Close()
		t.Fatalf("WaitForText: %v", err)
	}
	t.Cleanup(func() { _ = te.Close() })
	return te
}

func TestServerReturnsJSONWhenResponseExceedsLimit(t *testing.T) {
	te := startTestTerm(t)
	server := NewServer(te)
	server.d.Register("huge", func(_ *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
		return strings.Repeat("x", rpc.MaxMessageBytes), nil
	})
	client, daemon := net.Pipe()
	defer client.Close()
	go server.handleConn(daemon)
	const requestID = "oversized-response"
	if err := rpc.WriteMessage(client, rpc.Request{ID: requestID, Op: "huge"}); err != nil {
		t.Fatal(err)
	}
	var response rpc.Response
	if err := rpc.ReadMessage(client, &response); err != nil {
		t.Fatalf("read fallback response: %v", err)
	}
	if response.ID != requestID || response.OK || response.Error == nil || response.Error.Code != rpc.CodeInternal {
		t.Fatalf("response = %+v", response)
	}
}

func startTestServer(t *testing.T, te *engine.Term) (string, *Server) {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := NewServer(te)
	go func() { _ = s.Serve(context.Background(), l) }()
	t.Cleanup(func() {
		s.Stop()
		_ = l.Close()
		_ = os.Remove(sock)
	})
	return sock, s
}

func dialAndCall(t *testing.T, sock string, req rpc.Request) rpc.Response {
	t.Helper()
	c, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if err := rpc.WriteMessage(c, req); err != nil {
		t.Fatalf("write: %v", err)
	}
	var resp rpc.Response
	if err := rpc.ReadMessage(c, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	return resp
}

func TestStatusOp(t *testing.T) {
	te := startTestTerm(t)
	sock, _ := startTestServer(t, te)

	resp := dialAndCall(t, sock, rpc.Request{ID: "1", Op: rpc.OpStatus})
	if !resp.OK {
		t.Fatalf("status: %+v", resp.Error)
	}
}

func TestUnknownOp(t *testing.T) {
	te := startTestTerm(t)
	sock, _ := startTestServer(t, te)

	resp := dialAndCall(t, sock, rpc.Request{ID: "1", Op: "no-such-op"})
	if resp.OK {
		t.Fatalf("expected error response")
	}
	if resp.Error.Code != rpc.CodeInvalidArgument {
		t.Errorf("error code = %q, want %q", resp.Error.Code, rpc.CodeInvalidArgument)
	}
}

func TestStopTokenMustMatchCurrentGeneration(t *testing.T) {
	te := startTestTerm(t)
	d := NewDispatcher(te, WithStopToken("current-generation"))
	staleToken := "stale-generation"
	raw, err := json.Marshal(rpc.StopArgs{Token: &staleToken})
	if err != nil {
		t.Fatal(err)
	}
	resp := d.Dispatch(rpc.Request{ID: "1", Op: rpc.OpStop, Args: raw})
	if resp.OK || resp.Error == nil || resp.Error.Code != rpc.CodeFailedPrecondition {
		t.Fatalf("mismatched stop response = %+v, want FAILED_PRECONDITION", resp)
	}
	select {
	case <-te.ExitedCh():
		t.Fatal("mismatched token stopped the child")
	default:
	}
}

func TestExplicitEmptyStopTokenIsInvalid(t *testing.T) {
	te := startTestTerm(t)
	d := NewDispatcher(te, WithStopToken("current-generation"))
	empty := ""
	raw, err := json.Marshal(rpc.StopArgs{Token: &empty})
	if err != nil {
		t.Fatal(err)
	}
	resp := d.Dispatch(rpc.Request{ID: "1", Op: rpc.OpStop, Args: raw})
	if resp.OK || resp.Error == nil || resp.Error.Code != rpc.CodeInvalidArgument {
		t.Fatalf("empty token response = %+v, want INVALID_ARGUMENT", resp)
	}
	select {
	case <-te.ExitedCh():
		t.Fatal("empty token stopped the child")
	default:
	}
}
