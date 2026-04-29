package rpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestRoundTripRequest(t *testing.T) {
	want := Request{
		ID:   "abc",
		Op:   OpType,
		Args: json.RawMessage(`{"text":"hello"}`),
	}
	var buf bytes.Buffer
	if err := WriteMessage(&buf, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	var got Request
	if err := ReadMessage(&buf, &got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.ID != want.ID || got.Op != want.Op {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if string(got.Args) != string(want.Args) {
		t.Errorf("args: got %s, want %s", got.Args, want.Args)
	}
}

func TestRoundTripResponseError(t *testing.T) {
	want := Response{
		ID: "abc",
		OK: false,
		Error: &Error{
			Code:    CodeTimeout,
			Message: "wait timed out",
		},
	}
	var buf bytes.Buffer
	if err := WriteMessage(&buf, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	var got Response
	if err := ReadMessage(&buf, &got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.OK != false || got.Error == nil || got.Error.Code != CodeTimeout {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestReadShortHeader(t *testing.T) {
	var got Request
	err := ReadMessage(strings.NewReader("ab"), &got)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("expected ErrUnexpectedEOF, got %v", err)
	}
}

func TestWriteTooLarge(t *testing.T) {
	huge := make([]byte, MaxMessageBytes+1)
	for i := range huge {
		huge[i] = 'x'
	}
	var buf bytes.Buffer
	err := WriteMessage(&buf, struct {
		X string `json:"x"`
	}{X: string(huge)})
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("expected ErrTooLarge, got %v", err)
	}
}
