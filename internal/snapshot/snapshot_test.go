package snapshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareTextCreatesMissingSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshots", "screen.txt")

	res, err := CompareText(path, "hello\n", false)
	if err != nil {
		t.Fatalf("CompareText: %v", err)
	}
	if !res.Updated {
		t.Fatalf("Updated = false, want true")
	}
	if res.Path != path {
		t.Fatalf("Path = %q, want %q", res.Path, path)
	}
	assertFile(t, path, "hello\n")
}

func TestCompareTextUpdatesExistingSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screen.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := CompareText(path, "new\n", true)
	if err != nil {
		t.Fatalf("CompareText: %v", err)
	}
	if !res.Updated {
		t.Fatalf("Updated = false, want true")
	}
	assertFile(t, path, "new\n")
}

func TestCompareTextMatchesExistingSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screen.txt")
	if err := os.WriteFile(path, []byte("same\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := CompareText(path, "same\n", false)
	if err != nil {
		t.Fatalf("CompareText: %v", err)
	}
	if res.Updated {
		t.Fatalf("Updated = true, want false")
	}
	if res.Path != path {
		t.Fatalf("Path = %q, want %q", res.Path, path)
	}
}

func TestCompareTextReportsDiffOnMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screen.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := CompareText(path, "one\nthree\n", false)
	if err == nil {
		t.Fatal("CompareText unexpectedly succeeded")
	}
	msg := err.Error()
	for _, want := range []string{"snapshot mismatch", "- two", "+ three"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
}

func TestUnifiedDiff(t *testing.T) {
	if got := UnifiedDiff("same\n", "same\n"); got != "" {
		t.Fatalf("UnifiedDiff equal = %q, want empty", got)
	}

	got := UnifiedDiff("a\nb\n", "a\nc\nd\n")
	for _, want := range []string{"--- expected", "+++ actual", "@@ @@", " a", "-b", "+c", "+d"} {
		if !strings.Contains(got, want) {
			t.Fatalf("diff %q missing %q", got, want)
		}
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
