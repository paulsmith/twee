//go:build linux

// Package netstack contains all direct use of gVisor netstack.
package netstack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/fdbased"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

const nicID tcpip.NICID = 1

const (
	maxGVisorTCPInFlight = 1024
	// Leave one gVisor slot unused. This ensures an admission accepted by our
	// synchronous wrapper can never encounter tcp.Forwarder's silent-drop
	// threshold, even while an earlier callback is completing.
	maxTCPInFlight         = maxGVisorTCPInFlight - 1
	maxTCPRejectedAttempts = 1024
	tcpRejectedRetention   = 30 * time.Second
)

// Sink receives packet and flow records. Calls may be concurrent.
type Sink interface {
	RecordPacket(time.Time, Direction, []byte) error
	RecordFlow(Flow) error
}

// Publication maps a host listener to a private TCP address.
type Publication struct {
	Listen string
	Guest  string
}

// Config controls the netstack proxy.
type Config struct {
	MTU            int
	DialTimeout    time.Duration
	UDPIdleTimeout time.Duration
	DNSAddress     string
	Publications   []Publication
}

// Runtime owns one netstack and its host sockets.
type Runtime struct {
	stack     *stack.Stack
	link      stack.LinkEndpoint
	sink      Sink
	cfg       Config
	ctx       context.Context
	cancel    context.CancelFunc
	listeners []net.Listener
	work      runtimeWork
	tcp       *tcpAdmission
	closeOnce sync.Once
	failOnce  sync.Once
	errors    chan error
}

// New attaches tunFD in raw-IP mode and starts all requested host listeners.
func New(tunFD int, cfg Config, sink Sink) (*Runtime, error) {
	if sink == nil {
		return nil, errors.New("netstack: recording sink is required")
	}
	link, err := fdbased.New(&fdbased.Options{
		FDs:                []int{tunFD},
		MTU:                uint32(cfg.MTU),
		EthernetHeader:     false,
		PacketDispatchMode: fdbased.Readv,
	})
	if err != nil {
		return nil, fmt.Errorf("make gVisor TUN endpoint: %w", err)
	}
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runtime{stack: s, link: link, sink: sink, cfg: cfg, ctx: ctx, cancel: cancel, errors: make(chan error, 1)}
	captured := &captureEndpoint{LinkEndpoint: link, sink: sink, runtime: r}
	r.link = captured
	// tcp.Forwarder silently drops SYNs after maxInFlight without invoking its
	// handler. Keep the same bound in netwrap, where rejected attempts can be
	// recorded, and put a small admission layer in front of gVisor.
	tcpForwarder := tcp.NewForwarder(s, 0, maxGVisorTCPInFlight, r.handleTCP)
	r.tcp = newTCPAdmission(r, maxTCPInFlight, maxTCPRejectedAttempts, tcpRejectedRetention, tcpForwarder.HandlePacket)
	udpForwarder := udp.NewForwarder(s, r.handleUDP)
	// SetTransportProtocolHandler is init-only in gVisor, and CreateNIC
	// attaches the link and starts packet dispatch, so both handlers must be
	// registered before the NIC exists.
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, r.tcp.handlePacket)
	s.SetTransportProtocolHandler(udp.ProtocolNumber, udpForwarder.HandlePacket)
	if err := tcpipError("create gVisor NIC", s.CreateNIC(nicID, captured)); err != nil {
		cancel()
		link.Close()
		return nil, err
	}
	gateway := tcpip.AddrFrom4([4]byte{10, 0, 2, 2})
	protocolAddress := tcpip.ProtocolAddress{
		Protocol:          ipv4.ProtocolNumber,
		AddressWithPrefix: gateway.WithPrefix(),
	}
	if err := tcpipError("add gVisor gateway address", s.AddProtocolAddress(nicID, protocolAddress, stack.AddressProperties{})); err != nil {
		cancel()
		s.Close()
		return nil, err
	}
	if err := tcpipError("enable gVisor spoofing", s.SetSpoofing(nicID, true)); err != nil {
		cancel()
		s.Close()
		return nil, err
	}
	if err := tcpipError("enable gVisor promiscuous mode", s.SetPromiscuousMode(nicID, true)); err != nil {
		cancel()
		s.Close()
		return nil, err
	}
	s.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: nicID}})
	if err := r.startPublications(); err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

