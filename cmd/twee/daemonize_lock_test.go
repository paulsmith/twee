package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAcquireSessionLockFreePath(t *testing.T) {
	t.Setenv("TWEE_STATE_DIR", t.TempDir())
	lf, err := acquireSessionLock("fresh")
	if err != nil {
		t.Fatalf("acquireSessionLock: %v", err)
	}
	defer func() { _ = lf.Close() }()
	lp, _ := lockPath("fresh")
	if _, err := os.Stat(lp); err != nil {
		t.Fatalf("lock file missing: %v", err)
	}
}

func TestGenerationCleanupCannotRemoveReplacementState(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("TWEE_STATE_DIR", stateDir)
	lf, err := acquireSessionLock("replacement")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lf.Close() }()
	if err := writeLockMetadata(lf, sessionLockMetadata{PID: os.Getpid(), Token: "new-generation"}); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(stateDir, "replacement.sock")
	if err := os.WriteFile(sock, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}

	if removeSessionFilesForGeneration("replacement", "old-generation") {
		t.Fatal("old generation reported replacement cleanup")
	}
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("old generation removed replacement socket: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "replacement.lock")); err != nil {
		t.Fatalf("old generation removed replacement lock: %v", err)
	}
}

func TestAcquireSessionLockHeldByOther(t *testing.T) {
	t.Setenv("TWEE_STATE_DIR", t.TempDir())
	holder, err := acquireSessionLock("held")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer func() { _ = holder.Close() }()
	if _, err := acquireSessionLock("held"); err == nil {
		t.Fatal("second acquire succeeded while lock held")
	}
}

func TestAcquireSessionLockStaleFile(t *testing.T) {
	t.Setenv("TWEE_STATE_DIR", t.TempDir())
	lp, err := lockPath("stale")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lp, []byte("999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lf, err := acquireSessionLock("stale")
	if err != nil {
		t.Fatalf("acquire over stale lock file: %v", err)
	}
	defer func() { _ = lf.Close() }()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("re-flock own fd: %v", err)
	}
}
