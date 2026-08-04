//go:build linux

package netstack

import (
	"encoding/binary"

	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

func rawIPv4Packet(packet *stack.PacketBuffer) ([]byte, bool) {
	networkHeader := packet.NetworkHeader().Slice()
	var raw []byte
	if len(networkHeader) != 0 {
		transportHeader := packet.TransportHeader().Slice()
		data := packet.Data().ToBuffer()
		raw = make([]byte, 0, len(networkHeader)+len(transportHeader)+int(data.Size()))
		raw = append(raw, networkHeader...)
		raw = append(raw, transportHeader...)
		raw = append(raw, data.Flatten()...)
		data.Release()
	} else {
		buffer := packet.ToBuffer()
		raw = buffer.Flatten()
		buffer.Release()
	}
	if len(raw) == 0 || raw[0]>>4 != 4 {
		return nil, false
	}
	if len(raw) >= 4 {
		totalSize := int(binary.BigEndian.Uint16(raw[2:4]))
		if totalSize > 0 && totalSize <= len(raw) {
			return raw[:totalSize], true
		}
	}
	// Keep malformed version-4 packets that reached the IPv4 dispatcher.
	return raw, true
}
