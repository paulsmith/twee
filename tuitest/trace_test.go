package tuitest

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestTraceCapturesStartupOutput(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "startup.twee")
	term, err := Start(context.Background(),
		Command("/bin/sh", "-c", "printf 'startup\\r\\n'; sleep 30"),
		Size(40, 5),
		Trace(tracePath),
	)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := term.WaitForText("startup"); err != nil {
		_ = term.Close()
		t.Fatalf("WaitForText: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !traceHasOutput(t, tracePath, "startup") {
		t.Fatal("trace events missing startup output")
	}
}

func traceHasOutput(t *testing.T, tracePath, want string) bool {
	t.Helper()
	zr, err := zip.OpenReader(tracePath)
	if err != nil {
		t.Fatalf("open trace zip: %v", err)
	}
	defer zr.Close()
	ef, err := zr.Open("events.jsonl")
	if err != nil {
		t.Fatalf("events.jsonl: %v", err)
	}
	defer ef.Close()
	sc := bufio.NewScanner(ef)
	for sc.Scan() {
		var ev struct {
			Type  string `json:"type"`
			Bytes string `json:"bytes_b64"`
		}
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if ev.Type != "output" {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(ev.Bytes)
		if err != nil {
			t.Fatalf("decode output bytes: %v", err)
		}
		if strings.Contains(string(b), want) {
			return true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan events: %v", err)
	}
	return false
}
