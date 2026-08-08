package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/tracearchive"
	"github.com/paulsmith/twee/internal/tracepolicy"
)

func TestValidateRejectsUnsafeAndAmbiguousArchiveStructure(t *testing.T) {
	tests := []struct {
		name    string
		entries []zipTestEntry
		want    string
	}{
		{
			name: "duplicate network capture",
			entries: []zipTestEntry{
				{name: "manifest.json", body: `{"version":1}`},
				{name: "events.jsonl"},
				{name: tracepolicy.NetworkCaptureStream, body: string(testPCAP())},
				{name: tracepolicy.NetworkCaptureStream, body: string(testPCAP())},
			},
			want: "duplicate optional zip entry streams/network.pcap",
		},
		{
			name: "duplicate manifest",
			entries: []zipTestEntry{
				{name: "manifest.json", body: `{"version":1}`},
				{name: "manifest.json", body: `{"version":2}`},
				{name: "events.jsonl"},
			},
			want: "duplicate required zip entry manifest.json",
		},
		{
			name: "non-canonical path",
			entries: []zipTestEntry{
				{name: "manifest.json", body: `{"version":1}`},
				{name: "events.jsonl"},
				{name: "extra/../unsafe"},
			},
			want: "unsafe non-canonical zip entry path",
		},
		{
			name: "non-regular entry",
			entries: []zipTestEntry{
				{name: "manifest.json", body: `{"version":1}`},
				{name: "events.jsonl"},
				{name: "link", mode: os.ModeSymlink | 0o777},
			},
			want: "is not a regular file",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Validate(writeZipEntries(t, tt.entries))
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if result.Valid || !hasIssueContaining(result.Issues, tt.want) {
				t.Fatalf("result = %+v, want issue containing %q", result, tt.want)
			}
		})
	}
}

func TestValidateNetworkCaptureManifestAndPCAP(t *testing.T) {
	pcap := testPCAP()
	manifest := fmt.Sprintf(`{
		"version":1,
		"network_capture":{
			"format":"pcap",
			"stream":"streams/network.pcap",
			"gvisor_version":"test",
			"byte_limit":1024,
			"captured_bytes":%d,
			"packet_count":0,
			"truncated":false,
			"status":"complete"
		}
	}`, len(pcap))
	tests := []struct {
		name    string
		entries []zipTestEntry
		want    string
	}{
		{
			name:    "missing declared stream",
			entries: []zipTestEntry{{name: "manifest.json", body: manifest}, {name: "events.jsonl"}},
			want:    "declares network capture but streams/network.pcap is missing",
		},
		{
			name: "undeclared stream",
			entries: []zipTestEntry{
				{name: "manifest.json", body: `{"version":1}`}, {name: "events.jsonl"},
				{name: tracepolicy.NetworkCaptureStream, body: string(pcap)},
			},
			want: "present but not declared",
		},
		{
			name: "packet count mismatch",
			entries: []zipTestEntry{
				{name: "manifest.json", body: strings.Replace(manifest, `"packet_count":0`, `"packet_count":1`, 1)},
				{name: "events.jsonl"},
				{name: tracepolicy.NetworkCaptureStream, body: string(pcap)},
			},
			want: "packet_count 1 does not match stream packet count 0",
		},
		{
			name: "invalid magic",
			entries: []zipTestEntry{
				{name: "manifest.json", body: manifest}, {name: "events.jsonl"},
				{name: tracepolicy.NetworkCaptureStream, body: string(append([]byte{0, 0, 0, 0}, pcap[4:]...))},
			},
			want: "invalid PCAP magic",
		},
		{
			name: "truncated record",
			entries: []zipTestEntry{
				{name: "manifest.json", body: strings.Replace(manifest, fmt.Sprintf(`"captured_bytes":%d`, len(pcap)), fmt.Sprintf(`"captured_bytes":%d`, len(pcap)+8), 1)},
				{name: "events.jsonl"},
				{name: tracepolicy.NetworkCaptureStream, body: string(append(pcap, make([]byte, 8)...))},
			},
			want: "packet 1 header",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Validate(writeZipEntries(t, tt.entries))
			if err != nil {
				t.Fatal(err)
			}
			if result.Valid || !hasIssueContaining(result.Issues, tt.want) {
				t.Fatalf("result = %+v, want issue containing %q", result, tt.want)
			}
		})
	}
}

