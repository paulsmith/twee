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
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/paulsmith/twee/internal/play"
	"github.com/paulsmith/twee/internal/rpc"
	"github.com/paulsmith/twee/internal/vt"
)

const compositorCols, compositorRows = 160, 24

func withTERM(env []string, term string) []string {
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, "TERM=") {
			out = append(out, entry)
		}
	}
	return append(out, "TERM="+term)
}

func visibleTextFromHostBytes(out []byte, cols, rows int) string {
	if i := bytes.LastIndex(out, []byte("\x1b[?1049l")); i >= 0 {
		out = out[:i]
	}
	host := vt.New(cols, rows)
	_ = host.Feed(out)
	return vt.VisibleText(host.Snapshot())
}

func (p *watchedPTY) waitForVisible(t *testing.T, want string, cols, rows int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		out := append([]byte(nil), p.out...)
		p.mu.Unlock()
		if strings.Contains(visibleTextFromHostBytes(out, cols, rows), want) {
			return
		}
		select {
		case err := <-p.done:
			t.Fatalf("process exited before %q appeared: %v\n%s", want, err, visibleTextFromHostBytes(out, cols, rows))
		case <-time.After(10 * time.Millisecond):
		}
	}
	p.mu.Lock()
	out := append([]byte(nil), p.out...)
	p.mu.Unlock()
	t.Fatalf("timed out waiting for visible %q\n%s", want, visibleTextFromHostBytes(out, cols, rows))
}

func TestParseWrapAllowsNoRecorders(t *testing.T) {
	opts, err := parseWrapArgs([]string{"--no-status", "--", "/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.OutPath != "" || opts.TracePath != "" || !opts.NoStatus {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestWrapDefaultCreatesNoArtifacts(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	cmd := exec.Command(bin, "wrap", "--no-status", "--", "/bin/cat")
	cmd.Dir, cmd.Env = dir, append(os.Environ(), testEnv(t)...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ptmx.Close() }()
	makePTYRaw(t, ptmx)
	if _, err := ptmx.Write([]byte("hello\x1dq")); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, readErr := io.ReadAll(ptmx)
		waitErr := cmd.Wait()
		if errors.Is(readErr, syscall.EIO) {
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
		t.Fatal("wrap did not exit")
	}
	for _, pattern := range []string{"twee-script-*.json", "twee-trace-*.twee"} {
		if got, _ := filepath.Glob(filepath.Join(dir, pattern)); len(got) != 0 {
			t.Fatalf("unexpected artifacts: %v", got)
		}
	}
}

func TestWrapPropagatesNaturalChildFailure(t *testing.T) {
	bin := buildBinary(t)
	cmd := exec.Command(bin, "wrap", "--no-status", "--", "/bin/sh", "-c", "exit 7")
	cmd.Env = append(os.Environ(), testEnv(t)...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ptmx.Close() }()
	_, _ = io.ReadAll(ptmx)
	if err := cmd.Wait(); err == nil {
		t.Fatal("wrap succeeded for a failing child")
	} else if cmd.ProcessState.ExitCode() != 7 {
		t.Fatalf("exit = %d, want 7", cmd.ProcessState.ExitCode())
	}
}

func TestWrapImmediateArtifactsFinalizeOnNaturalExit(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "ops.json")
	tracePath := filepath.Join(dir, "session.twee")
	cmd := exec.Command(bin, "wrap", "--no-status", "--script-out", script, "--trace-out", tracePath, "--", "/bin/sh", "-c", "printf done")
	cmd.Dir, cmd.Env = dir, append(os.Environ(), testEnv(t)...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ptmx.Close() }()
	_, _ = io.ReadAll(ptmx)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Fatal(err)
	}
	if _, err := play.OpenBundle(tracePath); err != nil {
		t.Fatal(err)
	}
}

func TestWrapTraceNaturalExitRecordsExitCode(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "exit.twee")
	cmd := exec.Command(bin, "wrap", "--no-status", "--trace-out", path, "--", "/bin/sh", "-c", "exit 7")
	cmd.Dir, cmd.Env = dir, append(os.Environ(), testEnv(t)...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ptmx.Close() }()
	_, _ = io.ReadAll(ptmx)
	err = cmd.Wait()
	if err == nil || cmd.ProcessState.ExitCode() != 7 {
		t.Fatalf("exit err=%v code=%d", err, cmd.ProcessState.ExitCode())
	}
	b, err := play.OpenBundle(path)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ev := range b.Events {
		if ev.Type == "exit" {
			n++
			if ev.Code != 7 {
				t.Fatalf("exit=%d", ev.Code)
			}
		}
	}
	if n != 1 {
		t.Fatalf("exits=%d", n)
	}
}

