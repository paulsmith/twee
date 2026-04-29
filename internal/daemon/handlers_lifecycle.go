package daemon

import (
	"encoding/json"

	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/rpc"
)

func (d *Dispatcher) registerLifecycle() {
	d.Register(rpc.OpStatus, handleStatus)
	d.Register(rpc.OpStop, handleStop)
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

func handleStop(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	if err := t.Close(); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
	}
	return nil, nil
}
