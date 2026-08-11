package daemon

import (
	"encoding/base64"
	"time"

	"github.com/paulsmith/twee/internal/engine"
)

func commonFailureDetails(diagnostic engine.Diagnostic) map[string]any {
	screen, screenBytes, screenTruncated := engine.DiagnosticScreenText(diagnostic.Snapshot)
	details := map[string]any{
		"captured_at": diagnostic.CapturedAt.UTC().Format(time.RFC3339Nano),
		"generation":  diagnostic.Generation,
		"viewport": map[string]int{
			"cols": diagnostic.Snapshot.Cols,
			"rows": diagnostic.Snapshot.Rows,
		},
		"last_screen":       screen,
		"last_screen_bytes": screenBytes,
		"cursor":            cursorData(diagnostic.Snapshot.Cursor),
		"recent_events":     diagnosticEvents(diagnostic),
	}
	if screenTruncated {
		details["last_screen_truncated"] = true
	}
	if diagnostic.PresentationErr == nil && diagnostic.MouseErr == nil {
		details["modes"] = modeData(diagnostic)
	} else {
		modeErrors := map[string]string{}
		if diagnostic.PresentationErr != nil {
			modeErrors["presentation"] = diagnostic.PresentationErr.Error()
		}
		if diagnostic.MouseErr != nil {
			modeErrors["mouse"] = diagnostic.MouseErr.Error()
		}
		details["modes_error"] = modeErrors
	}
	if diagnostic.TracePath != "" {
		details["trace"] = map[string]string{
			"path":   diagnostic.TracePath,
			"status": diagnostic.TraceStatus,
		}
	}
	return details
}

func diagnosticEvents(diagnostic engine.Diagnostic) []map[string]any {
	inputs := diagnostic.RecentInputs
	if len(inputs) > 16 {
		inputs = inputs[len(inputs)-16:]
	}
	events := make([]map[string]any, 0, len(inputs)+1)
	for _, input := range inputs {
		description := engine.DiagnosticInputDescription(input)
		truncated := input.Kind != "type" && input.Kind != "paste" && len(input.Desc) > 512
		eventType := "input"
		if input.Kind == "resize" {
			eventType = "resize"
		}
		event := map[string]any{
			"type":        eventType,
			"kind":        input.Kind,
			"at":          input.When.UTC().Format(time.RFC3339Nano),
			"description": description,
		}
		if input.Kind == "type" || input.Kind == "paste" {
			event["redacted"] = true
		}
		if truncated {
			event["truncated"] = true
		}
		events = append(events, event)
	}
	if len(diagnostic.RecentOutput) > 0 {
		output := diagnostic.RecentOutput
		truncated := false
		if len(output) > 4096 {
			output = output[len(output)-4096:]
			truncated = true
		}
		event := map[string]any{
			"type":         "output",
			"at":           diagnostic.CapturedAt.UTC().Format(time.RFC3339Nano),
			"bytes":        len(output),
			"bytes_base64": base64.StdEncoding.EncodeToString(output),
		}
		if truncated {
			event["truncated"] = true
		}
		events = append(events, event)
	}
	return events
}
