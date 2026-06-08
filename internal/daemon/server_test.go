package daemon

import (
	"context"
	"net"
	"os"
	"path/filepath"
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
