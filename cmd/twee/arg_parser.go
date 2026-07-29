package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	arg "github.com/alexflint/go-arg"
)

const envClientSession = "TWEE_SESSION"

var errShortOption = errors.New("short options are not supported")

type nameOpt struct {
	Value   string
	Present bool
}

type rootArgs struct {
	Verb       string
	Args       []string
	GlobalName nameOpt
	Help       bool
	HelpKey    string
}

func nameOptFromPtr(v *string) nameOpt {
	if v == nil {
		return nameOpt{}
	}
	return nameOpt{Value: *v, Present: true}
}

func sessionName(local, global nameOpt, getenv func(string) (string, bool)) (string, error) {
	for _, candidate := range []nameOpt{local, global} {
		if !candidate.Present {
			continue
		}
		if err := validateName(candidate.Value); err != nil {
			return "", err
		}
		return candidate.Value, nil
	}
	if envName, ok := getenv(envClientSession); ok {
		if err := validateName(envName); err != nil {
			return "", err
		}
		return envName, nil
	}
	return "default", nil
}

func currentSessionName(local nameOpt) (string, error) {
	return sessionName(local, rootGlobalName, os.LookupEnv)
}

func resolveSessionNamePtr(local *string) (string, error) {
	return currentSessionName(nameOptFromPtr(local))
}

func mustResolveSessionNamePtr(verb string, local *string) string {
	name, err := resolveSessionNamePtr(local)
	if err != nil {
		fatalUsage("%s: %v", verb, err)
	}
	return name
}

var rootGlobalName nameOpt

func rejectShortOptions(args []string) error {
	for _, a := range args {
		if a == "--" {
			return nil
		}
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && a != "-" {
			return fmt.Errorf("%w: %s", errShortOption, a)
		}
	}
	return nil
}

func parseRootArgs(args []string) (rootArgs, error) {
	var root rootArgs
	if len(args) == 0 {
		return root, nil
	}

	for i := 0; i < len(args); {
		a := args[i]
		switch {
		case a == "--":
			return root, fmt.Errorf("missing verb before --")
		case a == "--help":
			root.Help = true
			root.HelpKey = ""
			return root, nil
		case a == "--name":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return root, fmt.Errorf("--name requires a value")
			}
			root.GlobalName = nameOpt{Value: args[i+1], Present: true}
			i += 2
		case strings.HasPrefix(a, "--name="):
			root.GlobalName = nameOpt{Value: strings.TrimPrefix(a, "--name="), Present: true}
			i++
		case strings.HasPrefix(a, "--"):
			return root, fmt.Errorf("unknown global option %s", a)
		case strings.HasPrefix(a, "-") && a != "-":
			return root, fmt.Errorf("%w: %s", errShortOption, a)
		default:
			root.Verb = a
			root.Args = append([]string(nil), args[i+1:]...)
			if key, ok := helpKey(root.Verb, root.Args); ok {
				root.Help = true
				root.HelpKey = key
			}
			if root.GlobalName.Present && !verbAcceptsGlobalName(root.Verb) {
				return root, fmt.Errorf("global --name is not valid for %s", root.Verb)
			}
			return root, nil
		}
	}
	return root, nil
}

func helpKey(verb string, args []string) (string, bool) {
	parts := []string{verb}
	for _, a := range args {
		switch {
		case a == "--":
			return "", false
		case a == "--help":
			return strings.Join(parts, " "), true
		case strings.HasPrefix(a, "--"):
			return "", false
		default:
			if len(parts) < 2 {
				parts = append(parts, a)
			}
		}
	}
	return "", false
}

func verbAcceptsGlobalName(verb string) bool {
	switch verb {
	case "start", "status", "stop", "diff", "text", "lines", "cursor", "size", "title", "mode",
		"scrollback", "snapshot", "cell", "region", "find", "resize", "screenshot", "signal",
		"key", "keys", "type", "paste", "wait", "record", "trace":
		return true
	default:
		return false
	}
}

