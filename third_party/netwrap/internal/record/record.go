// Package record writes packet captures and structured network flow logs.
package record

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	pcapGlobalHeaderSize = 24
	pcapPacketHeaderSize = 16
	pcapSnapLen          = 65535
	pcapLinkTypeRawIPv4  = 101
)

// Direction says which way a packet or flow crossed the private boundary.
type Direction string

const (
	// GuestToHost is traffic sent from the private network to the host.
	GuestToHost Direction = "guest_to_host"
	// HostToGuest is traffic sent from the host to the private network.
	HostToGuest Direction = "host_to_guest"
)

// Flow describes one TCP connection attempt or UDP flow.
type Flow struct {
	Protocol            string    `json:"protocol"`
	Direction           Direction `json:"direction"`
	Source              string    `json:"source"`
	OriginalDestination string    `json:"original_destination"`
	StartTime           time.Time `json:"start_time"`
	EndTime             time.Time `json:"end_time"`
	Result              string    `json:"result"`
	Error               string    `json:"error"`
	BytesSent           int64     `json:"bytes_sent"`
	BytesReceived       int64     `json:"bytes_received"`
}

// ErrCaptureLimit reports that a packet would exceed the PCAP size limit.
// No part of the packet is written. Later packets return the same error type.
type ErrCaptureLimit struct {
	Limit       int64
	Written     int64
	PacketBytes int64
}

// Error implements error.
func (e *ErrCaptureLimit) Error() string {
	return fmt.Sprintf("packet capture limit reached: limit %d bytes, already written %d bytes, packet record needs %d bytes", e.Limit, e.Written, e.PacketBytes)
}

// Recorder writes packet and flow events. Its methods are safe for concurrent use.
type Recorder struct {
	mu sync.Mutex

	pcapFile *os.File
	flowFile *os.File
	flows    *json.Encoder

	maxPCAPBytes int64
	pcapBytes    int64
	packetCount  uint64
	limitReached bool
	closed       bool
	// writeErr latches the first failed output write. A partial record
	// corrupts the file's tail, so no later record may append after it.
	writeErr error
}

// Stats describes the current packet capture.
type Stats struct {
	MaxBytes     int64
	BytesWritten int64
	PacketCount  uint64
	Truncated    bool
}

// Open creates a recorder with a PCAP output and, when flowPath is non-empty,
// a JSONL flow output.
// New parent directories use mode 0700 and output files use mode 0600.
func Open(pcapPath, flowPath string, maxPCAPBytes int64) (*Recorder, error) {
	pcapPath, flowPath, err := validateOpenArguments(pcapPath, flowPath, maxPCAPBytes)
	if err != nil {
		return nil, err
	}

	if err := makeParentDir(pcapPath); err != nil {
		return nil, fmt.Errorf("create PCAP parent directory: %w", err)
	}
	if flowPath != "" {
		if err := makeParentDir(flowPath); err != nil {
			return nil, fmt.Errorf("create flow log parent directory: %w", err)
		}
	}

	pcapFile, err := openOutputFile(pcapPath)
	if err != nil {
		return nil, fmt.Errorf("open PCAP output %q: %w", pcapPath, err)
	}
	if flowPath != "" {
		if same, err := openFileMatchesPath(pcapFile, flowPath); err != nil {
			return nil, errors.Join(
				fmt.Errorf("check flow log output %q: %w", flowPath, err),
				wrapCloseError("PCAP output", pcapFile.Close()),
			)
		} else if same {
			return nil, errors.Join(
				errors.New("open recorder: PCAP and flow log paths refer to the same file"),
				wrapCloseError("PCAP output", pcapFile.Close()),
			)
		}
	}

	var flowFile *os.File
	if flowPath != "" {
		flowFile, err = openOutputFile(flowPath)
		if err != nil {
			closeErr := pcapFile.Close()
			return nil, errors.Join(
				fmt.Errorf("open flow log output %q: %w", flowPath, err),
				wrapCloseError("PCAP output", closeErr),
			)
		}
	}

	if err := writePCAPHeader(pcapFile); err != nil {
		return nil, errors.Join(
			fmt.Errorf("write PCAP header: %w", err),
			wrapCloseError("PCAP output", pcapFile.Close()),
			wrapCloseError("flow log output", closeFile(flowFile)),
		)
	}

	var flows *json.Encoder
	if flowFile != nil {
		flows = json.NewEncoder(flowFile)
	}
	return &Recorder{
		pcapFile:     pcapFile,
		flowFile:     flowFile,
		flows:        flows,
		maxPCAPBytes: maxPCAPBytes,
		pcapBytes:    pcapGlobalHeaderSize,
	}, nil
}

