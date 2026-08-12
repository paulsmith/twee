package play

import "io"

func readCommands(r io.Reader, out chan<- command) {
	defer close(out)
	var b [1]byte
	for {
		n, err := r.Read(b[:])
		if n > 0 {
			switch b[0] {
			case ' ':
				out <- cmdPause
			case '.':
				out <- cmdStep
			case '>':
				out <- cmdFwd1s
			case '[':
				out <- cmdPreviousMarker
			case ']':
				out <- cmdNextMarker
			case 'm':
				out <- cmdListMarkers
			case 'r':
				out <- cmdRestart
			case 'h':
				out <- cmdToggleStatus
			case '-':
				out <- cmdSlower
			case '+':
				out <- cmdFaster
			case 'q', 0x03:
				out <- cmdQuit
				return
			}
		}
		if err != nil {
			return
		}
	}
}
