package rpc

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxMessageBytes caps a single request/response. Large enough for a
// snapshot of an 80x24 terminal with 4-byte cells plus headroom.
const MaxMessageBytes = 4 * 1024 * 1024

// ErrTooLarge is returned when a message exceeds MaxMessageBytes.
var ErrTooLarge = errors.New("rpc: message exceeds MaxMessageBytes")

// WriteMessage encodes v as JSON and writes it length-prefixed to w.
func WriteMessage(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("rpc: marshal: %w", err)
	}
	if len(body) > MaxMessageBytes {
		return ErrTooLarge
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(body)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	return nil
}

// ReadMessage reads one length-prefixed JSON message from r and
// unmarshals it into v.
func ReadMessage(r io.Reader, v any) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > MaxMessageBytes {
		return ErrTooLarge
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}
