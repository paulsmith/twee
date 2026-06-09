package engine

import (
	"archive/zip"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFinalizeArtifactsWritesTraceBundleAndSignalsDone(t *testing.T) {
	out := filepath.Join(t.TempDir(), "session.twee")
	te, err := Start(context.Background(), Config{
		Cmd:       []string{"/bin/sh", "-c", "echo done"},
		Cols:      40,
		Rows:      5,
		TracePath: out,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = te.Close() }()

	select {
	case <-te.ArtifactsDone():
		t.Fatal("ArtifactsDone closed before FinalizeArtifacts")
	default:
	}
	if got := te.FinalizedTracePath(); got != "" {
		t.Fatalf("FinalizedTracePath before finalize = %q, want empty", got)
	}

	if _, err := te.WaitForExit(WithTimeout(5 * time.Second)); err != nil {
		t.Fatalf("WaitForExit: %v", err)
	}
	if err := te.FinalizeArtifacts(); err != nil {
		t.Fatalf("FinalizeArtifacts: %v", err)
	}

	select {
	case <-te.ArtifactsDone():
	default:
		t.Fatal("ArtifactsDone not closed after FinalizeArtifacts")
	}
	if got := te.FinalizedTracePath(); got != out {
		t.Fatalf("FinalizedTracePath = %q, want %q", got, out)
	}
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("bundle is not a valid zip: %v", err)
	}
	_ = zr.Close()
}

func TestFinalizeArtifactsIdempotent(t *testing.T) {
	out := filepath.Join(t.TempDir(), "session.twee")
	te, err := Start(context.Background(), Config{
		Cmd:       []string{"/bin/sh", "-c", "echo done"},
		Cols:      40,
		Rows:      5,
		TracePath: out,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = te.Close() }()

	if _, err := te.WaitForExit(WithTimeout(5 * time.Second)); err != nil {
		t.Fatalf("WaitForExit: %v", err)
	}
	first := te.FinalizeArtifacts()
	second := te.FinalizeArtifacts()
	if first != nil || second != nil {
		t.Fatalf("FinalizeArtifacts errors = %v, %v, want nil, nil", first, second)
	}
	if got := te.FinalizedTracePath(); got != out {
		t.Fatalf("FinalizedTracePath = %q, want %q", got, out)
	}
}

func TestCloseFinalizesArtifacts(t *testing.T) {
	out := filepath.Join(t.TempDir(), "session.twee")
	te, err := Start(context.Background(), Config{
		Cmd:       []string{"/bin/sh", "-c", "echo done"},
		Cols:      40,
		Rows:      5,
		TracePath: out,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := te.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-te.ArtifactsDone():
	default:
		t.Fatal("ArtifactsDone not closed after Close")
	}
	if got := te.FinalizedTracePath(); got != out {
		t.Fatalf("FinalizedTracePath after Close = %q, want %q", got, out)
	}
	if _, err := zip.OpenReader(out); err != nil {
		t.Fatalf("bundle is not a valid zip: %v", err)
	}
}

func TestFinalizeArtifactsWithoutTrace(t *testing.T) {
	te := startEngineTerm(t, []string{"/bin/sh", "-c", "echo done"}, 40, 5)
	if _, err := te.WaitForExit(WithTimeout(5 * time.Second)); err != nil {
		t.Fatalf("WaitForExit: %v", err)
	}
	if err := te.FinalizeArtifacts(); err != nil {
		t.Fatalf("FinalizeArtifacts: %v", err)
	}
	select {
	case <-te.ArtifactsDone():
	default:
		t.Fatal("ArtifactsDone not closed after FinalizeArtifacts")
	}
	if got := te.FinalizedTracePath(); got != "" {
		t.Fatalf("FinalizedTracePath = %q, want empty", got)
	}
}

func TestDrainOutputIdempotentAndPreservesScreen(t *testing.T) {
	te := startEngineTerm(t, []string{"/bin/sh", "-c", "echo final-line; sleep 30"}, 40, 5)
	if err := te.WaitForText("final-line", WithTimeout(5*time.Second)); err != nil {
		t.Fatalf("WaitForText: %v", err)
	}
	te.DrainOutput()
	te.DrainOutput()
	if got := snapshotText(te.Snapshot()); !strings.Contains(got, "final-line") {
		t.Fatalf("snapshot after DrainOutput = %q, want it to contain final-line", got)
	}
	if err := te.Close(); err != nil {
		t.Fatalf("Close after DrainOutput: %v", err)
	}
}
