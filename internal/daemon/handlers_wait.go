package daemon

import (
	"encoding/json"
	"regexp"
	"time"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	optionalRegistrations = append(optionalRegistrations, func(d *Dispatcher) {
		d.Register(rpc.OpWaitText, handleWaitText)
		d.Register(rpc.OpWaitNoText, handleWaitNoText)
		d.Register(rpc.OpWaitStable, handleWaitStable)
		d.Register(rpc.OpWaitCursor, handleWaitCursor)
		d.Register(rpc.OpWaitExit, handleWaitExit)
		d.Register(rpc.OpSleep, handleSleep)
	})
}

func parseTimeout(s string, fallback time.Duration) (time.Duration, error) {
	if s == "" {
		return fallback, nil
	}
	return time.ParseDuration(s)
}

func handleWaitText(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.WaitTextArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	to, err := parseTimeout(a.Timeout, t.DefaultTimeout())
	if err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	if a.Regex {
		re, err := regexp.Compile(a.Text)
		if err != nil {
			return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
		}
		if err := t.WaitForTextRegex(re, engine.WithTimeout(to)); err != nil {
			return nil, waitErr(t, err)
		}
		return nil, nil
	}
	if err := t.WaitForText(a.Text, engine.WithTimeout(to)); err != nil {
		return nil, waitErr(t, err)
	}
	return nil, nil
}

func handleWaitNoText(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.WaitNoTextArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	to, err := parseTimeout(a.Timeout, t.DefaultTimeout())
	if err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	if err := t.WaitForNoText(a.Text, engine.WithTimeout(to)); err != nil {
		return nil, waitErr(t, err)
	}
	return nil, nil
}

func handleWaitStable(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.WaitStableArgs
	if err := json.Unmarshal(raw, &a); err != nil && len(raw) > 0 {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	quiet, err := parseTimeout(a.Quiet, t.StableQuietWindow())
	if err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	to, err := parseTimeout(a.Timeout, t.DefaultTimeout())
	if err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	if err := t.WaitForStableScreen(quiet, engine.WithTimeout(to)); err != nil {
		return nil, waitErr(t, err)
	}
	return nil, nil
}

func handleWaitCursor(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.WaitCursorArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	to, err := parseTimeout(a.Timeout, t.DefaultTimeout())
	if err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	if err := t.WaitForCursorAt(a.X, a.Y, engine.WithTimeout(to)); err != nil {
		return nil, waitErr(t, err)
	}
	return nil, nil
}

func handleWaitExit(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.WaitExitArgs
	if err := json.Unmarshal(raw, &a); err != nil && len(raw) > 0 {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	const defaultExitTimeout = 30 * time.Second
	to := defaultExitTimeout
	if a.Timeout != "" {
		v, err := time.ParseDuration(a.Timeout)
		if err != nil {
			return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
		}
		to = v
	}
	code, err := t.WaitForExit(engine.WithTimeout(to))
	if err != nil {
		return nil, waitErr(t, err)
	}
	// The child is gone; make its artifacts durable before answering so
	// the caller can rely on the trace bundle existing when this returns.
	_ = FinalizeArtifacts(t)
	return rpc.WaitExitData{ExitCode: code}, nil
}

func handleSleep(_ *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.SleepArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	d, err := time.ParseDuration(a.Duration)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	time.Sleep(d)
	return nil, nil
}

func waitErr(t *engine.Term, err error) *rpc.Error {
	details, _ := json.Marshal(map[string]any{
		"cause":       err.Error(),
		"last_screen": t.VisibleText(),
	})
	return &rpc.Error{Code: rpc.CodeTimeout, Message: err.Error(), Details: details}
}
