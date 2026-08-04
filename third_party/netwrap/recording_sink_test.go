package netwrap

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/paulsmith/twee/third_party/netwrap/internal/netstack"
	"github.com/paulsmith/twee/third_party/netwrap/internal/record"
)

func TestRecordingSinkTreatsCaptureLimitAsNonFatal(t *testing.T) {
	dir := t.TempDir()
	recorder, err := record.Open(dir+"/packets.pcap", dir+"/flows.jsonl", 24)
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	var warning bytes.Buffer
	sink := &recordingSink{recorder: recorder, warnings: &warning}
	if err := sink.RecordPacket(time.Now(), netstack.GuestToHost, []byte{0x45}); err != nil {
		t.Fatalf("capture limit returned a fatal error: %v", err)
	}
	if !strings.Contains(warning.String(), "network forwarding continues") {
		t.Fatalf("warning = %q", warning.String())
	}
	if err := sink.RecordFlow(netstack.Flow{Direction: netstack.GuestToHost}); err != nil {
		t.Fatalf("flow after capture limit: %v", err)
	}
}

func TestRecordingSinkReturnsOtherRecorderErrors(t *testing.T) {
	dir := t.TempDir()
	recorder, err := record.Open(dir+"/packets.pcap", dir+"/flows.jsonl", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{recorder: recorder, warnings: &bytes.Buffer{}}
	if err := sink.RecordFlow(netstack.Flow{Direction: netstack.GuestToHost}); err == nil {
		t.Fatal("RecordFlow succeeded after recorder close")
	}
}
