package bundle

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/paulsmith/twee/internal/tracepolicy"
)

// ValidateResult is what "twee bundle validate" reports for a bundle
// that was at least readable as a zip file (see Validate).
type ValidateResult struct {
	Valid  bool
	Events int
	Issues []string
}

// Validate checks path for zip integrity, a parseable manifest with a
// supported version, and events.jsonl lines that each parse as a known
// event type with non-decreasing timestamps.
//
// Unlike Inspect, Validate treats a bundle's content problems as Issues
// in the returned result rather than as errors — the whole point of
// "bundle validate" is to enumerate everything wrong with a bundle, not
// stop at the first problem. Validate only returns a non-nil error (a
// *LoadError with ErrIO) when it can't even read path as a file; once
// the bytes are in hand, every other problem (not a zip, missing
// manifest/events, corrupt JSON, bad version, corrupt event lines,
// unknown event types, out-of-order timestamps) becomes an Issue and
// ValidateResult.Valid is false.
func Validate(path string) (ValidateResult, error) {
	if _, err := os.Stat(path); err != nil {
		return ValidateResult{}, &LoadError{Kind: ErrIO, Err: err}
	}

	zr, err := zip.OpenReader(path)
	if err != nil {
		return ValidateResult{Issues: []string{"invalid zip: " + err.Error()}}, nil
	}
	defer zr.Close()

	var issues []string
	entries, structureIssues := checkArchive(&zr.Reader)
	issues = append(issues, structureIssues...)

	if f := entries["manifest.json"]; f != nil {
		body, rerr := readEntry(f)
		if rerr != nil {
			issues = append(issues, "manifest.json: "+rerr.Error())
		} else {
			var man struct {
				Version int `json:"version"`
			}
			if jerr := json.Unmarshal(body, &man); jerr != nil {
				issues = append(issues, "manifest.json: "+jerr.Error())
			} else if man.Version != 1 {
				issues = append(issues, fmt.Sprintf("unsupported bundle version %d", man.Version))
			}
		}
	}

	events := 0
	if f := entries["events.jsonl"]; f != nil {
		r, rerr := f.Open()
		if rerr != nil {
			issues = append(issues, "events.jsonl: "+rerr.Error())
		} else {
			limited := &io.LimitedReader{R: r, N: tracepolicy.MaxEventsBytes + 1}
			n, evIssues := validateEventLines(limited)
			if limited.N == 0 {
				evIssues = append(evIssues, fmt.Sprintf("events.jsonl: decompressed content exceeds %d bytes", tracepolicy.MaxEventsBytes))
			}
			if cerr := r.Close(); cerr != nil {
				evIssues = append(evIssues, "events.jsonl: "+cerr.Error())
			}
			events = n
			issues = append(issues, evIssues...)
		}
	}

	return ValidateResult{
		Valid:  len(issues) == 0,
		Events: events,
		Issues: issues,
	}, nil
}

// knownEventTypes are the event "type" values twee itself ever writes
// (see internal/trace.Trace's WriteOutput/WriteInput/WriteResize/
// WriteExit); anything else in events.jsonl is an unknown-type issue.
func knownEventType(t string) bool {
	switch t {
	case "output", "input", "resize", "exit":
		return true
	default:
		return false
	}
}

// validateEventLines parses body as newline-delimited JSON event
// records, returning the count of lines that at least parsed as JSON
// and a list of every issue found (malformed lines, unknown event
// types, and timestamps that go backwards) rather than stopping at the
// first one.
func validateEventLines(body io.Reader) (int, []string) {
	return validateEventLinesWithLimit(body, tracepolicy.MaxEventCount)
}

func validateEventLinesWithLimit(body io.Reader, maxEvents int) (int, []string) {
	var issues []string
	count := 0
	haveLast := false
	var lastTMS int64

	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), tracepolicy.MaxEventLineBytes)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var raw struct {
			TMS  int64  `json:"t_ms"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &raw); err != nil {
			issues = append(issues, fmt.Sprintf("events.jsonl line %d: %v", lineNo, err))
			continue
		}
		if count >= maxEvents {
			issues = append(issues, fmt.Sprintf("events.jsonl: event count exceeds %d", maxEvents))
			break
		}
		count++
		if !knownEventType(raw.Type) {
			issues = append(issues, fmt.Sprintf("events.jsonl line %d: unknown event type %q", lineNo, raw.Type))
		}
		if haveLast && raw.TMS < lastTMS {
			issues = append(issues, fmt.Sprintf("events.jsonl line %d: timestamp %d before previous %d", lineNo, raw.TMS, lastTMS))
		}
		lastTMS = raw.TMS
		haveLast = true
	}
	if err := sc.Err(); err != nil {
		issues = append(issues, "events.jsonl: "+err.Error())
	}
	return count, issues
}
