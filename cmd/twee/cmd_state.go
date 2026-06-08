package main

import (
	"flag"
	"strconv"
	"time"

	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("resize", runResize)
	register("sleep", runSleep)
	register("screenshot", runScreenshot)

	registerUsage("resize", `twee resize <cols> <rows> [-name <name>]
TIOCSWINSZ + SIGWINCH + model resize.`)
	registerUsage("sleep", `twee sleep <duration>
Client-side sleep (e.g. "200ms", "1s"). Emits an empty OK envelope.`)
	registerUsage("screenshot", `twee screenshot [-out <path.png>] [-name <name>]
Render the current screen to PNG. Without -out, the response includes
"png_base64". Synthetic bold; emoji cells render as the leftmost glyph
plus a space.`)
}

func runScreenshot(args []string) {
	fs := flag.NewFlagSet("screenshot", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	out := fs.String("out", "", "output PNG path; if empty, response includes png_base64")
	if err := fs.Parse(args); err != nil {
		fatalUsage("screenshot: %v", err)
	}
	pixelWidth, pixelHeight := nativeDisplayPixels()
	callAndEmit(*name, rpc.OpScreenshot, rpc.ScreenshotArgs{
		Out:         *out,
		PixelWidth:  pixelWidth,
		PixelHeight: pixelHeight,
	})
}

func runResize(args []string) {
	fs := flag.NewFlagSet("resize", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	if err := fs.Parse(args); err != nil {
		fatalUsage("resize: %v", err)
	}
	rest := fs.Args()
	if len(rest) != 2 {
		fatalUsage("resize: expected cols rows")
	}
	cols, err1 := strconv.Atoi(rest[0])
	rows, err2 := strconv.Atoi(rest[1])
	if err1 != nil || err2 != nil {
		fatalUsage("resize: cols and rows must be integers")
	}
	callAndEmit(*name, rpc.OpResize, rpc.ResizeArgs{Cols: cols, Rows: rows})
}

// runSleep is client-side: sleeps locally and emits an OK envelope.
func runSleep(args []string) {
	fs := flag.NewFlagSet("sleep", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		fatalUsage("sleep: %v", err)
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fatalUsage("sleep: expected one duration")
	}
	d, err := time.ParseDuration(rest[0])
	if err != nil {
		fatalUsage("sleep: %v", err)
	}
	time.Sleep(d)
	emitOK(nil)
}