func (r *Runtime) handleTCP(request *tcp.ForwarderRequest) {
	go func() {
		id := request.ID()
		// tcpAdmission reserved this work before tcp.Forwarder scheduled this
		// callback. It is therefore safe for Close to wait for this goroutine,
		// including while this callback is waiting to begin.
		defer r.tcp.finish()
		flow := newFlow("tcp", GuestToHost, endpoint(id.RemoteAddress, id.RemotePort), endpoint(id.LocalAddress, id.LocalPort))
		dialCtx, cancel := context.WithTimeout(r.ctx, r.cfg.DialTimeout)
		hostConn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp4", remapDNSDestination(flow.OriginalDestination, r.cfg.DNSAddress))
		cancel()
		if err != nil {
			request.Complete(true)
			r.tcp.release(id)
			r.recordCompletedFlow(&flow, err, 0, 0)
			return
		}
		var queue waiter.Queue
		ep, tcpErr := request.CreateEndpoint(&queue)
		if tcpErr != nil {
			request.Complete(true)
			r.tcp.release(id)
			_ = hostConn.Close()
			r.recordCompletedFlow(&flow, errors.New(tcpErr.String()), 0, 0)
			return
		}
		request.Complete(false)
		r.tcp.release(id)
		guestConn := gonet.NewTCPConn(&queue, ep)
		sent, received, copyErr := r.copyTCP(hostConn, guestConn)
		r.recordCompletedFlow(&flow, copyErr, sent, received)
	}()
}

func (r *Runtime) handleUDP(request *udp.ForwarderRequest) bool {
	if !r.work.start() {
		return false
	}
	id := request.ID()
	flow := newFlow("udp", GuestToHost, endpoint(id.RemoteAddress, id.RemotePort), endpoint(id.LocalAddress, id.LocalPort))
	// CreateEndpoint must finish before this handler returns: it registers the
	// endpoint with the stack's demultiplexer, so a back-to-back datagram on
	// the same 4-tuple is queued there instead of re-entering this forwarder
	// and failing with ErrPortInUse.
	var queue waiter.Queue
	ep, tcpErr := request.CreateEndpoint(&queue)
	if tcpErr != nil {
		r.recordCompletedFlow(&flow, errors.New(tcpErr.String()), 0, 0)
		r.work.done()
		return true
	}
	go func() {
		defer r.work.done()
		guestConn := gonet.NewUDPConn(&queue, ep)
		hostDestination := remapDNSDestination(flow.OriginalDestination, r.cfg.DNSAddress)
		dialer := net.Dialer{Timeout: r.cfg.DialTimeout}
		hostConn, err := dialer.DialContext(r.ctx, "udp4", hostDestination)
		if err != nil {
			_ = guestConn.Close()
			r.recordCompletedFlow(&flow, err, 0, 0)
			return
		}
		sent, received, copyErr := r.copyUDP(hostConn, guestConn)
		r.recordCompletedFlow(&flow, copyErr, sent, received)
	}()
	return true
}

func (r *Runtime) startPublications() error {
	for _, publication := range r.cfg.Publications {
		listener, err := net.Listen("tcp4", publication.Listen)
		if err != nil {
			return fmt.Errorf("listen on published TCP address %s: %w", publication.Listen, err)
		}
		r.listeners = append(r.listeners, listener)
		if !r.work.start() {
			_ = listener.Close()
			return errors.New("netstack: runtime is closing")
		}
		go r.acceptPublished(listener, publication.Guest)
	}
	return nil
}

