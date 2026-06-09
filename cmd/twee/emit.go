package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/paulsmith/twee/internal/rpc"
)

// stdoutFile returns the output writer for envelope JSON. Indirection
// keeps tests free to redirect.
func stdoutFile() io.Writer { return os.Stdout }

// emitOK writes {"ok": true, "data": data} to stdout and exits 0.
func emitOK(data any) {
	out := struct {
		OK   bool `json:"ok"`
		Data any  `json:"data"`
	}{OK: true, Data: data}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "twee: emit: %v\n", err)
		os.Exit(1)
	}
}

// emitError writes {"ok": false, "error": {...}} to stdout and exits.
func emitError(code, msg string, details json.RawMessage, exitCode int) {
	out := struct {
		OK    bool       `json:"ok"`
		Error *rpc.Error `json:"error"`
	}{OK: false, Error: &rpc.Error{Code: code, Message: msg, Details: details}}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "twee: emit: %v\n", err)
		os.Exit(1)
	}
	os.Exit(exitCode)
}

// fatalUsage prints to stderr and exits with code 2 (POSIX usage error).
func fatalUsage(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "twee: "+format+"\n", args...)
	os.Exit(2)
}
