package daemon

import (
	"encoding/json"
	"strings"
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

// TestDecodeArgsRejectsUnknownField pins down the documented footgun fix:
// {"op":"wait_text","args":{"pattern":"never"}} used to silently ignore
// the misnamed "pattern" key (the wire name is "text") and wait on the
// empty string, succeeding instantly. It must now be rejected outright.
func TestDecodeArgsRejectsUnknownField(t *testing.T) {
	_, got := decodeArgs[rpc.WaitTextArgs](json.RawMessage(`{"pattern":"never"}`))
	if got == nil {
		t.Fatal("decodeArgs unexpectedly accepted an unknown key")
	}
	if got.Code != rpc.CodeInvalidArgument {
		t.Fatalf("code = %q, want %q", got.Code, rpc.CodeInvalidArgument)
	}
	if !strings.Contains(got.Message, `"pattern"`) {
		t.Fatalf("message = %q, want it to name the offending key", got.Message)
	}
	for _, want := range []string{"text", "regex", "timeout"} {
		if !strings.Contains(got.Message, want) {
			t.Fatalf("message = %q, want it to list accepted key %q", got.Message, want)
		}
	}
}

func TestAcceptedArgKeys(t *testing.T) {
	got := acceptedArgKeys[rpc.FindArgs]()
	want := []string{"text", "regex"}
	if len(got) != len(want) {
		t.Fatalf("acceptedArgKeys = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("acceptedArgKeys = %#v, want %#v", got, want)
		}
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
