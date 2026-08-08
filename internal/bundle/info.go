package bundle

import (
	"fmt"
	"os"
	"time"

	"github.com/paulsmith/twee/internal/play"
	"github.com/paulsmith/twee/internal/trace"
)

// Info is what "twee bundle info" reports: the manifest fields that
// describe the recorded session, plus fields derived from the bundle as
// a whole.
type Info struct {
	Version        int            `json:"version"`
	Command        []string       `json:"command"`
	Cols           int            `json:"cols"`
	Rows           int            `json:"rows"`
	StartedAt      time.Time      `json:"started_at"`
	StoppedAt      time.Time      `json:"stopped_at"`
	DurationMs     int64          `json:"duration_ms"`
	SizeBytes      int64          `json:"size_bytes"`
	Events         map[string]int `json:"events"`
	NetworkCapture NetworkInfo    `json:"network_capture"`
}

// NetworkInfo summarizes the optional packet stream. Present is explicit so
// JSON consumers do not need to infer absence from omitted fields.
type NetworkInfo struct {
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

// Inspect reads path's manifest and events, returning a summary. A
// missing or unreadable file reports a *LoadError with ErrIO; a file
// that exists but isn't a usable .twee bundle (bad zip, manifest, or
// events) reports a *LoadError with ErrInvalid.
func Inspect(path string) (Info, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return Info{}, &LoadError{Kind: ErrIO, Err: err}
	}
	if fi.IsDir() {
		return Info{}, &LoadError{Kind: ErrIO, Err: fmt.Errorf("%s is a directory", path)}
	}

	b, err := play.OpenBundle(path)
	if err != nil {
		return Info{}, &LoadError{Kind: ErrInvalid, Err: err}
	}

	counts := make(map[string]int)
	for _, ev := range b.Events {
		counts[string(ev.Type)]++
	}

	dur := b.Manifest.StoppedAt.Sub(b.Manifest.StartedAt)
	if dur < 0 {
		dur = 0
	}

	return Info{
		Version:        b.Manifest.Version,
		Command:        append([]string(nil), b.Manifest.Command...),
		Cols:           b.Manifest.Cols,
		Rows:           b.Manifest.Rows,
		StartedAt:      b.Manifest.StartedAt,
		StoppedAt:      b.Manifest.StoppedAt,
		DurationMs:     dur.Milliseconds(),
		SizeBytes:      fi.Size(),
		Events:         counts,
		NetworkCapture: networkInfo(b.Manifest.Network),
	}, nil
}

func networkInfo(capture *trace.NetworkCapture) NetworkInfo {
	if capture == nil {
		return NetworkInfo{}
	}
	return NetworkInfo{
		Present: true, Format: capture.Format, Stream: capture.Stream,
		SizeBytes: capture.CapturedBytes, ByteLimit: capture.ByteLimit, PacketCount: capture.PacketCount,
		GVisorVersion: capture.GVisorVersion,
		PublishTCP:    append([]string(nil), capture.PublishTCP...),
		Truncated:     capture.Truncated, Status: capture.Status,
	}
}
