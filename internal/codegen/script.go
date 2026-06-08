package codegen

import (
	"encoding/json"

	"github.com/paulsmith/twee/internal/rpc"
)

type recorder struct {
	ops []rpc.Request
}

func (r *recorder) Type(text string) error {
	if text == "" {
		return nil
	}
	if len(r.ops) > 0 {
		last := &r.ops[len(r.ops)-1]
		if last.Op == rpc.OpType {
			var args rpc.TypeArgs
			if err := json.Unmarshal(last.Args, &args); err == nil {
				args.Text += text
				raw, err := json.Marshal(args)
				if err != nil {
					return err
				}
				last.Args = raw
				return nil
			}
		}
	}
	return r.append(rpc.OpType, rpc.TypeArgs{Text: text})
}

func (r *recorder) Key(key string) error {
	return r.append(rpc.OpKey, rpc.KeyArgs{Key: key})
}

func (r *recorder) Paste(text string) error {
	return r.append(rpc.OpPaste, rpc.PasteArgs{Text: text})
}

func (r *recorder) Resize(cols, rows int) error {
	return r.append(rpc.OpResize, rpc.ResizeArgs{Cols: cols, Rows: rows})
}

func (r *recorder) WaitStable() error {
	return r.append(rpc.OpWaitStable, rpc.WaitStableArgs{})
}

func (r *recorder) Requests() []rpc.Request {
	out := make([]rpc.Request, len(r.ops))
	copy(out, r.ops)
	return out
}

func (r *recorder) append(op string, args any) error {
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	r.ops = append(r.ops, rpc.Request{Op: op, Args: raw})
	return nil
}
