//go:build linux

package codegen

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestRunReapsNaturalExit(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()

	drainDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, master)
		if errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed) {
			err = nil
		}
		drainDone <- err
	}()

	pidFile := filepath.Join(t.TempDir(), "child.pid")
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Options{
			Command:  []string{"/bin/sh", "-c", `echo $$ > "$1"; exit 0`, "sh", pidFile},
			Stdin:    slave,
			Stdout:   slave,
			Stderr:   io.Discard,
			NoStatus: true,
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid child pid %q: %v", raw, err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		_, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid)))
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("child pid %d still exists after Run returned", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := slave.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	select {
	case err := <-drainDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("outer PTY drainer did not stop")
	}
}
