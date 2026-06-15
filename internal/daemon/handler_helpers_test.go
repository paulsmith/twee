package daemon

import (
	"encoding/json"
	"testing"

	"github.com/paulsmith/twee/internal/rpc"
)

func TestDecodeArgsInvalidJSON(t *testing.T) {
	_, got := decodeArgs[rpc.CellArgs](json.RawMessage(`{`))
	if got == nil {
		t.Fatal("decodeArgs unexpectedly accepted malformed JSON")
	}
	if got.Code != rpc.CodeInvalidArgument {
		t.Fatalf("code = %q, want %q", got.Code, rpc.CodeInvalidArgument)
	}
}

func TestDecodeOptionalArgs(t *testing.T) {
	got, errResp := decodeOptionalArgs[rpc.TraceStartArgs](nil)
	if errResp != nil {
		t.Fatalf("empty args error = %+v, want nil", errResp)
	}
	if got.Out != "" {
		t.Fatalf("empty args out = %q, want empty", got.Out)
	}

	got, errResp = decodeOptionalArgs[rpc.TraceStartArgs](json.RawMessage(`{"out":"session.twee"}`))
	if errResp != nil {
		t.Fatalf("valid args error = %+v, want nil", errResp)
	}
	if got.Out != "session.twee" {
		t.Fatalf("out = %q, want session.twee", got.Out)
	}
}
