package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	if a.Text == "" && !a.Regex {
		return nil, invalidArgumentMessage("text or regex required")
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
			return nil, waitErrDetails(t, err, map[string]any{"target": map[string]any{"text": a.Text, "regex": true}})
		}
		return nil, nil
	}
	if err := t.WaitForText(a.Text, engine.WithTimeout(to)); err != nil {
		return nil, waitErrDetails(t, err, map[string]any{"target": map[string]any{"text": a.Text, "regex": false}})
	}
	return nil, nil
}

func handleWaitNoText(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeArgs[rpc.WaitNoTextArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	if a.Text == "" {
		return nil, invalidArgumentMessage("text or regex required")
	}
	to, err := parseTimeout(a.Timeout, t.DefaultTimeout())
	if err != nil {
		return nil, invalidArgument(err)
	}
	if err := t.WaitForNoText(a.Text, engine.WithTimeout(to)); err != nil {
		return nil, waitErrDetails(t, err, map[string]any{"target": map[string]any{"absent_text": a.Text}})
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
	if len(a.Exclude) > 0 {
		exclude := make([]engine.Rect, len(a.Exclude))
		for i, rect := range a.Exclude {
			if rect.X < 0 || rect.Y < 0 || rect.W <= 0 || rect.H <= 0 {
				return nil, invalidArgumentMessage("exclude x/y must be >= 0 and w/h must be > 0")
			}
			exclude[i] = engine.Rect{X: rect.X, Y: rect.Y, W: rect.W, H: rect.H}
		}
		if err := t.WaitForStableScreenExcept(quiet, exclude, engine.WithTimeout(to)); err != nil {
			return nil, waitErrDetails(t, err, map[string]any{"target": map[string]any{"quiet": quiet.String(), "exclude": a.Exclude}})
		}
		return nil, nil
	}
	if err := t.WaitForStableScreen(quiet, engine.WithTimeout(to)); err != nil {
		return nil, waitErrDetails(t, err, map[string]any{"target": map[string]any{"quiet": quiet.String()}})
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
		return nil, waitErrDetails(t, err, map[string]any{"target": map[string]int{"x": a.X, "y": a.Y}})
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
		return nil, waitErrDetails(t, err, map[string]any{"target": map[string]string{"exit_within": to.String()}})
	}
	// The child is gone; make its artifacts durable before answering so
	// the caller can rely on the trace bundle existing when this returns.
	if err := FinalizeArtifacts(t); err != nil {
		return nil, ioFailure(fmt.Errorf("finalize trace: %w", err))
	}
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

// waitErrDetails builds a wait failure from its retained engine diagnostic.
// details.cause remains the short root cause while message contains the human
// readable dump. SESSION_ENDED distinguishes pump closure from timeout.
func waitErrDetails(t *engine.Term, err error, extra map[string]any) *rpc.Error {
	cause := err.Error()
	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		cause = unwrapped.Error()
	}
	diagnostic, ok := engine.DiagnosticFromError(err)
	if !ok {
		diagnostic = t.CaptureDiagnostic()
	}
	detailValues := commonFailureDetails(diagnostic)
	detailValues["cause"] = cause
	maps.Copy(detailValues, extra)
	details, _ := json.Marshal(detailValues)
	code := rpc.CodeTimeout
	if engine.IsSessionEnded(err) {
		code = rpc.CodeSessionEnded
	}
	return &rpc.Error{Code: code, Message: err.Error(), Details: details}
}
