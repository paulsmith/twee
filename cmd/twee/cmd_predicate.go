package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("assert", runAssert)
	registerUsage("assert", `twee assert <cell|region> ...
Subverbs:
  assert cell --x N --y N <predicate flags> [--name <name>]
  assert region [--x N --y N --w N --h N] [--match any|all] <predicate flags> [--name <name>]

Assertions evaluate the current viewport once. A mismatch exits nonzero with
code ASSERTION_FAILED. Region coordinates default to the whole viewport.`)
	registerUsage("assert cell", `twee assert cell --x N --y N <predicate flags> [--name <name>]
Assert that one physical terminal cell matches every supplied predicate.
Continuation cells of wide glyphs have --text= and --width 0.`+predicateHelp(false))
	registerUsage("assert region", `twee assert region [--x N --y N --w N --h N] [--match any|all] <predicate flags> [--name <name>]
Assert that cells in a rectangle match the predicate. Coordinates default to
the whole viewport. --match defaults to any; empty clipped regions fail.
--contains-style key=value may be repeated as shorthand for style/color flags,
for example: twee assert region --contains-style fg=palette:1`+predicateHelp(true))
	registerUsage("wait cell", `twee wait cell --x N --y N <predicate flags> [--timeout <dur>] [--name <name>]
Wait until one physical terminal cell matches every supplied predicate.
An initially out-of-bounds coordinate remains pending across resizes.`+predicateHelp(false))
}

func predicateHelp(contains bool) string {
	extra := ""
	if contains {
		extra = "\n  --contains-style key=value  repeatable style/color shorthand"
	}
	return `

Predicate flags (at least one required):
  --text TEXT                 exact cell grapheme; --text= matches empty text
  --width 0|1|2               continuation, narrow, or wide-leading cell
  --fg COLOR                  default, palette:N, #RRGGBB, or rgb:R,G,B
  --bg COLOR                  same forms as --fg
  --bold[=BOOL]               style constraints; bare flag means true
  --dim[=BOOL]
  --italic[=BOOL]
  --underline[=BOOL]
  --inverse[=BOOL]
  --strikethrough[=BOOL]` + extra
}

func runAssert(args []string) {
	if len(args) == 0 {
		fatalUsage("assert: missing subverb (cell|region)")
	}
	switch args[0] {
	case "cell":
		runAssertCell(args[1:])
	case "region":
		runAssertRegion(args[1:])
	default:
		fatalUsage("assert: unknown subverb %q", args[0])
	}
}

func runWaitCell(args []string) {
	if err := rejectDuplicateFlags(args, "--x", "--y"); err != nil {
		fatalUsage("wait cell: %v", err)
	}
	remaining, predicate, err := extractCellPredicateArgs(args, false)
	if err != nil {
		fatalUsage("wait cell: %v", err)
	}
	var opts struct {
		Name    *string `arg:"--name"`
		Timeout string  `arg:"--timeout"`
		X       *int    `arg:"--x,required"`
		Y       *int    `arg:"--y,required"`
	}
	if err := parseArg("wait cell", &opts, remaining); err != nil {
		fatalUsage("wait cell: %v", err)
	}
	if cellPredicateEmpty(predicate) {
		fatalUsage("wait cell: at least one cell predicate is required")
	}
	if *opts.X < 0 || *opts.Y < 0 {
		fatalUsage("wait cell: x and y must be >= 0")
	}
	callSessionAndEmit("wait cell", opts.Name, rpc.OpWaitCell, rpc.WaitCellArgs{
		X: opts.X, Y: opts.Y, Predicate: predicate, Timeout: opts.Timeout,
	})
}

