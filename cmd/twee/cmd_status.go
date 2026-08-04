package main

import (
	"time"

	"github.com/paulsmith/twee/internal/rpc"
)

// tombstoneStatusData is the "status" response shape for a session whose
// daemon is gone but left a tombstone behind. Distinct from the
// tombstone type itself (which is the on-disk shape) so the wire shape —
// which adds "running" and omits "signal" when inapplicable, matching
// rpc.StatusData's own field conventions — can evolve independently of
// what's persisted to disk.
type tombstoneStatusData struct {
	Name          string    `json:"name"`
	Running       bool      `json:"running"`
	Stopped       bool      `json:"stopped"`
	ExitCode      *int      `json:"exit_code"`
	Signal        string    `json:"signal,omitempty"`
	StoppedAt     time.Time `json:"stopped_at"`
	Command       []string  `json:"command"`
	TracePath     string    `json:"trace_path,omitempty"`
	ArtifactError string    `json:"artifact_error,omitempty"`
}

func init() {
	register("status", runStatus)
	registerUsage("status", `twee status [--name <name>]
Returns {cmd, cols, rows, started_at, running, exit_code}.
"running" is true while the child is alive; once it exits, "running"
becomes false and "exit_code" is populated.

If no daemon is reachable but a tombstone exists (the session ran to
completion, or was stopped, since this state dir was last cleared),
returns {"name", "running":false, "stopped", "exit_code", "signal",
"stopped_at", "command", "trace_path", "artifact_error"} instead of NOT_FOUND.
The final two fields are omitted when no trace was requested and when artifact
finalization succeeded. "signal" is present only when the child was terminated
by a signal rather than exiting normally.
A name with no daemon and no tombstone is still NOT_FOUND.`)
}

func runStatus(args []string) {
	var opts struct {
		Name *string `arg:"--name"`
	}
	if err := parseArg("status", &opts, args); err != nil {
		fatalUsage("status: %v", err)
	}
	name := mustResolveSessionNamePtr("status", opts.Name)
	if !daemonReachable(name) {
		if ts, ok := readTombstone(name); ok {
			emitOK(tombstoneStatusData{
				Name:          ts.Name,
				Running:       false,
				Stopped:       ts.Stopped,
				ExitCode:      ts.ExitCode,
				Signal:        ts.Signal,
				StoppedAt:     ts.StoppedAt,
				Command:       ts.Command,
				TracePath:     ts.TracePath,
				ArtifactError: ts.ArtifactError,
			})
			return
		}
	}
	callAndEmit(name, rpc.OpStatus, nil)
}
