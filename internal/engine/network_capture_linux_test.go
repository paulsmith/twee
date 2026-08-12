//go:build linux

package engine_test

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/bundle"
	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/termios"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/tracearchive"
	"github.com/paulsmith/twee/internal/tracepolicy"
	"github.com/paulsmith/twee/third_party/netwrap"
)

const networkCaptureHelperEnv = "TWEE_NETWORK_CAPTURE_TEST_HELPER"

func TestNetworkCaptureIncludesPublishedTCPExchange(t *testing.T) {
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

	tracePath := filepath.Join(t.TempDir(), "network.twee")
	te, err := engine.Start(t.Context(), engine.Config{
		Cmd: []string{executable, "-test.run=^TestNetworkCaptureGuestHelper$"},
		Env: map[string]string{networkCaptureHelperEnv: "1"},
		WholeSessionTrace: &engine.WholeSessionTraceConfig{
			Path: tracePath,
			Network: &engine.NetworkCaptureConfig{PublishTCP: []engine.TCPPublication{{
				Listen: hostAddress, Guest: "10.0.2.100:18091",
			}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = te.CloseWithGrace(0) })
	if err := te.WaitForText("network-helper-ready", engine.WithTimeout(5*time.Second)); err != nil {
		t.Fatal(err)
	}

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
	if _, err := te.WaitForExit(engine.WithTimeout(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := te.FinalizeArtifacts(); err != nil {
		t.Fatal(err)
	}
	validation, err := bundle.Validate(tracePath)
	if err != nil || !validation.Valid {
		t.Fatalf("bundle validation = %+v, %v", validation, err)
	}

	zr, err := zip.OpenReader(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
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
	if manifest.ChildPTYTermios == nil || manifest.ChildPTYTermios.Start.Status != termios.StatusCaptured || manifest.ChildPTYTermios.Exit == nil || manifest.ChildPTYTermios.Exit.Status != termios.StatusCaptured {
		t.Fatalf("child PTY termios = %+v, want captured start and exit", manifest.ChildPTYTermios)
	}
	wantPublication := hostAddress + "=18091"
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
		sawRequest, sawResponse, err := pcapGuestDirections(file, net.ParseIP("10.0.2.100").To4())
		if err != nil {
			t.Fatal(err)
		}
		if !sawRequest || !sawResponse {
			t.Fatalf("PCAP directions: request=%t response=%t, want both", sawRequest, sawResponse)
		}
		return
	}
	t.Fatal("bundle has no network capture stream")
}

func pcapGuestDirections(file *zip.File, guest net.IP) (request, response bool, returnErr error) {
	r, err := file.Open()
	if err != nil {
		return false, false, err
	}
	defer func() { returnErr = errors.Join(returnErr, r.Close()) }()
	data, err := io.ReadAll(r)
	if err != nil {
		return false, false, err
	}
	for offset := 24; offset+16 <= len(data); {
		captured := int(binary.LittleEndian.Uint32(data[offset+8 : offset+12]))
		offset += 16
		if captured < 20 || offset+captured > len(data) {
			return false, false, fmt.Errorf("malformed PCAP packet at offset %d", offset)
		}
		packet := data[offset : offset+captured]
		request = request || bytes.Equal(packet[16:20], guest)
		response = response || bytes.Equal(packet[12:16], guest)
		offset += captured
	}
	return request, response, nil
}

func TestNetworkCaptureGuestHelper(t *testing.T) {
	if os.Getenv(networkCaptureHelperEnv) != "1" {
		return
	}
	listener, err := net.Listen("tcp4", "10.0.2.100:18091")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	fmt.Println("network-helper-ready")
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	request, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.HasPrefix(request, "GET /health ") {
		t.Fatalf("request = %q, %v", request, err)
	}
	_, _ = io.WriteString(conn, "HTTP/1.0 200 OK\r\nContent-Length: 7\r\n\r\nhealthy")
}
