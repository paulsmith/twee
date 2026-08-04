package tracearchive

import (
	"archive/zip"
	"fmt"

	"github.com/paulsmith/twee/internal/tracepolicy"
)

// NetworkMetadata is the manifest subset needed to verify that a declared
// network capture and its archive stream agree.
type NetworkMetadata struct {
	Format        string
	Stream        string
	GVisorVersion string
	ByteLimit     int64
	CapturedBytes int64
	PacketCount   int64
	Truncated     bool
	Status        string
}

// CheckNetworkCapture validates manifest/stream presence, metadata, PCAP
// framing, and the stream CRC. It returns all independent issues it can find.
func CheckNetworkCapture(meta *NetworkMetadata, stream *zip.File) []string {
	if meta == nil {
		if stream != nil {
			return []string{tracepolicy.NetworkCaptureStream + " is present but not declared by manifest.json"}
		}
		return nil
	}

	var issues []string
	if stream == nil {
		issues = append(issues, "manifest.json declares network capture but "+tracepolicy.NetworkCaptureStream+" is missing")
	}
	if meta.Format != tracepolicy.NetworkCaptureFormat {
		issues = append(issues, fmt.Sprintf("manifest.json network_capture.format %q is unsupported", meta.Format))
	}
	if meta.Stream != tracepolicy.NetworkCaptureStream {
		issues = append(issues, fmt.Sprintf("manifest.json network_capture.stream %q must be %q", meta.Stream, tracepolicy.NetworkCaptureStream))
	}
	if meta.GVisorVersion == "" {
		issues = append(issues, "manifest.json network_capture.gvisor_version is empty")
	}
	if meta.ByteLimit <= 0 || meta.ByteLimit > tracepolicy.MaxNetworkCaptureBytes {
		issues = append(issues, fmt.Sprintf("manifest.json network_capture.byte_limit %d is outside 1..%d", meta.ByteLimit, tracepolicy.MaxNetworkCaptureBytes))
	}
	if meta.CapturedBytes < 0 || (meta.ByteLimit > 0 && meta.CapturedBytes > meta.ByteLimit) {
		issues = append(issues, fmt.Sprintf("manifest.json network_capture.captured_bytes %d is outside byte limit %d", meta.CapturedBytes, meta.ByteLimit))
	}
	if meta.PacketCount < 0 {
		issues = append(issues, fmt.Sprintf("manifest.json network_capture.packet_count %d is negative", meta.PacketCount))
	}
	wantStatus := tracepolicy.NetworkCaptureStatusComplete
	if meta.Truncated {
		wantStatus = tracepolicy.NetworkCaptureStatusTruncated
	}
	if meta.Status != wantStatus {
		issues = append(issues, fmt.Sprintf("manifest.json network_capture.status %q is inconsistent with truncated=%t", meta.Status, meta.Truncated))
	}

	if stream != nil {
		if meta.CapturedBytes >= 0 && uint64(meta.CapturedBytes) != stream.UncompressedSize64 {
			issues = append(issues, fmt.Sprintf("manifest.json network_capture.captured_bytes %d does not match stream size %d", meta.CapturedBytes, stream.UncompressedSize64))
		}
		info, err := ValidatePCAP(stream)
		if err != nil {
			issues = append(issues, tracepolicy.NetworkCaptureStream+": "+err.Error())
		} else if meta.PacketCount >= 0 && meta.PacketCount != info.Packets {
			issues = append(issues, fmt.Sprintf("manifest.json network_capture.packet_count %d does not match stream packet count %d", meta.PacketCount, info.Packets))
		}
	}
	return issues
}
