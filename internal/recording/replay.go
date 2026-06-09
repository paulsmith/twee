package recording

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/paulsmith/twee/internal/vt"
)

// ReplayInto reads a recording from path and feeds output and resize
// events into model. Input events are skipped (they are diagnostic
// metadata).
//
// On success returns the number of output bytes fed.
func ReplayInto(path string, model vt.Model) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	// Header
	if !sc.Scan() {
		return 0, errors.New("recording: empty")
	}
	var hdr Header
	if err := json.Unmarshal(sc.Bytes(), &hdr); err != nil {
		return 0, err
	}
	if hdr.Cols > 0 && hdr.Rows > 0 {
		_ = model.Resize(hdr.Cols, hdr.Rows)
	}
	total := 0
	for sc.Scan() {
		var ev Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			return total, err
		}
		switch ev.Type {
		case "output":
			b, err := base64.StdEncoding.DecodeString(ev.Bytes)
			if err != nil {
				return total, err
			}
			if err := model.Feed(b); err != nil {
				return total, err
			}
			total += len(b)
		case "resize":
			_ = model.Resize(ev.Cols, ev.Rows)
		}
	}
	if err := sc.Err(); err != nil && err != io.EOF {
		return total, err
	}
	return total, nil
}
