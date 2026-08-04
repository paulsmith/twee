//go:build linux

package netstack

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

func TestRawIPv4PacketUnparsedInbound(t *testing.T) {
	raw := testIPv4Packet()
	packet := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(raw)})
	defer packet.DecRef()
	assertRawIPv4Packet(t, packet, raw)
}

func TestRawIPv4PacketConsumedInboundHeaders(t *testing.T) {
	raw := testIPv4Packet()
	packet := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(raw)})
	defer packet.DecRef()
	if _, ok := packet.NetworkHeader().Consume(20); !ok {
		t.Fatal("could not consume network header")
	}
	if _, ok := packet.TransportHeader().Consume(8); !ok {
		t.Fatal("could not consume transport header")
	}
	assertRawIPv4Packet(t, packet, raw)
}

func TestRawIPv4PacketPushedOutboundHeaders(t *testing.T) {
	raw := testIPv4Packet()
	packet := stack.NewPacketBuffer(stack.PacketBufferOptions{
		ReserveHeaderBytes: 64,
		Payload:            buffer.MakeWithData(raw[28:]),
	})
	defer packet.DecRef()
	copy(packet.TransportHeader().Push(8), raw[20:28])
	copy(packet.NetworkHeader().Push(20), raw[:20])
	assertRawIPv4Packet(t, packet, raw)
}

func TestRawIPv4PacketRejectsNonIPv4Artifact(t *testing.T) {
	packet := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData([]byte{0, 1, 2, 3})})
	defer packet.DecRef()
	if raw, ok := rawIPv4Packet(packet); ok || raw != nil {
		t.Fatalf("rawIPv4Packet() = %x, %t; want nil, false", raw, ok)
	}
}

func TestRawIPv4PacketKeepsMalformedIPv4(t *testing.T) {
	want := []byte{0x45, 0, 0, 40}
	packet := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(want)})
	defer packet.DecRef()
	assertRawIPv4Packet(t, packet, want)
}

func TestCaptureDispatcherSkipsIPv6Protocol(t *testing.T) {
	raw := testIPv4Packet()
	packet := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(raw)})
	defer packet.DecRef()
	sink := &countingSink{}
	delegate := &countingDispatcher{}
	dispatcher := captureDispatcher{NetworkDispatcher: delegate, sink: sink, runtime: &Runtime{}}
	dispatcher.DeliverNetworkPacket(tcpip.NetworkProtocolNumber(0x86dd), packet)
	if sink.packets != 0 {
		t.Fatalf("recorded %d IPv6 packets; want 0", sink.packets)
	}
	if delegate.networkPackets != 1 {
		t.Fatalf("delivered %d packets; want 1", delegate.networkPackets)
	}
}

func TestCaptureDispatcherKeepsUnsupportedIPv4Protocol(t *testing.T) {
	raw := testIPv4Packet()
	raw[9] = 99
	packet := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(raw)})
	defer packet.DecRef()
	sink := &countingSink{}
	dispatcher := captureDispatcher{NetworkDispatcher: &countingDispatcher{}, sink: sink, runtime: &Runtime{}}
	dispatcher.DeliverNetworkPacket(ipv4.ProtocolNumber, packet)
	if sink.packets != 1 {
		t.Fatalf("recorded %d unsupported IPv4 packets; want 1", sink.packets)
	}
}

type countingSink struct{ packets int }

func (s *countingSink) RecordPacket(time.Time, Direction, []byte) error {
	s.packets++
	return nil
}

func (*countingSink) RecordFlow(Flow) error { return nil }

type countingDispatcher struct{ networkPackets int }

func (d *countingDispatcher) DeliverNetworkPacket(tcpip.NetworkProtocolNumber, *stack.PacketBuffer) {
	d.networkPackets++
}

func (*countingDispatcher) DeliverLinkPacket(tcpip.NetworkProtocolNumber, *stack.PacketBuffer) {}

func assertRawIPv4Packet(t *testing.T, packet *stack.PacketBuffer, want []byte) {
	t.Helper()
	got, ok := rawIPv4Packet(packet)
	if !ok {
		t.Fatal("rawIPv4Packet rejected IPv4 packet")
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("rawIPv4Packet() = %x; want %x", got, want)
	}
}

func testIPv4Packet() []byte {
	packet := make([]byte, 32)
	packet[0] = 0x45
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	packet[9] = 17
	copy(packet[12:16], []byte{10, 0, 2, 100})
	copy(packet[16:20], []byte{192, 0, 2, 1})
	copy(packet[20:28], []byte{0x04, 0xd2, 0x00, 0x35, 0x00, 0x0c, 0, 0})
	copy(packet[28:], []byte("test"))
	return packet
}
