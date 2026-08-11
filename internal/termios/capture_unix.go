//go:build linux || darwin

package termios

import "golang.org/x/sys/unix"

// Capture reads terminal attributes from fd.
func Capture(fd uintptr) Snapshot {
	attrs, err := unix.IoctlGetTermios(int(fd), ioctlReadTermios)
	if err != nil {
		return Snapshot{Status: StatusUnavailable, Error: err.Error()}
	}
	cc := make([]int, len(attrs.Cc))
	for i, value := range attrs.Cc {
		cc[i] = int(value)
	}
	return Snapshot{Status: StatusCaptured, State: &State{
		Canonical:         attrs.Lflag&unix.ICANON != 0,
		Echo:              attrs.Lflag&unix.ECHO != 0,
		Signals:           attrs.Lflag&unix.ISIG != 0,
		ExtendedInput:     attrs.Lflag&unix.IEXTEN != 0,
		InputFlowControl:  attrs.Iflag&unix.IXOFF != 0,
		OutputFlowControl: attrs.Iflag&unix.IXON != 0,
		OutputProcessing:  attrs.Oflag&unix.OPOST != 0,
		MapNLToCRNL:       attrs.Oflag&unix.ONLCR != 0,
		Raw: Raw{
			InputFlags: uint64(attrs.Iflag), OutputFlags: uint64(attrs.Oflag),
			ControlFlags: uint64(attrs.Cflag), LocalFlags: uint64(attrs.Lflag),
			ControlChars: cc, InputSpeed: uint64(attrs.Ispeed), OutputSpeed: uint64(attrs.Ospeed),
		},
	}}
}
