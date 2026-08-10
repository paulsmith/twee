package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"

	"github.com/paulsmith/twee/internal/engine"
	tinput "github.com/paulsmith/twee/internal/input"
	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	optionalRegistrations = append(optionalRegistrations, func(d *Dispatcher) {
		d.Register(rpc.OpType, handleType)
		d.Register(rpc.OpKey, handleKey)
		d.Register(rpc.OpPaste, handlePaste)
		d.Register(rpc.OpClick, handleClick)
		d.Register(rpc.OpHover, handleHover)
		d.Register(rpc.OpScroll, handleScroll)
		d.Register(rpc.OpDrag, handleDrag)
		d.Register(rpc.OpSignal, handleSignal)
		d.Register(rpc.OpResize, handleResize)
	})
}

func handleType(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeArgs[rpc.TypeArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	if err := t.Type(a.Text); err != nil {
		return nil, ioFailure(err)
	}
	return nil, nil
}

func handleKey(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeArgs[rpc.KeyArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	k, err := tinput.Parse(a.Key)
	if err != nil {
		return nil, invalidArgument(err)
	}
	if err := t.Key(k); err != nil {
		return nil, engineFailure(err)
	}
	return nil, nil
}

func handlePaste(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeArgs[rpc.PasteArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	var err error
	if a.Force {
		err = t.ForcePaste(a.Text)
	} else {
		err = t.Paste(a.Text)
	}
	if err != nil {
		return nil, engineFailure(err)
	}
	return nil, nil
}

func handleClick(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeArgs[rpc.ClickArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	if errResp := requireMouseCoordinates(
		mouseCoordinate{name: "x", value: a.X},
		mouseCoordinate{name: "y", value: a.Y},
	); errResp != nil {
		return nil, errResp
	}
	button, errResp := parseMouseButton(a.Button)
	if errResp != nil {
		return nil, errResp
	}
	modifiers, errResp := parseMouseModifiers(a.Modifiers)
	if errResp != nil {
		return nil, errResp
	}
	if err := t.Click(*a.X, *a.Y, button, modifiers); err != nil {
		return nil, engineFailure(err)
	}
	return nil, nil
}

func handleHover(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeArgs[rpc.HoverArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	if errResp := requireMouseCoordinates(
		mouseCoordinate{name: "x", value: a.X},
		mouseCoordinate{name: "y", value: a.Y},
	); errResp != nil {
		return nil, errResp
	}
	modifiers, errResp := parseMouseModifiers(a.Modifiers)
	if errResp != nil {
		return nil, errResp
	}
	if err := t.Hover(*a.X, *a.Y, modifiers); err != nil {
		return nil, engineFailure(err)
	}
	return nil, nil
}

func handleScroll(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeArgs[rpc.ScrollArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	if errResp := requireMouseCoordinates(
		mouseCoordinate{name: "x", value: a.X},
		mouseCoordinate{name: "y", value: a.Y},
	); errResp != nil {
		return nil, errResp
	}
	direction, err := tinput.ParseScrollDirection(a.Direction)
	if err != nil {
		return nil, invalidArgument(err)
	}
	ticks := 1
	if a.Ticks != nil {
		ticks = *a.Ticks
	}
	modifiers, errResp := parseMouseModifiers(a.Modifiers)
	if errResp != nil {
		return nil, errResp
	}
	if err := t.Scroll(*a.X, *a.Y, direction, ticks, modifiers); err != nil {
		return nil, engineFailure(err)
	}
	return nil, nil
}

func handleDrag(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeArgs[rpc.DragArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	if errResp := requireMouseCoordinates(
		mouseCoordinate{name: "from_x", value: a.FromX},
		mouseCoordinate{name: "from_y", value: a.FromY},
		mouseCoordinate{name: "to_x", value: a.ToX},
		mouseCoordinate{name: "to_y", value: a.ToY},
	); errResp != nil {
		return nil, errResp
	}
	button, errResp := parseMouseButton(a.Button)
	if errResp != nil {
		return nil, errResp
	}
	modifiers, errResp := parseMouseModifiers(a.Modifiers)
	if errResp != nil {
		return nil, errResp
	}
	if err := t.Drag(*a.FromX, *a.FromY, *a.ToX, *a.ToY, button, modifiers); err != nil {
		return nil, engineFailure(err)
	}
	return nil, nil
}

type mouseCoordinate struct {
	name  string
	value *int
}

func requireMouseCoordinates(coordinates ...mouseCoordinate) *rpc.Error {
	for _, coordinate := range coordinates {
		if coordinate.value == nil {
			return invalidArgumentMessage(fmt.Sprintf("missing required coordinate %q", coordinate.name))
		}
	}
	return nil
}

func parseMouseButton(name string) (tinput.MouseButton, *rpc.Error) {
	if name == "" {
		return tinput.ButtonLeft, nil
	}
	button, err := tinput.ParseMouseButton(name)
	if err != nil {
		return tinput.ButtonNone, invalidArgument(err)
	}
	return button, nil
}

func parseMouseModifiers(names []string) ([]tinput.MouseModifier, *rpc.Error) {
	modifiers := make([]tinput.MouseModifier, len(names))
	for i, name := range names {
		modifier, err := tinput.ParseMouseModifier(name)
		if err != nil {
			return nil, invalidArgument(err)
		}
		modifiers[i] = modifier
	}
	if _, err := tinput.NormalizeMouseModifiers(modifiers); err != nil {
		return nil, invalidArgument(err)
	}
	return modifiers, nil
}

func handleSignal(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeArgs[rpc.SignalArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	sig, err := parseSignal(a.Name)
	if err != nil {
		return nil, invalidArgument(err)
	}
	if err := t.Signal(sig); err != nil {
		return nil, ioFailure(err)
	}
	return nil, nil
}

func handleResize(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeArgs[rpc.ResizeArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	if a.Cols <= 0 || a.Rows <= 0 {
		return nil, invalidArgumentMessage("cols and rows must be > 0")
	}
	if err := t.Resize(a.Cols, a.Rows); err != nil {
		return nil, ioFailure(err)
	}
	return nil, nil
}

func parseSignal(name string) (os.Signal, error) {
	switch name {
	case "SIGTERM", "TERM":
		return syscall.SIGTERM, nil
	case "SIGKILL", "KILL":
		return syscall.SIGKILL, nil
	case "SIGINT", "INT":
		return syscall.SIGINT, nil
	case "SIGHUP", "HUP":
		return syscall.SIGHUP, nil
	case "SIGWINCH", "WINCH":
		return syscall.SIGWINCH, nil
	case "SIGUSR1", "USR1":
		return syscall.SIGUSR1, nil
	case "SIGUSR2", "USR2":
		return syscall.SIGUSR2, nil
	}
	return nil, fmt.Errorf("unknown signal %q", name)
}
