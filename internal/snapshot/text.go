// Package snapshot writes and compares text and cell snapshots.
package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// updateMode is true when the user runs go test with -tuitest-update.
// Implemented as a flag in the public package; this internal helper
// just receives the bool.
type Result struct {
	Updated bool // file was created or updated
	Path    string
}

// CompareText compares actual against the file at path. If the file
// doesn't exist, or update is true, it is written and Result.Updated
// is true. Otherwise, returns a non-nil error if the contents differ.
func CompareText(path, actual string, update bool) (Result, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, err
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return Result{}, err
	}
	if os.IsNotExist(err) || update {
		if err := os.WriteFile(path, []byte(actual), 0o600); err != nil {
			return Result{}, err
		}
		return Result{Updated: true, Path: path}, nil
	}
	if string(existing) == actual {
		return Result{Path: path}, nil
	}
	return Result{Path: path}, fmt.Errorf("snapshot mismatch in %s\n%s",
		path, unifiedDiff(string(existing), actual))
}

// unifiedDiff prints a minimal diff: per-line ± markers.
func unifiedDiff(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	var sb strings.Builder
	max := len(wl)
	if len(gl) > max {
		max = len(gl)
	}
	for i := 0; i < max; i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w == g {
			fmt.Fprintf(&sb, "  %s\n", w)
		} else {
			if i < len(wl) {
				fmt.Fprintf(&sb, "- %s\n", w)
			}
			if i < len(gl) {
				fmt.Fprintf(&sb, "+ %s\n", g)
			}
		}
	}
	return sb.String()
}
