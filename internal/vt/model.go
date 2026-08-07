package vt

// Model is the backend interface. Implementations must be safe for use
// from a single goroutine; the harness pump serializes access externally.
type Model interface {
	Feed(p []byte) error
	Resize(cols, rows int) error
	Snapshot() Snapshot
}

// PTYReplySource is optionally implemented by models that generate terminal
// protocol replies while consuming output. Draining transfers ownership.
type PTYReplySource interface{ DrainPTYReplies() [][]byte }

// PresentationSource optionally reports the small subset of terminal state
// that must be reflected by a host terminal which is proxying user input.
// It deliberately excludes arbitrary child presentation state.
type PresentationSource interface{ Presentation() Presentation }

// New returns the libghostty-vt-backed Model. The underlying type is
// unexported to keep the backend swappable.
func New(cols, rows int) Model {
	return newGhosttyTerm(cols, rows)
}
