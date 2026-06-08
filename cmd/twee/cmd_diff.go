package main

import (
	"flag"

	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("diff", runDiff)
	registerUsage("diff", `twee diff -against <path> [-name <name>]
Text diff between the current visible viewport and a saved text-snapshot
file. Always exits 0; branch on data.equal. Returns:
  {equal: bool, unified: string, current: string, expected: string}
where "unified" is a 3-line-context unified diff.`)
}

func runDiff(args []string) {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	against := fs.String("against", "", "path to expected snapshot file")
	if err := fs.Parse(args); err != nil {
		fatalUsage("diff: %v", err)
	}
	if *against == "" {
		fatalUsage("diff: --against is required")
	}
	callAndEmit(*name, rpc.OpDiff, rpc.DiffArgs{Against: *against})
}
