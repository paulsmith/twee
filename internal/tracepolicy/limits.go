// Package tracepolicy defines resource limits shared by trace bundle readers.
package tracepolicy

const (
	NetworkCaptureFormat = "pcap"
	NetworkCaptureStream = "streams/network.pcap"

	NetworkCaptureStatusComplete  = "complete"
	NetworkCaptureStatusTruncated = "truncated"
)

// These conservative pre-release limits admit long recordings while bounding
// decompression and retained memory from untrusted bundles.
const (
	MaxArchiveEntries           = 1024
	MaxEntryNameBytes           = 255
	MaxArchiveEntryNameBytes    = 64 * 1024
	MaxManifestBytes            = 1 * 1024 * 1024
	MaxEventsBytes              = 64 * 1024 * 1024
	MaxEventLineBytes           = 1 * 1024 * 1024
	MaxEventCount               = 1_000_000
	MaxDecodedPayloadBytes      = 32 * 1024 * 1024
	MaxModeTransitions          = 10_000
	MaxTerminalCells            = 100_000
	MaxInspectReplayBytes       = 16 * 1024 * 1024
	MaxNetworkCaptureBytes      = 64 * 1024 * 1024
	MaxArchiveUncompressedBytes = MaxManifestBytes + MaxEventsBytes + MaxNetworkCaptureBytes
)
