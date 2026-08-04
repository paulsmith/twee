// Package tracearchive validates and reads the ZIP container shared by .twee
// trace bundle consumers.
package tracearchive

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/paulsmith/twee/internal/tracepolicy"
)

// Check validates the structure and declared sizes of a trace bundle. It
// returns the required entries only when the archive is unambiguous and safe
// to read. Callers must still validate entry contents and CRCs.
func Check(zr *zip.Reader) (map[string]*zip.File, []string) {
	var issues []string
	entries := make(map[string]*zip.File, 2)
	counts := make(map[string]int, 2)
	if len(zr.File) > tracepolicy.MaxArchiveEntries {
		issues = append(issues, fmt.Sprintf("too many zip entries: %d (maximum %d)", len(zr.File), tracepolicy.MaxArchiveEntries))
	}

	var nameBytes uint64
	var uncompressed uint64
	var nonNetworkUncompressed uint64
	for _, f := range zr.File {
		nameBytes += uint64(len(f.Name))
		uncompressed = saturatingAdd(uncompressed, f.UncompressedSize64)
		if f.Name != tracepolicy.NetworkCaptureStream {
			nonNetworkUncompressed = saturatingAdd(nonNetworkUncompressed, f.UncompressedSize64)
		}
		if len(f.Name) == 0 || len(f.Name) > tracepolicy.MaxEntryNameBytes {
			issues = append(issues, fmt.Sprintf("unsafe zip entry name length %d", len(f.Name)))
		}
		if !fs.ValidPath(f.Name) || path.Clean(f.Name) != f.Name || strings.Contains(f.Name, `\`) {
			issues = append(issues, fmt.Sprintf("unsafe non-canonical zip entry path %q", f.Name))
		}
		if !f.Mode().IsRegular() {
			issues = append(issues, fmt.Sprintf("zip entry %q is not a regular file", f.Name))
		}
		switch f.Name {
		case "manifest.json":
			counts[f.Name]++
			entries[f.Name] = f
			if f.UncompressedSize64 > tracepolicy.MaxManifestBytes {
				issues = append(issues, fmt.Sprintf("manifest.json declares unreasonable uncompressed size %d", f.UncompressedSize64))
			}
		case "events.jsonl":
			counts[f.Name]++
			entries[f.Name] = f
			if f.UncompressedSize64 > tracepolicy.MaxEventsBytes {
				issues = append(issues, fmt.Sprintf("events.jsonl declares unreasonable uncompressed size %d", f.UncompressedSize64))
			}
		case tracepolicy.NetworkCaptureStream:
			counts[f.Name]++
			entries[f.Name] = f
			if f.UncompressedSize64 > tracepolicy.MaxNetworkCaptureBytes {
				issues = append(issues, fmt.Sprintf("streams/network.pcap declares unreasonable uncompressed size %d", f.UncompressedSize64))
			}
		}
	}
	if nameBytes > tracepolicy.MaxArchiveEntryNameBytes {
		issues = append(issues, fmt.Sprintf("zip entry names total %d bytes (maximum %d)", nameBytes, tracepolicy.MaxArchiveEntryNameBytes))
	}
	if uncompressed > tracepolicy.MaxArchiveUncompressedBytes {
		issues = append(issues, fmt.Sprintf("zip declares unreasonable total uncompressed size %d", uncompressed))
	}
	if nonNetworkUncompressed > tracepolicy.MaxManifestBytes+tracepolicy.MaxEventsBytes {
		issues = append(issues, fmt.Sprintf("zip declares unreasonable total uncompressed size %d excluding network capture", nonNetworkUncompressed))
	}
	for _, name := range []string{"manifest.json", "events.jsonl"} {
		switch counts[name] {
		case 0:
			issues = append(issues, "missing "+name)
		case 1:
		default:
			issues = append(issues, fmt.Sprintf("duplicate required zip entry %s (%d copies)", name, counts[name]))
		}
	}
	if counts[tracepolicy.NetworkCaptureStream] > 1 {
		issues = append(issues, fmt.Sprintf("duplicate optional zip entry %s (%d copies)", tracepolicy.NetworkCaptureStream, counts[tracepolicy.NetworkCaptureStream]))
	}
	if len(issues) != 0 {
		return nil, issues
	}
	return entries, nil
}

func saturatingAdd(a, b uint64) uint64 {
	if ^uint64(0)-a < b {
		return ^uint64(0)
	}
	return a + b
}

// Read reads and CRC-checks an entry without trusting its declared size as the
// only decompression bound.
func Read(f *zip.File) ([]byte, error) {
	limit := uint64(tracepolicy.MaxEventsBytes)
	if f.Name == "manifest.json" {
		limit = tracepolicy.MaxManifestBytes
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	body, err := io.ReadAll(io.LimitReader(rc, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(body)) > limit {
		return nil, fmt.Errorf("decompressed content exceeds %d bytes", limit)
	}
	return body, nil
}
