package daemon

import (
	"encoding/json"
	"time"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
)

func (d *Dispatcher) registerLifecycle() {
	d.Register(rpc.OpStatus, handleStatus)
	d.Register(rpc.OpStop, d.handleStop)
}

func handleStatus(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	snap := t.Snapshot()
	data := rpc.StatusData{
		Cmd:       t.Cmd(),
		Cols:      snap.Cols,
		Rows:      snap.Rows,
		StartedAt: t.StartedAt(),
		Running:   true,
	}
	select {
	case <-t.ExitedCh():
		data.Running = false
		c := t.ExitCode()
		data.ExitCode = &c
	default:
	}
	return data, nil
}

func (d *Dispatcher) handleStop(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeOptionalArgs[rpc.StopArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	if a.Token != nil && *a.Token == "" {
		return nil, invalidArgumentMessage("stop token must not be empty")
	}
	if a.Token != nil && *a.Token != d.stopToken {
		return nil, &rpc.Error{
			Code:    rpc.CodeFailedPrecondition,
			Message: "stop token does not own the current session generation",
		}
	}
	grace := engine.DefaultCloseGrace
	if a.Grace != "" {
		d, err := time.ParseDuration(a.Grace)
		if err != nil {
			return nil, invalidArgument(err)
		}
		if d < 0 {
			return nil, invalidArgumentMessage("grace must not be negative")
		}
		grace = d
	}
	// Recorded before CloseWithGrace so the session's tombstone (written
	// at teardown, once this handler has returned) can tell an explicit
	// "twee stop" apart from the child exiting on its own.
	t.MarkStopRequested()
	if a.SuppressTombstone {
		t.SuppressTombstone()
	}
	if err := t.CloseWithGrace(grace); err != nil {
		return nil, ioFailure(err)
	}
	return map[string]any{"trace_path": t.FinalizedTracePath()}, nil
}
