package netwrap

import (
	"os"
	"testing"
)

func TestCreateResolverSource(t *testing.T) {
	want := []byte("nameserver 10.0.2.3\noptions single-request\n")
	path, cleanup, err := createResolverSource(t.TempDir(), want)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("mode %s is not a regular file", info.Mode())
	}
	if permission := info.Mode().Perm(); permission != 0o600 {
		t.Fatalf("permission = %o; want 600", permission)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("contents = %q; want %q", got, want)
	}
	cleanup()
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("source still exists after cleanup: %v", err)
	}
}
