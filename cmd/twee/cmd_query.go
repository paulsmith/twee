package main

import (
	"flag"
	"strconv"

	"github.com/paulsmith/twee/internal/rpc"
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

	registerUsage("text", `twee text [-name <name>]
Print the visible viewport as one string under data.text. Pipe through
"jq -r .data.text" to render the rendered screen.`)
	registerUsage("lines", `twee lines [-name <name>]
Visible viewport as a string array under data.lines.`)
	registerUsage("cursor", `twee cursor [-name <name>]
Cursor state: {x, y, visible, shape}.`)
	registerUsage("size", `twee size [-name <name>]
Terminal dimensions: {cols, rows}.`)
	registerUsage("title", `twee title [-name <name>]
Window title (OSC 0/2): {title}.`)
	registerUsage("mode", `twee mode [-name <name>]
Active VT modes: {decckm, bracketed_paste, alt_screen, mouse, ...}.`)
	registerUsage("scrollback", `twee scrollback [-name <name>]
Scrollback lines (only if retention is enabled at start).`)
	registerUsage("snapshot", `twee snapshot [-name <name>]
Full snapshot: {size, cursor, lines: [{cells: [Cell]}]}.`)
	registerUsage("cell", `twee cell <x> <y> [-name <name>]
Single cell at (x, y) with style.`)
	registerUsage("region", `twee region <x> <y> <w> <h> [-name <name>]
Rectangle of cells at (x, y) with width w and height h.`)
	registerUsage("find", `twee find <text> [-regex] [-name <name>]
Find matches in the visible viewport. Returns an array of
{x, y, w, h, line, text} matches.`)
}

func runQuery(verb, op string, args []string) {
	fs := flag.NewFlagSet(verb, flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	if err := fs.Parse(args); err != nil {
		fatalUsage("%s: %v", verb, err)
	}
	callAndEmit(*name, op, nil)
}

func runCell(args []string) {
	fs := flag.NewFlagSet("cell", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	if err := fs.Parse(args); err != nil {
		fatalUsage("cell: %v", err)
	}
	rest := fs.Args()
	if len(rest) != 2 {
		fatalUsage("cell: expected x y")
	}
	x, err1 := strconv.Atoi(rest[0])
	y, err2 := strconv.Atoi(rest[1])
	if err1 != nil || err2 != nil {
		fatalUsage("cell: x and y must be integers")
	}
	callAndEmit(*name, rpc.OpCell, rpc.CellArgs{X: x, Y: y})
}

func runRegion(args []string) {
	fs := flag.NewFlagSet("region", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	if err := fs.Parse(args); err != nil {
		fatalUsage("region: %v", err)
	}
	rest := fs.Args()
	if len(rest) != 4 {
		fatalUsage("region: expected x y w h")
	}
	vals := make([]int, 4)
	for i := range vals {
		v, err := strconv.Atoi(rest[i])
		if err != nil {
			fatalUsage("region: arg %d is not an integer", i)
		}
		vals[i] = v
	}
	callAndEmit(*name, rpc.OpRegion, rpc.RegionArgs{X: vals[0], Y: vals[1], W: vals[2], H: vals[3]})
}

func runFind(args []string) {
	fs := flag.NewFlagSet("find", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	regex := fs.Bool("regex", false, "treat text as a regular expression")
	if err := fs.Parse(args); err != nil {
		fatalUsage("find: %v", err)
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fatalUsage("find: expected one text argument")
	}
	callAndEmit(*name, rpc.OpFind, rpc.FindArgs{Text: rest[0], Regex: *regex})
}