func TestWrapCompositorCPRTraceOrder(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "cpr.twee")
	child := `stty raw -echo; printf '\033[6n'; reply=''; for i in {1..16}; do IFS= read -r -N 1 c || break; reply+="$c"; [[ "$c" == R ]] && break; done; printf 'GOT-CPR:%q\r\n' "$reply"`
	cmd := exec.Command(bin, "wrap", "--trace-out", path, "--", "/bin/bash", "-c", child)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), testEnv(t)...)
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ptmx.Close() }()
	out, err := waitCodegenPTY(cmd, ptmx, 5*time.Second)
	if err != nil {
		t.Fatalf("err=%v out=%q", err, out)
	}
	b, err := play.OpenBundle(path)
	if err != nil {
		t.Fatal(err)
	}
	hasEnter := bytes.Contains(out, []byte("\x1b[?1049h"))
	hasExit := bytes.Contains(out, []byte("\x1b[?1049l"))
	exitAt := bytes.LastIndex(out, []byte("\x1b[?1049l"))
	if !hasEnter || !hasExit {
		t.Fatalf("host enter=%t exit=%t", hasEnter, hasExit)
	}
	outer := vt.New(80, 24)
	if err := outer.Feed(out[:exitAt]); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(vt.VisibleText(outer.Snapshot()), "GOT-CPR:") {
		t.Fatalf("host marker=false")
	}
	q, r, m, x, n := -1, -1, -1, -1, 0
	for i, e := range b.Events {
		if e.Type == "output" && bytes.Contains(e.Bytes, []byte("\x1b[6n")) {
			q = i
		}
		if e.Type == "input" && e.Kind == "terminal_reply" && bytes.Contains(e.Bytes, []byte("\x1b[1;1R")) {
			r = i
		}
		if e.Type == "output" && bytes.Contains(e.Bytes, []byte("GOT-CPR:")) {
			m = i
		}
		if e.Type == "exit" {
			x = i
			n++
		}
	}
	if q < 0 || r <= q || m <= r || x <= m || n != 1 {
		t.Fatalf("order q=%d r=%d m=%d x=%d n=%d", q, r, m, x, n)
	}
}

func TestWrapLateScriptIsPartial(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	cmd := exec.Command(bin, "wrap", "--", "/bin/sh", "-c", "printf prompt; cat")
	cmd.Dir, cmd.Env = dir, withTERM(append(os.Environ(), testEnv(t)...), "xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: compositorCols, Rows: compositorRows})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ptmx.Close() }()
	makePTYRaw(t, ptmx)
	p := watchPTY(cmd, ptmx)
	p.waitForVisible(t, "prompt", compositorCols, compositorRows, 5*time.Second)
	p.write(t, []byte{0x1d, 's'})
	p.waitForVisible(t, "partial", compositorCols, compositorRows, 5*time.Second)
	p.write(t, []byte("active"))
	p.write(t, []byte{0x1d, 's'})
	p.write(t, []byte{0x1d, 'q'})
	out, err := p.finish(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("(partial)")) {
		t.Fatalf("missing partial summary: %q", out)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "twee-script-*.json"))
	if len(files) != 1 {
		t.Fatalf("scripts=%v", files)
	}
	raw, _ := os.ReadFile(files[0])
	var ops []rpc.Request
	if err := json.Unmarshal(raw, &ops); err != nil {
		t.Fatal(err)
	}
	if len(ops) < 2 || ops[0].Op != rpc.OpResize || !bytes.Contains(raw, []byte("active")) {
		t.Fatalf("ops=%s", raw)
	}
}

