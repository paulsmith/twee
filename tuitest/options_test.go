package tuitest

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOptionsEnvDirDefaultTimeoutAndCursor(t *testing.T) {
	dir := t.TempDir()
	term := Run(t, "/bin/sh",
		Args("-c", "printf '%s\\r\\n%s\\r\\n' \"$(basename \"$PWD\")\" \"$TWEE_OPTION_TEST\"; sleep 30"),
		Dir(dir),
		Env("TWEE_OPTION_TEST", "present"),
		DefaultTimeout(2*time.Second),
		Size(50, 6))

	term.ExpectText(filepath.Base(dir))
	term.ExpectText("present")
	if got := term.Cursor(); !got.Visible {
		t.Fatalf("Cursor visible = false, want true")
	}
}

func TestRecordOptionWritesTweeBundle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.twee")
	term := Run(t, "/bin/sh",
		Args("-c", "printf 'ready\\r\\n'; sleep 30"),
		Record(path),
		Size(20, 4))

	term.ExpectText("ready")
	term.Type("abc")
	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !traceHasInput(t, path, "abc") {
		t.Fatalf("trace bundle missing input event for typed text")
	}
}

func TestNetworkCaptureOptionConfiguresWholeSessionTrace(t *testing.T) {
	cfg := newConfig()
	NetworkCapture("session.twee", TCPPublication{
		Listen: "127.0.0.1:8080", Guest: "10.0.2.100:3000",
	})(cfg)

	engineConfig := cfg.toEngine()
	whole := engineConfig.WholeSessionTrace
	if whole == nil || whole.Path != "session.twee" || whole.Network == nil {
		t.Fatalf("whole-session trace = %+v", whole)
	}
	if got := whole.Network.PublishTCP; len(got) != 1 || got[0].Listen != "127.0.0.1:8080" || got[0].Guest != "10.0.2.100:3000" {
		t.Fatalf("publications = %+v", got)
	}
}

func TestNetworkCaptureWithEmptyPathFailsClosed(t *testing.T) {
	cfg := newConfig()
	NetworkCapture("")(cfg)
	if cfg.toEngine().WholeSessionTrace == nil {
		t.Fatal("NetworkCapture with an empty path omitted the network configuration")
	}
}

func TestWithContextCancelsWait(t *testing.T) {
	term := Run(t, "/bin/sh", Args("-c", "sleep 30"), Size(20, 4))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := term.WaitForText("never", WithContext(ctx), WithTimeout(time.Second))
	if err == nil {
		t.Fatal("WaitForText unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestCtrlReExport(t *testing.T) {
	if Ctrl('c') != CtrlC {
		t.Fatalf("Ctrl('c') = %v, want CtrlC %v", Ctrl('c'), CtrlC)
	}
}
