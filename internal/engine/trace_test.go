package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCloseReturnsTraceCloseError(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "session.twee")
	term, err := Start(context.Background(), Config{
		Cmd:               []string{"/bin/sh", "-c", "sleep 30"},
		Cols:              40,
		Rows:              5,
		WholeSessionTrace: &WholeSessionTraceConfig{Path: tracePath},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := os.Mkdir(tracePath, 0o755); err != nil {
		_ = term.Close()
		t.Fatalf("Mkdir final trace path: %v", err)
	}
	if err := term.Close(); err == nil {
		t.Fatal("Close succeeded despite trace final path being a directory")
	}
}
