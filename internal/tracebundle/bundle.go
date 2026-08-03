// Package tracebundle opens and decodes .twee trace bundles.
package tracebundle

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/tracepolicy"
)

// Bundle is the decoded contents of a .twee trace bundle.
type Bundle struct {
	Manifest trace.Manifest
	Events   []Event
	MaxCols  int
	MaxRows  int
}

// Event is one decoded events.jsonl record.
type Event struct {
	TMS   int64
	Type  string
	Bytes []byte
	Kind  string
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

// Open opens and decodes a .twee zip bundle.
func Open(path string) (Bundle, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return Bundle{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer zr.Close()

	entries, issues := checkArchive(&zr.Reader)
	if len(issues) != 0 {
		return Bundle{}, fmt.Errorf("invalid bundle structure: %s", strings.Join(issues, "; "))
	}
	manifestBody, err := readEntry(entries["manifest.json"])
	if err != nil {
		return Bundle{}, fmt.Errorf("read manifest.json: %w", err)
	}
	var man trace.Manifest
	if err := json.NewDecoder(bytes.NewReader(manifestBody)).Decode(&man); err != nil {
		return Bundle{}, fmt.Errorf("decode manifest.json: %w", err)
	}
	if man.Version != 1 {
		return Bundle{}, fmt.Errorf("unsupported bundle version %d", man.Version)
	}

	eventsReader, err := entries["events.jsonl"].Open()
	if err != nil {
		return Bundle{}, fmt.Errorf("read events.jsonl: %w", err)
	}
	limitedEvents := &io.LimitedReader{R: eventsReader, N: tracepolicy.MaxEventsBytes + 1}
	events, err := decodeEvents(limitedEvents)
	closeErr := eventsReader.Close()
	if err != nil {
		return Bundle{}, err
	}
	if closeErr != nil {
		return Bundle{}, fmt.Errorf("read events.jsonl: %w", closeErr)
	}
	if limitedEvents.N == 0 {
		return Bundle{}, fmt.Errorf("read events.jsonl: decompressed content exceeds %d bytes", tracepolicy.MaxEventsBytes)
	}

	maxCols, maxRows := man.Cols, man.Rows
	for _, ev := range events {
		if ev.Type == "resize" {
			if ev.Cols > maxCols {
				maxCols = ev.Cols
			}
			if ev.Rows > maxRows {
				maxRows = ev.Rows
			}
		}
	}
	return Bundle{Manifest: man, Events: events, MaxCols: maxCols, MaxRows: maxRows}, nil
}

func checkArchive(zr *zip.Reader) (map[string]*zip.File, []string) {
	var issues []string
	entries := make(map[string]*zip.File, 2)
	counts := make(map[string]int, 2)
	if len(zr.File) > tracepolicy.MaxArchiveEntries {
		issues = append(issues, fmt.Sprintf("too many zip entries: %d (maximum %d)", len(zr.File), tracepolicy.MaxArchiveEntries))
	}
	var nameBytes, uncompressed uint64
	for _, f := range zr.File {
		nameBytes += uint64(len(f.Name))
		if ^uint64(0)-uncompressed < f.UncompressedSize64 {
			uncompressed = ^uint64(0)
		} else {
			uncompressed += f.UncompressedSize64
		}
		if len(f.Name) == 0 || len(f.Name) > tracepolicy.MaxEntryNameBytes {
			issues = append(issues, fmt.Sprintf("unsafe zip entry name length %d", len(f.Name)))
		}
		if !fs.ValidPath(f.Name) || path.Clean(f.Name) != f.Name || strings.Contains(f.Name, `\`) {
			issues = append(issues, fmt.Sprintf("unsafe non-canonical zip entry path %q", f.Name))
		}
		if !f.Mode().IsRegular() {
			issues = append(issues, fmt.Sprintf("zip entry %q is not a regular file", f.Name))
		}
		switch f.Name {
		case "manifest.json":
			counts[f.Name]++
			entries[f.Name] = f
			if f.UncompressedSize64 > tracepolicy.MaxManifestBytes {
				issues = append(issues, fmt.Sprintf("manifest.json declares unreasonable uncompressed size %d", f.UncompressedSize64))
			}
		case "events.jsonl":
			counts[f.Name]++
			entries[f.Name] = f
			if f.UncompressedSize64 > tracepolicy.MaxEventsBytes {
				issues = append(issues, fmt.Sprintf("events.jsonl declares unreasonable uncompressed size %d", f.UncompressedSize64))
			}
		}
	}
	if nameBytes > tracepolicy.MaxArchiveEntryNameBytes {
		issues = append(issues, fmt.Sprintf("zip entry names total %d bytes (maximum %d)", nameBytes, tracepolicy.MaxArchiveEntryNameBytes))
	}
	if uncompressed > tracepolicy.MaxArchiveUncompressedBytes {
		issues = append(issues, fmt.Sprintf("zip declares unreasonable total uncompressed size %d", uncompressed))
	}
	for _, name := range []string{"manifest.json", "events.jsonl"} {
		if counts[name] == 0 {
			issues = append(issues, "missing "+name)
		} else if counts[name] > 1 {
			issues = append(issues, fmt.Sprintf("duplicate required zip entry %s (%d copies)", name, counts[name]))
		}
	}
	if len(issues) != 0 {
		return nil, issues
	}
	return entries, nil
}

func readEntry(f *zip.File) ([]byte, error) {
	limit := uint64(tracepolicy.MaxEventsBytes)
	if f.Name == "manifest.json" {
		limit = tracepolicy.MaxManifestBytes
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	body, err := io.ReadAll(io.LimitReader(rc, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(body)) > limit {
		return nil, fmt.Errorf("decompressed content exceeds %d bytes", limit)
	}
	return body, nil
}

type eventJSON struct {
	TMS   int64             `json:"t_ms"`
	Type  string            `json:"type"`
	Bytes string            `json:"bytes_b64,omitempty"`
	Kind  string            `json:"kind,omitempty"`
	Key   string            `json:"key,omitempty"`
	Cols  int               `json:"cols,omitempty"`
	Rows  int               `json:"rows,omitempty"`
	Code  int               `json:"code,omitempty"`
	Mouse *trace.MouseInput `json:"mouse,omitempty"`
}

func decodeEvents(r io.Reader) ([]Event, error) {
	return decodeEventsWithLimits(r, tracepolicy.MaxEventCount, tracepolicy.MaxDecodedPayloadBytes)
}

func decodeEventsWithLimits(r io.Reader, maxEvents, maxDecodedPayload int) ([]Event, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), tracepolicy.MaxEventLineBytes)

	var events []Event
	lineNo := 0
	decodedBytes := 0
	for sc.Scan() {
		lineNo++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if len(events) >= maxEvents {
			return nil, fmt.Errorf("events.jsonl: event count exceeds %d", maxEvents)
		}
		var raw eventJSON
		if err := json.Unmarshal(line, &raw); err != nil {
			return nil, fmt.Errorf("events.jsonl line %d: %w", lineNo, err)
		}
		var decoded []byte
		if raw.Bytes != "" {
			b, err := base64.StdEncoding.DecodeString(raw.Bytes)
			if err != nil {
				return nil, fmt.Errorf("events.jsonl line %d: decode bytes_b64: %w", lineNo, err)
			}
			decoded = b
			if len(decoded) > maxDecodedPayload-decodedBytes {
				return nil, fmt.Errorf("events.jsonl: decoded payload exceeds %d bytes", maxDecodedPayload)
			}
			decodedBytes += len(decoded)
		}
		events = append(events, Event{
			TMS: raw.TMS, Type: strings.TrimSpace(raw.Type), Bytes: decoded,
			Kind: raw.Kind, Key: raw.Key, Cols: raw.Cols, Rows: raw.Rows, Code: raw.Code,
			Mouse: raw.Mouse,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read events.jsonl: %w", err)
	}
	return events, nil
}
