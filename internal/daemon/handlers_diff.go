package daemon

import (
	"encoding/json"
	"os"

	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/rpc"
	"github.com/paulsmith/research/twee/internal/snapshot"
)

func init() {
	optionalRegistrations = append(optionalRegistrations, func(d *Dispatcher) {
		d.Register(rpc.OpDiff, handleDiff)
	})
}

func handleDiff(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.DiffArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	if a.Against == "" {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: "against is required"}
	}
	expectedBytes, err := os.ReadFile(a.Against)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
	}
	expected := string(expectedBytes)
	current := t.VisibleText()
	d := snapshot.UnifiedDiff(expected, current)
	return rpc.DiffData{
		Equal:    expected == current,
		Unified:  d,
		Current:  current,
		Expected: expected,
	}, nil
}
