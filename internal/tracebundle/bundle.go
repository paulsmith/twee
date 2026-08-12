// Package tracebundle opens and decodes .twee trace bundles.
package tracebundle

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/paulsmith/twee/internal/termios"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/tracearchive"
	"github.com/paulsmith/twee/internal/tracepolicy"
)

// Bundle is the decoded contents of a .twee trace bundle.
type Bundle struct {
	Manifest trace.Manifest
	Events   []Event
	MaxCols  int
	MaxRows  int
	LastTMS  int64
}

// Validation reports every independently detectable content issue found while
// opening and decoding a bundle. Events counts records whose common event
// header parsed, including records with another typed-field or payload issue.
type Validation struct {
	Valid  bool
	Events int
	Issues []string
}

// Event is one decoded events.jsonl record.
type Event struct {
	TMS   int64
	Type  trace.EventType
	Bytes []byte
	Kind  trace.InputKind
	Key   string
	Cols  int
	Rows  int
	Code  int
	Mouse *trace.MouseInput
}

// TraceTime returns the event's timestamp as a duration from session start.
func (e Event) TraceTime() time.Duration {
	return time.Duration(e.TMS) * time.Millisecond
}

// Open opens and fully validates a .twee zip bundle, returning a decoded
// bundle only when no content issues were found.
func Open(path string) (Bundle, error) {
	bundle, validation, err := OpenValidated(path)
	if err != nil {
		return Bundle{}, err
	}
	if !validation.Valid {
		return Bundle{}, fmt.Errorf("invalid bundle: %s", strings.Join(validation.Issues, "; "))
	}
	return bundle, nil
}

// OpenValidated opens path once, exhaustively validates the archive and fully
// decodes every event through the same read path. Filesystem failures are
// returned as errors. Malformed readable content is returned in Validation;
// Bundle is non-zero only when Validation.Valid is true.
func OpenValidated(path string) (Bundle, Validation, error) {
	return openValidated(path, os.Open, true)
}

// Validate fully validates path without retaining its event records. Its bundle
// metadata includes the manifest, replay dimensions, and final event timestamp.
func Validate(path string) (Bundle, Validation, error) {
	return openValidated(path, os.Open, false)
}

