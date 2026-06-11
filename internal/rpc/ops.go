package rpc

// Op names. Multi-word verbs join with underscore; sub-verbs (trace start /
// wait text) become a single op name.
const (
	// Input
	OpType   = "type"
	OpKey    = "key"
	OpPaste  = "paste"
	OpSignal = "signal"

	// Queries
	OpText       = "text"
	OpLines      = "lines"
	OpCell       = "cell"
	OpRegion     = "region"
	OpCursor     = "cursor"
	OpFind       = "find"
	OpSize       = "size"
	OpTitle      = "title"
	OpMode       = "mode"
	OpScrollback = "scrollback"
	OpSnapshot   = "snapshot"

	// State changes
	OpResize     = "resize"
	OpScreenshot = "screenshot"
	OpTraceStart = "trace_start"
	OpTraceStop  = "trace_stop"
	OpDiff       = "diff"

	// Waits
	OpWaitText   = "wait_text"
	OpWaitNoText = "wait_no_text"
	OpWaitStable = "wait_stable"
	OpWaitCursor = "wait_cursor"
	OpWaitExit   = "wait_exit"

	// Misc
	OpSleep = "sleep"

	// Lifecycle
	OpStop   = "stop"
	OpStatus = "status"
)
