//go:build linux

package netstack

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/checksum"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

func TestRecordCompletedFlow(t *testing.T) {
	for _, test := range []struct {
		name      string
		err       error
		result    string
		flowError string
	}{
		{name: "success", result: "success"},
		{name: "failure", err: errors.New("copy failed"), result: "failed", flowError: "copy failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sink := &flowSink{}
			runtime := &Runtime{sink: sink}
			flow := Flow{StartTime: time.Now()}

			runtime.recordCompletedFlow(&flow, test.err, 12, 34)

			if sink.calls != 1 {
				t.Fatalf("RecordFlow calls = %d; want 1", sink.calls)
			}
			if sink.flow.Result != test.result {
				t.Fatalf("Result = %q; want %q", sink.flow.Result, test.result)
			}
			if sink.flow.Error != test.flowError {
				t.Fatalf("Error = %q; want %q", sink.flow.Error, test.flowError)
			}
			if sink.flow.BytesSent != 12 || sink.flow.BytesReceived != 34 {
				t.Fatalf("bytes = (%d, %d); want (12, 34)", sink.flow.BytesSent, sink.flow.BytesReceived)
			}
			if sink.flow.EndTime.IsZero() {
				t.Fatal("EndTime is zero")
			}
		})
	}
}

func TestTCPAdmissionRecordsCapacityFailureOnce(t *testing.T) {
	sink := &flowsSink{}
	runtime := &Runtime{sink: sink}
	forwarded := 0
	admission := newTCPAdmission(runtime, 1, 4, time.Minute, func(stack.TransportEndpointID, *stack.PacketBuffer) bool {
		forwarded++
		return true
	})
	first := testTCPID(10001)
	second := testTCPID(10002)

	if !admission.handlePacket(first, testSYNPacket(t)) {
		t.Fatal("first SYN was not handled")
	}
	if !admission.handlePacket(first, testSYNPacket(t)) {
		t.Fatal("first SYN retransmission was not handled")
	}
	if !admission.handlePacket(second, testSYNPacket(t)) {
		t.Fatal("capacity-rejected SYN was not handled")
	}
	if !admission.handlePacket(second, testSYNPacket(t)) {
		t.Fatal("capacity-rejected SYN retransmission was not handled")
	}

	if forwarded != 1 {
		t.Fatalf("forwarded SYNs = %d; want 1", forwarded)
	}
	flows := sink.snapshot()
	if len(flows) != 1 {
		t.Fatalf("recorded flows = %d; want 1", len(flows))
	}
	if flows[0].Result != "failed" || flows[0].Error != "tcp forwarder capacity exceeded" {
		t.Fatalf("capacity flow = %#v; want a failed capacity flow", flows[0])
	}
	if flows[0].Source != endpoint(second.RemoteAddress, second.RemotePort) {
		t.Fatalf("capacity flow source = %q; want %q", flows[0].Source, endpoint(second.RemoteAddress, second.RemotePort))
	}

	admission.release(first)
	admission.finish()
	runtime.work.closeAndWait()
}

