package daemon

import (
	"encoding/json"
	"fmt"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
)

// Dispatcher maps op names to handler functions.
type Dispatcher struct {
	term     *engine.Term
	handlers map[string]Handler
}

// Handler processes one request's args and returns either a data
// payload or an error.
type Handler func(t *engine.Term, args json.RawMessage) (any, *rpc.Error)

// NewDispatcher returns a Dispatcher with all built-in handlers
// registered. Later milestones (M3, M5, M6) add registerInput,
// registerQueries, registerWaits, etc.
func NewDispatcher(t *engine.Term) *Dispatcher {
	d := &Dispatcher{term: t, handlers: map[string]Handler{}}
	d.registerLifecycle()
	for _, reg := range optionalRegistrations {
		reg(d)
	}
	return d
}

// optionalRegistrations is appended to from init() in handler files
// added in later milestones; this lets each milestone land independently
// without modifying NewDispatcher.
var optionalRegistrations []func(*Dispatcher)

// Register installs a handler for an op name. Panics on duplicate.
func (d *Dispatcher) Register(op string, h Handler) {
	if _, exists := d.handlers[op]; exists {
		panic(fmt.Sprintf("daemon: duplicate op %q", op))
	}
	d.handlers[op] = h
}

// Dispatch executes one request against the registered handlers.
func (d *Dispatcher) Dispatch(req rpc.Request) rpc.Response {
	resp := rpc.Response{ID: req.ID}
	h, ok := d.handlers[req.Op]
	if !ok {
		resp.OK = false
		resp.Error = invalidArgumentMessage(fmt.Sprintf("unknown op %q", req.Op))
		return resp
	}
	data, errResp := h(d.term, req.Args)
	if errResp != nil {
		resp.OK = false
		resp.Error = errResp
		return resp
	}
	resp.OK = true
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			resp.OK = false
			resp.Error = rpcError(rpc.CodeInternal, "marshal data: "+err.Error())
			return resp
		}
		resp.Data = raw
	} else {
		resp.Data = json.RawMessage("null")
	}
	return resp
}
