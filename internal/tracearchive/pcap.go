package tracearchive

import (
	"archive/zip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/paulsmith/twee/internal/tracepolicy"
)

const (
	pcapGlobalHeaderBytes = 24
	pcapRecordHeaderBytes = 16
	pcapVersionMajor      = 2
	pcapVersionMinor      = 4
	pcapLinkTypeRaw       = 101
	maxSensibleSnapLen    = 1 << 20
)

// PCAPInfo describes the framing that was verified while fully reading a
// classic PCAP entry. A full read also verifies the zip entry's CRC.
type PCAPInfo struct {
	Bytes   int64
	Packets int64
	SnapLen uint32
}

// ValidatePCAPFile validates a staged classic PCAP before it is attached to a
// bundle.
func ValidatePCAPFile(path string) (PCAPInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return PCAPInfo{}, err
	}
	counted := &countingReader{r: io.LimitReader(f, tracepolicy.MaxNetworkCaptureBytes+1)}
	info, validateErr := validatePCAPStream(counted)
	closeErr := f.Close()
	if validateErr != nil {
		return PCAPInfo{}, validateErr
	}
	if closeErr != nil {
		return PCAPInfo{}, closeErr
	}
	if counted.n > tracepolicy.MaxNetworkCaptureBytes {
		return PCAPInfo{}, fmt.Errorf("content exceeds %d bytes", tracepolicy.MaxNetworkCaptureBytes)
	}
	info.Bytes = counted.n
	return info, nil
}

// ValidatePCAP performs a bounded, CRC-checking read of a classic PCAP zip
// entry. Twee captures raw IP packets, so only LINKTYPE_RAW is accepted.
func ValidatePCAP(f *zip.File) (PCAPInfo, error) {
	if f == nil {
		return PCAPInfo{}, errors.New("missing network capture")
	}
	if f.UncompressedSize64 > tracepolicy.MaxNetworkCaptureBytes {
		return PCAPInfo{}, fmt.Errorf("declared size %d exceeds %d bytes", f.UncompressedSize64, tracepolicy.MaxNetworkCaptureBytes)
	}
	rc, err := f.Open()
	if err != nil {
		return PCAPInfo{}, err
	}
	defer rc.Close()

	counted := &countingReader{r: io.LimitReader(rc, tracepolicy.MaxNetworkCaptureBytes+1)}
	info, err := validatePCAPStream(counted)
	if err != nil {
		return PCAPInfo{}, err
	}
	if counted.n > tracepolicy.MaxNetworkCaptureBytes {
		return PCAPInfo{}, fmt.Errorf("decompressed content exceeds %d bytes", tracepolicy.MaxNetworkCaptureBytes)
	}
	if uint64(counted.n) != f.UncompressedSize64 {
		return PCAPInfo{}, fmt.Errorf("read %d bytes, zip declares %d", counted.n, f.UncompressedSize64)
	}
	info.Bytes = counted.n
	return info, nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}

func validatePCAPStream(r io.Reader) (PCAPInfo, error) {
	var header [pcapGlobalHeaderBytes]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return PCAPInfo{}, fmt.Errorf("global header: %w", err)
	}
	order, nanos, err := pcapByteOrder(header[:4])
	if err != nil {
		return PCAPInfo{}, err
	}
	major := order.Uint16(header[4:6])
	minor := order.Uint16(header[6:8])
	if major != pcapVersionMajor || minor != pcapVersionMinor {
		return PCAPInfo{}, fmt.Errorf("unsupported PCAP version %d.%d", major, minor)
	}
	snapLen := order.Uint32(header[16:20])
	if snapLen == 0 || snapLen > maxSensibleSnapLen {
		return PCAPInfo{}, fmt.Errorf("invalid snapshot length %d", snapLen)
	}
	if linkType := order.Uint32(header[20:24]); linkType != pcapLinkTypeRaw {
		return PCAPInfo{}, fmt.Errorf("unsupported link type %d (want LINKTYPE_RAW %d)", linkType, pcapLinkTypeRaw)
	}

	info := PCAPInfo{SnapLen: snapLen}
	var record [pcapRecordHeaderBytes]byte
	for {
		n, err := io.ReadFull(r, record[:])
		if errors.Is(err, io.EOF) && n == 0 {
			return info, nil
		}
		if err != nil {
			return PCAPInfo{}, fmt.Errorf("packet %d header: %w", info.Packets+1, err)
		}
		fraction := order.Uint32(record[4:8])
		fractionLimit := uint32(1_000_000)
		if nanos {
			fractionLimit = 1_000_000_000
		}
		if fraction >= fractionLimit {
			return PCAPInfo{}, fmt.Errorf("packet %d has invalid timestamp fraction %d", info.Packets+1, fraction)
		}
		included := order.Uint32(record[8:12])
		original := order.Uint32(record[12:16])
		if included > snapLen {
			return PCAPInfo{}, fmt.Errorf("packet %d captured length %d exceeds snapshot length %d", info.Packets+1, included, snapLen)
		}
		if included > original {
			return PCAPInfo{}, fmt.Errorf("packet %d captured length %d exceeds original length %d", info.Packets+1, included, original)
		}
		if _, err := io.CopyN(io.Discard, r, int64(included)); err != nil {
			return PCAPInfo{}, fmt.Errorf("packet %d data: %w", info.Packets+1, err)
		}
		info.Packets++
	}
}

func pcapByteOrder(magic []byte) (binary.ByteOrder, bool, error) {
	switch [4]byte(magic) {
	case [4]byte{0xd4, 0xc3, 0xb2, 0xa1}:
		return binary.LittleEndian, false, nil
	case [4]byte{0xa1, 0xb2, 0xc3, 0xd4}:
		return binary.BigEndian, false, nil
	case [4]byte{0x4d, 0x3c, 0xb2, 0xa1}:
		return binary.LittleEndian, true, nil
	case [4]byte{0xa1, 0xb2, 0x3c, 0x4d}:
		return binary.BigEndian, true, nil
	default:
		return nil, false, fmt.Errorf("invalid PCAP magic %x", magic)
	}
}