func TestTCPAdmissionReleaseAllowsNextWhileRuntimeWorkContinues(t *testing.T) {
	sink := &blockingFlowSink{started: make(chan struct{}), unblock: make(chan struct{})}
	runtime := &Runtime{sink: sink}
	forwarded := 0
	admission := newTCPAdmission(runtime, 1, 4, time.Minute, func(stack.TransportEndpointID, *stack.PacketBuffer) bool {
		forwarded++
		return true
	})
	first := testTCPID(10101)
	second := testTCPID(10102)
	if !admission.handlePacket(first, testSYNPacket(t)) {
		t.Fatal("first SYN was not handled")
	}

	// ForwarderRequest.Complete releases admission while its proxy copy and
	// final flow record still hold the independently reserved runtime work.
	admission.release(first)
	if !admission.handlePacket(second, testSYNPacket(t)) {
		t.Fatal("second SYN was not handled after first request completed")
	}
	if forwarded != 2 {
		t.Fatalf("forwarded SYNs = %d; want 2", forwarded)
	}
	admission.release(second)
	admission.finish() // The first proxy and its flow record have finished.

	// Keep the second flow record in progress. Runtime shutdown must still
	// wait, even though both admission slots have already been released.
	flowFinished := make(chan struct{})
	go func() {
		flow := newFlow("tcp", GuestToHost, "10.0.2.100:10102", "192.0.2.10:443")
		runtime.recordCompletedFlow(&flow, nil, 1, 2)
		admission.finish()
		close(flowFinished)
	}()
	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("final flow record did not start")
	}

	closed := make(chan struct{})
	go func() {
		runtime.work.closeAndWait()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("runtime work ended while active proxy flows remained")
	case <-time.After(20 * time.Millisecond):
	}
	close(sink.unblock)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("runtime work did not end after final flow recording finished")
	}
	select {
	case <-flowFinished:
	case <-time.After(time.Second):
		t.Fatal("final flow goroutine did not finish")
	}
}

func TestTCPAdmissionForwardFailureBalancesAdmissionAndWork(t *testing.T) {
	runtime := &Runtime{sink: &flowsSink{}}
	admission := newTCPAdmission(runtime, 1, 4, time.Minute, func(stack.TransportEndpointID, *stack.PacketBuffer) bool {
		return false
	})
	if admission.handlePacket(testTCPID(10103), testSYNPacket(t)) {
		t.Fatal("handlePacket() = true; want underlying forward failure")
	}
	admission.mu.Lock()
	admitted := admission.admitted
	attempts := len(admission.attempts)
	admission.mu.Unlock()
	if admitted != 0 || attempts != 0 {
		t.Fatalf("forward failure left admitted=%d attempts=%d; want both zero", admitted, attempts)
	}
	done := make(chan struct{})
	go func() {
		runtime.work.closeAndWait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("forward failure leaked runtime work")
	}
}

func TestTCPAdmissionExpiresRejectedAttempt(t *testing.T) {
	sink := &flowsSink{}
	runtime := &Runtime{sink: sink}
	now := time.Now()
	admission := newTCPAdmission(runtime, 0, 4, time.Second, func(stack.TransportEndpointID, *stack.PacketBuffer) bool {
		t.Fatal("capacity-zero admission forwarded a SYN")
		return false
	})
	admission.now = func() time.Time { return now }
	id := testTCPID(10003)
	admission.handlePacket(id, testSYNPacket(t))
	now = now.Add(time.Second)
	admission.handlePacket(id, testSYNPacket(t))
	if got := len(sink.snapshot()); got != 2 {
		t.Fatalf("flows after retention expiry = %d; want 2", got)
	}
	runtime.work.closeAndWait()
}

func TestTCPAdmissionRejectedCacheOverflowPreservesEvidence(t *testing.T) {
	sink := &flowsSink{}
	runtime := &Runtime{sink: sink}
	admission := newTCPAdmission(runtime, 0, 1, time.Minute, func(stack.TransportEndpointID, *stack.PacketBuffer) bool {
		t.Fatal("capacity-zero admission forwarded a SYN")
		return false
	})
	first := testTCPID(10004)
	overflow := testTCPID(10005)
	admission.handlePacket(first, testSYNPacket(t))
	admission.handlePacket(overflow, testSYNPacket(t))
	admission.handlePacket(overflow, testSYNPacket(t))

	if got := len(sink.snapshot()); got != 3 {
		t.Fatalf("flows with dedupe-cache overflow = %d; want 3", got)
	}
	admission.mu.Lock()
	deferred := len(admission.attempts)
	admission.mu.Unlock()
	if deferred != 1 {
		t.Fatalf("cached rejected attempts = %d; want hard cap 1", deferred)
	}
	runtime.work.closeAndWait()
}

