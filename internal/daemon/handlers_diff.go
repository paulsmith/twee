package daemon

import (
	"encoding/json"
	"os"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
	"github.com/paulsmith/twee/internal/snapshot"
)

func init() {
	optionalRegistrations = append(optionalRegistrations, func(d *Dispatcher) {
		d.Register(rpc.OpDiff, handleDiff)
	})
}

func handleDiff(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeArgs[rpc.DiffArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	if a.Against == "" {
		return nil, invalidArgumentMessage("against is required")
	}
	expectedBytes, err := os.ReadFile(a.Against)
	if err != nil {
		return nil, ioFailure(err)
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
