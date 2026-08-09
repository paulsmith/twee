package record

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRecorderWritesStandardPCAPAndJSONL(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pcapPath := filepath.Join(root, "private", "capture.pcap")
	flowPath := filepath.Join(root, "private", "flows.jsonl")
	recorder, err := Open(pcapPath, flowPath, 4096)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	packetTime := time.Date(2026, time.August, 1, 14, 15, 16, 123456789, time.FixedZone("test", -5*60*60))
	packet := []byte{0x45, 0x00, 0x00, 0x14}
	if err := recorder.RecordPacket(packetTime, GuestToHost, packet); err != nil {
		t.Fatalf("RecordPacket: %v", err)
	}
	flow := Flow{
		Protocol:            "tcp",
		Direction:           HostToGuest,
		Source:              "127.0.0.1:43210",
		OriginalDestination: "10.0.0.2:8080",
		StartTime:           packetTime,
		EndTime:             packetTime.Add(1500 * time.Millisecond),
		Result:              "connected",
		Error:               "",
		BytesSent:           123,
		BytesReceived:       456,
	}
	if err := recorder.RecordFlow(flow); err != nil {
		t.Fatalf("RecordFlow: %v", err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	pcap := readFile(t, pcapPath)
	if len(pcap) != pcapGlobalHeaderSize+pcapPacketHeaderSize+len(packet) {
		t.Fatalf("PCAP length = %d, want %d", len(pcap), pcapGlobalHeaderSize+pcapPacketHeaderSize+len(packet))
	}
	if got := binary.LittleEndian.Uint32(pcap[0:4]); got != 0xa1b2c3d4 {
		t.Errorf("magic = %#x, want %#x", got, uint32(0xa1b2c3d4))
	}
	if got := binary.LittleEndian.Uint16(pcap[4:6]); got != 2 {
		t.Errorf("major version = %d, want 2", got)
	}
	if got := binary.LittleEndian.Uint16(pcap[6:8]); got != 4 {
		t.Errorf("minor version = %d, want 4", got)
	}
	if got := binary.LittleEndian.Uint32(pcap[16:20]); got != pcapSnapLen {
		t.Errorf("snapshot length = %d, want %d", got, pcapSnapLen)
	}
	if got := binary.LittleEndian.Uint32(pcap[20:24]); got != pcapLinkTypeRawIPv4 {
		t.Errorf("link type = %d, want %d", got, pcapLinkTypeRawIPv4)
	}
	packetHeader := pcap[pcapGlobalHeaderSize : pcapGlobalHeaderSize+pcapPacketHeaderSize]
	if got := binary.LittleEndian.Uint32(packetHeader[0:4]); got != uint32(packetTime.Unix()) {
		t.Errorf("packet seconds = %d, want %d", got, packetTime.Unix())
	}
	if got := binary.LittleEndian.Uint32(packetHeader[4:8]); got != 123456 {
		t.Errorf("packet microseconds = %d, want 123456", got)
	}
	if got := binary.LittleEndian.Uint32(packetHeader[8:12]); got != uint32(len(packet)) {
		t.Errorf("included length = %d, want %d", got, len(packet))
	}
	if got := binary.LittleEndian.Uint32(packetHeader[12:16]); got != uint32(len(packet)) {
		t.Errorf("original length = %d, want %d", got, len(packet))
	}
	if got := pcap[pcapGlobalHeaderSize+pcapPacketHeaderSize:]; !bytes.Equal(got, packet) {
		t.Errorf("packet = %x, want %x", got, packet)
	}

	flowJSON := readFile(t, flowPath)
	wantJSON := fmt.Sprintf("{\"protocol\":\"tcp\",\"direction\":\"host_to_guest\",\"source\":\"127.0.0.1:43210\",\"original_destination\":\"10.0.0.2:8080\",\"start_time\":%q,\"end_time\":%q,\"result\":\"connected\",\"error\":\"\",\"bytes_sent\":123,\"bytes_received\":456}\n", flow.StartTime.Format(time.RFC3339Nano), flow.EndTime.Format(time.RFC3339Nano))
	if string(flowJSON) != wantJSON {
		t.Errorf("flow JSONL:\n got %s want %s", flowJSON, wantJSON)
	}

	assertMode(t, filepath.Dir(pcapPath), 0o700)
	assertMode(t, pcapPath, 0o600)
	assertMode(t, flowPath, 0o600)
}

func TestCaptureLimitStopsPacketsButNotFlows(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pcapPath := filepath.Join(root, "capture.pcap")
	flowPath := filepath.Join(root, "flows.jsonl")
	firstPacket := []byte{0x45, 1, 2, 3}
	limit := int64(pcapGlobalHeaderSize + pcapPacketHeaderSize + len(firstPacket))
	recorder, err := Open(pcapPath, flowPath, limit)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := recorder.RecordPacket(time.Now(), GuestToHost, firstPacket); err != nil {
		t.Fatalf("first RecordPacket: %v", err)
	}

	secondPacket := []byte{0x45}
	err = recorder.RecordPacket(time.Now(), HostToGuest, secondPacket)
	var limitErr *ErrCaptureLimit
	if !errors.As(err, &limitErr) {
		t.Fatalf("second RecordPacket error = %v, want *ErrCaptureLimit", err)
	}
	if limitErr.Limit != limit || limitErr.Written != limit || limitErr.PacketBytes != pcapPacketHeaderSize+int64(len(secondPacket)) {
		t.Errorf("limit error = %+v", limitErr)
	}

	err = recorder.RecordPacket(time.Now(), GuestToHost, nil)
	if !errors.As(err, &limitErr) {
		t.Fatalf("later RecordPacket error = %v, want *ErrCaptureLimit", err)
	}
	if err := recorder.RecordFlow(Flow{Direction: GuestToHost, StartTime: time.Now(), EndTime: time.Now()}); err != nil {
		t.Fatalf("RecordFlow after limit: %v", err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := int64(len(readFile(t, pcapPath))); got != limit {
		t.Errorf("PCAP size = %d, want %d", got, limit)
	}
	flowLines := countLines(t, flowPath)
	if flowLines != 1 {
		t.Errorf("flow lines = %d, want 1", flowLines)
	}
}

func TestWriteFailureLatchesAndStopsAppending(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pcapPath := filepath.Join(root, "capture.pcap")
	flowPath := filepath.Join(root, "flows.jsonl")
	recorder, err := Open(pcapPath, flowPath, 1<<20)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Swap in a read-only descriptor for the same file so exactly one write
	// fails, then restore the writable one: an unlatched recorder would
	// append well-formed records after the corrupt tail.
	writable := recorder.pcapFile
	readOnly, err := os.Open(pcapPath)
	if err != nil {
		t.Fatalf("open read-only descriptor: %v", err)
	}
	defer readOnly.Close()
	recorder.pcapFile = readOnly

	failErr := recorder.RecordPacket(time.Now(), GuestToHost, []byte{0x45, 1, 2, 3})
	if failErr == nil {
		t.Fatal("RecordPacket on a read-only descriptor succeeded")
	}
	recorder.pcapFile = writable

	if err := recorder.RecordPacket(time.Now(), GuestToHost, []byte{0x45, 4, 5, 6}); !errors.Is(err, failErr) {
		t.Fatalf("RecordPacket after write failure = %v; want retained %v", err, failErr)
	}
	if err := recorder.RecordFlow(Flow{Direction: GuestToHost}); !errors.Is(err, failErr) {
		t.Fatalf("RecordFlow after write failure = %v; want retained %v", err, failErr)
	}
	if got := len(readFile(t, pcapPath)); got != pcapGlobalHeaderSize {
		t.Fatalf("PCAP size = %d; want header-only %d after failed writes", got, pcapGlobalHeaderSize)
	}
	if got, want := recorder.Stats(), (Stats{MaxBytes: 1 << 20, BytesWritten: pcapGlobalHeaderSize}); got != want {
		t.Fatalf("Stats = %+v; want unchanged %+v", got, want)
	}
	if err := recorder.Close(); !errors.Is(err, failErr) {
		t.Fatalf("Close = %v; want retained %v", err, failErr)
	}
}

func TestRecorderAllowsDisabledFlowLogAndReportsStats(t *testing.T) {
	t.Parallel()

	pcapPath := filepath.Join(t.TempDir(), "capture.pcap")
	limit := int64(pcapGlobalHeaderSize + pcapPacketHeaderSize + 4)
	recorder, err := Open(pcapPath, "", limit)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := recorder.RecordFlow(Flow{}); err != nil {
		t.Fatalf("RecordFlow with disabled log: %v", err)
	}
	if err := recorder.RecordPacket(time.Now(), GuestToHost, []byte{0x45, 0, 0, 0}); err != nil {
		t.Fatalf("RecordPacket: %v", err)
	}
	var limitErr *ErrCaptureLimit
	if err := recorder.RecordPacket(time.Now(), GuestToHost, []byte{0x45}); !errors.As(err, &limitErr) {
		t.Fatalf("second RecordPacket = %v; want capture limit", err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, want := recorder.Stats(), (Stats{MaxBytes: limit, BytesWritten: limit, PacketCount: 1, Truncated: true}); got != want {
		t.Fatalf("Stats = %+v; want %+v", got, want)
	}
}

func TestConcurrentRecordingWritesEachEventOnce(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pcapPath := filepath.Join(root, "capture.pcap")
	flowPath := filepath.Join(root, "flows.jsonl")
	recorder, err := Open(pcapPath, flowPath, 1<<20)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	const events = 100
	start := make(chan struct{})
	errorsByEvent := make(chan error, events*2)
	var wg sync.WaitGroup
	for i := range events {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			<-start
			packet := []byte{0x45, byte(id), byte(id >> 8)}
			errorsByEvent <- recorder.RecordPacket(time.Unix(int64(id+1), 0), GuestToHost, packet)
		}(i)
		go func(id int) {
			defer wg.Done()
			<-start
			errorsByEvent <- recorder.RecordFlow(Flow{
				Protocol:  "udp",
				Direction: HostToGuest,
				Source:    fmt.Sprintf("source-%d", id),
				StartTime: time.Unix(int64(id+1), 0),
				EndTime:   time.Unix(int64(id+2), 0),
			})
		}(i)
	}
	close(start)
	wg.Wait()
	close(errorsByEvent)
	for err := range errorsByEvent {
		if err != nil {
			t.Errorf("record event: %v", err)
		}
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	wantPCAPSize := pcapGlobalHeaderSize + events*(pcapPacketHeaderSize+3)
	if got := len(readFile(t, pcapPath)); got != wantPCAPSize {
		t.Errorf("PCAP size = %d, want %d", got, wantPCAPSize)
	}
	if got := countLines(t, flowPath); got != events {
		t.Errorf("flow lines = %d, want %d", got, events)
	}

	file, err := os.Open(flowPath)
	if err != nil {
		t.Fatalf("open flow file: %v", err)
	}
	defer file.Close()
	seen := make(map[string]bool, events)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var flow Flow
		if err := json.Unmarshal(scanner.Bytes(), &flow); err != nil {
			t.Fatalf("decode flow: %v", err)
		}
		if seen[flow.Source] {
			t.Errorf("duplicate flow source %q", flow.Source)
		}
		seen[flow.Source] = true
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan flow file: %v", err)
	}
	if len(seen) != events {
		t.Errorf("unique flow count = %d, want %d", len(seen), events)
	}
}

func TestOpenRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pcapPath string
		flowPath string
		limit    int64
	}{
		{name: "empty PCAP", pcapPath: "", flowPath: "flow", limit: 24},
		{name: "small limit", pcapPath: "pcap", flowPath: "flow", limit: 23},
		{name: "same path", pcapPath: "output", flowPath: "output", limit: 24},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Open(test.pcapPath, test.flowPath, test.limit)
			if err == nil {
				t.Fatal("Open succeeded, want an error")
			}
		})
	}
}

func TestOpenRejectsPathsForSameExistingFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pcapPath := filepath.Join(root, "capture")
	flowPath := filepath.Join(root, "flow")
	if err := os.WriteFile(pcapPath, []byte("keep"), 0o600); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := os.Link(pcapPath, flowPath); err != nil {
		t.Fatalf("create hard link: %v", err)
	}
	if _, err := Open(pcapPath, flowPath, 24); err == nil {
		t.Fatal("Open succeeded, want an error")
	}
	if got := string(readFile(t, pcapPath)); got != "keep" {
		t.Errorf("existing output was changed to %q", got)
	}
}