// RecordPacket adds one raw IPv4 packet to the PCAP output.
func (r *Recorder) RecordPacket(at time.Time, direction Direction, packet []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return errors.New("record packet: recorder is closed")
	}
	if r.writeErr != nil {
		return r.writeErr
	}
	packetRecordBytes := int64(pcapPacketHeaderSize) + int64(len(packet))
	if r.limitReached || packetRecordBytes > r.maxPCAPBytes-r.pcapBytes {
		r.limitReached = true
		return &ErrCaptureLimit{
			Limit:       r.maxPCAPBytes,
			Written:     r.pcapBytes,
			PacketBytes: packetRecordBytes,
		}
	}
	if err := validateDirection(direction); err != nil {
		return fmt.Errorf("record packet: %w", err)
	}
	if len(packet) > pcapSnapLen {
		return fmt.Errorf("record packet: packet length %d exceeds PCAP snapshot length %d", len(packet), pcapSnapLen)
	}
	seconds := at.Unix()
	if seconds < 0 || seconds > math.MaxUint32 {
		return fmt.Errorf("record packet: timestamp %s is outside classic PCAP range", at.Format(time.RFC3339Nano))
	}

	var record bytes.Buffer
	record.Grow(pcapPacketHeaderSize + len(packet))
	header := []uint32{
		uint32(seconds),
		uint32(at.Nanosecond() / 1_000),
		uint32(len(packet)),
		uint32(len(packet)),
	}
	for _, value := range header {
		if err := binary.Write(&record, binary.LittleEndian, value); err != nil {
			return fmt.Errorf("record packet: encode PCAP packet header: %w", err)
		}
	}
	if _, err := record.Write(packet); err != nil {
		return fmt.Errorf("record packet: buffer packet: %w", err)
	}
	if _, err := io.Copy(r.pcapFile, &record); err != nil {
		r.writeErr = fmt.Errorf("record packet: write PCAP output: %w", err)
		return r.writeErr
	}
	r.pcapBytes += packetRecordBytes
	r.packetCount++
	return nil
}

// RecordFlow appends one flow as a JSON object followed by a newline.
func (r *Recorder) RecordFlow(flow Flow) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return errors.New("record flow: recorder is closed")
	}
	if r.writeErr != nil {
		return r.writeErr
	}
	if r.flows == nil {
		return nil
	}
	if err := validateDirection(flow.Direction); err != nil {
		return fmt.Errorf("record flow: %w", err)
	}
	if err := r.flows.Encode(flow); err != nil {
		r.writeErr = fmt.Errorf("record flow: write JSONL output: %w", err)
		return r.writeErr
	}
	return nil
}

// Close closes both outputs. It is safe to call Close more than once.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true

	return errors.Join(
		r.writeErr,
		wrapCloseError("PCAP output", r.pcapFile.Close()),
		wrapCloseError("flow log output", closeFile(r.flowFile)),
	)
}

// Stats returns a consistent snapshot of capture metadata.
func (r *Recorder) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Stats{
		MaxBytes:     r.maxPCAPBytes,
		BytesWritten: r.pcapBytes,
		PacketCount:  r.packetCount,
		Truncated:    r.limitReached,
	}
}

func validateOpenArguments(pcapPath, flowPath string, maxPCAPBytes int64) (string, string, error) {
	if strings.TrimSpace(pcapPath) == "" {
		return "", "", errors.New("open recorder: PCAP path is empty")
	}
	if maxPCAPBytes < pcapGlobalHeaderSize {
		return "", "", fmt.Errorf("open recorder: PCAP byte limit %d is smaller than the %d-byte PCAP header", maxPCAPBytes, pcapGlobalHeaderSize)
	}

	pcapPath = filepath.Clean(pcapPath)
	if strings.TrimSpace(flowPath) != "" {
		flowPath = filepath.Clean(flowPath)
	} else {
		flowPath = ""
	}
	pcapAbs, err := filepath.Abs(pcapPath)
	if err != nil {
		return "", "", fmt.Errorf("open recorder: resolve PCAP path: %w", err)
	}
	flowAbs := ""
	if flowPath != "" {
		flowAbs, err = filepath.Abs(flowPath)
		if err != nil {
			return "", "", fmt.Errorf("open recorder: resolve flow log path: %w", err)
		}
		if pcapAbs == flowAbs {
			return "", "", errors.New("open recorder: PCAP and flow log paths must differ")
		}
		if outputsAreSameExistingFile(pcapAbs, flowAbs) {
			return "", "", errors.New("open recorder: PCAP and flow log paths refer to the same file")
		}
	}
	return pcapAbs, flowAbs, nil
}

func closeFile(file *os.File) error {
	if file == nil {
		return nil
	}
	return file.Close()
}

func outputsAreSameExistingFile(first, second string) bool {
	firstInfo, firstErr := os.Stat(first)
	secondInfo, secondErr := os.Stat(second)
	return firstErr == nil && secondErr == nil && os.SameFile(firstInfo, secondInfo)
}

func openFileMatchesPath(file *os.File, path string) (bool, error) {
	fileInfo, err := file.Stat()
	if err != nil {
		return false, err
	}
	pathInfo, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(fileInfo, pathInfo), nil
}

func makeParentDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o700)
}

func openOutputFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		return nil, errors.Join(err, wrapCloseError("output file", file.Close()))
	}
	return file, nil
}

func writePCAPHeader(file *os.File) error {
	header := []any{
		uint32(0xa1b2c3d4),
		uint16(2),
		uint16(4),
		int32(0),
		uint32(0),
		uint32(pcapSnapLen),
		uint32(pcapLinkTypeRawIPv4),
	}
	for _, value := range header {
		if err := binary.Write(file, binary.LittleEndian, value); err != nil {
			return err
		}
	}
	return nil
}

func validateDirection(direction Direction) error {
	switch direction {
	case GuestToHost, HostToGuest:
		return nil
	default:
		return fmt.Errorf("invalid direction %q", direction)
	}
}

func wrapCloseError(name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("close %s: %w", name, err)
}
