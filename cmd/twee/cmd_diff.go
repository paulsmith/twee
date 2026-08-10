package main

import (
	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("diff", runDiff)
	registerUsage("diff", `twee diff --against <path> [--name <name>]
Text diff between the current visible viewport and a saved text-snapshot
file. A completed comparison exits 0 whether equal or unequal; branch on
data.equal. Usage, file, session, and transport failures exit nonzero. Returns:
  {equal: bool, unified: string, current: string, expected: string}
where "unified" is a 3-line-context unified diff.`)
}

func runDiff(args []string) {
	var opts struct {
		Name    *string `arg:"--name"`
		Against string  `arg:"--against,required"`
	}
	if err := parseArg("diff", &opts, args); err != nil {
		fatalUsage("diff: %v", err)
	}
	against, err := absOutPath(opts.Against)
	if err != nil {
		fatalUsage("diff: %v", err)
	}
	callSessionAndEmit("diff", opts.Name, rpc.OpDiff, rpc.DiffArgs{Against: against})
}