func splitExplicitBoundary(verb string, args []string) ([]string, []string, error) {
	for i, a := range args {
		if a != "--" {
			continue
		}
		before := append([]string(nil), args[:i]...)
		after := append([]string(nil), args[i+1:]...)
		if err := rejectShortOptions(before); err != nil {
			return nil, nil, err
		}
		if len(after) == 0 {
			return nil, nil, fmt.Errorf("%s: missing argument after --", verb)
		}
		return before, after, nil
	}
	if err := rejectShortOptions(args); err != nil {
		return nil, nil, err
	}
	return nil, nil, fmt.Errorf("%s: missing -- before command or payload", verb)
}

func requireSeparateValues(args []string, names ...string) error {
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	for i, a := range args {
		name := a
		if idx := strings.IndexByte(a, '='); idx >= 0 {
			name = a[:idx]
		}
		if !wanted[name] || strings.Contains(a, "=") {
			continue
		}
		if i+1 >= len(args) || args[i+1] == "--" || strings.HasPrefix(args[i+1], "--") {
			return hintDashValue(fmt.Errorf("missing value for %s", a), args)
		}
	}
	return nil
}

// hintDashValue augments a missing-value error when the token after the
// flag begins with a dash: the parser refused it as a value, and the
// equals form is how to pass it.
func hintDashValue(err error, args []string) error {
	msg := err.Error()
	const prefix = "missing value for "
	idx := strings.LastIndex(msg, prefix)
	if idx < 0 {
		return err
	}
	flag := strings.TrimSpace(msg[idx+len(prefix):])
	for i, a := range args {
		if a != flag || i+1 >= len(args) {
			continue
		}
		if next := args[i+1]; strings.HasPrefix(next, "-") {
			return fmt.Errorf("%s (the next token %q begins with '-'; pass dash-leading values as %s=VALUE)", msg, next, flag)
		}
	}
	return err
}

// rejectDuplicateFlags reports an error if any of the given long-option
// flags (e.g. "--cols") appears more than once in args, whether given as
// "--flag value" or "--flag=value". go-arg silently keeps the last value
// for a repeated scalar flag; for required numeric flags like coordinates
// and sizes that's more likely a typo than an intentional override, so we
// reject it explicitly instead.
func rejectDuplicateFlags(args []string, flags ...string) error {
	want := make(map[string]bool, len(flags))
	for _, f := range flags {
		want[f] = true
	}
	seen := make(map[string]bool, len(flags))
	for _, a := range args {
		name := a
		if idx := strings.IndexByte(a, '='); idx >= 0 {
			name = a[:idx]
		}
		if !want[name] {
			continue
		}
		if seen[name] {
			return fmt.Errorf("duplicate %s", name)
		}
		seen[name] = true
	}
	return nil
}

func positiveIntFlag(flag string, value *string) (int, bool, error) {
	if value == nil {
		return 0, false, nil
	}
	n, err := strconv.Atoi(*value)
	if err != nil || n <= 0 {
		return 0, true, fmt.Errorf("%s must be a positive integer", flag)
	}
	return n, true, nil
}

func positiveIntFlagOrDefault(flag string, value *string, def int) (int, error) {
	n, ok, err := positiveIntFlag(flag, value)
	if err != nil {
		return 0, err
	}
	if !ok {
		return def, nil
	}
	return n, nil
}

func parseArg(program string, dest any, args []string) error {
	if err := rejectShortOptions(args); err != nil {
		return err
	}
	p, err := arg.NewParser(arg.Config{
		Program:   "twee " + program,
		IgnoreEnv: true,
		Out:       io.Discard,
	}, dest)
	if err != nil {
		return err
	}
	if err := p.Parse(args); err != nil {
		if errors.Is(err, arg.ErrHelp) {
			return fmt.Errorf("%s: use --help before -- to show static help", program)
		}
		return hintDashValue(err, args)
	}
	return nil
}
