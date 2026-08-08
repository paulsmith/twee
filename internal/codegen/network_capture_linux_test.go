//go:build linux

package codegen

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/paulsmith/twee/internal/bundle"
	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/tracearchive"
	"github.com/paulsmith/twee/internal/tracepolicy"
	"github.com/paulsmith/twee/third_party/netwrap"
)

const wrapNetworkCaptureHelperEnv = "TWEE_WRAP_NETWORK_CAPTURE_TEST_HELPER"

func TestRunNetworkCaptureIncludesPublishedTCPExchange(t *testing.T) {
	if err := netwrap.Preflight(); err != nil {
		t.Skipf("netwrap prerequisites unavailable: %v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	probe, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hostAddress := probe.Addr().String()
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}

	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	var output lockedBuffer
	readDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(&output, master)
		if errors.Is(copyErr, syscall.EIO) || errors.Is(copyErr, os.ErrClosed) {
			copyErr = nil
		}
		readDone <- copyErr
	}()

	tracePath := filepath.Join(t.TempDir(), "network.twee")
	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(t.Context(), Options{
			Command: []string{executable, "-test.run=^TestWrapNetworkCaptureGuestHelper$"},
			Env:     map[string]string{wrapNetworkCaptureHelperEnv: "1"},
			Cols:    80, Rows: 24, TracePath: tracePath, NetworkCapture: true,
			PublishTCP: []engine.TCPPublication{{
				Listen: hostAddress, Guest: "10.0.2.100:18092",
			}},
			Stdin: slave, Stdout: slave, Stderr: slave, NoStatus: true,
		})
	}()
	waitForBufferText(t, &output, "network-helper-ready", 5*time.Second)

	conn, err := net.DialTimeout("tcp4", hostAddress, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(conn, "GET /health HTTP/1.0\r\nHost: test\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(conn)
	_ = conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(response), "200 OK") || !strings.Contains(string(response), "healthy") {
		t.Fatalf("response = %q", response)
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wrap did not exit")
	}
	_ = slave.Close()
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("outer PTY reader did not stop")
	}

	validation, err := bundle.Validate(tracePath)
	if err != nil || !validation.Valid {
		t.Fatalf("bundle validation = %+v, %v", validation, err)
	}
	zr, err := zip.OpenReader(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	manifestFile, err := zr.Open("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest trace.Manifest
	if err := json.NewDecoder(manifestFile).Decode(&manifest); err != nil {
		_ = manifestFile.Close()
		t.Fatal(err)
	}
	if err := manifestFile.Close(); err != nil {
		t.Fatal(err)
	}
	wantPublication := hostAddress + "=18092"
	if manifest.Network == nil || len(manifest.Network.PublishTCP) != 1 || manifest.Network.PublishTCP[0] != wantPublication {
		t.Fatalf("network manifest = %+v, want publication %q", manifest.Network, wantPublication)
	}
	if strings.Contains(manifest.Network.PublishTCP[0], "10.0.2.100") {
		t.Fatalf("network manifest exposes private guest address: %+v", manifest.Network)
	}
	for _, file := range zr.File {
		if file.Name != tracepolicy.NetworkCaptureStream {
			continue
		}
		info, err := tracearchive.ValidatePCAP(file)
		if err != nil {
			t.Fatal(err)
		}
		if info.Packets == 0 || info.Bytes <= 24 {
			t.Fatalf("PCAP info = %+v, want captured packets", info)
		}
		return
	}
	t.Fatal("bundle has no network capture stream")
}

func TestWrapNetworkCaptureGuestHelper(t *testing.T) {
	if os.Getenv(wrapNetworkCaptureHelperEnv) != "1" {
		return
	}
	listener, err := net.Listen("tcp4", "10.0.2.100:18092")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	fmt.Println("network-helper-ready")
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	request, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.HasPrefix(request, "GET /health ") {
		t.Fatalf("request = %q, %v", request, err)
	}
	_, _ = io.WriteString(conn, "HTTP/1.0 200 OK\r\nContent-Length: 7\r\n\r\nhealthy")
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func waitForBufferText(t *testing.T, output *lockedBuffer, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(output.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in %q", want, output.String())
}
