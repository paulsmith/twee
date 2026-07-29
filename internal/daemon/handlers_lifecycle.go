package daemon

import (
	"encoding/json"
	"time"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
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

func handleStop(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeOptionalArgs[rpc.StopArgs](raw)
	if errResp != nil {
		return nil, errResp
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
	if err := t.CloseWithGrace(grace); err != nil {
		return nil, ioFailure(err)
	}
	return nil, nil
}
