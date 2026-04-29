package vt

// Model is the backend interface. Implementations must be safe for use
// from a single goroutine; the harness pump serializes access externally.
type Model interface {
	Feed(p []byte) error
	Resize(cols, rows int) error
	Snapshot() Snapshot
}

// New returns the libghostty-vt-backed Model. The underlying type is
// unexported to keep the backend swappable.
func New(cols, rows int) Model {
	return newGhosttyTerm(cols, rows)
}
