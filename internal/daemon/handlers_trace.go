package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	optionalRegistrations = append(optionalRegistrations, func(d *Dispatcher) {
		d.Register(rpc.OpTraceStart, handleTraceStart)
		d.Register(rpc.OpTraceStop, handleTraceStop)
	})
}

// FinalizeArtifacts makes the session's artifacts durable: it drains the
// remaining output and finalizes the trace. Idempotent. Used at daemon teardown
// so a trace survives the child exiting before `trace stop`.
func FinalizeArtifacts(t *engine.Term) error {
	t.DrainOutput()
	return t.FinalizeArtifacts()
}

func handleTraceStart(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeOptionalArgs[rpc.TraceStartArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	if a.Out == "" {
		dir, err := os.MkdirTemp("", "twee-trace-")
		if err != nil {
			return nil, ioFailure(err)
		}
		a.Out = filepath.Join(dir, fmt.Sprintf("session-%d.twee", time.Now().UnixNano()))
	}
	if err := t.EnableTrace(a.Out); err != nil {
		return nil, ioFailure(err)
	}
	return map[string]string{"out": a.Out}, nil
}

func handleTraceStop(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	path := t.TracePath()
	if path == "" {
		// No active trace: either it was already finalized (report the
		// bundle rather than failing) or there was never one to stop.
		if p := t.FinalizedTracePath(); p != "" {
			return map[string]any{"path": p, "already_finalized": true}, nil
		}
		return nil, notFoundMessage("no active trace")
	}
	if err := t.DisableTrace(); err != nil {
		return nil, ioFailure(err)
	}
	return map[string]string{"path": path}, nil
}
