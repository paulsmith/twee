// Package inspect computes summaries for .twee trace bundles.
package inspect

import (
	"time"

	"github.com/paulsmith/twee/internal/termios"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/tracebundle"
)

// Summary is the JSON/text inspect shape for a .twee trace bundle.
type Summary struct {
	Path            string                 `json:"path"`
	Version         int                    `json:"version"`
	Command         []string               `json:"command"`
	Duration        string                 `json:"duration"`
	DurationMS      int64                  `json:"duration_ms"`
	EventSpanMS     int64                  `json:"event_span_ms"`
	StartedAt       *time.Time             `json:"started_at"`
	StoppedAt       *time.Time             `json:"stopped_at"`
	Terminal        Terminal               `json:"terminal"`
	Events          EventSummary           `json:"events"`
	Markers         []MarkerSummary        `json:"markers"`
	Exit            ExitSummary            `json:"exit"`
	Network         NetworkSummary         `json:"network_capture"`
	ChildPTYTermios ChildPTYTermiosSummary `json:"child_pty_termios"`
	Replay          ReplaySummary          `json:"replay"`
}

// Terminal summarizes initial and maximum terminal dimensions.
type Terminal struct {
	Cols    int `json:"cols"`
	Rows    int `json:"rows"`
	MaxCols int `json:"max_cols"`
	MaxRows int `json:"max_rows"`
}

// EventSummary summarizes event counts.
type EventSummary struct {
	Total       int            `json:"total"`
	ByType      map[string]int `json:"by_type"`
	InputByKind map[string]int `json:"input_by_kind"`
}

// MarkerSummary identifies one marker in recording order.
type MarkerSummary struct {
	EventIndex int    `json:"event_index"`
	TMS        int64  `json:"t_ms"`
	Label      string `json:"label"`
}

// ExitSummary reports whether an exit event was recorded.
type ExitSummary struct {
	Recorded bool `json:"recorded"`
	Code     *int `json:"code"`
}

// NetworkSummary describes the optional packet capture in the bundle.
type NetworkSummary struct {
	Present       bool     `json:"present"`
	Format        string   `json:"format,omitempty"`
	Stream        string   `json:"stream,omitempty"`
	SizeBytes     int64    `json:"size_bytes,omitempty"`
	ByteLimit     int64    `json:"byte_limit,omitempty"`
	PacketCount   int64    `json:"packet_count,omitempty"`
	GVisorVersion string   `json:"gvisor_version,omitempty"`
	PublishTCP    []string `json:"publish_tcp,omitempty"`
	Truncated     bool     `json:"truncated"`
	Status        string   `json:"status,omitempty"`
}

// ChildPTYTermiosSummary describes child PTY terminal attributes captured at
// trace start and, when available, child exit.
type ChildPTYTermiosSummary struct {
	Present       bool              `json:"present"`
	SchemaVersion int               `json:"schema_version,omitempty"`
	Platform      string            `json:"platform,omitempty"`
	Start         *termios.Snapshot `json:"start,omitempty"`
	Exit          *termios.Snapshot `json:"exit,omitempty"`
}

// Summarize computes an inspect summary for bundle.
func Summarize(path string, bundle tracebundle.Bundle) Summary {
	spanMS := eventSpanMS(bundle.Events)
	duration := durationFrom(bundle, spanMS)

	s := Summary{
		Path:        path,
		Version:     bundle.Manifest.Version,
		Command:     append([]string(nil), bundle.Manifest.Command...),
		Duration:    duration.String(),
		DurationMS:  duration.Milliseconds(),
		EventSpanMS: spanMS,
		StartedAt:   timePtr(bundle.Manifest.StartedAt),
		StoppedAt:   timePtr(bundle.Manifest.StoppedAt),
		Terminal: Terminal{
			Cols:    bundle.Manifest.Cols,
			Rows:    bundle.Manifest.Rows,
			MaxCols: bundle.MaxCols,
			MaxRows: bundle.MaxRows,
		},
		Events: EventSummary{
			Total:       len(bundle.Events),
			ByType:      map[string]int{},
			InputByKind: map[string]int{},
		},
		Markers: []MarkerSummary{},
	}

	for eventIndex, ev := range bundle.Events {
		s.Events.ByType[string(ev.Type)]++
		if ev.Type == trace.EventTypeInput && ev.Kind != "" {
			s.Events.InputByKind[string(ev.Kind)]++
		}
		if ev.Type == trace.EventTypeMarker {
			s.Markers = append(s.Markers, MarkerSummary{EventIndex: eventIndex, TMS: ev.TMS, Label: ev.Label})
		}
		if ev.Type == trace.EventTypeExit {
			code := ev.Code
			s.Exit.Recorded = true
			s.Exit.Code = &code
		}
	}
	if record := bundle.Manifest.ChildPTYTermios; record != nil {
		start := termios.CloneSnapshot(record.Start)
		s.ChildPTYTermios = ChildPTYTermiosSummary{
			Present: true, SchemaVersion: record.SchemaVersion,
			Platform: record.Platform, Start: &start,
		}
		if record.Exit != nil {
			exit := termios.CloneSnapshot(*record.Exit)
			s.ChildPTYTermios.Exit = &exit
		}
	}
	if capture := bundle.Manifest.Network; capture != nil {
		s.Network = NetworkSummary{
			Present: true, Format: capture.Format, Stream: capture.Stream,
			SizeBytes: capture.CapturedBytes, ByteLimit: capture.ByteLimit, PacketCount: capture.PacketCount,
			GVisorVersion: capture.GVisorVersion,
			PublishTCP:    append([]string(nil), capture.PublishTCP...),
			Truncated:     capture.Truncated, Status: capture.Status,
		}
	}
	return s
}

func eventSpanMS(events []tracebundle.Event) int64 {
	var span int64
	for _, ev := range events {
		if ev.TMS > span {
			span = ev.TMS
		}
	}
	return span
}

func durationFrom(bundle tracebundle.Bundle, fallbackMS int64) time.Duration {
	started, stopped := bundle.Manifest.StartedAt, bundle.Manifest.StoppedAt
	if !started.IsZero() && !stopped.IsZero() {
		d := stopped.Sub(started)
		if d >= 0 {
			return d
		}
	}
	return time.Duration(fallbackMS) * time.Millisecond
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	v := t
	return &v
}
