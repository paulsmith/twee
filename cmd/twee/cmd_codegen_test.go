package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/paulsmith/research/twee/internal/play"
	"github.com/paulsmith/research/twee/internal/rpc"
	"github.com/paulsmith/research/twee/internal/vt"
	"golang.org/x/term"
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

func TestParseCodegenArgsTraceOut(t *testing.T) {
	opts, err := parseCodegenArgs([]string{
		"/bin/cat", "--out", "ops.json", "--trace-out", "session.twee",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.TracePath != "session.twee" {
		t.Fatalf("TracePath = %q, want session.twee", opts.TracePath)
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
	makePTYRaw(t, ptmx)
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

func TestCodegenTraceOutWritesBundleFromPTYInput(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	outPath := filepath.Join(dir, "ops.json")
	tracePath := filepath.Join(dir, "session.twee")
	cmd := exec.Command(bin, "codegen", "/bin/cat", "--out", outPath, "--no-waits", "--trace-out", tracePath)
	cmd.Env = append(os.Environ(), testEnv(t)...)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	makePTYRaw(t, ptmx)
	defer ptmx.Close()

	if _, err := ptmx.Write([]byte("abc\x1dq")); err != nil {
		t.Fatal(err)
	}

	out, err := waitCodegenPTY(cmd, ptmx, 5*time.Second)
	if err != nil {
		t.Fatalf("codegen: %v\n%s", err, out)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("script missing at %s: %v", outPath, err)
	}

	bundle, err := play.OpenBundle(tracePath)
	if err != nil {
		t.Fatalf("OpenBundle: %v", err)
	}
	if bundle.Manifest.Cols != 80 || bundle.Manifest.Rows != 24 {
		t.Fatalf("manifest size = %dx%d, want 80x24", bundle.Manifest.Cols, bundle.Manifest.Rows)
	}
	if len(bundle.Manifest.Command) != 1 || bundle.Manifest.Command[0] != "/bin/cat" {
		t.Fatalf("manifest command = %#v, want [/bin/cat]", bundle.Manifest.Command)
	}
	if !eventsContain(bundle.Events, "output", "", "", []byte("abc")) {
		t.Fatalf("trace missing output event containing abc: %#v", bundle.Events)
	}
	if !eventsContain(bundle.Events, "input", "type", "", []byte("abc")) {
		t.Fatalf("trace missing typed input event: %#v", bundle.Events)
	}
}

func TestCodegenHotkeyToggleWritesTraceBundle(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	outPath := filepath.Join(dir, "ops.json")
	cmd := exec.Command(bin, "codegen", "/bin/cat", "--out", outPath, "--no-waits")
	cmd.Env = append(os.Environ(), testEnv(t)...)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	makePTYRaw(t, ptmx)
	proc := watchPTY(cmd, ptmx)
	defer ptmx.Close()

	proc.write(t, []byte("before"))
	proc.waitFor(t, []byte("before"), 5*time.Second)
	proc.write(t, []byte{0x1d, 't'})
	proc.waitFor(t, []byte("started trace recording"), 5*time.Second)
	proc.write(t, []byte("during"))
	proc.waitFor(t, []byte("during"), 5*time.Second)
	proc.write(t, []byte{0x1d, 't'})
	proc.waitFor(t, []byte("stopped trace recording"), 5*time.Second)
	proc.write(t, []byte("after"))
	proc.waitFor(t, []byte("after"), 5*time.Second)
	proc.write(t, []byte{0x1d, 'q'})

	out, err := proc.finish(5 * time.Second)
	if err != nil {
		t.Fatalf("codegen: %v\n%s", err, out)
	}

	matches, err := filepath.Glob(filepath.Join(dir, "ops-trace-*.twee"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("hotkey trace matches = %#v, want one", matches)
	}
	bundle, err := play.OpenBundle(matches[0])
	if err != nil {
		t.Fatalf("OpenBundle: %v", err)
	}
	if text := traceVisibleText(bundle); !strings.Contains(text, "before") {
		t.Fatalf("trace seed did not reconstruct before; visible text:\n%s", text)
	}
	if !eventsContain(bundle.Events, "input", "type", "", []byte("during")) {
		t.Fatalf("trace missing typed input during traced interval: %#v", bundle.Events)
	}
	if eventsContain(bundle.Events, "input", "type", "", []byte("after")) {
		t.Fatalf("trace includes input after tracing stopped: %#v", bundle.Events)
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
	makePTYRaw(t, ptmx)
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

type watchedPTY struct {
	cmd  *exec.Cmd
	ptmx *os.File
	done chan error
	mu   sync.Mutex
	out  []byte
}

func watchPTY(cmd *exec.Cmd, ptmx *os.File) *watchedPTY {
	p := &watchedPTY{cmd: cmd, ptmx: ptmx, done: make(chan error, 1)}
	go func() {
		buf := make([]byte, 4096)
		var readErr error
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				p.mu.Lock()
				p.out = append(p.out, buf[:n]...)
				p.mu.Unlock()
			}
			if err != nil {
				if !errors.Is(err, syscall.EIO) && !errors.Is(err, io.EOF) {
					readErr = err
				}
				break
			}
		}
		p.done <- errors.Join(readErr, cmd.Wait())
	}()
	return p
}

func makePTYRaw(t *testing.T, ptmx *os.File) {
	t.Helper()
	if _, err := term.MakeRaw(int(ptmx.Fd())); err != nil {
		t.Fatalf("make PTY raw: %v", err)
	}
}

func (p *watchedPTY) write(t *testing.T, b []byte) {
	t.Helper()
	if _, err := p.ptmx.Write(b); err != nil {
		t.Fatal(err)
	}
}

func (p *watchedPTY) waitFor(t *testing.T, want []byte, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		ok := bytes.Contains(p.out, want)
		out := append([]byte(nil), p.out...)
		p.mu.Unlock()
		if ok {
			return
		}
		select {
		case err := <-p.done:
			t.Fatalf("process exited before %q appeared: %v\n%s", want, err, out)
		case <-time.After(10 * time.Millisecond):
		}
	}
	p.mu.Lock()
	out := append([]byte(nil), p.out...)
	p.mu.Unlock()
	t.Fatalf("timed out waiting for %q\n%s", want, out)
}

func (p *watchedPTY) finish(timeout time.Duration) ([]byte, error) {
	select {
	case err := <-p.done:
		p.mu.Lock()
		defer p.mu.Unlock()
		return append([]byte(nil), p.out...), err
	case <-time.After(timeout):
		return nil, errors.New("codegen did not exit")
	}
}

func waitCodegenPTY(cmd *exec.Cmd, ptmx *os.File, timeout time.Duration) ([]byte, error) {
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
		return got.out, got.err
	case <-time.After(timeout):
		return nil, errors.New("codegen did not exit")
	}
}

func eventsContain(events []play.Event, typ, kind, key string, bytes []byte) bool {
	for _, ev := range events {
		if ev.Type != typ {
			continue
		}
		if kind != "" && ev.Kind != kind {
			continue
		}
		if key != "" && ev.Key != key {
			continue
		}
		if len(bytes) == 0 || bytesContains(ev.Bytes, bytes) {
			return true
		}
	}
	return false
}

func bytesContains(got, want []byte) bool {
	return bytes.Contains(got, want)
}

func traceVisibleText(bundle play.Bundle) string {
	model := vt.New(bundle.Manifest.Cols, bundle.Manifest.Rows)
	for _, ev := range bundle.Events {
		switch ev.Type {
		case "output":
			_ = model.Feed(ev.Bytes)
		case "resize":
			_ = model.Resize(ev.Cols, ev.Rows)
		}
	}
	return vt.VisibleText(model.Snapshot())
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
	makePTYRaw(t, ptmx)
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
