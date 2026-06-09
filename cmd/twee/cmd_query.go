package main

import (
	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	register("text", func(args []string) { runQuery("text", rpc.OpText, args) })
	register("lines", func(args []string) { runQuery("lines", rpc.OpLines, args) })
	register("cursor", func(args []string) { runQuery("cursor", rpc.OpCursor, args) })
	register("size", func(args []string) { runQuery("size", rpc.OpSize, args) })
	register("title", func(args []string) { runQuery("title", rpc.OpTitle, args) })
	register("mode", func(args []string) { runQuery("mode", rpc.OpMode, args) })
	register("scrollback", func(args []string) { runQuery("scrollback", rpc.OpScrollback, args) })
	register("snapshot", func(args []string) { runQuery("snapshot", rpc.OpSnapshot, args) })
	register("cell", runCell)
	register("region", runRegion)
	register("find", runFind)

	registerUsage("text", `twee text [--name <name>]
Print the visible viewport as one string under data.text. Pipe through
"jq -r .data.text" to render the rendered screen.`)
	registerUsage("lines", `twee lines [--name <name>]
Visible viewport as a string array under data.lines.`)
	registerUsage("cursor", `twee cursor [--name <name>]
Cursor state: {x, y, visible, shape}.`)
	registerUsage("size", `twee size [--name <name>]
Terminal dimensions: {cols, rows}.`)
	registerUsage("title", `twee title [--name <name>]
Window title (OSC 0/2): {title}.`)
	registerUsage("mode", `twee mode [--name <name>]
Active VT modes: {decckm, bracketed_paste, alt_screen, mouse, ...}.`)
	registerUsage("scrollback", `twee scrollback [--name <name>]
Scrollback lines (only if retention is enabled at start).`)
	registerUsage("snapshot", `twee snapshot [--name <name>]
Full snapshot: {size, cursor, lines: [{cells: [Cell]}]}.`)
	registerUsage("cell", `twee cell <x> <y> [--name <name>]
Single cell at (x, y) with style.`)
	registerUsage("region", `twee region <x> <y> <w> <h> [--name <name>]
Rectangle of cells at (x, y) with width w and height h.`)
	registerUsage("find", `twee find --pattern TEXT [--regex] [--name <name>]
Find matches in the visible viewport. Returns an array of
{x, y, w, h, line, text} matches.`)
}

func runQuery(verb, op string, args []string) {
	var opts struct {
		Name *string `arg:"--name"`
	}
	if err := parseArg(verb, &opts, args); err != nil {
		fatalUsage("%s: %v", verb, err)
	}
	callAndEmit(mustCurrentSessionName(verb, nameOptFromPtr(opts.Name)), op, nil)
}

func runCell(args []string) {
	var opts struct {
		Name *string `arg:"--name"`
		X    int     `arg:"positional,required"`
		Y    int     `arg:"positional,required"`
	}
	if err := parseArg("cell", &opts, args); err != nil {
		fatalUsage("cell: %v", err)
	}
	callAndEmit(mustCurrentSessionName("cell", nameOptFromPtr(opts.Name)), rpc.OpCell, rpc.CellArgs{X: opts.X, Y: opts.Y})
}

func runRegion(args []string) {
	var opts struct {
		Name *string `arg:"--name"`
		X    int     `arg:"positional,required"`
		Y    int     `arg:"positional,required"`
		W    int     `arg:"positional,required"`
		H    int     `arg:"positional,required"`
	}
	if err := parseArg("region", &opts, args); err != nil {
		fatalUsage("region: %v", err)
	}
	callAndEmit(mustCurrentSessionName("region", nameOptFromPtr(opts.Name)), rpc.OpRegion, rpc.RegionArgs{X: opts.X, Y: opts.Y, W: opts.W, H: opts.H})
}

func runFind(args []string) {
	var opts struct {
		Name    *string `arg:"--name"`
		Pattern string  `arg:"--pattern,required"`
		Regex   bool    `arg:"--regex"`
	}
	if err := parseArg("find", &opts, args); err != nil {
		fatalUsage("find: %v", err)
	}
	callAndEmit(mustCurrentSessionName("find", nameOptFromPtr(opts.Name)), rpc.OpFind, rpc.FindArgs{Text: opts.Pattern, Regex: opts.Regex})
}
