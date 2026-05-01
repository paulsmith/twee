package play

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"
)

const kittyQuery = "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"

type terminalOps interface {
	IsTerminal(fd int) bool
	GetSize(fd int) (width, height int, err error)
	MakeRaw(fd int) (*term.State, error)
	Restore(fd int, oldState *term.State) error
}

type realTerminalOps struct{}

func (realTerminalOps) IsTerminal(fd int) bool { return term.IsTerminal(fd) }
func (realTerminalOps) GetSize(fd int) (int, int, error) {
	return term.GetSize(fd)
}
func (realTerminalOps) MakeRaw(fd int) (*term.State, error) { return term.MakeRaw(fd) }
func (realTerminalOps) Restore(fd int, old *term.State) error {
	return term.Restore(fd, old)
}

type preflightOptions struct {
	StdinFD  int
	StdoutFD int
	In       io.Reader
	Out      io.Writer
	Term     terminalOps
	Timeout  time.Duration
}

func defaultPreflightOptions(stdin, stdout *os.File) preflightOptions {
	return preflightOptions{
		StdinFD:  int(stdin.Fd()),
		StdoutFD: int(stdout.Fd()),
		In:       stdin,
		Out:      stdout,
		Term:     realTerminalOps{},
		Timeout:  200 * time.Millisecond,
	}
}

func checkStdoutTTY(opts preflightOptions) error {
	if opts.Term == nil {
		opts.Term = realTerminalOps{}
	}
	if !opts.Term.IsTerminal(opts.StdoutFD) {
		return fmt.Errorf("twee play: refusing to play to a non-tty")
	}
	return nil
}

func preflightBundle(bundle Bundle, opts preflightOptions) error {
	if err := checkStdoutTTY(opts); err != nil {
		return err
	}
	if opts.Term == nil {
		opts.Term = realTerminalOps{}
	}
	if opts.Timeout == 0 {
		opts.Timeout = 200 * time.Millisecond
	}
	width, height, err := opts.Term.GetSize(opts.StdoutFD)
	if err != nil {
		return fmt.Errorf("twee play: terminal size: %w", err)
	}
	needCols, needRows := bundle.MaxCols, bundle.MaxRows+2
	if needCols < 1 {
		needCols = 1
	}
	if needRows < 3 {
		needRows = 3
	}
	if width < needCols || height < needRows {
		return fmt.Errorf("twee play: terminal is %dx%d; trace needs at least %dx%d",
			width, height, needCols, needRows)
	}
	if err := queryKitty(opts); err != nil {
		return err
	}
	return nil
}

func queryKitty(opts preflightOptions) error {
	if opts.Term == nil {
		opts.Term = realTerminalOps{}
	}
	if opts.Timeout == 0 {
		opts.Timeout = 200 * time.Millisecond
	}
	old, err := opts.Term.MakeRaw(opts.StdinFD)
	if err != nil {
		return fmt.Errorf("twee play: raw mode: %w", err)
	}
	defer opts.Term.Restore(opts.StdinFD, old)

	if _, err := io.WriteString(opts.Out, kittyQuery); err != nil {
		return fmt.Errorf("twee play: kitty query: %w", err)
	}
	if d, ok := opts.In.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = d.SetReadDeadline(time.Now().Add(opts.Timeout))
		defer d.SetReadDeadline(time.Time{})
	}

	type result struct {
		reply []byte
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		reply, err := readAPCReply(opts.In, 4096)
		ch <- result{reply: reply, err: err}
	}()

	select {
	case res := <-ch:
		if res.err != nil || !bytes.Contains(res.reply, []byte("\x1b_Gi=31;OK\x1b\\")) {
			return fmt.Errorf("twee play: kitty graphics protocol not detected")
		}
		return nil
	case <-time.After(opts.Timeout):
		return fmt.Errorf("twee play: kitty graphics protocol not detected")
	}
}

func readAPCReply(r io.Reader, limit int) ([]byte, error) {
	var out bytes.Buffer
	var b [1]byte
	for out.Len() < limit {
		n, err := r.Read(b[:])
		if n > 0 {
			out.WriteByte(b[0])
			if bytes.HasSuffix(out.Bytes(), []byte("\x1b\\")) {
				return out.Bytes(), nil
			}
		}
		if err != nil {
			return out.Bytes(), err
		}
	}
	return out.Bytes(), fmt.Errorf("reply too large")
}
