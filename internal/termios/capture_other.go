//go:build !linux && !darwin

package termios

// Capture reports that termios capture is unavailable on this platform.
func Capture(uintptr) Snapshot {
	return Snapshot{Status: StatusUnsupported, Error: "unsupported platform"}
}
