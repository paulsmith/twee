package main

import (
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
Active VT modes: {decckm, application_keypad, bracketed_paste, focus_events,
kitty_keyboard_known, kitty_keyboard_flags, alt_screen, mouse, mouse_known,
mouse_raw, ...}. mouse is authoritative only when mouse_known is true;
mouse_raw and the individual mouse fields retain backend mode bits.
bracketed_paste is the same modeled DEC mode 2004 state that gates an
unforced "twee paste". A nonzero Kitty flag value means named key input is
currently unsupported.`)
	registerUsage("scrollback", `twee scrollback [--name <name>]
Scrollback lines. Retention is not yet implemented; this currently
always returns an empty list.`)
	registerUsage("snapshot", `twee snapshot [--name <name>]
Full snapshot: {size, cursor, lines: [{cells: [Cell]}]}.`)
	registerUsage("cell", `twee cell --x <n> --y <n> [--name <name>]
Single cell at (x, y): {text, width, fg, bg, bold, dim, italic,
underline, inverse, strikethrough}. fg/bg are {"kind":"default"},
{"kind":"palette","index":N}, or {"kind":"rgb","r":N,"g":N,"b":N}.`)
	registerUsage("region", `twee region --x <n> --y <n> --w <n> --h <n> [--name <name>]
Rectangle of cells at (x, y) with width w and height h: an array of
rows, each an array of cell objects shaped like "cell"'s output.`)
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
	callSessionAndEmit(verb, opts.Name, op, nil)
}

func runCell(args []string) {
	if err := rejectDuplicateFlags(args, "--x", "--y"); err != nil {
		fatalUsage("cell: %v", err)
	}
	var opts struct {
		Name *string `arg:"--name"`
		X    int     `arg:"--x,required"`
		Y    int     `arg:"--y,required"`
	}
	if err := parseArg("cell", &opts, args); err != nil {
		fatalUsage("cell: %v", err)
	}
	callSessionAndEmit("cell", opts.Name, rpc.OpCell, rpc.CellArgs{X: opts.X, Y: opts.Y})
}

func runRegion(args []string) {
	if err := rejectDuplicateFlags(args, "--x", "--y", "--w", "--h"); err != nil {
		fatalUsage("region: %v", err)
	}
	var opts struct {
		Name *string `arg:"--name"`
		X    int     `arg:"--x,required"`
		Y    int     `arg:"--y,required"`
		W    int     `arg:"--w,required"`
		H    int     `arg:"--h,required"`
	}
	if err := parseArg("region", &opts, args); err != nil {
		fatalUsage("region: %v", err)
	}
	callSessionAndEmit("region", opts.Name, rpc.OpRegion, rpc.RegionArgs{X: opts.X, Y: opts.Y, W: opts.W, H: opts.H})
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
	callSessionAndEmit("find", opts.Name, rpc.OpFind, rpc.FindArgs{Text: opts.Pattern, Regex: opts.Regex})
}
