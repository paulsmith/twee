package engine

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/termios"
	"github.com/paulsmith/twee/internal/tracebundle"
)

func TestFinalizeArtifactsWritesTraceBundleAndSignalsDone(t *testing.T) {
	out := filepath.Join(t.TempDir(), "session.twee")
	te, err := Start(context.Background(), Config{
		Cmd:               []string{"/bin/sh", "-c", "echo done"},
		Cols:              40,
		Rows:              5,
		WholeSessionTrace: &WholeSessionTraceConfig{Path: out},
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
	if err := te.ArtifactError(); err != nil {
		t.Fatalf("ArtifactError = %v, want nil", err)
	}
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("bundle is not a valid zip: %v", err)
	}
	_ = zr.Close()
	if !traceBundleHasExitCode(t, out, 0) {
		t.Fatal("bundle missing exit event with code 0")
	}
}

func TestFinalizeArtifactsCapturesChildPTYTermios(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("termios capture is unsupported on this platform")
	}
	out := filepath.Join(t.TempDir(), "termios.twee")
	te, err := Start(context.Background(), Config{
		Cmd:               []string{"/bin/sh", "-c", "stty -echo -icanon -isig; exit 0"},
		WholeSessionTrace: &WholeSessionTraceConfig{Path: out},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = te.Close() }()
	if _, err := te.WaitForExit(WithTimeout(5 * time.Second)); err != nil {
		t.Fatalf("WaitForExit: %v", err)
	}
	if err := te.FinalizeArtifacts(); err != nil {
		t.Fatalf("FinalizeArtifacts: %v", err)
	}

	bundle, err := tracebundle.Open(out)
	if err != nil {
		t.Fatalf("Open trace: %v", err)
	}
	record := bundle.Manifest.ChildPTYTermios
	if record == nil || record.Start.Status != termios.StatusCaptured || record.Start.State == nil {
		t.Fatalf("start termios = %+v, want captured", record)
	}
	if !record.Start.State.Canonical || !record.Start.State.Echo || !record.Start.State.Signals {
		t.Fatalf("start modes = %+v", record.Start.State)
	}
	if record.Exit == nil || record.Exit.Status != termios.StatusCaptured || record.Exit.State == nil {
		t.Fatalf("exit termios = %+v, want captured", record)
	}
	if record.Exit.State.Canonical || record.Exit.State.Echo || record.Exit.State.Signals {
		t.Fatalf("exit modes = %+v", record.Exit.State)
	}
}

func TestFinalizeArtifactsIdempotent(t *testing.T) {
	out := filepath.Join(t.TempDir(), "session.twee")
	te, err := Start(context.Background(), Config{
		Cmd:               []string{"/bin/sh", "-c", "echo done"},
		Cols:              40,
		Rows:              5,
		WholeSessionTrace: &WholeSessionTraceConfig{Path: out},
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
		Cmd:               []string{"/bin/sh", "-c", "echo done"},
		Cols:              40,
		Rows:              5,
		WholeSessionTrace: &WholeSessionTraceConfig{Path: out},
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

func TestEnableTraceRefusedAfterFinalize(t *testing.T) {
	te := startEngineTerm(t, []string{"/bin/sh", "-c", "echo done"}, 40, 5)
	if _, err := te.WaitForExit(WithTimeout(5 * time.Second)); err != nil {
		t.Fatalf("WaitForExit: %v", err)
	}
	if err := te.FinalizeArtifacts(); err != nil {
		t.Fatalf("FinalizeArtifacts: %v", err)
	}
	out := filepath.Join(t.TempDir(), "late.twee")
	if err := te.EnableTrace(out); err == nil {
		t.Fatal("EnableTrace after FinalizeArtifacts succeeded; want error (the trace could never be written)")
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

func traceBundleHasExitCode(t *testing.T, path string, want int) bool {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open trace zip: %v", err)
	}
	defer zr.Close()
	ef, err := zr.Open("events.jsonl")
	if err != nil {
		t.Fatalf("events.jsonl: %v", err)
	}
	defer ef.Close()
	sc := bufio.NewScanner(ef)
	for sc.Scan() {
		var ev struct {
			Type string `json:"type"`
			Code int    `json:"code"`
		}
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if ev.Type == "exit" && ev.Code == want {
			return true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan events: %v", err)
	}
	return false
}