func TestOpenRejectsDanglingSymlinkToOtherOutput(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pcapPath := filepath.Join(root, "capture")
	flowPath := filepath.Join(root, "flow-link")
	if err := os.Symlink(pcapPath, flowPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := Open(pcapPath, flowPath, 24); err == nil {
		t.Fatal("Open succeeded, want an error")
	}
}

func TestValidationAndCloseBehavior(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	recorder, err := Open(filepath.Join(root, "capture"), filepath.Join(root, "flow"), 4096)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := recorder.RecordPacket(time.Now(), Direction("sideways"), []byte{0x45}); err == nil {
		t.Error("RecordPacket with invalid direction succeeded")
	}
	if err := recorder.RecordPacket(time.Unix(-1, 0), GuestToHost, []byte{0x45}); err == nil {
		t.Error("RecordPacket with old time succeeded")
	}
	if err := recorder.RecordPacket(time.Now(), GuestToHost, make([]byte, pcapSnapLen+1)); err == nil {
		t.Error("RecordPacket with oversized packet succeeded")
	}
	if err := recorder.RecordFlow(Flow{Direction: Direction("sideways")}); err == nil {
		t.Error("RecordFlow with invalid direction succeeded")
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := recorder.RecordPacket(time.Now(), GuestToHost, []byte{0x45}); err == nil {
		t.Error("RecordPacket after Close succeeded")
	}
	if err := recorder.RecordFlow(Flow{Direction: GuestToHost}); err == nil {
		t.Error("RecordFlow after Close succeeded")
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode for %s = %04o, want %04o", path, got, want)
	}
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return count
}
