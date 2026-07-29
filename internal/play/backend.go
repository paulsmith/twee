package play

import (
	"fmt"
	"io"
)

// Backend identifies a terminal graphics protocol used by play.
type Backend string

const (
	BackendAuto   Backend = "auto"
	BackendKitty  Backend = "kitty"
	BackendITerm2 Backend = "iterm2"
	BackendSixel  Backend = "sixel"
)

// ValidBackend reports whether b is a supported play backend name.
func ValidBackend(b Backend) bool {
	switch b {
	case BackendAuto, BackendKitty, BackendITerm2, BackendSixel:
		return true
	default:
		return false
	}
}

type playbackSink interface {
	frameSink
	io.Closer
}

func newFrameSink(backend Backend, w io.Writer, terminalCols int, pixels displayPixels) (playbackSink, error) {
	switch backend {
	case BackendKitty:
		return newKittySink(w, terminalCols), nil
	case BackendITerm2:
		return newITerm2Sink(w, terminalCols), nil
	case BackendSixel:
		if pixels.Width <= 0 || pixels.Height <= 0 {
			return nil, fmt.Errorf("twee play: sixel backend requires reliable terminal pixel geometry")
		}
		return newSixelSink(w, terminalCols), nil
	default:
		return nil, fmt.Errorf("twee play: unsupported backend %q", backend)
	}
}