func TestValidateNetworkCaptureChecksCRC(t *testing.T) {
	pcap := testPCAP()
	manifest := fmt.Sprintf(`{"version":1,"network_capture":{"format":"pcap","stream":"streams/network.pcap","gvisor_version":"test","byte_limit":1024,"captured_bytes":%d,"truncated":false,"status":"complete"}}`, len(pcap))
	path := writeZipEntries(t, []zipTestEntry{
		{name: "manifest.json", body: manifest}, {name: "events.jsonl"},
		{name: tracepolicy.NetworkCaptureStream, body: string(pcap)},
	})
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(body, pcap)
	if i < 0 {
		t.Fatal("PCAP bytes not found in stored zip")
	}
	body[i+12] ^= 0xff
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Validate(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid || !hasIssueContaining(result.Issues, "checksum") {
		t.Fatalf("result = %+v, want checksum issue", result)
	}
}

func testPCAP() []byte {
	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint32(0xa1b2c3d4))
	_ = binary.Write(&out, binary.LittleEndian, uint16(2))
	_ = binary.Write(&out, binary.LittleEndian, uint16(4))
	_ = binary.Write(&out, binary.LittleEndian, int32(0))
	_ = binary.Write(&out, binary.LittleEndian, uint32(0))
	_ = binary.Write(&out, binary.LittleEndian, uint32(65535))
	_ = binary.Write(&out, binary.LittleEndian, uint32(101))
	return out.Bytes()
}

func TestCheckArchiveRejectsUnreasonableDeclarations(t *testing.T) {
	zr := &zip.Reader{File: []*zip.File{
		{FileHeader: zip.FileHeader{Name: "manifest.json", UncompressedSize64: tracepolicy.MaxManifestBytes + 1}},
		{FileHeader: zip.FileHeader{Name: "events.jsonl", UncompressedSize64: tracepolicy.MaxEventsBytes + 1}},
	}}
	_, issues := tracearchive.Check(zr)
	if !hasIssueContaining(issues, "manifest.json declares unreasonable") ||
		!hasIssueContaining(issues, "events.jsonl declares unreasonable") ||
		!hasIssueContaining(issues, "total uncompressed size") {
		t.Fatalf("issues = %v, want unreasonable per-entry and total declarations", issues)
	}
}

func TestCheckArchiveRejectsUnreasonableEntryNamesAndCount(t *testing.T) {
	files := make([]*zip.File, tracepolicy.MaxArchiveEntries+1)
	files[0] = &zip.File{FileHeader: zip.FileHeader{Name: "manifest.json"}}
	files[1] = &zip.File{FileHeader: zip.FileHeader{Name: "events.jsonl"}}
	files[2] = &zip.File{FileHeader: zip.FileHeader{Name: strings.Repeat("x", tracepolicy.MaxEntryNameBytes+1)}}
	for i := 3; i < len(files); i++ {
		files[i] = &zip.File{FileHeader: zip.FileHeader{Name: fmt.Sprintf("extra-%d", i)}}
	}
	_, issues := tracearchive.Check(&zip.Reader{File: files})
	if !hasIssueContaining(issues, "too many zip entries") ||
		!hasIssueContaining(issues, "unsafe zip entry name length") {
		t.Fatalf("issues = %v, want entry-count and entry-name-length issues", issues)
	}
}

func TestValidateRejectsCompressedEventsBomb(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bomb.twee")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	mw, _ := zw.Create("manifest.json")
	_, _ = io.WriteString(mw, `{"version":1}`)
	ew, _ := zw.Create("events.jsonl")
	if _, err := io.CopyN(ew, bundleZeroReader{}, tracepolicy.MaxEventsBytes+1); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := Validate(path)
	if err != nil || result.Valid || !hasIssueContaining(result.Issues, "uncompressed size") {
		t.Fatalf("Validate = %+v, %v; want compressed-bomb rejection", result, err)
	}
}

