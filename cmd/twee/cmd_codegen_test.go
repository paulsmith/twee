package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/paulsmith/research/twee/internal/rpc"
)

func TestParseCodegenArgsInterspersedFlags(t *testing.T) {
	opts, err := parseCodegenArgs([]string{
		"/bin/cat", "-out", "ops.json", "--no-waits", "-cols", "100",
		"-rows", "40", "-env", "FOO=bar", "--", "--literal-child-flag",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.OutPath != "ops.json" || !opts.NoWaits || opts.Cols != 100 || opts.Rows != 40 {
		t.Fatalf("opts = %+v", opts)
	}
	if got := opts.Env["FOO"]; got != "bar" {
		t.Fatalf("env FOO = %q", got)
	}
	wantCmd := []string{"/bin/cat", "--literal-child-flag"}
	if len(opts.Command) != len(wantCmd) {
		t.Fatalf("cmd = %#v, want %#v", opts.Command, wantCmd)
	}
	for i := range wantCmd {
		if opts.Command[i] != wantCmd[i] {
			t.Fatalf("cmd = %#v, want %#v", opts.Command, wantCmd)
		}
	}
}

func TestLeadingResize(t *testing.T) {
	ops := []rpc.Request{
		{Op: rpc.OpResize, Args: json.RawMessage(`{"cols":120,"rows":40}`)},
		{Op: rpc.OpType, Args: json.RawMessage(`{"text":"x"}`)},
	}
	got, ok := leadingResize(ops)
	if !ok {
		t.Fatal("leadingResize returned false")
	}
	if got.Cols != 120 || got.Rows != 40 {
		t.Fatalf("resize = %+v, want 120x40", got)
	}
}

func TestCodegenWritesScriptFromPTYInput(t *testing.T) {
	bin := buildBinary(t)
	outPath := filepath.Join(t.TempDir(), "ops.json")
	cmd := exec.Command(bin, "codegen", "/bin/cat", "--out", outPath, "--no-waits")
	cmd.Env = append(os.Environ(), testEnv(t)...)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer ptmx.Close()

	if _, err := ptmx.Write([]byte("abc\x1dq")); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct {
		out []byte
		err error
	}, 1)
	go func() {
		out, readErr := io.ReadAll(ptmx)
		waitErr := cmd.Wait()
		if readErr != nil && errors.Is(readErr, syscall.EIO) {
			readErr = nil
		}
		done <- struct {
			out []byte
			err error
		}{out: out, err: errors.Join(readErr, waitErr)}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("codegen: %v\n%s", got.err, got.out)
		}
		if !bytes.Contains(got.out, []byte("abc")) {
			t.Fatalf("proxied output missing input echo:\n%s", got.out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("codegen did not exit after Ctrl+] q")
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var ops []rpc.Request
	if err := json.Unmarshal(raw, &ops); err != nil {
		t.Fatalf("decode script: %v\n%s", err, raw)
	}
	if len(ops) != 2 {
		t.Fatalf("ops = %d, want 2: %s", len(ops), raw)
	}
	var resize rpc.ResizeArgs
	if err := json.Unmarshal(ops[0].Args, &resize); err != nil {
		t.Fatal(err)
	}
	if ops[0].Op != rpc.OpResize || resize.Cols != 80 || resize.Rows != 24 {
		t.Fatalf("op = %s %s", ops[0].Op, ops[0].Args)
	}
	var args rpc.TypeArgs
	if err := json.Unmarshal(ops[1].Args, &args); err != nil {
		t.Fatal(err)
	}
	if ops[1].Op != rpc.OpType || args.Text != "abc" {
		t.Fatalf("op = %s %s", ops[1].Op, ops[1].Args)
	}
}

func TestCodegenDoesNotHangOnHighVolumeChildExit(t *testing.T) {
	bin := buildBinary(t)
	outPath := filepath.Join(t.TempDir(), "ops.json")
	cmd := exec.Command(bin, "codegen", "/bin/sh", "-c", "i=0; while [ $i -lt 400 ]; do printf '0123456789abcdef0123456789abcdef\\n'; i=$((i+1)); done", "--out", outPath, "--no-waits")
	cmd.Env = append(os.Environ(), testEnv(t)...)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer ptmx.Close()

	done := make(chan error, 1)
	go func() {
		_, readErr := io.ReadAll(ptmx)
		waitErr := cmd.Wait()
		if readErr != nil && errors.Is(readErr, syscall.EIO) {
			readErr = nil
		}
		done <- errors.Join(readErr, waitErr)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("codegen hung after high-volume child exit")
	}
}

func TestCodegenFlushesFinalWaitOnChildExit(t *testing.T) {
	bin := buildBinary(t)
	outPath := filepath.Join(t.TempDir(), "ops.json")
	cmd := exec.Command(bin, "codegen", "/bin/sh", "-c", "read line; printf 'done\\n'", "--out", outPath)
	cmd.Env = append(os.Environ(), testEnv(t)...)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer ptmx.Close()

	if _, err := ptmx.Write([]byte("x\r")); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct {
		out []byte
		err error
	}, 1)
	go func() {
		out, readErr := io.ReadAll(ptmx)
		waitErr := cmd.Wait()
		if readErr != nil && errors.Is(readErr, syscall.EIO) {
			readErr = nil
		}
		done <- struct {
			out []byte
			err error
		}{out: out, err: errors.Join(readErr, waitErr)}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("codegen: %v\n%s", got.err, got.out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("codegen did not exit after child exit")
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var ops []rpc.Request
	if err := json.Unmarshal(raw, &ops); err != nil {
		t.Fatalf("decode script: %v\n%s", err, raw)
	}
	if len(ops) == 0 || ops[len(ops)-1].Op != rpc.OpWaitStable {
		t.Fatalf("last op = %#v, want wait_stable in %s", ops, raw)
	}
}
