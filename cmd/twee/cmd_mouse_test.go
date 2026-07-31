package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/input"
)

func TestParseClickArgs(t *testing.T) {
	name, got, err := parseClickArgs([]string{
		"--x", "0", "--y", "4", "--name", "demo",
		"--button", "right", "--modifier", "ctrl", "--modifier", "shift",
	})
	if err != nil {
		t.Fatal(err)
	}
	if name == nil || *name != "demo" {
		t.Fatalf("name = %v, want demo", name)
	}
	if got.X == nil || *got.X != 0 || got.Y == nil || *got.Y != 4 {
		t.Fatalf("coordinates = (%v,%v), want (0,4)", got.X, got.Y)
	}
	if got.Button != "right" {
		t.Fatalf("button = %q, want right", got.Button)
	}
	if strings.Join(got.Modifiers, ",") != "ctrl,shift" {
		t.Fatalf("modifiers = %#v, want ctrl,shift", got.Modifiers)
	}
}

func TestParseMouseDefaultsAreOmittedForDaemonDefaults(t *testing.T) {
	_, click, err := parseClickArgs([]string{"--x", "0", "--y", "0"})
	if err != nil {
		t.Fatal(err)
	}
	if click.Button != "" || len(click.Modifiers) != 0 {
		t.Fatalf("click defaults = %+v, want omitted button and modifiers", click)
	}

	_, scroll, err := parseScrollArgs([]string{
		"--x", "2", "--y", "3", "--direction", "down",
	})
	if err != nil {
		t.Fatal(err)
	}
	if scroll.Ticks != nil {
		t.Fatalf("scroll ticks = %v, want omitted daemon default", scroll.Ticks)
	}
}

func TestParseMouseArgsRequireEveryCoordinate(t *testing.T) {
	tests := []struct {
		name string
		fn   func([]string) error
		args []string
	}{
		{"click x", clickParseError, []string{"--y", "1"}},
		{"click y", clickParseError, []string{"--x", "1"}},
		{"hover x", hoverParseError, []string{"--y", "1"}},
		{"hover y", hoverParseError, []string{"--x", "1"}},
		{"scroll x", scrollParseError, []string{"--y", "1", "--direction", "up"}},
		{"scroll y", scrollParseError, []string{"--x", "1", "--direction", "up"}},
		{"drag from x", dragParseError, []string{"--from-y", "1", "--to-x", "2", "--to-y", "3"}},
		{"drag from y", dragParseError, []string{"--from-x", "1", "--to-x", "2", "--to-y", "3"}},
		{"drag to x", dragParseError, []string{"--from-x", "1", "--from-y", "2", "--to-y", "3"}},
		{"drag to y", dragParseError, []string{"--from-x", "1", "--from-y", "2", "--to-x", "3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn(tt.args); err == nil {
				t.Fatal("parse unexpectedly succeeded")
			}
		})
	}
}

func TestParseMouseArgsRejectDuplicateScalarFlags(t *testing.T) {
	tests := []struct {
		name string
		fn   func([]string) error
		args []string
	}{
		{"click coordinate", clickParseError, []string{"--x", "1", "--x=2", "--y", "3"}},
		{"click button", clickParseError, []string{"--x", "1", "--y", "2", "--button", "left", "--button=right"}},
		{"hover coordinate", hoverParseError, []string{"--x", "1", "--y", "2", "--y=3"}},
		{"scroll direction", scrollParseError, []string{"--x", "1", "--y", "2", "--direction", "up", "--direction=down"}},
		{"scroll ticks", scrollParseError, []string{"--x", "1", "--y", "2", "--direction", "up", "--ticks", "1", "--ticks=2"}},
		{"drag endpoint", dragParseError, []string{"--from-x", "1", "--from-y", "2", "--to-x", "3", "--to-y", "4", "--to-x=5"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn(tt.args); err == nil || !strings.Contains(err.Error(), "duplicate") {
				t.Fatalf("parse error = %v, want duplicate error", err)
			}
		})
	}
}

