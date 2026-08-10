// Package daemon serves twee RPC ops over a Unix socket against a
// single engine.Term.
package daemon

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
)

// Server owns one engine.Term and serves RPC over a net.Listener.
type Server struct {
	term *engine.Term
	d    *Dispatcher

	wg     sync.WaitGroup
	stopCh chan struct{}
	once   sync.Once
}

// NewServer constructs a Server wrapping the given Term.
func NewServer(t *engine.Term) *Server {
	return &Server{
		term:   t,
		d:      NewDispatcher(t),
		stopCh: make(chan struct{}),
	}
}

// Serve accepts connections on l until either the listener errors, the
// context cancels, or Stop is called.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	go func() {
		select {
		case <-ctx.Done():
			_ = l.Close()
		case <-s.stopCh:
			_ = l.Close()
		}
	}()
	for {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				s.wg.Wait()
				return nil
			}
			return err
		}
		s.wg.Go(func() { s.handleConn(conn) })
	}
}

// Stop signals the accept loop to exit. Existing connections drain.
func (s *Server) Stop() {
	s.once.Do(func() { close(s.stopCh) })
}

func (s *Server) handleConn(c net.Conn) {
	defer c.Close()
	var req rpc.Request
	if err := rpc.ReadMessage(c, &req); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return
		}
		_ = rpc.WriteMessage(c, rpc.Response{
			OK: false,
			Error: &rpc.Error{
				Code:    rpc.CodeIO,
				Message: "rpc read: " + err.Error(),
			},
		})
		return
	}
	resp := s.d.Dispatch(req)
	_ = rpc.WriteMessage(c, resp)
}
