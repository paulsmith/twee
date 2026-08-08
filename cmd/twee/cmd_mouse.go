package main

import (
	"fmt"

	"github.com/paulsmith/twee/internal/input"
	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("click", runClick)
	register("hover", runHover)
	register("scroll", runScroll)
	register("drag", runDrag)

	registerUsage("click", `twee click --x <n> --y <n> [--button left|middle|right]
    [--modifier shift|alt|ctrl]... [--name <name>]
Click a zero-based viewport cell. The button defaults to left.
--modifier is repeatable; duplicate modifiers are rejected.`)

	registerUsage("hover", `twee hover --x <n> --y <n>
    [--modifier shift|alt|ctrl]... [--name <name>]
Move the mouse to a zero-based viewport cell without a pressed button.
The child must have enabled any-event mouse tracking (mode 1003).`)

	registerUsage("scroll", `twee scroll --x <n> --y <n> --direction up|down
    [--ticks <n>] [--modifier shift|alt|ctrl]... [--name <name>]
Send vertical wheel input at a zero-based viewport cell. Ticks defaults
to 1 and must be between 1 and 100. This does not scroll twee history.`)

	registerUsage("drag", `twee drag --from-x <n> --from-y <n> --to-x <n> --to-y <n>
    [--button left|middle|right] [--modifier shift|alt|ctrl]...
    [--name <name>]
Drag cell by cell between two zero-based viewport coordinates. The button
defaults to left. A zero-length drag is a click.`)
}

func runClick(args []string) {
	name, rpcArgs, err := parseClickArgs(args)
	if err != nil {
		fatalUsage("click: %v", err)
	}
	callSessionAndEmit("click", name, rpc.OpClick, rpcArgs)
}

func runHover(args []string) {
	name, rpcArgs, err := parseHoverArgs(args)
	if err != nil {
		fatalUsage("hover: %v", err)
	}
	callSessionAndEmit("hover", name, rpc.OpHover, rpcArgs)
}

func runScroll(args []string) {
	name, rpcArgs, err := parseScrollArgs(args)
	if err != nil {
		fatalUsage("scroll: %v", err)
	}
	callSessionAndEmit("scroll", name, rpc.OpScroll, rpcArgs)
}

func runDrag(args []string) {
	name, rpcArgs, err := parseDragArgs(args)
	if err != nil {
		fatalUsage("drag: %v", err)
	}
	callSessionAndEmit("drag", name, rpc.OpDrag, rpcArgs)
}

func parseClickArgs(args []string) (*string, rpc.ClickArgs, error) {
	if err := rejectDuplicateFlags(args, "--x", "--y", "--button"); err != nil {
		return nil, rpc.ClickArgs{}, err
	}
	var opts struct {
		Name      *string  `arg:"--name"`
		X         int      `arg:"--x,required"`
		Y         int      `arg:"--y,required"`
		Button    *string  `arg:"--button"`
		Modifiers []string `arg:"--modifier,separate"`
	}
	if err := parseArg("click", &opts, args); err != nil {
		return nil, rpc.ClickArgs{}, err
	}
	button, err := parseMouseButton(opts.Button)
	if err != nil {
		return nil, rpc.ClickArgs{}, err
	}
	modifiers, err := parseMouseModifiers(opts.Modifiers)
	if err != nil {
		return nil, rpc.ClickArgs{}, err
	}
	return opts.Name, rpc.ClickArgs{
		X: intPointer(opts.X), Y: intPointer(opts.Y),
		Button: button, Modifiers: modifiers,
	}, nil
}

func parseHoverArgs(args []string) (*string, rpc.HoverArgs, error) {
	if err := rejectDuplicateFlags(args, "--x", "--y"); err != nil {
		return nil, rpc.HoverArgs{}, err
	}
	var opts struct {
		Name      *string  `arg:"--name"`
		X         int      `arg:"--x,required"`
		Y         int      `arg:"--y,required"`
		Modifiers []string `arg:"--modifier,separate"`
	}
	if err := parseArg("hover", &opts, args); err != nil {
		return nil, rpc.HoverArgs{}, err
	}
	modifiers, err := parseMouseModifiers(opts.Modifiers)
	if err != nil {
		return nil, rpc.HoverArgs{}, err
	}
	return opts.Name, rpc.HoverArgs{
		X: intPointer(opts.X), Y: intPointer(opts.Y), Modifiers: modifiers,
	}, nil
}

