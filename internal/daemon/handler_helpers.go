package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
)

// decodeArgs decodes raw into T, rejecting any JSON key that isn't one of
// T's own fields. A misnamed key (e.g. "pattern" instead of "text") is a
// documented footgun for ops like wait_text: silently ignoring it left the
// op waiting on its zero value, which for a string field succeeds
// instantly instead of failing loudly. DisallowUnknownFields turns that
// into an INVALID_ARGUMENT naming the bad key and T's accepted keys.
func decodeArgs[T any](raw json.RawMessage) (T, *rpc.Error) {
	var args T
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&args); err != nil {
		return args, unknownArgOr[T](invalidArgument(err), err)
	}
	return args, nil
}

// unknownArgOr rewrites a DisallowUnknownFields decode error into a
// message naming the offending key and T's accepted keys; any other
// decode error (malformed JSON, wrong type, ...) passes through as-is.
func unknownArgOr[T any](fallback *rpc.Error, err error) *rpc.Error {
	const prefix = "json: unknown field "
	msg := err.Error()
	if !strings.HasPrefix(msg, prefix) {
		return fallback
	}
	key := strings.Trim(strings.TrimPrefix(msg, prefix), `"`)
	keys := acceptedArgKeys[T]()
	if len(keys) == 0 {
		return invalidArgumentMessage(fmt.Sprintf("unknown arg %q", key))
	}
	return invalidArgumentMessage(fmt.Sprintf("unknown arg %q (accepted: %s)", key, strings.Join(keys, ", ")))
}

// acceptedArgKeys returns the JSON field names of args type T, in field
// declaration order, for use in "unknown arg" error messages.
func acceptedArgKeys[T any]() []string {
	t := reflect.TypeFor[T]()
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	keys := make([]string, 0, t.NumField())
	for f := range t.Fields() {
		f := f
		tag, ok := f.Tag.Lookup("json")
		if !ok {
			keys = append(keys, f.Name)
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		keys = append(keys, name)
	}
	return keys
}

func decodeOptionalArgs[T any](raw json.RawMessage) (T, *rpc.Error) {
	if len(raw) == 0 {
		var args T
		return args, nil
	}
	return decodeArgs[T](raw)
}

func invalidArgument(err error) *rpc.Error {
	return rpcError(rpc.CodeInvalidArgument, err.Error())
}

func invalidArgumentMessage(message string) *rpc.Error {
	return rpcError(rpc.CodeInvalidArgument, message)
}

func ioFailure(err error) *rpc.Error {
	return rpcError(rpc.CodeIO, err.Error())
}

func engineFailure(err error) *rpc.Error {
	var requestErr *engine.RequestError
	if !errors.As(err, &requestErr) {
		return internalFailure(err)
	}

	var code string
	switch requestErr.Kind {
	case engine.RequestErrorInvalidArgument:
		code = rpc.CodeInvalidArgument
	case engine.RequestErrorFailedPrecondition:
		code = rpc.CodeFailedPrecondition
	case engine.RequestErrorIO:
		code = rpc.CodeIO
	default:
		return internalFailure(err)
	}

	resp := rpcError(code, requestErr.Error())
	if requestErr.Details != nil {
		details, marshalErr := json.Marshal(requestErr.Details)
		if marshalErr != nil {
			return internalFailure(fmt.Errorf("marshal error details: %w", marshalErr))
		}
		resp.Details = details
	}
	return resp
}

func internalFailure(err error) *rpc.Error {
	return rpcError(rpc.CodeInternal, err.Error())
}

func notFoundMessage(message string) *rpc.Error {
	return rpcError(rpc.CodeNotFound, message)
}

func rpcError(code, message string) *rpc.Error {
	return &rpc.Error{Code: code, Message: message}
}
