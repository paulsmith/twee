package daemon

import (
	"encoding/json"

	"github.com/paulsmith/twee/internal/rpc"
)

func decodeArgs[T any](raw json.RawMessage) (T, *rpc.Error) {
	var args T
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, invalidArgument(err)
	}
	return args, nil
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

func internalFailure(err error) *rpc.Error {
	return rpcError(rpc.CodeInternal, err.Error())
}

func notFoundMessage(message string) *rpc.Error {
	return rpcError(rpc.CodeNotFound, message)
}

func rpcError(code, message string) *rpc.Error {
	return &rpc.Error{Code: code, Message: message}
}
