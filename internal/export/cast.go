package export

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/tracebundle"
)

// castResult reports information intentionally omitted from an asciicast.
type castResult struct {
	OmittedEvents int
}

// exportCast writes an asciicast v2 NDJSON file. Output is accumulated across
// consecutive trace events because a UTF-8 code point may span PTY reads.
func exportCast(path, outPath string, includeInput bool) (castResult, error) {
	bundle, validation, err := tracebundle.Validate(path)
	if err != nil {
		return castResult{}, fmt.Errorf("twee export: %w", err)
	}
	if !validation.Valid {
		return castResult{}, fmt.Errorf("twee export: invalid bundle: %s", strings.Join(validation.Issues, "; "))
	}

	stage, f, err := newStagedOutput(outPath, ".cast")
	if err != nil {
		return castResult{}, fmt.Errorf("twee export: %w", err)
	}
	defer stage.abort()

	enc := json.NewEncoder(f)
	if err := enc.Encode(struct {
		Version  int     `json:"version"`
		Width    int     `json:"width"`
		Height   int     `json:"height"`
		Duration float64 `json:"duration"`
	}{Version: 2, Width: bundle.Manifest.Cols, Height: bundle.Manifest.Rows, Duration: float64(bundle.LastTMS) / 1000}); err != nil {
		_ = f.Close()
		return castResult{}, fmt.Errorf("twee export: write cast header: %w", err)
	}

	var result castResult
	var output []byte
	outputTMS := int64(0)
	flushOutput := func(final bool) error {
		data, rest, invalid := castUTF8(output, final)
		if data != "" {
			if err := enc.Encode([]any{float64(outputTMS) / 1000, "o", data}); err != nil {
				return err
			}
		}
		if invalid {
			result.OmittedEvents++
		}
		output = rest
		if len(output) == 0 {
			outputTMS = 0
		}
		return nil
	}

	err = tracebundle.Stream(path, func(event tracebundle.Event) error {
		if event.Type == trace.EventTypeOutput {
			if len(output) == 0 {
				outputTMS = event.TMS
			}
			output = append(output, event.Bytes...)
			return flushOutput(false)
		}
		if err := flushOutput(true); err != nil {
			return err
		}
		kind, data, ok := castEvent(event, includeInput)
		if !ok {
			result.OmittedEvents++
			return nil
		}
		return enc.Encode([]any{float64(event.TMS) / 1000, kind, data})
	})
	if err != nil {
		_ = f.Close()
		return castResult{}, fmt.Errorf("twee export: write cast event: %w", err)
	}
	if err := flushOutput(true); err != nil {
		_ = f.Close()
		return castResult{}, fmt.Errorf("twee export: write cast event: %w", err)
	}
	if err := f.Close(); err != nil {
		return castResult{}, fmt.Errorf("twee export: close cast: %w", err)
	}
	if err := stage.commit(); err != nil {
		return castResult{}, fmt.Errorf("twee export: commit cast: %w", err)
	}
	return result, nil
}

// castUTF8 returns complete valid UTF-8, any incomplete suffix retained for a
// later output chunk, and whether it discarded malformed bytes.
func castUTF8(data []byte, final bool) (valid string, rest []byte, invalid bool) {
	var out []byte
	for len(data) != 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			if !final && !utf8.FullRune(data) {
				return string(out), data, invalid
			}
			invalid = true
			data = data[1:]
			continue
		}
		out = append(out, data[:size]...)
		data = data[size:]
	}
	return string(out), nil, invalid
}

func castEvent(event tracebundle.Event, includeInput bool) (kind, data string, ok bool) {
	switch event.Type {
	case trace.EventTypeResize:
		return "r", fmt.Sprintf("%dx%d", event.Cols, event.Rows), true
	case trace.EventTypeInput:
		if includeInput && (event.Kind == trace.InputKindType || event.Kind == trace.InputKindKey || event.Kind == trace.InputKindPaste) && utf8.Valid(event.Bytes) {
			return "i", string(event.Bytes), true
		}
	}
	return "", "", false
}
