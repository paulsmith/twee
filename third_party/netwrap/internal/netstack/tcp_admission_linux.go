//go:build linux

package netstack

import (
	"errors"
	"sync"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// runtimeWork closes the WaitGroup Add/Wait race. Every asynchronous operation
// must reserve work before it can be scheduled, and Close prevents further
// reservations before it starts to wait.
type runtimeWork struct {
	mu      sync.Mutex
	closing bool
	wg      sync.WaitGroup
}

func (w *runtimeWork) start() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closing {
		return false
	}
	w.wg.Add(1)
	return true
}

func (w *runtimeWork) done() {
	w.wg.Done()
}

func (w *runtimeWork) closeAndWait() {
	w.mu.Lock()
	w.closing = true
	w.mu.Unlock()
	w.wg.Wait()
}

type tcpAttempt struct {
	admitted   bool
	rejectedAt time.Time
}

// tcpAdmission observes every valid initial SYN before gVisor's fixed-size
// tcp.Forwarder can silently drop it. Accepted attempts reserve runtime work
// synchronously, which also covers the interval before gVisor starts its
// callback. Rejected attempts are retained for a bounded period so SYN
// retransmissions do not produce duplicate failed flow records. The rejection
// cache has a hard cap; once full, netwrap prefers recording evidence over
// silently dropping it, so retransmissions for uncached attempts may repeat.
type tcpAdmission struct {
	runtime       *Runtime
	limit         int
	rejectedLimit int
	retention     time.Duration
	forward       func(stack.TransportEndpointID, *stack.PacketBuffer) bool
	now           func() time.Time

	mu       sync.Mutex
	attempts map[stack.TransportEndpointID]tcpAttempt
	admitted int
	rejected int
}

func newTCPAdmission(runtime *Runtime, limit, rejectedLimit int, retention time.Duration, forward func(stack.TransportEndpointID, *stack.PacketBuffer) bool) *tcpAdmission {
	return &tcpAdmission{
		runtime: runtime, limit: limit, rejectedLimit: rejectedLimit, retention: retention, forward: forward,
		now: time.Now, attempts: make(map[stack.TransportEndpointID]tcpAttempt),
	}
}

func (a *tcpAdmission) handlePacket(id stack.TransportEndpointID, pkt *stack.PacketBuffer) bool {
	if !validInitialTCPSYN(id, pkt) {
		return a.forward(id, pkt)
	}

	if !a.runtime.work.start() {
		// Close has detached input and will not close the sink until all prior
		// reservations complete, so a packet arriving afterward is not an
		// observable connection attempt for this runtime.
		return true
	}

	now := a.now()
	a.mu.Lock()
	a.pruneRejectedLocked(now)
	if _, exists := a.attempts[id]; exists {
		a.mu.Unlock()
		a.runtime.work.done()
		return true
	}
	if a.admitted < a.limit {
		a.attempts[id] = tcpAttempt{admitted: true}
		a.admitted++
		a.mu.Unlock()
		if a.forward(id, pkt) {
			return true
		}
		// Keep our state in lockstep with tcp.Forwarder if it rejects a packet
		// for an additional gVisor validity reason.
		a.release(id)
		a.finish()
		return false
	}
	if a.rejected < a.rejectedLimit {
		a.attempts[id] = tcpAttempt{rejectedAt: now}
		a.rejected++
	}
	a.mu.Unlock()

	flow := newFlow("tcp", GuestToHost, endpoint(id.RemoteAddress, id.RemotePort), endpoint(id.LocalAddress, id.LocalPort))
	a.runtime.recordCompletedFlow(&flow, errors.New("tcp forwarder capacity exceeded"), 0, 0)
	a.runtime.work.done()
	// Match tcp.Forwarder's capacity behavior: the SYN is consumed and no
	// connection is established. Unlike tcp.Forwarder, evidence is retained.
	return true
}

// release frees the bounded pending-forwarder slot. The independently reserved
// runtime work remains active until finish, so established proxying and final
// flow recording are still part of Runtime.Close.
func (a *tcpAdmission) release(id stack.TransportEndpointID) {
	a.mu.Lock()
	if attempt, ok := a.attempts[id]; ok && attempt.admitted {
		delete(a.attempts, id)
		a.admitted--
	}
	a.mu.Unlock()
}

func (a *tcpAdmission) finish() {
	a.runtime.work.done()
}

func (a *tcpAdmission) pruneRejectedLocked(now time.Time) {
	for id, attempt := range a.attempts {
		if !attempt.rejectedAt.IsZero() && now.Sub(attempt.rejectedAt) >= a.retention {
			delete(a.attempts, id)
			a.rejected--
		}
	}
}

func validInitialTCPSYN(id stack.TransportEndpointID, pkt *stack.PacketBuffer) bool {
	hdr := header.TCP(pkt.TransportHeader().Slice())
	_, checksumValid, ok := header.TCPValid(
		hdr,
		func() uint16 { return pkt.Data().Checksum() },
		uint16(pkt.Data().Size()),
		id.RemoteAddress,
		id.LocalAddress,
		pkt.RXChecksumValidated,
	)
	return ok && checksumValid && hdr.Flags().Contains(header.TCPFlagSyn) && !hdr.Flags().Contains(header.TCPFlagAck)
}