func TestParseMouseArgsRejectInvalidEnumsAndModifiers(t *testing.T) {
	tests := []struct {
		name string
		fn   func([]string) error
		args []string
		want string
	}{
		{"button", clickParseError, []string{"--x", "1", "--y", "2", "--button", "primary"}, "--button"},
		{"direction", scrollParseError, []string{"--x", "1", "--y", "2", "--direction", "left"}, "--direction"},
		{"unknown modifier", hoverParseError, []string{"--x", "1", "--y", "2", "--modifier", "meta"}, "unknown --modifier"},
		{"duplicate modifier", hoverParseError, []string{"--x", "1", "--y", "2", "--modifier", "alt", "--modifier", "alt"}, "duplicate --modifier"},
		{"zero ticks", scrollParseError, []string{"--x", "1", "--y", "2", "--direction", "up", "--ticks", "0"}, "--ticks"},
		{"too many ticks", scrollParseError, []string{"--x", "1", "--y", "2", "--direction", "up", "--ticks", "101"}, "--ticks"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn(tt.args); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parse error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestParseScrollAndDragArgs(t *testing.T) {
	_, scroll, err := parseScrollArgs([]string{
		"--x", "8", "--y", "9", "--direction", "up", "--ticks", "100",
		"--modifier", "alt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if scroll.X == nil || *scroll.X != 8 || scroll.Y == nil || *scroll.Y != 9 ||
		scroll.Direction != "up" || scroll.Ticks == nil || *scroll.Ticks != input.MaxScrollTicks ||
		len(scroll.Modifiers) != 1 || scroll.Modifiers[0] != "alt" {
		t.Fatalf("scroll = %+v", scroll)
	}

	_, drag, err := parseDragArgs([]string{
		"--from-x", "0", "--from-y", "1", "--to-x", "20", "--to-y", "10",
		"--button", "middle", "--modifier", "shift",
	})
	if err != nil {
		t.Fatal(err)
	}
	if drag.FromX == nil || *drag.FromX != 0 || drag.FromY == nil || *drag.FromY != 1 ||
		drag.ToX == nil || *drag.ToX != 20 || drag.ToY == nil || *drag.ToY != 10 ||
		drag.Button != "middle" || len(drag.Modifiers) != 1 || drag.Modifiers[0] != "shift" {
		t.Fatalf("drag = %+v", drag)
	}
}

func TestMouseOpsReachRunAndDoDispatcher(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)

	// A child that never enables mouse tracking makes a valid mouse request
	// fail with FAILED_PRECONDITION. That response proves the op reached its
	// registered mouse handler through the ordinary run/do script path; an
	// unregistered op would instead be INVALID_ARGUMENT "unknown op".
	runScript := writeScript(t, `[{"op":"click","args":{"x":0,"y":0}}]`)
	runCmd := exec.Command(bin,
		"run", "--script", runScript, "--", "/bin/sh", "-c", "sleep 30",
	)
	runCmd.Env = append(os.Environ(), env...)
	assertScriptErrorCode(t, runCmd, "FAILED_PRECONDITION")

	name := "mouse-dispatch"
	t.Cleanup(func() {
		stop := exec.Command(bin, "stop", "--name", name)
		stop.Env = append(os.Environ(), env...)
		_ = stop.Run()
	})
	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")
	doScript := writeScript(t, `[
		{"op":"drag","args":{"from_x":0,"from_y":0,"to_x":1,"to_y":1}}
	]`)
	doCmd := exec.Command(bin, "do", "--name", name, "--script", doScript)
	doCmd.Env = append(os.Environ(), env...)
	assertScriptErrorCode(t, doCmd, "FAILED_PRECONDITION")
	mustOK(t, bin, env, "stop", "--name", name)
}

func assertScriptErrorCode(t *testing.T, cmd *exec.Cmd, want string) {
	t.Helper()
	timer := time.AfterFunc(5*time.Second, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	defer timer.Stop()

	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 {
		t.Fatalf("%v: error = %v, output = %s; want exit 1", cmd.Args, err, out)
	}
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if response.Error.Code != want {
		t.Fatalf("%v: error code = %q, want %q; response=%s",
			cmd.Args, response.Error.Code, want, out)
	}
}

func clickParseError(args []string) error {
	_, _, err := parseClickArgs(args)
	return err
}

func hoverParseError(args []string) error {
	_, _, err := parseHoverArgs(args)
	return err
}

func scrollParseError(args []string) error {
	_, _, err := parseScrollArgs(args)
	return err
}

func dragParseError(args []string) error {
	_, _, err := parseDragArgs(args)
	return err
}
