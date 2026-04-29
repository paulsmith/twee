package rpc

// Error codes. Closed set; agents may branch on these.
const (
	CodeTimeout         = "TIMEOUT"
	CodeNotFound        = "NOT_FOUND"
	CodeAlreadyRunning  = "ALREADY_RUNNING"
	CodeChildExited     = "CHILD_EXITED"
	CodeInvalidArgument = "INVALID_ARGUMENT"
	CodeIO              = "IO"
	CodeInternal        = "INTERNAL"
)