func runAssertCell(args []string) {
	if err := rejectDuplicateFlags(args, "--x", "--y"); err != nil {
		fatalUsage("assert cell: %v", err)
	}
	remaining, predicate, err := extractCellPredicateArgs(args, false)
	if err != nil {
		fatalUsage("assert cell: %v", err)
	}
	var opts struct {
		Name *string `arg:"--name"`
		X    *int    `arg:"--x,required"`
		Y    *int    `arg:"--y,required"`
	}
	if err := parseArg("assert cell", &opts, remaining); err != nil {
		fatalUsage("assert cell: %v", err)
	}
	if cellPredicateEmpty(predicate) {
		fatalUsage("assert cell: at least one cell predicate is required")
	}
	if *opts.X < 0 || *opts.Y < 0 {
		fatalUsage("assert cell: x and y must be >= 0")
	}
	callSessionAndEmit("assert cell", opts.Name, rpc.OpAssertCell, rpc.AssertCellArgs{
		X: opts.X, Y: opts.Y, Predicate: predicate,
	})
}

func runAssertRegion(args []string) {
	if err := rejectDuplicateFlags(args, "--x", "--y", "--w", "--h", "--match"); err != nil {
		fatalUsage("assert region: %v", err)
	}
	remaining, predicate, err := extractCellPredicateArgs(args, true)
	if err != nil {
		fatalUsage("assert region: %v", err)
	}
	var opts struct {
		Name  *string `arg:"--name"`
		X     *int    `arg:"--x"`
		Y     *int    `arg:"--y"`
		W     *int    `arg:"--w"`
		H     *int    `arg:"--h"`
		Match string  `arg:"--match"`
	}
	if err := parseArg("assert region", &opts, remaining); err != nil {
		fatalUsage("assert region: %v", err)
	}
	if cellPredicateEmpty(predicate) {
		fatalUsage("assert region: at least one cell predicate is required")
	}
	present := 0
	for _, value := range []*int{opts.X, opts.Y, opts.W, opts.H} {
		if value != nil {
			present++
		}
	}
	if present != 0 && present != 4 {
		fatalUsage("assert region: x, y, w, and h must be provided together")
	}
	if present == 4 && (*opts.X < 0 || *opts.Y < 0 || *opts.W <= 0 || *opts.H <= 0) {
		fatalUsage("assert region: x/y must be >= 0 and w/h must be > 0")
	}
	if opts.Match != "" && opts.Match != "any" && opts.Match != "all" {
		fatalUsage("assert region: --match must be any or all")
	}
	callSessionAndEmit("assert region", opts.Name, rpc.OpAssertRegion, rpc.AssertRegionArgs{
		X: opts.X, Y: opts.Y, W: opts.W, H: opts.H, Match: opts.Match, Predicate: predicate,
	})
}

func cellPredicateEmpty(predicate rpc.CellPredicate) bool {
	return predicate.Text == nil && predicate.Width == nil && predicate.Fg == nil && predicate.Bg == nil &&
		predicate.Bold == nil && predicate.Dim == nil && predicate.Italic == nil && predicate.Underline == nil &&
		predicate.Inverse == nil && predicate.Strikethrough == nil
}

func extractCellPredicateArgs(args []string, allowContainsStyle bool) ([]string, rpc.CellPredicate, error) {
	remaining := make([]string, 0, len(args))
	predicate := rpc.CellPredicate{}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			remaining = append(remaining, args[i:]...)
			break
		}
		name, inline, hasInline := strings.Cut(arg, "=")
		switch name {
		case "--text", "--width", "--fg", "--bg":
			value, consumed, err := predicateFlagValue(args, i, name, inline, hasInline, true)
			if err != nil {
				return nil, predicate, err
			}
			i += consumed
			if err := setPredicateValue(&predicate, seen, strings.TrimPrefix(name, "--"), value); err != nil {
				return nil, predicate, err
			}
		case "--bold", "--dim", "--italic", "--underline", "--inverse", "--strikethrough":
			value := "true"
			consumed := 0
			if hasInline {
				value = inline
			} else if i+1 < len(args) && (args[i+1] == "true" || args[i+1] == "false") {
				value, consumed = args[i+1], 1
			}
			i += consumed
			if err := setPredicateValue(&predicate, seen, strings.TrimPrefix(name, "--"), value); err != nil {
				return nil, predicate, err
			}
		case "--contains-style":
			if !allowContainsStyle {
				remaining = append(remaining, arg)
				continue
			}
			value, consumed, err := predicateFlagValue(args, i, name, inline, hasInline, false)
			if err != nil {
				return nil, predicate, err
			}
			i += consumed
			key, value, ok := strings.Cut(value, "=")
			if !ok {
				value = "true"
			}
			if err := setPredicateValue(&predicate, seen, key, value); err != nil {
				return nil, predicate, fmt.Errorf("--contains-style: %w", err)
			}
		default:
			remaining = append(remaining, arg)
		}
	}
	return remaining, predicate, nil
}