func openValidated(path string, openFile func(string) (*os.File, error), collectEvents bool) (Bundle, Validation, error) {
	f, err := openFile(path)
	if err != nil {
		return Bundle{}, Validation{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	fi, err := f.Stat()
	if err != nil {
		return Bundle{}, Validation{}, fmt.Errorf("stat %s: %w", path, err)
	}
	if fi.IsDir() {
		return Bundle{}, Validation{}, fmt.Errorf("open %s: is a directory", path)
	}

	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		if errors.Is(err, zip.ErrFormat) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return invalid([]string{"invalid zip: " + err.Error()}, 0)
		}
		return Bundle{}, Validation{}, fmt.Errorf("read %s: %w", path, err)
	}

	entries, issues := tracearchive.Check(zr)
	var man trace.Manifest
	manifestValid := false
	if manifestFile := entries["manifest.json"]; manifestFile != nil {
		manifestBody, readErr := tracearchive.Read(manifestFile)
		if readErr != nil {
			issues = append(issues, "manifest.json: "+readErr.Error())
		} else if decodeErr := json.Unmarshal(manifestBody, &man); decodeErr != nil {
			issues = append(issues, "manifest.json: "+decodeErr.Error())
		} else if man.Version != 1 {
			issues = append(issues, fmt.Sprintf("unsupported bundle version %d", man.Version))
		} else {
			manifestValid = true
			if sizeIssue := terminalSizeIssue(man.Cols, man.Rows); sizeIssue != "" {
				issues = append(issues, "manifest.json: "+sizeIssue)
			}
			issues = append(issues, childPTYTermiosIssues(man.ChildPTYTermios)...)
		}
	}

	var network *tracearchive.NetworkMetadata
	if manifestValid {
		network = networkMetadata(man.Network)
		issues = append(issues, tracearchive.CheckNetworkCapture(network, entries[tracepolicy.NetworkCaptureStream])...)
	} else if stream := entries[tracepolicy.NetworkCaptureStream]; stream != nil {
		// Manifest defects prevent consistency checks, but PCAP integrity is
		// still independent and can be checked without interpreting metadata.
		if _, pcapErr := tracearchive.ValidatePCAP(stream); pcapErr != nil {
			issues = append(issues, tracepolicy.NetworkCaptureStream+": "+pcapErr.Error())
		}
	}

	var events []Event
	eventCount := 0
	lastEventTMS := int64(0)
	eventMaxCols, eventMaxRows := 0, 0
	if eventsFile := entries["events.jsonl"]; eventsFile != nil {
		r, openErr := eventsFile.Open()
		if openErr != nil {
			issues = append(issues, "events.jsonl: "+openErr.Error())
		} else {
			limited := &io.LimitedReader{R: r, N: tracepolicy.MaxEventsBytes + 1}
			decodedEvents, count, lastTMS, maxEventCols, maxEventRows, eventIssues := inspectEvents(limited, collectEvents)
			events = decodedEvents
			eventCount = count
			if maxEventCols > eventMaxCols {
				eventMaxCols = maxEventCols
			}
			if maxEventRows > eventMaxRows {
				eventMaxRows = maxEventRows
			}
			if lastTMS > 0 {
				lastEventTMS = lastTMS
			}
			issues = append(issues, eventIssues...)
			if limited.N == 0 {
				issues = append(issues, fmt.Sprintf("events.jsonl: decompressed content exceeds %d bytes", tracepolicy.MaxEventsBytes))
			}
			if closeErr := r.Close(); closeErr != nil {
				issues = append(issues, "events.jsonl: "+closeErr.Error())
			}
		}
	}

	if len(issues) != 0 {
		return invalid(issues, eventCount)
	}

	maxCols, maxRows := man.Cols, man.Rows
	if eventMaxCols > maxCols {
		maxCols = eventMaxCols
	}
	if eventMaxRows > maxRows {
		maxRows = eventMaxRows
	}
	if collectEvents {
		for _, ev := range events {
			if ev.Type == trace.EventTypeResize {
				if ev.Cols > maxCols {
					maxCols = ev.Cols
				}
				if ev.Rows > maxRows {
					maxRows = ev.Rows
				}
			}
		}
	}
	return Bundle{Manifest: man, Events: events, MaxCols: maxCols, MaxRows: maxRows, LastTMS: lastEventTMS}, Validation{Valid: true, Events: eventCount}, nil
}

func invalid(issues []string, events int) (Bundle, Validation, error) {
	return Bundle{}, Validation{Events: events, Issues: issues}, nil
}

func networkMetadata(capture *trace.NetworkCapture) *tracearchive.NetworkMetadata {
	if capture == nil {
		return nil
	}
	return &tracearchive.NetworkMetadata{
		Format: capture.Format, Stream: capture.Stream, GVisorVersion: capture.GVisorVersion,
		ByteLimit: capture.ByteLimit, CapturedBytes: capture.CapturedBytes, PacketCount: capture.PacketCount,
		Truncated: capture.Truncated, Status: capture.Status,
	}
}

func decodeEvents(r io.Reader) ([]Event, error) {
	return decodeEventsWithLimits(r, tracepolicy.MaxEventCount, tracepolicy.MaxDecodedPayloadBytes)
}

func decodeEventsWithLimits(r io.Reader, maxEvents, maxDecodedPayload int) ([]Event, error) {
	events, _, _, _, _, issues := inspectEventsWithLimits(r, maxEvents, maxDecodedPayload, true, nil)
	if len(issues) != 0 {
		return nil, fmt.Errorf("%s", strings.Join(issues, "; "))
	}
	return events, nil
}

