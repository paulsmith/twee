// Package play implements trace bundle playback for the twee CLI.
package play

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/paulsmith/twee/internal/trace"
)

// Bundle is the decoded contents needed for playback.
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

func (e Event) traceTime() time.Duration {
	return time.Duration(e.TMS) * time.Millisecond
}

// TraceTime returns the event's timestamp as a duration from session start.
func (e Event) TraceTime() time.Duration { return e.traceTime() }

// OpenBundle opens and decodes a .twee zip bundle.
func OpenBundle(path string) (Bundle, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return Bundle{}, fmt.Errorf("twee play: open %s: %w", path, err)
	}
	defer zr.Close()

	mf, err := openZipFile(&zr.Reader, "manifest.json")
	if err != nil {
		return Bundle{}, err
	}
	var man trace.Manifest
	if err := json.NewDecoder(mf).Decode(&man); err != nil {
		_ = mf.Close()
		return Bundle{}, fmt.Errorf("twee play: decode manifest.json: %w", err)
	}
	if err := mf.Close(); err != nil {
		return Bundle{}, fmt.Errorf("twee play: close manifest.json: %w", err)
	}
	if man.Version != 1 {
		return Bundle{}, fmt.Errorf("twee play: unsupported bundle version %d", man.Version)
	}

	ef, err := openZipFile(&zr.Reader, "events.jsonl")
	if err != nil {
		return Bundle{}, err
	}
	events, err := decodeEvents(ef)
	closeErr := ef.Close()
	if err != nil {
		return Bundle{}, err
	}
	if closeErr != nil {
		return Bundle{}, fmt.Errorf("twee play: close events.jsonl: %w", closeErr)
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

func openZipFile(zr *zip.Reader, name string) (io.ReadCloser, error) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("twee play: open %s: %w", name, err)
			}
			return rc, nil
		}
	}
	return nil, fmt.Errorf("twee play: missing %s", name)
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
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var events []Event
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var raw eventJSON
		if err := json.Unmarshal(line, &raw); err != nil {
			return nil, fmt.Errorf("twee play: events.jsonl line %d: %w", lineNo, err)
		}
		var decoded []byte
		if raw.Bytes != "" {
			b, err := base64.StdEncoding.DecodeString(raw.Bytes)
			if err != nil {
				return nil, fmt.Errorf("twee play: events.jsonl line %d: decode bytes_b64: %w", lineNo, err)
			}
			decoded = b
		}
		events = append(events, Event{
			TMS: raw.TMS, Type: strings.TrimSpace(raw.Type), Bytes: decoded,
			Kind: raw.Kind, Key: raw.Key, Cols: raw.Cols, Rows: raw.Rows, Code: raw.Code,
			Mouse: raw.Mouse,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("twee play: read events.jsonl: %w", err)
	}
	return events, nil
}