func predicateFlagValue(args []string, i int, name, inline string, hasInline, allowEmpty bool) (string, int, error) {
	if hasInline {
		if inline == "" && !allowEmpty {
			return "", 0, fmt.Errorf("%s requires a value", name)
		}
		return inline, 0, nil
	}
	if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
		return "", 0, fmt.Errorf("%s requires a value", name)
	}
	return args[i+1], 1, nil
}

func setPredicateValue(predicate *rpc.CellPredicate, seen map[string]bool, key, value string) error {
	if seen[key] {
		return fmt.Errorf("duplicate predicate %q", key)
	}
	seen[key] = true
	switch key {
	case "text":
		predicate.Text = &value
	case "width":
		width, err := strconv.Atoi(value)
		if err != nil || width < 0 || width > 2 {
			return fmt.Errorf("width must be 0, 1, or 2")
		}
		predicate.Width = &width
	case "fg", "bg":
		color, err := parsePredicateColor(value)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		if key == "fg" {
			predicate.Fg = color
		} else {
			predicate.Bg = color
		}
	case "bold", "dim", "italic", "underline", "inverse", "strikethrough":
		state, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%s must be true or false", key)
		}
		switch key {
		case "bold":
			predicate.Bold = &state
		case "dim":
			predicate.Dim = &state
		case "italic":
			predicate.Italic = &state
		case "underline":
			predicate.Underline = &state
		case "inverse":
			predicate.Inverse = &state
		case "strikethrough":
			predicate.Strikethrough = &state
		}
	default:
		return fmt.Errorf("unknown predicate %q", key)
	}
	return nil
}

func parsePredicateColor(value string) (*rpc.ColorPredicate, error) {
	if value == "default" {
		return &rpc.ColorPredicate{Kind: rpc.ColorKindDefault}, nil
	}
	if indexText, ok := strings.CutPrefix(value, "palette:"); ok {
		index, err := strconv.ParseUint(indexText, 10, 8)
		if err != nil {
			return nil, fmt.Errorf("palette index must be 0..255")
		}
		v := uint8(index)
		return &rpc.ColorPredicate{Kind: rpc.ColorKindPalette, Index: &v}, nil
	}
	if hex, ok := strings.CutPrefix(value, "#"); ok {
		if len(hex) != 6 {
			return nil, fmt.Errorf("RGB hex color must be #RRGGBB")
		}
		n, err := strconv.ParseUint(hex, 16, 24)
		if err != nil {
			return nil, fmt.Errorf("RGB hex color must be #RRGGBB")
		}
		r, g, b := uint8(n>>16), uint8(n>>8), uint8(n)
		return &rpc.ColorPredicate{Kind: rpc.ColorKindRGB, R: &r, G: &g, B: &b}, nil
	}
	if rgb, ok := strings.CutPrefix(value, "rgb:"); ok {
		parts := strings.Split(rgb, ",")
		if len(parts) != 3 {
			return nil, fmt.Errorf("RGB color must be rgb:R,G,B")
		}
		channels := [3]uint8{}
		for i, part := range parts {
			n, err := strconv.ParseUint(part, 10, 8)
			if err != nil {
				return nil, fmt.Errorf("RGB channels must be 0..255")
			}
			channels[i] = uint8(n)
		}
		return &rpc.ColorPredicate{Kind: rpc.ColorKindRGB, R: &channels[0], G: &channels[1], B: &channels[2]}, nil
	}
	return nil, fmt.Errorf("color must be default, palette:N, #RRGGBB, or rgb:R,G,B")
}
