package rpc

// Error codes. Closed set; agents may branch on these.
const (
	CodeTimeout            = "TIMEOUT"
	CodeNotFound           = "NOT_FOUND"
	CodeAmbiguousMatch     = "AMBIGUOUS_MATCH"
	CodeInvalidSelection   = "INVALID_SELECTION"
	CodeAlreadyRunning     = "ALREADY_RUNNING"
	CodeChildExited        = "CHILD_EXITED"
	CodeInvalidArgument    = "INVALID_ARGUMENT"
	CodeFailedPrecondition = "FAILED_PRECONDITION"
	CodeAssertionFailed    = "ASSERTION_FAILED"
	CodeIO                 = "IO"
	CodeInternal           = "INTERNAL"
	// CodeSessionEnded marks a wait (text / no-text / cell / cursor) that was
	// still pending when the session ended — child exited, or `twee
	// stop` — rather than its deadline firing. It carries the same
	// details.cause/details.last_screen as CodeTimeout. `wait exit` never
	// uses this code: the session ending is its success path, not a
	// failure. `wait stable` doesn't currently use it either — see
	// engine.IsSessionEnded's doc comment.
	CodeSessionEnded = "SESSION_ENDED"
)
