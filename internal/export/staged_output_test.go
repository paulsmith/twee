package export

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStagedOutputCommitPreservesModeAndExtension(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "replay.mp4")
	if err := os.WriteFile(destination, []byte("previous artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	staged, file, err := newStagedOutput(destination, ".mp4")
	if err != nil {
		t.Fatal(err)
	}
	if base := filepath.Base(staged.temporary); !strings.HasPrefix(base, ".replay.") || !strings.HasSuffix(base, ".tmp.mp4") {
		t.Fatalf("temporary filename = %q, want extension-preserving staged name", base)
	}
	if _, err := file.WriteString("replacement artifact"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := staged.commit(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "replacement artifact" {
		t.Fatalf("committed output = %q, err = %v", got, err)
	}
	if runtime.GOOS != "windows" {
		assertFileMode(t, destination, 0o600)
	}
	if staged.temporary != "" {
		t.Fatalf("temporary path after commit = %q, want empty", staged.temporary)
	}
}

func TestStagedOutputAbortRemovesOnlyTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "replay.html")
	if err := os.WriteFile(destination, []byte("previous artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	staged, file, err := newStagedOutput(destination, "")
	if err != nil {
		t.Fatal(err)
	}
	temporary := staged.temporary
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	staged.abort()
	staged.abort()
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Fatalf("temporary output still exists after abort: %v", err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "previous artifact" {
		t.Fatalf("destination after abort = %q, err = %v", got, err)
	}
}
