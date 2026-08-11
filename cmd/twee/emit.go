package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/paulsmith/twee/internal/rpc"
)

type outputPolicy struct{ machine bool }

var output outputPolicy

// stdoutFile returns the output writer for envelope JSON. Indirection
// keeps tests free to redirect.
func stdoutFile() io.Writer { return os.Stdout }

func (p outputPolicy) emitJSON(value any) {
	if err := json.NewEncoder(stdoutFile()).Encode(value); err != nil {
		fmt.Fprintf(os.Stderr, "twee: emit: %v\n", err)
		os.Exit(1)
	}
}

func (p outputPolicy) exit(code int) { os.Exit(code) }

// emitOK writes {"ok": true, "data": data} to stdout and exits 0.
func emitOK(data any) {
	out := struct {
		OK   bool `json:"ok"`
		Data any  `json:"data"`
	}{OK: true, Data: data}
	output.emitJSON(out)
}

// emitError writes {"ok": false, "error": {...}} to stdout and exits.
func emitError(code, msg string, details json.RawMessage, exitCode int) {
	out := struct {
		OK    bool       `json:"ok"`
		Error *rpc.Error `json:"error"`
	}{OK: false, Error: &rpc.Error{Code: code, Message: msg, Details: details}}
	output.emitJSON(out)
	output.exit(exitCode)
}

func emitNDJSON(value any) { output.emitJSON(value) }

func exitAfterNDJSONFailure(cleanup func()) {
	cleanup()
	output.exit(1)
}

// fatalUsage prints to stderr and exits with code 2 (POSIX usage error).
func fatalUsage(format string, args ...any) {
	if output.machine {
		emitError(rpc.CodeInvalidArgument, fmt.Sprintf(format, args...), nil, 2)
	}
	fmt.Fprintf(os.Stderr, "twee: "+format+"\n", args...)
	os.Exit(2)
}

func fatalRuntime(code, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if output.machine {
		emitError(code, msg, nil, 1)
	}
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

func emitTextSuccess(value string) {
	if output.machine {
		emitOK(map[string]string{"text": value})
		return
	}
	if _, err := fmt.Fprintln(os.Stdout, value); err != nil {
		fatalRuntime(rpc.CodeIO, "write stdout: %v", err)
	}
}