func TestTCPAdmissionConcurrentRejectedRetransmitsAreDeduplicated(t *testing.T) {
	sink := &flowsSink{}
	runtime := &Runtime{sink: sink}
	admission := newTCPAdmission(runtime, 0, 4, time.Minute, func(stack.TransportEndpointID, *stack.PacketBuffer) bool {
		t.Fatal("capacity-zero admission forwarded a SYN")
		return false
	})
	id := testTCPID(10006)
	pkt := testSYNPacket(t)
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			if !admission.handlePacket(id, pkt) {
				t.Error("rejected SYN was not handled")
			}
		})
	}
	wg.Wait()
	if got := len(sink.snapshot()); got != 1 {
		t.Fatalf("flows for concurrent retransmits = %d; want 1", got)
	}
	runtime.work.closeAndWait()
}

func TestUDPForwarderDeliversBackToBackDatagrams(t *testing.T) {
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on host UDP socket: %v", err)
	}
	defer listener.Close()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("create TUN stand-in socket pair: %v", err)
	}
	guest := os.NewFile(uintptr(fds[1]), "guest-tun")
	defer guest.Close()
	sink := &flowsSink{}
	// Datagrams target the private DNS address, so the forwarder dials the
	// host listener standing in as the resolver — the musl parallel-query
	// pattern that sends several datagrams on one 4-tuple.
	runtime, err := New(fds[0], Config{
		MTU:            1500,
		DialTimeout:    2 * time.Second,
		UDPIdleTimeout: 250 * time.Millisecond,
		DNSAddress:     listener.LocalAddr().String(),
	}, sink)
	if err != nil {
		unix.Close(fds[0])
		t.Fatalf("New: %v", err)
	}
	// The caller keeps ownership of the TUN descriptor, matching how
	// run_linux.go closes its tunFile after Runtime.Close.
	defer unix.Close(fds[0])
	defer runtime.Close()

	const datagrams = 8
	for i := range datagrams {
		payload := fmt.Sprintf("datagram-%d", i)
		if _, err := guest.Write(testUDPDatagram(40000, 53, [4]byte{10, 0, 2, 3}, []byte(payload))); err != nil {
			t.Fatalf("inject datagram %d: %v", i, err)
		}
	}

	received := make(map[string]int)
	buf := make([]byte, 2048)
	deadline := time.Now().Add(5 * time.Second)
	for count := range datagrams {
		if err := listener.SetReadDeadline(deadline); err != nil {
			t.Fatalf("set host read deadline: %v", err)
		}
		n, _, err := listener.ReadFrom(buf)
		if err != nil {
			t.Fatalf("host received %d of %d datagrams: %v", count, datagrams, err)
		}
		received[string(buf[:n])]++
	}
	for i := range datagrams {
		payload := fmt.Sprintf("datagram-%d", i)
		if received[payload] != 1 {
			t.Errorf("payload %q delivered %d times; want exactly once", payload, received[payload])
		}
	}

	// The single flow completes at the idle timeout; a failed flow would mean
	// a datagram re-entered the forwarder and lost its payload.
	flowDeadline := time.Now().Add(5 * time.Second)
	for len(sink.snapshot()) == 0 {
		if time.Now().After(flowDeadline) {
			t.Fatal("no UDP flow was recorded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, flow := range sink.snapshot() {
		if flow.Result != "success" {
			t.Errorf("flow = %+v; want success", flow)
		}
	}
}

func testUDPDatagram(sourcePort, destinationPort uint16, destination [4]byte, payload []byte) []byte {
	total := header.IPv4MinimumSize + header.UDPMinimumSize + len(payload)
	packet := make([]byte, total)
	ip := header.IPv4(packet)
	ip.Encode(&header.IPv4Fields{
		TotalLength: uint16(total),
		TTL:         64,
		Protocol:    uint8(header.UDPProtocolNumber),
		SrcAddr:     tcpip.AddrFrom4([4]byte{10, 0, 2, 100}),
		DstAddr:     tcpip.AddrFrom4(destination),
	})
	ip.SetChecksum(^ip.CalculateChecksum())
	udpHeader := header.UDP(packet[header.IPv4MinimumSize:])
	udpHeader.Encode(&header.UDPFields{
		SrcPort: sourcePort,
		DstPort: destinationPort,
		Length:  uint16(header.UDPMinimumSize + len(payload)),
	})
	copy(udpHeader.Payload(), payload)
	xsum := header.PseudoHeaderChecksum(header.UDPProtocolNumber, ip.SourceAddress(), ip.DestinationAddress(), udpHeader.Length())
	udpHeader.SetChecksum(^udpHeader.CalculateChecksum(checksum.Checksum(payload, xsum)))
	return packet
}

func TestRuntimeWorkWaitsForReservedCallback(t *testing.T) {
	var work runtimeWork
	if !work.start() {
		t.Fatal("start() = false")
	}
	done := make(chan struct{})
	go func() {
		work.closeAndWait()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("closeAndWait returned before reserved work completed")
	case <-time.After(20 * time.Millisecond):
	}
	if work.start() {
		t.Fatal("start() succeeded after close began")
	}
	work.done()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("closeAndWait did not return after work completed")
	}
}

