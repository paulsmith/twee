package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"regexp/syntax"
	"unicode/utf8"

	"github.com/paulsmith/twee/internal/rpc"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/tracebundle"
)

type traceOutputMatch struct {
	TMS        int64 `json:"t_ms"`
	EventStart int   `json:"event_start"`
	EventEnd   int   `json:"event_end"`
}

type outputStream struct {
	data     []byte
	segments []outputSegment
}

type outputSegment struct {
	start int
	end   int
	event int
	tms   int64
}

func runTraceContainsOutput(args []string) {
	if rootGlobalName.Present {
		fatalUsage("trace contains-output: global --name is not valid for an offline trace query")
	}
	if err := rejectDuplicateFlags(args, "--text", "--hex", "--regex"); err != nil {
		fatalUsage("trace contains-output: %v", err)
	}
	var opts struct {
		Path  string  `arg:"positional,required"`
		Text  *string `arg:"--text"`
		Hex   *string `arg:"--hex"`
		Regex *string `arg:"--regex"`
	}
	if err := parseArg("trace contains-output", &opts, args); err != nil {
		fatalUsage("trace contains-output: %v", err)
	}

	selectors := 0
	for _, value := range []*string{opts.Text, opts.Hex, opts.Regex} {
		if value != nil {
			selectors++
		}
	}
	if selectors != 1 {
		fatalUsage("trace contains-output: exactly one of --text, --hex, or --regex is required")
	}

	var (
		literal []byte
		re      *regexp.Regexp
		kind    string
	)
	switch {
	case opts.Text != nil:
		if *opts.Text == "" {
			fatalUsage("trace contains-output: --text must not be empty")
		}
		if !utf8.ValidString(*opts.Text) {
			fatalUsage("trace contains-output: --text must be valid UTF-8; use --hex for arbitrary bytes")
		}
		literal, kind = []byte(*opts.Text), "text"
	case opts.Hex != nil:
		if *opts.Hex == "" {
			fatalUsage("trace contains-output: --hex must not be empty")
		}
		var err error
		literal, err = hex.DecodeString(*opts.Hex)
		if err != nil {
			fatalUsage("trace contains-output: invalid --hex: %v", err)
		}
		kind = "hex"
	case opts.Regex != nil:
		if *opts.Regex == "" {
			fatalUsage("trace contains-output: --regex must not be empty")
		}
		parsed, err := syntax.Parse(*opts.Regex, syntax.Perl)
		if err != nil {
			fatalUsage("trace contains-output: invalid --regex: %v", err)
		}
		if regexpCanMatchEmpty(parsed) {
			fatalUsage("trace contains-output: --regex must consume at least one byte")
		}
		re, err = regexp.Compile(*opts.Regex)
		if err != nil {
			fatalUsage("trace contains-output: invalid --regex: %v", err)
		}
		kind = "regex"
	}

	bundle, validation, err := tracebundle.OpenValidated(opts.Path)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	if !validation.Valid {
		emitInvalidBundle("trace contains-output", validation.Issues)
	}

	output := collectTraceOutput(bundle.Events)
	if re != nil && !utf8.Valid(output.data) {
		details, _ := json.Marshal(map[string]any{
			"path":         opts.Path,
			"pattern_kind": kind,
		})
		emitError(rpc.CodeInvalidArgument,
			"trace contains-output: --regex requires valid UTF-8 output; use --hex for arbitrary bytes",
			details, 1)
	}
	match, ok := output.find(literal, re)
	if !ok {
		details, _ := json.Marshal(map[string]any{
			"path":         opts.Path,
			"pattern_kind": kind,
		})
		emitError(rpc.CodeAssertionFailed, fmt.Sprintf("trace contains-output: no %s match", kind), details, 1)
	}
	emitOK(match)
}

func findTraceOutput(events []tracebundle.Event, literal []byte, re *regexp.Regexp) (traceOutputMatch, bool) {
	return collectTraceOutput(events).find(literal, re)
}

func collectTraceOutput(events []tracebundle.Event) outputStream {
	total := 0
	for _, event := range events {
		if event.Type == trace.EventTypeOutput {
			total += len(event.Bytes)
		}
	}
	output := outputStream{
		data:     make([]byte, 0, total),
		segments: make([]outputSegment, 0),
	}
	for i, event := range events {
		if event.Type != trace.EventTypeOutput || len(event.Bytes) == 0 {
			continue
		}
		start := len(output.data)
		output.data = append(output.data, event.Bytes...)
		output.segments = append(output.segments, outputSegment{start: start, end: len(output.data), event: i, tms: event.TMS})
	}
	return output
}

func (output outputStream) find(literal []byte, re *regexp.Regexp) (traceOutputMatch, bool) {
	var bounds []int
	if re != nil {
		bounds = re.FindIndex(output.data)
	} else if start := bytes.Index(output.data, literal); start >= 0 {
		bounds = []int{start, start + len(literal)}
	}
	if len(bounds) != 2 || bounds[0] == bounds[1] {
		return traceOutputMatch{}, false
	}

	startSegment, endSegment := -1, -1
	for i, segment := range output.segments {
		if startSegment < 0 && segment.end > bounds[0] {
			startSegment = i
		}
		if segment.end >= bounds[1] {
			endSegment = i
			break
		}
	}
	if startSegment < 0 || endSegment < 0 {
		return traceOutputMatch{}, false
	}
	return traceOutputMatch{
		TMS:        output.segments[endSegment].tms,
		EventStart: output.segments[startSegment].event,
		EventEnd:   output.segments[endSegment].event,
	}, true
}

func regexpCanMatchEmpty(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpNoMatch:
		return false
	case syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText,
		syntax.OpEndText, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return true
	case syntax.OpLiteral, syntax.OpCharClass, syntax.OpAnyCharNotNL, syntax.OpAnyChar:
		return false
	case syntax.OpCapture, syntax.OpPlus:
		return regexpCanMatchEmpty(re.Sub[0])
	case syntax.OpStar, syntax.OpQuest:
		return true
	case syntax.OpRepeat:
		return re.Min == 0 || regexpCanMatchEmpty(re.Sub[0])
	case syntax.OpConcat:
		for _, sub := range re.Sub {
			if !regexpCanMatchEmpty(sub) {
				return false
			}
		}
		return true
	case syntax.OpAlternate:
		for _, sub := range re.Sub {
			if regexpCanMatchEmpty(sub) {
				return true
			}
		}
		return false
	default:
		panic("unhandled regexp syntax op: " + re.Op.String())
	}
}
