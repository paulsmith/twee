package tracearchive

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/tracepolicy"
)

// testPacket is one classic PCAP record. The header lengths are written as
// given so tests can disagree with the appended data on purpose.
type testPacket struct {
	seconds  uint32
	fraction uint32
	included uint32
	original uint32
	data     []byte
}

// pcapSpec describes a classic PCAP file to synthesize.
type pcapSpec struct {
	order    binary.ByteOrder
	nanos    bool
	major    uint16
	minor    uint16
	snapLen  uint32
	linkType uint32
	packets  []testPacket
}

// recorderSpec mirrors the framing netwrap's recorder produces: little-endian
// microsecond timestamps, version 2.4, snaplen 65535, LINKTYPE_RAW.
func recorderSpec() pcapSpec {
	return pcapSpec{
		order:    binary.LittleEndian,
		major:    pcapVersionMajor,
		minor:    pcapVersionMinor,
		snapLen:  65535,
		linkType: pcapLinkTypeRaw,
	}
}

func buildPCAP(spec pcapSpec) []byte {
	magic := uint32(0xa1b2c3d4)
	if spec.nanos {
		magic = 0xa1b23c4d
	}
	var buf bytes.Buffer
	for _, field := range []any{
		magic,
		spec.major,
		spec.minor,
		int32(0),  // thiszone
		uint32(0), // sigfigs
		spec.snapLen,
		spec.linkType,
	} {
		_ = binary.Write(&buf, spec.order, field)
	}
	for _, p := range spec.packets {
		for _, field := range []uint32{p.seconds, p.fraction, p.included, p.original} {
			_ = binary.Write(&buf, spec.order, field)
		}
		buf.Write(p.data)
	}
	return buf.Bytes()
}

func writePCAPFile(t *testing.T, b []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "capture.pcap")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestBuildPCAPMatchesRecorderFraming pins the synthesis helper to the exact
// global header bytes third_party/netwrap/internal/record writes, so the
// validator cases below exercise the same framing real captures use.
func TestBuildPCAPMatchesRecorderFraming(t *testing.T) {
	want := []byte{
		0xd4, 0xc3, 0xb2, 0xa1, // little-endian microsecond magic
		0x02, 0x00, 0x04, 0x00, // version 2.4
		0x00, 0x00, 0x00, 0x00, // thiszone
		0x00, 0x00, 0x00, 0x00, // sigfigs
		0xff, 0xff, 0x00, 0x00, // snaplen 65535
		0x65, 0x00, 0x00, 0x00, // LINKTYPE_RAW 101
	}
	if got := buildPCAP(recorderSpec()); !bytes.Equal(got, want) {
		t.Fatalf("buildPCAP header = %x, want recorder header %x", got, want)
	}
}

