// Package bundle inspects .twee trace bundles for the "twee bundle info"
// and "twee bundle validate" CLI subverbs: manifest/event summaries and
// integrity checks, respectively, without opening a terminal or
// replaying the recording.
package bundle

// ErrKind classifies why Inspect or Validate could not even attempt to
// read a bundle — as opposed to Validate's own Issues, which describe
// problems found in a bundle that *was* readable. Callers such as the
// CLI map an ErrKind onto their own error-code scheme (e.g. rpc.CodeIO /
// rpc.CodeInvalidArgument) without needing to string-match error text.
type ErrKind int

const (
	// ErrIO means the path itself couldn't be read: missing file,
	// permission error, or similar filesystem-level problem.
	ErrIO ErrKind = iota
	// ErrInvalid means the file was readable but its content wasn't a
	// usable .twee bundle: not a zip, missing manifest.json/events.jsonl,
	// corrupt JSON, or an unsupported version.
	ErrInvalid
)

// LoadError wraps a bundle-loading failure with its ErrKind.
type LoadError struct {
	Kind ErrKind
	Err  error
}

func (e *LoadError) Error() string { return e.Err.Error() }
func (e *LoadError) Unwrap() error { return e.Err }