func inspectEvents(r io.Reader, collect bool) ([]Event, int, int64, int, int, []string) {
	return inspectEventsWithLimits(r, tracepolicy.MaxEventCount, tracepolicy.MaxDecodedPayloadBytes, collect, nil)
}

func inspectEventsWithLimits(r io.Reader, maxEvents, maxDecodedPayload int, collect bool, visit func(Event) error) ([]Event, int, int64, int, int, []string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), tracepolicy.MaxEventLineBytes)

	var events []Event
	var issues []string
	lineNo := 0
	count := 0
	haveLast := false
	var lastTMS int64
	decodedBytes := 0
	maxCols, maxRows := 0, 0
	payloadLimitReported := false
	for sc.Scan() {
		lineNo++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var header struct {
			TMS  int64           `json:"t_ms"`
			Type trace.EventType `json:"type"`
		}
		if err := json.Unmarshal(line, &header); err != nil {
			issues = append(issues, fmt.Sprintf("events.jsonl line %d: %v", lineNo, err))
			continue
		}
		if count >= maxEvents {
			issues = append(issues, fmt.Sprintf("events.jsonl: event count exceeds %d", maxEvents))
			break
		}
		count++
		if !header.Type.IsKnown() {
			issues = append(issues, fmt.Sprintf("events.jsonl line %d: unknown event type %q", lineNo, header.Type))
		}
		if header.TMS < 0 {
			issues = append(issues, fmt.Sprintf("events.jsonl line %d: timestamp %d is negative", lineNo, header.TMS))
		} else if header.TMS > math.MaxInt64/int64(time.Millisecond) {
			issues = append(issues, fmt.Sprintf("events.jsonl line %d: timestamp %d exceeds time.Duration range", lineNo, header.TMS))
		}
		if haveLast && header.TMS < lastTMS {
			issues = append(issues, fmt.Sprintf("events.jsonl line %d: timestamp %d before previous %d", lineNo, header.TMS, lastTMS))
		}
		lastTMS = header.TMS
		haveLast = true
		var raw trace.EventRecord
		if err := json.Unmarshal(line, &raw); err != nil {
			issues = append(issues, fmt.Sprintf("events.jsonl line %d: %v", lineNo, err))
			continue
		}
		if header.Type == trace.EventTypeResize {
			if sizeIssue := terminalSizeIssue(raw.Cols, raw.Rows); sizeIssue != "" {
				issues = append(issues, fmt.Sprintf("events.jsonl line %d: %s", lineNo, sizeIssue))
			}
			if raw.Cols > maxCols {
				maxCols = raw.Cols
			}
			if raw.Rows > maxRows {
				maxRows = raw.Rows
			}
		}
		var decoded []byte
		if raw.Bytes != "" {
			b, err := base64.StdEncoding.DecodeString(raw.Bytes)
			if err != nil {
				issues = append(issues, fmt.Sprintf("events.jsonl line %d: decode bytes_b64: %v", lineNo, err))
				continue
			}
			decoded = b
			if len(decoded) > maxDecodedPayload-decodedBytes {
				if !payloadLimitReported {
					issues = append(issues, fmt.Sprintf("events.jsonl: decoded payload exceeds %d bytes", maxDecodedPayload))
					payloadLimitReported = true
				}
				continue
			}
			decodedBytes += len(decoded)
		}
		event := Event{
			TMS: raw.TMS, Type: trace.EventType(strings.TrimSpace(string(raw.Type))), Bytes: decoded,
			Kind: raw.Kind, Key: raw.Key, Cols: raw.Cols, Rows: raw.Rows, Code: raw.Code,
			Mouse: raw.Mouse,
		}
		if visit != nil {
			if err := visit(event); err != nil {
				issues = append(issues, err.Error())
				break
			}
		}
		if collect {
			events = append(events, event)
		}
	}
	if err := sc.Err(); err != nil {
		issues = append(issues, "events.jsonl: "+err.Error())
	}
	return events, count, lastTMS, maxCols, maxRows, issues
}