func TestValidatePCAPFile(t *testing.T) {
	smallPacket := func(fraction uint32) testPacket {
		return testPacket{fraction: fraction, included: 4, original: 4, data: make([]byte, 4)}
	}
	tests := []struct {
		name        string
		build       func() []byte
		wantPackets int64
		wantSnapLen uint32
		wantErr     string
		wantIs      error
	}{
		{
			name: "little endian microsecond",
			build: func() []byte {
				spec := recorderSpec()
				spec.packets = []testPacket{smallPacket(999_999)}
				return buildPCAP(spec)
			},
			wantPackets: 1,
			wantSnapLen: 65535,
		},
		{
			name: "big endian microsecond",
			build: func() []byte {
				spec := recorderSpec()
				spec.order = binary.BigEndian
				spec.snapLen = 4096
				spec.packets = []testPacket{{fraction: 999_999, included: 64, original: 64, data: make([]byte, 64)}}
				return buildPCAP(spec)
			},
			wantPackets: 1,
			wantSnapLen: 4096,
		},
		{
			name: "little endian nanosecond",
			build: func() []byte {
				spec := recorderSpec()
				spec.nanos = true
				spec.packets = []testPacket{smallPacket(999_999_999)}
				return buildPCAP(spec)
			},
			wantPackets: 1,
			wantSnapLen: 65535,
		},
		{
			name: "big endian nanosecond",
			build: func() []byte {
				spec := recorderSpec()
				spec.order = binary.BigEndian
				spec.nanos = true
				spec.packets = []testPacket{smallPacket(999_999_999)}
				return buildPCAP(spec)
			},
			wantPackets: 1,
			wantSnapLen: 65535,
		},
		{
			name:        "header only",
			build:       func() []byte { return buildPCAP(recorderSpec()) },
			wantPackets: 0,
			wantSnapLen: 65535,
		},
		{
			name: "multiple packets",
			build: func() []byte {
				spec := recorderSpec()
				for _, n := range []uint32{1, 40, 1500} {
					spec.packets = append(spec.packets, testPacket{included: n, original: n, data: make([]byte, n)})
				}
				return buildPCAP(spec)
			},
			wantPackets: 3,
			wantSnapLen: 65535,
		},
		{
			name: "snap length at sensible maximum",
			build: func() []byte {
				spec := recorderSpec()
				spec.snapLen = maxSensibleSnapLen
				return buildPCAP(spec)
			},
			wantPackets: 0,
			wantSnapLen: maxSensibleSnapLen,
		},
		{
			name: "invalid magic",
			build: func() []byte {
				b := buildPCAP(recorderSpec())
				copy(b, []byte{0, 0, 0, 0})
				return b
			},
			wantErr: "invalid PCAP magic",
		},
		{
			name: "unsupported version",
			build: func() []byte {
				spec := recorderSpec()
				spec.minor = 3
				return buildPCAP(spec)
			},
			wantErr: "unsupported PCAP version 2.3",
		},
		{
			name: "zero snap length",
			build: func() []byte {
				spec := recorderSpec()
				spec.snapLen = 0
				return buildPCAP(spec)
			},
			wantErr: "invalid snapshot length 0",
		},
		{
			name: "oversized snap length",
			build: func() []byte {
				spec := recorderSpec()
				spec.snapLen = maxSensibleSnapLen + 1
				return buildPCAP(spec)
			},
			wantErr: "invalid snapshot length 1048577",
		},
		{
			name: "unsupported link type",
			build: func() []byte {
				spec := recorderSpec()
				spec.linkType = 1 // LINKTYPE_ETHERNET
				return buildPCAP(spec)
			},
			wantErr: "unsupported link type 1",
		},
		{
			name: "microsecond fraction too large",
			build: func() []byte {
				spec := recorderSpec()
				spec.packets = []testPacket{smallPacket(1_000_000)}
				return buildPCAP(spec)
			},
			wantErr: "packet 1 has invalid timestamp fraction 1000000",
		},
		{
			name: "nanosecond fraction too large",
			build: func() []byte {
				spec := recorderSpec()
				spec.nanos = true
				spec.packets = []testPacket{smallPacket(1_000_000_000)}
				return buildPCAP(spec)
			},
			wantErr: "packet 1 has invalid timestamp fraction 1000000000",
		},
		{
			name: "captured length exceeds snap length",
			build: func() []byte {
				spec := recorderSpec()
				spec.snapLen = 8
				spec.packets = []testPacket{{included: 9, original: 9, data: make([]byte, 9)}}
				return buildPCAP(spec)
			},
			wantErr: "captured length 9 exceeds snapshot length 8",
		},
		{
			name: "captured length exceeds original length",
			build: func() []byte {
				spec := recorderSpec()
				spec.packets = []testPacket{{included: 4, original: 3, data: make([]byte, 4)}}
				return buildPCAP(spec)
			},
			wantErr: "captured length 4 exceeds original length 3",
		},
		{
			name: "truncated record header",
			build: func() []byte {
				spec := recorderSpec()
				spec.packets = []testPacket{smallPacket(0)}
				return buildPCAP(spec)[:pcapGlobalHeaderBytes+8]
			},
			wantErr: "packet 1 header",
			wantIs:  io.ErrUnexpectedEOF,
		},
		{
			name: "truncated packet body",
			build: func() []byte {
				spec := recorderSpec()
				spec.packets = []testPacket{{included: 8, original: 8, data: make([]byte, 8)}}
				return buildPCAP(spec)[:pcapGlobalHeaderBytes+pcapRecordHeaderBytes+4]
			},
			wantErr: "packet 1 data",
			wantIs:  io.EOF,
		},
		{
			name:    "empty file",
			build:   func() []byte { return nil },
			wantErr: "global header",
			wantIs:  io.EOF,
		},
		{
			name:    "truncated global header",
			build:   func() []byte { return buildPCAP(recorderSpec())[:10] },
			wantErr: "global header",
			wantIs:  io.ErrUnexpectedEOF,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.build()
			info, err := ValidatePCAPFile(writePCAPFile(t, b))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ValidatePCAPFile = %+v, want error containing %q", info, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ValidatePCAPFile error = %q, want substring %q", err, tt.wantErr)
				}
				if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
					t.Fatalf("ValidatePCAPFile error = %v, want errors.Is %v", err, tt.wantIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidatePCAPFile: %v", err)
			}
			want := PCAPInfo{Bytes: int64(len(b)), Packets: tt.wantPackets, SnapLen: tt.wantSnapLen}
			if info != want {
				t.Fatalf("ValidatePCAPFile = %+v, want %+v", info, want)
			}
		})
	}
}

func readSoleZipEntry(t *testing.T, archive []byte) *zip.File {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 1 {
		t.Fatalf("archive has %d entries, want 1", len(zr.File))
	}
	return zr.File[0]
}

// zipPCAPEntry stores body as the sole, honestly declared entry of an
// in-memory zip archive.
func zipPCAPEntry(t *testing.T, body []byte) *zip.File {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(tracepolicy.NetworkCaptureStream)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return readSoleZipEntry(t, buf.Bytes())
}