func TestWrapRecordersOneShotAndIndependent(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	cmd := exec.Command(bin, "wrap", "--", "/bin/cat")
	cmd.Dir, cmd.Env = dir, withTERM(append(os.Environ(), testEnv(t)...), "xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: compositorCols, Rows: compositorRows})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ptmx.Close() }()
	makePTYRaw(t, ptmx)
	p := watchPTY(cmd, ptmx)
	p.write(t, []byte{0x1d, 's'})
	p.write(t, []byte{0x1d, 't'})
	p.write(t, []byte("both"))
	p.write(t, []byte{0x1d, 's'})
	p.write(t, []byte{0x1d, 's'})
	p.write(t, []byte("traceonly"))
	p.write(t, []byte{0x1d, 't'})
	p.write(t, []byte{0x1d, 't'})
	p.write(t, []byte{0x1d, 'q'})
	if _, err := p.finish(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	scripts, _ := filepath.Glob(filepath.Join(dir, "twee-script-*.json"))
	traces, _ := filepath.Glob(filepath.Join(dir, "twee-trace-*.twee"))
	if len(scripts) != 1 || len(traces) != 1 {
		t.Fatalf("scripts=%v traces=%v", scripts, traces)
	}
	raw, _ := os.ReadFile(scripts[0])
	if !bytes.Contains(raw, []byte("both")) || bytes.Contains(raw, []byte("traceonly")) {
		t.Fatalf("script=%s", raw)
	}
	b, err := play.OpenBundle(traces[0])
	if err != nil {
		t.Fatal(err)
	}
	if !eventsContain(b.Events, "input", "type", "", []byte("both")) || !eventsContain(b.Events, "input", "type", "", []byte("traceonly")) {
		t.Fatal("trace inputs missing")
	}
	for _, ev := range b.Events {
		if ev.Type == "output" {
			for _, bad := range []string{"twee wrap", filepath.Base(scripts[0]), filepath.Base(traces[0]), "^]s", "⠋"} {
				if bytes.Contains(ev.Bytes, []byte(bad)) {
					t.Fatalf("trace output contaminated with %q", bad)
				}
			}
		}
	}
}

func TestWrapCompositorMirrorsCommonInputModes(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "modes.twee")
	child := `stty raw -echo; printf '\033[?1h\033=\033[?2004h\033[?1004h\033[?1000h\033[?1006hREADY\r\n'; paste=$'\e[200~paste\e[201~'; got=''; for ((i=0; i<${#paste}; i++)); do IFS= read -r -N 1 c || exit 1; got+="$c"; done; [[ "$got" == "$paste" ]] || { printf 'MODE-BAD-PASTE:%q\r\n' "$got"; exit 1; }; printf 'PASTE-OK\r\n'; mouse=$'\e[<0;1;1M'; got=''; for ((i=0; i<${#mouse}; i++)); do IFS= read -r -N 1 c || exit 1; got+="$c"; done; [[ "$got" == "$mouse" ]] || { printf 'MODE-BAD-MOUSE:%q\r\n' "$got"; exit 1; }; printf 'MODE-OK\r\n'`
	cmd := exec.Command(bin, "wrap", "--trace-out", tracePath, "--", "/bin/bash", "-c", child)
	cmd.Dir = dir
	cmd.Env = withTERM(append(os.Environ(), testEnv(t)...), "xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: compositorCols, Rows: compositorRows})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ptmx.Close() }()
	makePTYRaw(t, ptmx)
	p := watchPTY(cmd, ptmx)
	for _, seq := range []string{"\x1b[?1h", "\x1b[?66h", "\x1b[?2004h", "\x1b[?1004h", "\x1b[?1000h", "\x1b[?1006h"} {
		p.waitFor(t, []byte(seq), 5*time.Second)
	}
	p.waitForVisible(t, "READY", compositorCols, compositorRows, 5*time.Second)
	p.write(t, []byte("\x1b[200~paste\x1b[201~"))
	p.waitForVisible(t, "PASTE-OK", compositorCols, compositorRows, 5*time.Second)
	p.write(t, []byte("\x1b[<0;1;24M")) // parent-owned status row
	p.write(t, []byte("\x1b[<0;1;1M"))
	out, err := p.finish(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, seq := range []string{"\x1b[?1l", "\x1b[?66l", "\x1b[?2004l", "\x1b[?1004l", "\x1b[?1000l", "\x1b[?1006l", "\x1b[?1049l"} {
		if !bytes.Contains(out, []byte(seq)) {
			t.Fatalf("cleanup missing %q", seq)
		}
	}
	b, err := play.OpenBundle(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	if !eventsContain(b.Events, "input", "paste", "", []byte("\x1b[200~paste\x1b[201~")) || !eventsContain(b.Events, "input", "unknown", "", []byte("\x1b[<0;1;1M")) || eventsContain(b.Events, "input", "unknown", "", []byte("\x1b[<0;1;24M")) {
		t.Fatalf("trace did not retain paste and mouse input: %#v", b.Events)
	}
	if !eventsContain(b.Events, "output", "", "", []byte("MODE-OK")) || !eventsContain(b.Events, "exit", "", "", nil) {
		t.Fatalf("trace missing completion: %#v", b.Events)
	}
}

func TestWrapCompositorCoalescesBurstRedraws(t *testing.T) {
	bin := buildBinary(t)
	child := `for i in {1..30}; do printf '\033[H\033[2Jframe-%02d' "$i"; sleep 0.001; done`
	cmd := exec.Command(bin, "wrap", "--", "/bin/bash", "-c", child)
	cmd.Env = withTERM(append(os.Environ(), testEnv(t)...), "xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: compositorCols, Rows: compositorRows})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ptmx.Close() }()
	makePTYRaw(t, ptmx)
	out, err := waitCodegenPTY(cmd, ptmx, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := visibleTextFromHostBytes(out, compositorCols, compositorRows); !strings.Contains(got, "frame-30") {
		t.Fatalf("final frame missing:\n%s", got)
	}
	if frames := bytes.Count(out, []byte("\x1b[?2026h")); frames > 8 {
		t.Fatalf("burst produced %d host redraw transactions, want coalesced output", frames)
	}
}

func TestWrapCompositorFlushesPendingFrameAtPTYEOF(t *testing.T) {
	bin := buildBinary(t)
	child := `printf 'FINAL-FRAME'; exec 0>&- 1>&- 2>&-; sleep 1`
	cmd := exec.Command(bin, "wrap", "--", "/bin/bash", "-c", child)
	cmd.Env = withTERM(append(os.Environ(), testEnv(t)...), "xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: compositorCols, Rows: compositorRows})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ptmx.Close() }()
	makePTYRaw(t, ptmx)
	p := watchPTY(cmd, ptmx)
	p.waitForVisible(t, "FINAL-FRAME", compositorCols, compositorRows, 500*time.Millisecond)
	select {
	case err := <-p.done:
		t.Fatalf("wrap exited before the EOF frame assertion: %v", err)
	default:
	}
	if _, err := p.finish(3 * time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestWrapNoStatusRawPassthrough(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	cmd := exec.Command(bin, "wrap", "--no-status", "--", "/bin/sh", "-c", "printf '\033[31mRAW-MARK\033[0m'; cat")
	cmd.Dir, cmd.Env = dir, append(os.Environ(), testEnv(t)...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ptmx.Close() }()
	makePTYRaw(t, ptmx)
	p := watchPTY(cmd, ptmx)
	p.waitFor(t, []byte("RAW-MARK"), 5*time.Second)
	p.write(t, []byte{0x1d, 's'})
	p.waitFor(t, []byte("started script recording"), 5*time.Second)
	p.write(t, []byte{0x1d, 's', 0x1d, 'q'})
	out, err := p.finish(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("\x1b[31mRAW-MARK\x1b[0m")) {
		t.Fatalf("raw bytes missing: %q", out)
	}
	if bytes.Contains(out, []byte("?1049h")) || bytes.Contains(out, []byte("twee wrap │")) {
		t.Fatalf("compositor leaked: %q", out)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "twee-script-*.json"))
	if len(files) != 1 {
		t.Fatalf("scripts=%v", files)
	}
}

func TestParseWrapStartsIndependentRecorders(t *testing.T) {
	opts, err := parseWrapArgs([]string{"--script-out", "ops.json", "--trace-out", "session.twee", "--", "/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.OutPath != "ops.json" || opts.TracePath != "session.twee" {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestWrapHelpDocumentsOneShotControls(t *testing.T) {
	help := commandRegistry["wrap"].Usage
	for _, want := range []string{"Ctrl+] s", "Ctrl+] t", "--script-out", "--network-capture", "--publish-tcp", "--no-status"} {
		if !strings.Contains(help, want) {
			t.Fatalf("wrap help missing %q: %s", want, help)
		}
	}
}
