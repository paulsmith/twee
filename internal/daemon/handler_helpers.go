package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
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

// decodeCanonicalArgs additionally rejects duplicate and noncanonical JSON
// field names. Use it for predicate operations, where silently replacing one
// constraint with another could turn a failed assertion into success.
func decodeCanonicalArgs[T any](raw json.RawMessage) (T, *rpc.Error) {
	if err := validateCanonicalJSON(raw, reflect.TypeFor[T]()); err != nil {
		var args T
		return args, invalidArgument(err)
	}
	return decodeArgs[T](raw)
}

func validateCanonicalJSON(raw json.RawMessage, typ reflect.Type) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := validateCanonicalJSONValue(dec, typ); err != nil {
		return err
	}
	if token, err := dec.Token(); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected JSON token %v after value", token)
	}
	return nil
}

func validateCanonicalJSONValue(dec *json.Decoder, typ reflect.Type) error {
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeFor[json.RawMessage]() || typ != nil && typ.Kind() == reflect.Interface {
		typ = nil
	}

	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delim {
	case '{':
		var fields map[string]reflect.Type
		if typ != nil && typ.Kind() == reflect.Struct {
			fields = canonicalJSONFields(typ)
		}
		seen := make(map[string]bool)
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key is not a string")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			seen[key] = true

			var fieldType reflect.Type
			if fields != nil {
				var exists bool
				fieldType, exists = fields[key]
				if !exists {
					for canonical := range fields {
						if strings.EqualFold(key, canonical) {
							return fmt.Errorf("noncanonical JSON field %q (want %q)", key, canonical)
						}
					}
					accepted := make([]string, 0, len(fields))
					for canonical := range fields {
						accepted = append(accepted, canonical)
					}
					slices.Sort(accepted)
					return fmt.Errorf("unknown JSON field %q (accepted: %s)", key, strings.Join(accepted, ", "))
				}
			}
			if err := validateCanonicalJSONValue(dec, fieldType); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	case '[':
		var elemType reflect.Type
		if typ != nil && (typ.Kind() == reflect.Array || typ.Kind() == reflect.Slice) {
			elemType = typ.Elem()
		}
		for dec.More() {
			if err := validateCanonicalJSONValue(dec, elemType); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
}

func canonicalJSONFields(typ reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type)
	for field := range typ.Fields() {
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field.Type
	}
	return fields
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
	requestErr, ok := errors.AsType[*engine.RequestError](err)
	if !ok {
		return internalFailure(err)
	}

	var code string
	if requestErr.Code != "" {
		code = requestErr.Code
	} else {
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
