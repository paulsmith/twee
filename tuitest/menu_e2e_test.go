package tuitest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/paulsmith/twee/tuitest"
)

func ensureMenuBuilt(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("TWEE_FIXTURES_DIR")
	if dir == "" {
		// Find module root by walking up from the test cwd.
		cwd, _ := os.Getwd()
		for {
			if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
				break
			}
			parent := filepath.Dir(cwd)
			if parent == cwd {
				t.Fatal("could not find module root")
			}
			cwd = parent
		}
		dir = cwd
	}
	bin := filepath.Join(dir, "bin", "menu")
	if _, err := os.Stat(bin); err == nil {
		return bin
	}
	cmd := exec.Command("go", "build", "-o", bin, "./fixtures/menu")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build menu fixture: %v\n%s", err, out)
	}
	return bin
}

func TestMenuNavigation(t *testing.T) {
	bin := ensureMenuBuilt(t)
	term := tuitest.Run(t, bin, tuitest.Size(40, 10))

	term.ExpectText("Choose an option")
	// Wait for first item to be highlighted.
	if err := term.WaitForText("> first"); err != nil {
		t.Fatal(err)
	}
	term.Key(tuitest.Down)
	if err := term.WaitForText("> second"); err != nil {
		t.Fatal(err)
	}
	term.Key(tuitest.Enter)
	if err := term.WaitForText("selected: second", tuitest.WithTimeout(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	term.ExpectNoText("panic")
}

func TestMenuSnapshotInitialScreen(t *testing.T) {
	bin := ensureMenuBuilt(t)
	term := tuitest.Run(t, bin, tuitest.Size(20, 6))
	if err := term.WaitForText("> first"); err != nil {
		t.Fatal(err)
	}
	if err := term.WaitForStableScreen(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	term.ExpectTextSnapshot("initial")
}