func (r *Runtime) acceptPublished(listener net.Listener, guest string) {
	defer r.work.done()
	for {
		hostConn, err := listener.Accept()
		if err != nil {
			if r.ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			r.fail(fmt.Errorf("accept published TCP connection on %s: %w", listener.Addr(), err))
			return
		}
		if !r.work.start() {
			_ = hostConn.Close()
			return
		}
		go func() {
			defer r.work.done()
			defer func() { _ = hostConn.Close() }()
			flow := newFlow("tcp", HostToGuest, hostConn.RemoteAddr().String(), guest)
			address, err := fullAddress(guest)
			if err != nil {
				r.recordCompletedFlow(&flow, err, 0, 0)
				return
			}
			dialCtx, cancel := context.WithTimeout(r.ctx, r.cfg.DialTimeout)
			guestConn, err := gonet.DialContextTCP(dialCtx, r.stack, address, ipv4.ProtocolNumber)
			cancel()
			if err != nil {
				r.recordCompletedFlow(&flow, err, 0, 0)
				return
			}
			sent, received, copyErr := r.copyTCP(guestConn, hostConn)
			r.recordCompletedFlow(&flow, copyErr, sent, received)
		}()
	}
}

func (r *Runtime) copyTCP(destination, source net.Conn) (int64, int64, error) {
	defer func() { _ = destination.Close() }()
	defer func() { _ = source.Close() }()
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-r.ctx.Done():
			_ = destination.Close()
			_ = source.Close()
		case <-stopWatch:
		}
	}()
	type copyResult struct {
		n   int64
		err error
	}
	toDestination := make(chan copyResult, 1)
	toSource := make(chan copyResult, 1)
	go func() {
		n, err := io.Copy(destination, source)
		closeWrite(destination)
		toDestination <- copyResult{n: n, err: cleanCopyError(err)}
	}()
	go func() {
		n, err := io.Copy(source, destination)
		closeWrite(source)
		toSource <- copyResult{n: n, err: cleanCopyError(err)}
	}()
	var first, second copyResult
	select {
	case first = <-toDestination:
		select {
		case second = <-toSource:
		case <-r.ctx.Done():
			_ = destination.Close()
			_ = source.Close()
			second = <-toSource
		}
		return first.n, second.n, errors.Join(first.err, second.err)
	case first = <-toSource:
		select {
		case second = <-toDestination:
		case <-r.ctx.Done():
			_ = destination.Close()
			_ = source.Close()
			second = <-toDestination
		}
		return second.n, first.n, errors.Join(first.err, second.err)
	}
}

func (r *Runtime) copyUDP(host, guest net.Conn) (int64, int64, error) {
	defer func() { _ = host.Close() }()
	defer func() { _ = guest.Close() }()
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-r.ctx.Done():
			_ = host.Close()
			_ = guest.Close()
		case <-stopWatch:
		}
	}()
	var sent atomic.Int64
	var received atomic.Int64
	done := make(chan error, 2)
	touch := func() {
		deadline := time.Now().Add(r.cfg.UDPIdleTimeout)
		_ = host.SetDeadline(deadline)
		_ = guest.SetDeadline(deadline)
	}
	pump := func(dst, src net.Conn, counter *atomic.Int64) {
		buf := make([]byte, 65535)
		for {
			touch()
			n, err := src.Read(buf)
			if err != nil {
				if timeoutError(err) {
					done <- nil
				} else {
					done <- cleanCopyError(err)
				}
				return
			}
			written, err := dst.Write(buf[:n])
			counter.Add(int64(written))
			if err != nil {
				done <- cleanCopyError(err)
				return
			}
		}
	}
	go pump(host, guest, &sent)
	go pump(guest, host, &received)
	err := <-done
	_ = host.Close()
	_ = guest.Close()
	second := <-done
	return sent.Load(), received.Load(), errors.Join(err, second)
}

// Close stops host listeners, packet I/O, and active proxy connections.
func (r *Runtime) Close() {
	r.closeOnce.Do(func() {
		r.cancel()
		for _, listener := range r.listeners {
			_ = listener.Close()
		}
		// Stop input before waiting. tcpAdmission has already registered every
		// accepted TCP callback with work, so no callback can access the stack or
		// sink after this method proceeds to close them.
		r.link.Attach(nil)
		r.work.closeAndWait()
		r.stack.Close()
		r.link.Close()
		r.link.Wait()
	})
}

// Errors reports the first fatal recording error.
func (r *Runtime) Errors() <-chan error {
	return r.errors
}

