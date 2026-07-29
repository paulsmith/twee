package daemon

import (
	"encoding/json"
	"errors"
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
	a, errResp := decodeArgs[rpc.WaitTextArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	to, err := parseTimeout(a.Timeout, t.DefaultTimeout())
	if err != nil {
		return nil, invalidArgument(err)
	}
	if a.Regex {
		// Multi-line mode: the pattern is matched against the whole
		// viewport joined by "\n" (see WaitForTextRegex), so without
		// (?m) a bare ^/$ would only anchor at the start/end of the
		// entire viewport rather than each line.
		re, err := regexp.Compile("(?m)" + a.Text)
		if err != nil {
			return nil, invalidArgument(err)
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
	a, errResp := decodeArgs[rpc.WaitNoTextArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	to, err := parseTimeout(a.Timeout, t.DefaultTimeout())
	if err != nil {
		return nil, invalidArgument(err)
	}
	if err := t.WaitForNoText(a.Text, engine.WithTimeout(to)); err != nil {
		return nil, waitErr(t, err)
	}
	return nil, nil
}

func handleWaitStable(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeOptionalArgs[rpc.WaitStableArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	quiet, err := parseTimeout(a.Quiet, t.StableQuietWindow())
	if err != nil {
		return nil, invalidArgument(err)
	}
	to, err := parseTimeout(a.Timeout, t.DefaultTimeout())
	if err != nil {
		return nil, invalidArgument(err)
	}
	if err := t.WaitForStableScreen(quiet, engine.WithTimeout(to)); err != nil {
		return nil, waitErr(t, err)
	}
	return nil, nil
}

func handleWaitCursor(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeArgs[rpc.WaitCursorArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	to, err := parseTimeout(a.Timeout, t.DefaultTimeout())
	if err != nil {
		return nil, invalidArgument(err)
	}
	if err := t.WaitForCursorAt(a.X, a.Y, engine.WithTimeout(to)); err != nil {
		return nil, waitErr(t, err)
	}
	return nil, nil
}

func handleWaitExit(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeOptionalArgs[rpc.WaitExitArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	const defaultExitTimeout = 30 * time.Second
	to := defaultExitTimeout
	if a.Timeout != "" {
		v, err := time.ParseDuration(a.Timeout)
		if err != nil {
			return nil, invalidArgument(err)
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
	return rpc.WaitExitData{ExitCode: code, TracePath: t.FinalizedTracePath()}, nil
}

func handleSleep(_ *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeArgs[rpc.SleepArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	d, err := time.ParseDuration(a.Duration)
	if err != nil {
		return nil, invalidArgument(err)
	}
	time.Sleep(d)
	return nil, nil
}

// waitErr builds the TIMEOUT envelope for a failed wait. Message carries
// the full diagnostic dump (see engine.Term.Diagnostic); details.cause
// is deliberately just the short root cause (e.g. "pump: timeout" or
// "pump: closed") rather than a second copy of that dump. Every
// WaitForXxx helper wraps its root cause with %w before appending the
// dump, so unwrapping once recovers it; wait exit's error isn't wrapped
// at all (it has no dump to begin with), so it falls back to the full
// (already short) message unchanged.
func waitErr(t *engine.Term, err error) *rpc.Error {
	cause := err.Error()
	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		cause = unwrapped.Error()
	}
	details, _ := json.Marshal(map[string]any{
		"cause":       cause,
		"last_screen": t.VisibleText(),
	})
	return &rpc.Error{Code: rpc.CodeTimeout, Message: err.Error(), Details: details}
}
