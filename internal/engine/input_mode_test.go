package engine

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/paulsmith/twee/internal/input"
	"github.com/paulsmith/twee/internal/trace"
)

func TestCursorKeysFollowDECCKMTransitionsAndTraceEncodedBytes(t *testing.T) {
	term := startEngineTerm(t, []string{
		"/bin/bash", "-c",
		"stty raw -echo; printf 'NORMAL'; IFS= read -r -N 3 normal; printf '\033[?1hAPP'; IFS= read -r -N 3 app; printf '\033[?1lRESET'; IFS= read -r -N 3 reset; [[ $normal == $'\\e[A' && $app == $'\\eOA' && $reset == $'\\e[A' ]] && printf 'OK' || printf 'BAD:%q:%q:%q' \"$normal\" \"$app\" \"$reset\"; sleep 30",
	}, 80, 5)
	tracePath := filepath.Join(t.TempDir(), "keys.twee")
	if err := term.EnableTrace(tracePath); err != nil {
		t.Fatalf("EnableTrace: %v", err)
	}

	if err := term.WaitForText("NORMAL"); err != nil {
		t.Fatalf("WaitForText NORMAL: %v", err)
	}
	if err := term.Key(input.KeyUp); err != nil {
		t.Fatalf("normal Up: %v", err)
	}
	if err := term.WaitForText("APP"); err != nil {
		t.Fatalf("WaitForText APP: %v", err)
	}
	if err := term.Key(input.KeyUp); err != nil {
		t.Fatalf("application Up: %v", err)
	}
	if err := term.WaitForText("RESET"); err != nil {
		t.Fatalf("WaitForText RESET: %v", err)
	}
	if err := term.Key(input.KeyUp); err != nil {
		t.Fatalf("reset Up: %v", err)
	}
	if err := term.WaitForText("OK"); err != nil {
		t.Fatalf("WaitForText OK: %v", err)
	}
	if err := term.DisableTrace(); err != nil {
		t.Fatalf("DisableTrace: %v", err)
	}

	got := readKeyTraceEvents(t, tracePath)
	want := []keyTraceEvent{
		{Key: "Up", Bytes: []byte("\x1b[A")},
		{Key: "Up", Bytes: []byte("\x1bOA")},
		{Key: "Up", Bytes: []byte("\x1b[A")},
	}
	if len(got) != len(want) {
		t.Fatalf("key trace events = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i].Key != want[i].Key || !bytes.Equal(got[i].Bytes, want[i].Bytes) {
			t.Errorf("key trace event %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestPasteFollowsBracketedPasteMode(t *testing.T) {
	term := startEngineTerm(t, []string{
		"/bin/bash", "-c",
		"stty raw -echo; printf 'DISABLED'; IFS= read -r -N 1; printf '\033[?2004hENABLED'; IFS= read -r -N 13 pasted; [[ $pasted == $'\\e[200~x\\e[201~' ]] && printf 'OK'; sleep 30",
	}, 80, 5)
	if err := term.WaitForText("DISABLED"); err != nil {
		t.Fatalf("WaitForText DISABLED: %v", err)
	}
	if err := term.Paste("x"); err == nil {
		t.Fatal("Paste succeeded with mode 2004 disabled")
	}
	if err := term.Type("x"); err != nil {
		t.Fatalf("release child: %v", err)
	}
	if err := term.WaitForText("ENABLED"); err != nil {
		t.Fatalf("WaitForText ENABLED: %v", err)
	}
	if err := term.Paste("x"); err != nil {
		t.Fatalf("Paste with mode 2004 enabled: %v", err)
	}
	if err := term.WaitForText("OK"); err != nil {
		t.Fatalf("WaitForText OK: %v", err)
	}
}

func TestKeyRejectsActiveKittyKeyboardProtocol(t *testing.T) {
	term := startEngineTerm(t, []string{
		"/bin/bash", "-c",
		"printf '\033[>1uREADY'; sleep 30",
	}, 80, 5)
	if err := term.WaitForText("READY"); err != nil {
		t.Fatalf("WaitForText READY: %v", err)
	}
	err := term.Key(input.KeyUp)
	var requestErr *RequestError
	if !errors.As(err, &requestErr) {
		t.Fatalf("Key error = %v (%T), want *RequestError", err, err)
	}
	if requestErr.Kind != RequestErrorFailedPrecondition {
		t.Fatalf("Key error kind = %v, want failed precondition", requestErr.Kind)
	}
}

type keyTraceEvent struct {
	Key   string
	Bytes []byte
}

func readKeyTraceEvents(t *testing.T, path string) []keyTraceEvent {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open trace: %v", err)
	}
	defer func() { _ = zr.Close() }()
	eventsFile, err := zr.Open("events.jsonl")
	if err != nil {
		t.Fatalf("open events.jsonl: %v", err)
	}
	defer func() { _ = eventsFile.Close() }()

	var events []keyTraceEvent
	scanner := bufio.NewScanner(eventsFile)
	for scanner.Scan() {
		var event trace.EventRecord
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode trace event: %v", err)
		}
		if event.Type != trace.EventTypeInput || event.Kind != trace.InputKindKey {
			continue
		}
		encoded, err := base64.StdEncoding.DecodeString(event.Bytes)
		if err != nil {
			t.Fatalf("decode trace bytes: %v", err)
		}
		events = append(events, keyTraceEvent{Key: event.Key, Bytes: encoded})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan events: %v", err)
	}
	return events
}
