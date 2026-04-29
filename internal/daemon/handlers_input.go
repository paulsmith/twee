package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"

	"github.com/paulsmith/research/twee/internal/engine"
	tinput "github.com/paulsmith/research/twee/internal/input"
	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	optionalRegistrations = append(optionalRegistrations, func(d *Dispatcher) {
		d.Register(rpc.OpType, handleType)
		d.Register(rpc.OpKey, handleKey)
		d.Register(rpc.OpPaste, handlePaste)
		d.Register(rpc.OpSignal, handleSignal)
		d.Register(rpc.OpResize, handleResize)
	})
}

func handleType(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.TypeArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	if err := t.Type(a.Text); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
	}
	return nil, nil
}

func handleKey(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.KeyArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	k, err := tinput.Parse(a.Key)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	if err := t.Key(k); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
	}
	return nil, nil
}

func handlePaste(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.PasteArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	if err := t.Paste(a.Text); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
	}
	return nil, nil
}

func handleSignal(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.SignalArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	sig, err := parseSignal(a.Name)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	if err := t.Signal(sig); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
	}
	return nil, nil
}

func handleResize(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.ResizeArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	if a.Cols <= 0 || a.Rows <= 0 {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: "cols and rows must be > 0"}
	}
	if err := t.Resize(a.Cols, a.Rows); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
	}
	return nil, nil
}

func parseSignal(name string) (os.Signal, error) {
	switch name {
	case "SIGTERM", "TERM":
		return syscall.SIGTERM, nil
	case "SIGKILL", "KILL":
		return syscall.SIGKILL, nil
	case "SIGINT", "INT":
		return syscall.SIGINT, nil
	case "SIGHUP", "HUP":
		return syscall.SIGHUP, nil
	case "SIGWINCH", "WINCH":
		return syscall.SIGWINCH, nil
	case "SIGUSR1", "USR1":
		return syscall.SIGUSR1, nil
	case "SIGUSR2", "USR2":
		return syscall.SIGUSR2, nil
	}
	return nil, fmt.Errorf("unknown signal %q", name)
}
