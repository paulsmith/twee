package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	optionalRegistrations = append(optionalRegistrations, func(d *Dispatcher) {
		d.Register(rpc.OpRecordStart, handleRecordStart)
		d.Register(rpc.OpRecordStop, handleRecordStop)
	})
}

func handleRecordStart(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.RecordStartArgs
	if err := json.Unmarshal(raw, &a); err != nil && len(raw) > 0 {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	if a.Out == "" {
		dir, err := os.MkdirTemp("", "twee-record-")
		if err != nil {
			return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
		}
		a.Out = filepath.Join(dir, fmt.Sprintf("session-%d.jsonl", time.Now().UnixNano()))
	}
	if err := t.EnableRecording(a.Out); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
	}
	return map[string]string{"out": a.Out}, nil
}

func handleRecordStop(t *engine.Term, _ json.RawMessage) (any, *rpc.Error) {
	path := t.RecordPath()
	if err := t.DisableRecording(); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
	}
	return map[string]string{"path": path}, nil
}
