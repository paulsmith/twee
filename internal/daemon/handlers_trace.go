package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/render"
	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	optionalRegistrations = append(optionalRegistrations, func(d *Dispatcher) {
		d.Register(rpc.OpTraceStart, handleTraceStart)
		d.Register(rpc.OpTraceStop, handleTraceStop)
	})
}

// FinalizeArtifacts makes the session's artifacts durable: it drains the
// remaining output, captures a final screenshot into the active trace (if
// any), and finalizes the trace and recording. Idempotent. Used at daemon
// teardown so a trace survives the child exiting before `trace stop`.
func FinalizeArtifacts(t *engine.Term) error {
	t.DrainOutput()
	if png, err := renderScreenshot(t); err == nil {
		t.TraceAddScreenshot(png)
	}
	return t.FinalizeArtifacts()
}

func handleTraceStart(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.TraceStartArgs
	if err := json.Unmarshal(raw, &a); err != nil && len(raw) > 0 {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	if a.Out == "" {
		dir, err := os.MkdirTemp("", "twee-trace-")
		if err != nil {
			return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
		}
		a.Out = filepath.Join(dir, fmt.Sprintf("session-%d.twee", time.Now().UnixNano()))
	}
	if err := t.EnableTrace(a.Out); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
	}
	// Capture initial screenshot.
	if png, err := renderScreenshot(t); err == nil {
		t.TraceAddScreenshot(png)
	}
	return map[string]string{"out": a.Out}, nil
}

func handleTraceStop(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	path := t.TracePath()
	// Capture final screenshot before closing the trace.
	if png, err := renderScreenshot(t); err == nil {
		t.TraceAddScreenshot(png)
	}
	if err := t.DisableTrace(); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
	}
	return map[string]string{"path": path}, nil
}

func renderScreenshot(t *engine.Term) ([]byte, error) {
	snap := t.Snapshot()
	img, err := render.Render(snap, render.Default())
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := render.EncodePNG(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