func TestValidateEventLinesEnforcesCountAndLineSize(t *testing.T) {
	event := `{"type":"output"}` + "\n"
	count, issues := validateEventLinesWithLimit(strings.NewReader(strings.Repeat(event, 4)), 3)
	if count != 3 || !hasIssueContaining(issues, "event count exceeds 3") {
		t.Fatalf("count=%d issues=%v", count, issues)
	}
	_, issues = validateEventLines(strings.NewReader(strings.Repeat("x", tracepolicy.MaxEventLineBytes+1)))
	if !hasIssueContaining(issues, "token too long") {
		t.Fatalf("line issues=%v", issues)
	}
}

type bundleZeroReader struct{}

func (bundleZeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

type zipTestEntry struct {
	name string
	body string
	mode os.FileMode
}

func writeZipEntries(t *testing.T, entries []zipTestEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "malformed.twee")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, entry := range entries {
		h := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		if entry.mode != 0 {
			h.SetMode(entry.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidateValidBundle(t *testing.T) {
	path := writeSyntheticTrace(t)
	result, err := Validate(path)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !result.Valid {
		t.Fatalf("valid = false, issues = %v", result.Issues)
	}
	if result.Events != 5 { // 2 output + 1 input + 1 resize + 1 exit
		t.Errorf("events = %d, want 5", result.Events)
	}
	if len(result.Issues) != 0 {
		t.Errorf("issues = %v, want none", result.Issues)
	}
}

func TestValidateMissingFileIsIO(t *testing.T) {
	_, err := Validate(filepath.Join(t.TempDir(), "missing.twee"))
	if err == nil {
		t.Fatal("want error for missing file")
	}
	var le *LoadError
	if !errors.As(err, &le) || le.Kind != ErrIO {
		t.Fatalf("error = %v, want *LoadError{Kind: ErrIO}", err)
	}
}

func TestValidateTruncatedZipReportsIssueNotError(t *testing.T) {
	// A real zip, truncated partway through — not even a parseable zip
	// container anymore.
	full := writeSyntheticTrace(t)
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	truncated := filepath.Join(t.TempDir(), "truncated.twee")
	if err := os.WriteFile(truncated, raw[:len(raw)/2], 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Validate(truncated)
	if err != nil {
		t.Fatalf("Validate: %v, want a reported issue instead of a hard error", err)
	}
	if result.Valid {
		t.Fatal("valid = true, want false for a truncated zip")
	}
	if !hasIssueContaining(result.Issues, "invalid zip") {
		t.Errorf("issues = %v, want one mentioning an invalid zip", result.Issues)
	}
}

func TestValidateGarbageManifest(t *testing.T) {
	path := writeRawZip(t, map[string]string{
		"manifest.json": `{not valid json`,
		"events.jsonl":  "",
	})
	result, err := Validate(path)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.Valid {
		t.Fatal("valid = true, want false for a garbage manifest")
	}
	if !hasIssueContaining(result.Issues, "manifest.json") {
		t.Errorf("issues = %v, want one mentioning manifest.json", result.Issues)
	}
}

func TestValidateUnsupportedVersion(t *testing.T) {
	path := writeRawZip(t, map[string]string{
		"manifest.json": `{"version":2,"cols":10,"rows":3}`,
		"events.jsonl":  "",
	})
	result, err := Validate(path)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.Valid {
		t.Fatal("valid = true, want false for an unsupported version")
	}
	if !hasIssueContaining(result.Issues, "unsupported bundle version 2") {
		t.Errorf("issues = %v, want one naming the unsupported version", result.Issues)
	}
}

func TestValidateMissingManifestOrEvents(t *testing.T) {
	path := writeRawZip(t, map[string]string{
		"events.jsonl": "",
	})
	result, _ := Validate(path)
	if result.Valid || !hasIssueContaining(result.Issues, "missing manifest.json") {
		t.Errorf("issues = %v, want missing manifest.json", result.Issues)
	}

	path2 := writeRawZip(t, map[string]string{
		"manifest.json": `{"version":1,"cols":10,"rows":3}`,
	})
	result2, _ := Validate(path2)
	if result2.Valid || !hasIssueContaining(result2.Issues, "missing events.jsonl") {
		t.Errorf("issues = %v, want missing events.jsonl", result2.Issues)
	}
}

func TestValidateCorruptEventsLine(t *testing.T) {
	path := writeRawZip(t, map[string]string{
		"manifest.json": `{"version":1,"cols":10,"rows":3}`,
		"events.jsonl": strings.Join([]string{
			`{"t_ms":0,"type":"output"}`,
			`{this is not json`,
			`{"t_ms":10,"type":"exit","code":0}`,
		}, "\n"),
	})
	result, err := Validate(path)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.Valid {
		t.Fatal("valid = true, want false for a corrupt events line")
	}
	if !hasIssueContaining(result.Issues, "events.jsonl line 2") {
		t.Errorf("issues = %v, want one naming line 2", result.Issues)
	}
	// The two lines that did parse should still be counted.
	if result.Events != 2 {
		t.Errorf("events = %d, want 2 (the malformed line doesn't count)", result.Events)
	}
}

func TestValidateIgnoresUnrelatedEventFields(t *testing.T) {
	path := writeRawZip(t, map[string]string{
		"manifest.json": `{"version":1,"cols":10,"rows":3}`,
		"events.jsonl":  `{"t_ms":0,"type":"output","cols":"not-a-number"}`,
	})
	result, err := Validate(path)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !result.Valid || result.Events != 1 || len(result.Issues) != 0 {
		t.Fatalf("result = %+v, want one valid event with no issues", result)
	}
}

func TestValidateUnknownEventType(t *testing.T) {
	path := writeRawZip(t, map[string]string{
		"manifest.json": `{"version":1,"cols":10,"rows":3}`,
		"events.jsonl":  `{"t_ms":0,"type":"teleport"}`,
	})
	result, _ := Validate(path)
	if result.Valid {
		t.Fatal("valid = true, want false for an unknown event type")
	}
	if !hasIssueContaining(result.Issues, `unknown event type "teleport"`) {
		t.Errorf("issues = %v, want one naming the unknown type", result.Issues)
	}
}

func TestValidateOutOfOrderTimestamps(t *testing.T) {
	path := writeRawZip(t, map[string]string{
		"manifest.json": `{"version":1,"cols":10,"rows":3}`,
		"events.jsonl": strings.Join([]string{
			`{"t_ms":100,"type":"output"}`,
			`{"t_ms":50,"type":"output"}`,
		}, "\n"),
	})
	result, _ := Validate(path)
	if result.Valid {
		t.Fatal("valid = true, want false for out-of-order timestamps")
	}
	if !hasIssueContaining(result.Issues, "timestamp 50 before previous 100") {
		t.Errorf("issues = %v, want one naming the out-of-order timestamps", result.Issues)
	}
}

// TestValidateCollectsMultipleIssues pins down that Validate doesn't
// stop at the first problem: a bundle with several independent defects
// should report all of them in one call.
func TestValidateCollectsMultipleIssues(t *testing.T) {
	path := writeRawZip(t, map[string]string{
		"manifest.json": `{"version":2,"cols":10,"rows":3}`,
		"events.jsonl": strings.Join([]string{
			`{"t_ms":0,"type":"teleport"}`,
			`{bad json`,
		}, "\n"),
	})
	result, _ := Validate(path)
	if result.Valid {
		t.Fatal("valid = true, want false")
	}
	if len(result.Issues) < 3 {
		t.Fatalf("issues = %v, want at least 3 (version, unknown type, bad json line)", result.Issues)
	}
}

func hasIssueContaining(issues []string, substr string) bool {
	for _, i := range issues {
		if strings.Contains(i, substr) {
			return true
		}
	}
	return false
}
