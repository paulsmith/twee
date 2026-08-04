//go:build linux

package netwrap

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

const captureWarningHelperEnv = "_NETWRAP_CAPTURE_WARNING_TEST_HELPER"

// TestCaptureWarningGuestHelper is the guest command for
// TestCaptureLimitWarningReachesPTYMaster: it generates guest traffic until
// the supervising test stops it.
func TestCaptureWarningGuestHelper(t *testing.T) {
	if os.Getenv(captureWarningHelperEnv) != "1" {
		return
	}
	conn, err := net.Dial("udp4", "10.0.2.2:9")
	if err != nil {
		os.Exit(3)
	}
	payload := bytes.Repeat([]byte{0x55}, 64)
	for {
		_, _ = conn.Write(payload)
		time.Sleep(20 * time.Millisecond)
	}
}

// TestCaptureLimitWarningReachesPTYMaster mirrors ptyrunner's descriptor
// lifecycle: the caller's slave copy closes right after Start, so the warning
// must arrive through the dedicated Warnings duplicate, and the master must
// still observe EOF once the command exits and the duplicate closes.
func TestCaptureLimitWarningReachesPTYMaster(t *testing.T) {
	if err := Preflight(); err != nil {
		t.Skipf("netwrap prerequisites unavailable: %v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("open PTY: %v", err)
	}
	defer master.Close()
	warningsFd, err := unix.FcntlInt(slave.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		_ = slave.Close()
		t.Fatalf("dup PTY slave: %v", err)
	}
	warnings := os.NewFile(uintptr(warningsFd), slave.Name())
	process, err := Start(context.Background(), Config{
		Command:        []string{executable, "-test.run=^TestCaptureWarningGuestHelper$"},
		Env:            append(os.Environ(), captureWarningHelperEnv+"=1"),
		Stdin:          slave,
		Stdout:         slave,
		Stderr:         slave,
		ControllingTTY: true,
		Warnings:       warnings,
		PCAPPath:       filepath.Join(t.TempDir(), "capture.pcap"),
		// Header-only limit: the first captured packet exceeds it.
		MaxPCAPBytes: 24,
		DNSAddress:   "127.0.0.1:53",
	})
	if err != nil {
		_ = slave.Close()
		_ = warnings.Close()
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = process.CloseWithGrace(0) })
	if err := slave.Close(); err != nil {
		t.Fatalf("close PTY slave: %v", err)
	}

	var mu sync.Mutex
	var output strings.Builder
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			mu.Lock()
			output.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	const want = "netwrap: packet capture stopped at the 24 byte limit"
	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		got := output.String()
		mu.Unlock()
		if strings.Contains(got, want) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("warning did not reach the PTY master; output:\n%s", got)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := process.CloseWithGrace(0); err != nil {
		t.Fatalf("CloseWithGrace: %v", err)
	}
	result, err := process.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !result.Capture.Truncated {
		t.Fatalf("capture = %+v; want truncated", result.Capture)
	}
	if err := warnings.Close(); err != nil {
		t.Fatalf("close warnings duplicate: %v", err)
	}
	select {
	case <-readDone:
	case <-time.After(2 * time.Second):
		t.Fatal("PTY master did not reach EOF after command exit")
	}
}