// Stream decodes events from path one at a time without retaining them. Callers
// that require complete source validation must call Validate before Stream.
func Stream(path string, visit func(Event) error) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	entries, issues := tracearchive.Check(zr)
	if len(issues) != 0 {
		return fmt.Errorf("invalid bundle: %s", strings.Join(issues, "; "))
	}
	eventsFile := entries["events.jsonl"]
	if eventsFile == nil {
		return fmt.Errorf("invalid bundle: missing events.jsonl")
	}
	r, err := eventsFile.Open()
	if err != nil {
		return fmt.Errorf("events.jsonl: %w", err)
	}
	defer func() { _ = r.Close() }()
	limited := &io.LimitedReader{R: r, N: tracepolicy.MaxEventsBytes + 1}
	_, _, _, _, _, issues = inspectEventsWithLimits(limited, tracepolicy.MaxEventCount, tracepolicy.MaxDecodedPayloadBytes, false, visit)
	if limited.N == 0 {
		issues = append(issues, fmt.Sprintf("events.jsonl: decompressed content exceeds %d bytes", tracepolicy.MaxEventsBytes))
	}
	if len(issues) != 0 {
		return fmt.Errorf("invalid bundle: %s", strings.Join(issues, "; "))
	}
	return nil
}

func childPTYTermiosIssues(record *termios.Record) []string {
	if record == nil {
		return nil
	}
	var issues []string
	if record.SchemaVersion != 1 {
		issues = append(issues, fmt.Sprintf("manifest.json: child_pty_termios schema version %d is unsupported", record.SchemaVersion))
	}
	if record.Platform == "" {
		issues = append(issues, "manifest.json: child_pty_termios.platform is empty")
	}
	issues = append(issues, termiosSnapshotIssues("start", record.Start)...)
	if record.Exit != nil {
		issues = append(issues, termiosSnapshotIssues("exit", *record.Exit)...)
	}
	return issues
}

func termiosSnapshotIssues(endpoint string, snapshot termios.Snapshot) []string {
	prefix := "manifest.json: child_pty_termios." + endpoint
	var issues []string
	switch snapshot.Status {
	case termios.StatusCaptured:
		if snapshot.State == nil {
			issues = append(issues, prefix+" status captured requires state")
		}
		if snapshot.Error != "" {
			issues = append(issues, prefix+" status captured must not include error")
		}
	case termios.StatusUnavailable, termios.StatusUnsupported:
		if snapshot.State != nil {
			issues = append(issues, prefix+" status "+snapshot.Status+" must not include state")
		}
	default:
		issues = append(issues, fmt.Sprintf("%s has invalid status %q", prefix, snapshot.Status))
	}
	if snapshot.State != nil {
		controlChars := snapshot.State.Raw.ControlChars
		if len(controlChars) == 0 {
			issues = append(issues, prefix+" raw.control_chars is empty")
		}
		if len(controlChars) > 64 {
			issues = append(issues, prefix+" raw.control_chars exceeds 64 entries")
		}
		for i, value := range controlChars {
			if value < 0 || value > 255 {
				issues = append(issues, fmt.Sprintf("%s raw.control_chars[%d] is outside 0..255", prefix, i))
			}
		}
	}
	return issues
}

func terminalSizeIssue(cols, rows int) string {
	if cols < 1 || cols > 65535 || rows < 1 || rows > 65535 {
		return fmt.Sprintf("terminal size %dx%d is outside 1..65535", cols, rows)
	}
	if cols > tracepolicy.MaxTerminalCells/rows {
		return fmt.Sprintf("terminal size %dx%d exceeds %d cells", cols, rows, tracepolicy.MaxTerminalCells)
	}
	return ""
}