func parseScrollArgs(args []string) (*string, rpc.ScrollArgs, error) {
	if err := rejectDuplicateFlags(args, "--x", "--y", "--direction", "--ticks"); err != nil {
		return nil, rpc.ScrollArgs{}, err
	}
	var opts struct {
		Name      *string  `arg:"--name"`
		X         int      `arg:"--x,required"`
		Y         int      `arg:"--y,required"`
		Direction string   `arg:"--direction,required"`
		Ticks     *int     `arg:"--ticks"`
		Modifiers []string `arg:"--modifier,separate"`
	}
	if err := parseArg("scroll", &opts, args); err != nil {
		return nil, rpc.ScrollArgs{}, err
	}
	direction, err := input.ParseScrollDirection(opts.Direction)
	if err != nil || direction.String() != opts.Direction {
		return nil, rpc.ScrollArgs{}, fmt.Errorf("--direction must be up or down")
	}
	if opts.Ticks != nil && (*opts.Ticks <= 0 || *opts.Ticks > input.MaxScrollTicks) {
		return nil, rpc.ScrollArgs{}, fmt.Errorf("--ticks must be between 1 and %d", input.MaxScrollTicks)
	}
	modifiers, err := parseMouseModifiers(opts.Modifiers)
	if err != nil {
		return nil, rpc.ScrollArgs{}, err
	}
	return opts.Name, rpc.ScrollArgs{
		X: intPointer(opts.X), Y: intPointer(opts.Y),
		Direction: direction.String(), Ticks: opts.Ticks, Modifiers: modifiers,
	}, nil
}

func parseDragArgs(args []string) (*string, rpc.DragArgs, error) {
	if err := rejectDuplicateFlags(args,
		"--from-x", "--from-y", "--to-x", "--to-y", "--button",
	); err != nil {
		return nil, rpc.DragArgs{}, err
	}
	var opts struct {
		Name      *string  `arg:"--name"`
		FromX     int      `arg:"--from-x,required"`
		FromY     int      `arg:"--from-y,required"`
		ToX       int      `arg:"--to-x,required"`
		ToY       int      `arg:"--to-y,required"`
		Button    *string  `arg:"--button"`
		Modifiers []string `arg:"--modifier,separate"`
	}
	if err := parseArg("drag", &opts, args); err != nil {
		return nil, rpc.DragArgs{}, err
	}
	button, err := parseMouseButton(opts.Button)
	if err != nil {
		return nil, rpc.DragArgs{}, err
	}
	modifiers, err := parseMouseModifiers(opts.Modifiers)
	if err != nil {
		return nil, rpc.DragArgs{}, err
	}
	return opts.Name, rpc.DragArgs{
		FromX: intPointer(opts.FromX), FromY: intPointer(opts.FromY),
		ToX: intPointer(opts.ToX), ToY: intPointer(opts.ToY),
		Button: button, Modifiers: modifiers,
	}, nil
}

func parseMouseButton(value *string) (string, error) {
	if value == nil {
		return "", nil // Omitted on the wire; the daemon defaults to left.
	}
	button, err := input.ParseMouseButton(*value)
	if err != nil || button.String() != *value {
		return "", fmt.Errorf("--button must be left, middle, or right")
	}
	return button.String(), nil
}

func parseMouseModifiers(values []string) ([]string, error) {
	parsed := make([]input.MouseModifier, 0, len(values))
	modifiers := make([]string, 0, len(values))
	for _, value := range values {
		modifier, err := input.ParseMouseModifier(value)
		if err != nil || modifier.String() != value {
			return nil, fmt.Errorf("unknown --modifier %q (want shift, alt, or ctrl)", value)
		}
		parsed = append(parsed, modifier)
		if _, err := input.NormalizeMouseModifiers(parsed); err != nil {
			return nil, fmt.Errorf("duplicate --modifier %q", value)
		}
		modifiers = append(modifiers, modifier.String())
	}
	return modifiers, nil
}

func intPointer(value int) *int { return &value }
