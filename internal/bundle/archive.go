package bundle

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
)

const (
	maxArchiveEntries          = 1024
	maxEntryNameBytes          = 255
	maxArchiveEntryNameBytes   = 64 * 1024
	maxArchiveUncompressedSize = 1 << 30
	maxManifestSize            = 1 << 20
	maxEventsSize              = maxArchiveUncompressedSize - maxManifestSize
)

// checkArchive validates the structure and size declarations of a trace bundle.
// It returns required entries only when the archive is unambiguous and safe to
// read. Callers must still validate entry contents and CRCs.
func checkArchive(zr *zip.Reader) (map[string]*zip.File, []string) {
	var issues []string
	entries := make(map[string]*zip.File, 2)
	counts := make(map[string]int, 2)
	if len(zr.File) > maxArchiveEntries {
		issues = append(issues, fmt.Sprintf("too many zip entries: %d (maximum %d)", len(zr.File), maxArchiveEntries))
	}

	var nameBytes uint64
	var uncompressed uint64
	for _, f := range zr.File {
		nameBytes += uint64(len(f.Name))
		uncompressed = saturatingAdd(uncompressed, f.UncompressedSize64)
		if len(f.Name) == 0 || len(f.Name) > maxEntryNameBytes {
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
			if f.UncompressedSize64 > maxManifestSize {
				issues = append(issues, fmt.Sprintf("manifest.json declares unreasonable uncompressed size %d", f.UncompressedSize64))
			}
		case "events.jsonl":
			counts[f.Name]++
			entries[f.Name] = f
			if f.UncompressedSize64 > maxEventsSize {
				issues = append(issues, fmt.Sprintf("events.jsonl declares unreasonable uncompressed size %d", f.UncompressedSize64))
			}
		}
	}
	if nameBytes > maxArchiveEntryNameBytes {
		issues = append(issues, fmt.Sprintf("zip entry names total %d bytes (maximum %d)", nameBytes, maxArchiveEntryNameBytes))
	}
	if uncompressed > maxArchiveUncompressedSize {
		issues = append(issues, fmt.Sprintf("zip declares unreasonable total uncompressed size %d", uncompressed))
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

// readEntry reads and CRC-checks an entry without trusting its size declaration
// as the only decompression bound.
func readEntry(f *zip.File) ([]byte, error) {
	limit := uint64(maxEventsSize)
	if f.Name == "manifest.json" {
		limit = maxManifestSize
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
