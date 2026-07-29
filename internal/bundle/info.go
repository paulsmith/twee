package bundle

import (
	"fmt"
	"os"
	"time"

	"github.com/paulsmith/twee/internal/play"
)

// Info is what "twee bundle info" reports: the manifest fields that
// describe the recorded session, plus fields derived from the bundle as
// a whole.
type Info struct {
	Version    int            `json:"version"`
	Command    []string       `json:"command"`
	Cols       int            `json:"cols"`
	Rows       int            `json:"rows"`
	StartedAt  time.Time      `json:"started_at"`
	StoppedAt  time.Time      `json:"stopped_at"`
	DurationMs int64          `json:"duration_ms"`
	SizeBytes  int64          `json:"size_bytes"`
	Events     map[string]int `json:"events"`
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
		counts[ev.Type]++
	}

	dur := b.Manifest.StoppedAt.Sub(b.Manifest.StartedAt)
	if dur < 0 {
		dur = 0
	}

	return Info{
		Version:    b.Manifest.Version,
		Command:    append([]string(nil), b.Manifest.Command...),
		Cols:       b.Manifest.Cols,
		Rows:       b.Manifest.Rows,
		StartedAt:  b.Manifest.StartedAt,
		StoppedAt:  b.Manifest.StoppedAt,
		DurationMs: dur.Milliseconds(),
		SizeBytes:  fi.Size(),
		Events:     counts,
	}, nil
}