func (r *Runtime) fail(err error) {
	if err == nil {
		return
	}
	r.failOnce.Do(func() {
		r.errors <- fmt.Errorf("record network evidence: %w", err)
		r.cancel()
	})
}

func (r *Runtime) recordFlow(flow Flow) {
	if err := r.sink.RecordFlow(flow); err != nil {
		r.fail(err)
	}
}

// recordCompletedFlow finalizes flow and submits it to the recording sink.
func (r *Runtime) recordCompletedFlow(flow *Flow, err error, sent, received int64) {
	finishFlow(flow, resultFor(err), err, sent, received)
	r.recordFlow(*flow)
}

type captureEndpoint struct {
	stack.LinkEndpoint
	sink    Sink
	runtime *Runtime
}

func (e *captureEndpoint) Attach(dispatcher stack.NetworkDispatcher) {
	if dispatcher == nil {
		e.LinkEndpoint.Attach(nil)
		return
	}
	e.LinkEndpoint.Attach(&captureDispatcher{NetworkDispatcher: dispatcher, sink: e.sink, runtime: e.runtime})
}

func (e *captureEndpoint) WritePackets(packets stack.PacketBufferList) (int, tcpip.Error) {
	copies := make([][]byte, 0, packets.Len())
	for _, packet := range packets.AsSlice() {
		copy, _ := rawIPv4Packet(packet)
		copies = append(copies, copy)
	}
	written, err := e.LinkEndpoint.WritePackets(packets)
	for _, packet := range copies[:written] {
		if packet == nil {
			continue
		}
		if recordErr := e.sink.RecordPacket(time.Now(), HostToGuest, packet); recordErr != nil {
			e.runtime.fail(recordErr)
		}
	}
	return written, err
}

type captureDispatcher struct {
	stack.NetworkDispatcher
	sink    Sink
	runtime *Runtime
}

func (d *captureDispatcher) DeliverNetworkPacket(protocol tcpip.NetworkProtocolNumber, packet *stack.PacketBuffer) {
	if protocol == ipv4.ProtocolNumber {
		raw, ok := rawIPv4Packet(packet)
		if !ok {
			d.NetworkDispatcher.DeliverNetworkPacket(protocol, packet)
			return
		}
		if err := d.sink.RecordPacket(time.Now(), GuestToHost, raw); err != nil {
			d.runtime.fail(err)
		}
	}
	d.NetworkDispatcher.DeliverNetworkPacket(protocol, packet)
}

func endpoint(address tcpip.Address, port uint16) string {
	return net.JoinHostPort(net.IP(address.AsSlice()).String(), strconv.Itoa(int(port)))
}

func fullAddress(address string) (tcpip.FullAddress, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return tcpip.FullAddress{}, err
	}
	ip := net.ParseIP(host).To4()
	if ip == nil {
		return tcpip.FullAddress{}, errors.New("published guest address must be IPv4")
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return tcpip.FullAddress{}, err
	}
	return tcpip.FullAddress{NIC: nicID, Addr: tcpip.AddrFrom4([4]byte(ip)), Port: uint16(port)}, nil
}

func newFlow(protocol string, direction Direction, source, destination string) Flow {
	return Flow{Protocol: protocol, Direction: direction, Source: source, OriginalDestination: destination, StartTime: time.Now()}
}

func finishFlow(flow *Flow, result string, err error, sent, received int64) {
	flow.EndTime = time.Now()
	flow.Result = result
	flow.BytesSent = sent
	flow.BytesReceived = received
	if err != nil {
		flow.Error = err.Error()
	}
}

func resultFor(err error) string {
	if err != nil {
		return "failed"
	}
	return "success"
}

func tcpipError(action string, err tcpip.Error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %s", action, err.String())
}

func cleanCopyError(err error) error {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func timeoutError(err error) bool {
	netErr, ok := errors.AsType[net.Error](err)
	return ok && netErr.Timeout()
}

func closeWrite(conn net.Conn) {
	type closeWriter interface{ CloseWrite() error }
	if closer, ok := conn.(closeWriter); ok {
		_ = closer.CloseWrite()
	}
}
