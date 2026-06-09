package main

import (
	"os"
	"syscall"
	"testing"
)

func TestAcquireSessionLockFreePath(t *testing.T) {
	t.Setenv("TWEE_STATE_DIR", t.TempDir())
	lf, err := acquireSessionLock("fresh")
	if err != nil {
		t.Fatalf("acquireSessionLock: %v", err)
	}
	defer lf.Close()
	lp, _ := lockPath("fresh")
	if _, err := os.Stat(lp); err != nil {
		t.Fatalf("lock file missing: %v", err)
	}
}

func TestAcquireSessionLockHeldByOther(t *testing.T) {
	t.Setenv("TWEE_STATE_DIR", t.TempDir())
	holder, err := acquireSessionLock("held")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer holder.Close()
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
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("re-flock own fd: %v", err)
	}
}