// rawZipEntry deflates body itself and writes the entry with CreateRaw, so
// edit can lie about the sizes or checksum that zip.Writer.Create would
// otherwise compute.
func rawZipEntry(t *testing.T, body []byte, edit func(*zip.FileHeader)) *zip.File {
	t.Helper()
	var deflated bytes.Buffer
	fw, err := flate.NewWriter(&deflated, flate.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := fw.Close(); err != nil {
		t.Fatal(err)
	}
	header := &zip.FileHeader{
		Name:               tracepolicy.NetworkCaptureStream,
		Method:             zip.Deflate,
		CRC32:              crc32.ChecksumIEEE(body),
		CompressedSize64:   uint64(deflated.Len()),
		UncompressedSize64: uint64(len(body)),
	}
	edit(header)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateRaw(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(deflated.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return readSoleZipEntry(t, buf.Bytes())
}

func TestValidatePCAPZipEntry(t *testing.T) {
	spec := recorderSpec()
	spec.packets = []testPacket{
		{included: 4, original: 4, data: make([]byte, 4)},
		{included: 40, original: 40, data: make([]byte, 40)},
	}
	pcap := buildPCAP(spec)
	info, err := ValidatePCAP(zipPCAPEntry(t, pcap))
	if err != nil {
		t.Fatalf("ValidatePCAP: %v", err)
	}
	want := PCAPInfo{Bytes: int64(len(pcap)), Packets: 2, SnapLen: 65535}
	if info != want {
		t.Fatalf("ValidatePCAP = %+v, want %+v", info, want)
	}
}

func TestValidatePCAPMissingEntry(t *testing.T) {
	if _, err := ValidatePCAP(nil); err == nil || !strings.Contains(err.Error(), "missing network capture") {
		t.Fatalf("ValidatePCAP(nil) error = %v, want missing capture error", err)
	}
}

func TestValidatePCAPRejectsOversizedDeclarationWithoutReading(t *testing.T) {
	// The header-only zip.File has no backing archive, so any attempt to
	// open it would fail: a declared-size error proves nothing was read.
	f := &zip.File{FileHeader: zip.FileHeader{
		Name:               tracepolicy.NetworkCaptureStream,
		UncompressedSize64: tracepolicy.MaxNetworkCaptureBytes + 1,
	}}
	if _, err := ValidatePCAP(f); err == nil || !strings.Contains(err.Error(), "declared size") {
		t.Fatalf("ValidatePCAP error = %v, want declared-size error", err)
	}
}

func TestValidatePCAPVerifiesCRC(t *testing.T) {
	spec := recorderSpec()
	spec.packets = []testPacket{{included: 4, original: 4, data: []byte{1, 2, 3, 4}}}
	entry := rawZipEntry(t, buildPCAP(spec), func(h *zip.FileHeader) { h.CRC32 ^= 0xffffffff })
	if _, err := ValidatePCAP(entry); err == nil || !errors.Is(err, zip.ErrChecksum) {
		t.Fatalf("ValidatePCAP error = %v, want zip.ErrChecksum", err)
	}
}

// oversizedPCAP returns a structurally valid capture exactly one byte past
// the policy limit, ending on a record boundary so only the byte cap can
// reject it.
func oversizedPCAP(t *testing.T) []byte {
	t.Helper()
	spec := recorderSpec()
	body := make([]byte, spec.snapLen)
	remaining := int64(tracepolicy.MaxNetworkCaptureBytes) + 1 - pcapGlobalHeaderBytes
	for remaining > pcapRecordHeaderBytes {
		n := min(remaining-pcapRecordHeaderBytes, int64(len(body)))
		spec.packets = append(spec.packets, testPacket{included: uint32(n), original: uint32(n), data: body[:n]})
		remaining -= pcapRecordHeaderBytes + n
	}
	b := buildPCAP(spec)
	if int64(len(b)) != tracepolicy.MaxNetworkCaptureBytes+1 {
		t.Fatalf("oversized capture is %d bytes, want %d", len(b), tracepolicy.MaxNetworkCaptureBytes+1)
	}
	return b
}

func TestValidatePCAPFileRejectsOversizedContent(t *testing.T) {
	// Only the staged-file path is testable here: for zip entries,
	// archive/zip itself rejects any read past the declared
	// UncompressedSize64 with ErrFormat, so ValidatePCAP's decompressed
	// byte cap and read-vs-declared mismatch checks stay defense in depth
	// that honest synthesis cannot reach.
	pcap := oversizedPCAP(t)
	if _, err := ValidatePCAPFile(writePCAPFile(t, pcap)); err == nil || !strings.Contains(err.Error(), "content exceeds") {
		t.Fatalf("ValidatePCAPFile error = %v, want content-size error", err)
	}
}