func testTCPID(remotePort uint16) stack.TransportEndpointID {
	return stack.TransportEndpointID{
		LocalAddress:  tcpip.AddrFrom4([4]byte{192, 0, 2, 10}),
		LocalPort:     443,
		RemoteAddress: tcpip.AddrFrom4([4]byte{10, 0, 2, 100}),
		RemotePort:    remotePort,
	}
}

func testSYNPacket(t *testing.T) *stack.PacketBuffer {
	t.Helper()
	raw := make([]byte, 40)
	raw[0] = 0x45 // IPv4, 20-byte header.
	raw[2], raw[3] = 0, 40
	raw[8] = 64
	raw[9] = 6 // TCP.
	copy(raw[12:16], []byte{10, 0, 2, 100})
	copy(raw[16:20], []byte{192, 0, 2, 10})
	raw[20], raw[21] = 0x27, 0x11
	raw[22], raw[23] = 0x01, 0xbb
	raw[32] = 0x50 // 20-byte TCP header.
	raw[33] = 0x02 // SYN.
	pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(raw)})
	if _, ok := pkt.NetworkHeader().Consume(20); !ok {
		t.Fatal("could not consume IPv4 header")
	}
	if _, ok := pkt.TransportHeader().Consume(20); !ok {
		t.Fatal("could not consume TCP header")
	}
	pkt.NetworkProtocolNumber = ipv4.ProtocolNumber
	pkt.RXChecksumValidated = true
	t.Cleanup(pkt.DecRef)
	return pkt
}

type flowSink struct {
	calls int
	flow  Flow
}

func (s *flowSink) RecordPacket(time.Time, Direction, []byte) error { return nil }

func (s *flowSink) RecordFlow(flow Flow) error {
	s.calls++
	s.flow = flow
	return nil
}

type flowsSink struct {
	mu    sync.Mutex
	flows []Flow
}

type blockingFlowSink struct {
	started chan struct{}
	unblock chan struct{}
	once    sync.Once
}

func (*blockingFlowSink) RecordPacket(time.Time, Direction, []byte) error { return nil }

func (s *blockingFlowSink) RecordFlow(Flow) error {
	s.once.Do(func() { close(s.started) })
	<-s.unblock
	return nil
}

func (*flowsSink) RecordPacket(time.Time, Direction, []byte) error { return nil }

func (s *flowsSink) RecordFlow(flow Flow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flows = append(s.flows, flow)
	return nil
}

func (s *flowsSink) snapshot() []Flow {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Flow(nil), s.flows...)
}
