package rpc

// Op names. Multi-word verbs join with underscore; sub-verbs (trace start /
// wait text) become a single op name.
const (
	// Input
	OpType      = "type"
	OpKey       = "key"
	OpPaste     = "paste"
	OpClick     = "click"
	OpFindClick = "find_click"
	OpHover     = "hover"
	OpScroll    = "scroll"
	OpDrag      = "drag"
	OpSignal    = "signal"

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
	OpTraceMark  = "trace_mark"
	OpTraceStop  = "trace_stop"
	OpDiff       = "diff"

	// Waits
	OpWaitText   = "wait_text"
	OpWaitNoText = "wait_no_text"
	OpWaitStable = "wait_stable"
	OpWaitCell   = "wait_cell"
	OpWaitCursor = "wait_cursor"
	OpWaitExit   = "wait_exit"

	// Assertions
	OpAssertCell   = "assert_cell"
	OpAssertRegion = "assert_region"

	// Misc
	OpSleep = "sleep"

	// Lifecycle
	OpStop   = "stop"
	OpStatus = "status"
)
