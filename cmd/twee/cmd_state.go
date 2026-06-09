package main

import (
	"time"

	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	register("resize", runResize)
	register("sleep", runSleep)
	register("screenshot", runScreenshot)

	registerUsage("resize", `twee resize <cols> <rows> [--name <name>]
TIOCSWINSZ + SIGWINCH + model resize.`)
	registerUsage("sleep", `twee sleep <duration>
Client-side sleep (e.g. "200ms", "1s"). Emits an empty OK envelope.`)
	registerUsage("screenshot", `twee screenshot [--out <path.png>] [--name <name>]
Render the current screen to PNG. Without --out, the response includes
"png_base64". Synthetic bold; emoji cells render as the leftmost glyph
plus a space.`)
}

func runScreenshot(args []string) {
	var opts struct {
		Name *string `arg:"--name"`
		Out  string  `arg:"--out"`
	}
	if err := parseArg("screenshot", &opts, args); err != nil {
		fatalUsage("screenshot: %v", err)
	}
	pixelWidth, pixelHeight := nativeDisplayPixels()
	callAndEmit(mustCurrentSessionName("screenshot", nameOptFromPtr(opts.Name)), rpc.OpScreenshot, rpc.ScreenshotArgs{
		Out:         opts.Out,
		PixelWidth:  pixelWidth,
		PixelHeight: pixelHeight,
	})
}

func runResize(args []string) {
	var opts struct {
		Name *string `arg:"--name"`
		Cols int     `arg:"positional,required"`
		Rows int     `arg:"positional,required"`
	}
	if err := parseArg("resize", &opts, args); err != nil {
		fatalUsage("resize: %v", err)
	}
	callAndEmit(mustCurrentSessionName("resize", nameOptFromPtr(opts.Name)), rpc.OpResize, rpc.ResizeArgs{Cols: opts.Cols, Rows: opts.Rows})
}

// runSleep is client-side: sleeps locally and emits an OK envelope.
func runSleep(args []string) {
	var opts struct {
		Duration string `arg:"positional,required"`
	}
	if err := parseArg("sleep", &opts, args); err != nil {
		fatalUsage("sleep: %v", err)
	}
	d, err := time.ParseDuration(opts.Duration)
	if err != nil {
		fatalUsage("sleep: %v", err)
	}
	time.Sleep(d)
	emitOK(nil)
}
