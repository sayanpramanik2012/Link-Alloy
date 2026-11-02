package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

const (
	stackNICID        = 1
	stackMTU          = 1420
	maxTCPFlows       = 1024
	maxUDPFlows       = 512
	udpIdleLimit      = 45 * time.Second
	tcpIdleLimit      = 5 * time.Minute
	tcpHalfCloseLimit = 15 * time.Second
	tcpRetireGrace    = 2 * time.Minute
)

var ErrFlowCapacity = errors.New("the transport flow limit has been reached")

type TransportStats struct {
	CapturedPackets   uint64 `json:"capturedPackets"`
	ReplyPackets      uint64 `json:"replyPackets"`
	UnsupportedDrops  uint64 `json:"unsupportedDrops"`
	UnknownFlowDrops  uint64 `json:"unknownFlowDrops"`
	CapacityDrops     uint64 `json:"capacityDrops"`
	BackpressureDrops uint64 `json:"backpressureDrops"`
	ActiveTCP         int    `json:"activeTCP"`
	ActiveUDP         int    `json:"activeUDP"`
}

type UpstreamDialer func(context.Context, uint32, string, string) (net.Conn, error)

// ForwardingStack is Link Alloy's transport engine. It accepts IP packets after
// local NAT, terminates TCP/UDP in an isolated user-space stack, and creates a
// separate upstream socket pinned to the selected physical interface.
type ForwardingStack struct {
	stack             *stack.Stack
	link              *channel.Endpoint
	flows             *FlowTable
	scheduler         *Scheduler
	dial              UpstreamDialer
	eventSink         EventSink
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	close             sync.Once
	closed            chan struct{}
	workMu            sync.Mutex
	closing           bool
	running           bool
	activeTCP         int
	activeUDP         int
	tcpLimit          int
	udpLimit          int
	tcpIdleTimeout    time.Duration
	halfCloseTimeout  time.Duration
	udpIdleTimeout    time.Duration
	capturedPackets   atomic.Uint64
	replyPackets      atomic.Uint64
	unsupportedDrops  atomic.Uint64
	unknownFlowDrops  atomic.Uint64
	capacityDrops     atomic.Uint64
	backpressureDrops atomic.Uint64
	sinkMu            sync.RWMutex
}

func NewForwardingStack(virtualIPv4, virtualIPv6 netip.Addr, flows *FlowTable, scheduler *Scheduler, dial UpstreamDialer) (*ForwardingStack, error) {
	if flows == nil || scheduler == nil {
		return nil, errors.New("flow table and link scheduler are required")
	}
	if !virtualIPv4.Is4() || !virtualIPv6.Is6() || virtualIPv6.Is4In6() {
		return nil, errors.New("the user-space stack requires one IPv4 and one IPv6 address")
	}
	if dial == nil {
		dial = dialOnInterface
	}
	ctx, cancel := context.WithCancel(context.Background())
	link := channel.New(2048, stackMTU, "")
	stk := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	if err := stk.CreateNIC(stackNICID, link); err != nil {
		cancel()
		stk.Close()
		return nil, fmt.Errorf("create user-space network interface: %s", err)
	}
	addresses := []tcpip.ProtocolAddress{
		{Protocol: ipv4.ProtocolNumber, AddressWithPrefix: tcpip.AddressWithPrefix{Address: tcpip.AddrFrom4(virtualIPv4.As4()), PrefixLen: tunnelIPv4Prefix}},
		{Protocol: ipv6.ProtocolNumber, AddressWithPrefix: tcpip.AddressWithPrefix{Address: tcpip.AddrFrom16(virtualIPv6.As16()), PrefixLen: tunnelIPv6Prefix}},
	}
	for _, address := range addresses {
		if err := stk.AddProtocolAddress(stackNICID, address, stack.AddressProperties{}); err != nil {
			link.Close()
			cancel()
			stk.Close()
			return nil, fmt.Errorf("assign virtual address %s: %s", address.AddressWithPrefix, err)
		}
	}
	stk.SetRouteTable([]tcpip.Route{
		{Destination: header.IPv4EmptySubnet, NIC: stackNICID},
		{Destination: header.IPv6EmptySubnet, NIC: stackNICID},
	})
	engine := &ForwardingStack{stack: stk, link: link, flows: flows, scheduler: scheduler, dial: dial, ctx: ctx, cancel: cancel, closed: make(chan struct{}), tcpLimit: maxTCPFlows, udpLimit: maxUDPFlows, tcpIdleTimeout: tcpIdleLimit, halfCloseTimeout: tcpHalfCloseLimit, udpIdleTimeout: udpIdleLimit}
	tcpForwarder := tcp.NewForwarder(stk, 1<<20, maxTCPFlows, engine.handleTCP)
	udpForwarder := udp.NewForwarder(stk, engine.handleUDP)
	stk.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpForwarder.HandlePacket)
	stk.SetTransportProtocolHandler(udp.ProtocolNumber, udpForwarder.HandlePacket)
	return engine, nil
}

// InjectCaptured translates an OS packet to a local virtual endpoint and injects
// it into the user-space stack. The source packet is left unmodified.
func (e *ForwardingStack) InjectCaptured(packet []byte) (Flow, bool, error) {
	if err := e.ctx.Err(); err != nil {
		return Flow{}, false, err
	}
	e.capturedPackets.Add(1)
	rewritten, flow, created, err := e.flows.RewriteOutbound(packet)
	if err != nil {
		switch {
		case errors.Is(err, ErrVirtualPortsFull):
			e.capacityDrops.Add(1)
		case errors.Is(err, ErrUnsupportedPacket):
			e.unsupportedDrops.Add(1)
		case errors.Is(err, ErrUnknownFlow):
			e.unknownFlowDrops.Add(1)
		}
		return Flow{}, false, err
	}
	if created && flow.Key.Protocol == protocolTCP {
		e.emit(EngineEvent{Level: "info", Category: "flow-captured", Message: "TCP connection captured from an application"})
	}
	var protocol tcpip.NetworkProtocolNumber
	switch rewritten[0] >> 4 {
	case 4:
		protocol = header.IPv4ProtocolNumber
	case 6:
		protocol = header.IPv6ProtocolNumber
	default:
		return Flow{}, false, ErrUnsupportedPacket
	}
	packetBuffer := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(rewritten)})
	e.link.InjectInbound(protocol, packetBuffer)
	packetBuffer.DecRef()
	return flow, created, nil
}

// ReadReply removes one packet emitted by the user-space stack and restores its
// remote source address so the original application accepts the response.
func (e *ForwardingStack) ReadReply(ctx context.Context) ([]byte, Flow, error) {
	packet := e.link.ReadContext(ctx)
	if packet == nil {
		if err := ctx.Err(); err != nil {
			return nil, Flow{}, err
		}
		return nil, Flow{}, errors.New("user-space packet queue closed")
	}
	defer packet.DecRef()
	segments := packet.AsSlices()
	length := 0
	for _, segment := range segments {
		length += len(segment)
	}
	raw := make([]byte, length)
	offset := 0
	for _, segment := range segments {
		offset += copy(raw[offset:], segment)
	}
	restored, flow, err := e.flows.RewriteInbound(raw)
	if err == nil {
		e.replyPackets.Add(1)
	}
	return restored, flow, err
}

// RunTunnel pumps IP packets between the Windows tunnel adapter and the
// user-space stack. Unsupported fragmented or non-TCP/UDP packets are dropped
// individually so one app's ICMP or fragmented traffic cannot stop other flows.
func (e *ForwardingStack) RunTunnel(ctx context.Context, device TunnelDevice) error {
	if device == nil {
		return errors.New("a Windows tunnel device is required")
	}
	e.workMu.Lock()
	if e.closing {
		e.workMu.Unlock()
		return context.Canceled
	}
	if e.running {
		e.workMu.Unlock()
		return errors.New("transport packet pump is already running")
	}
	e.running = true
	e.wg.Add(3)
	e.workMu.Unlock()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer e.cancel()
	stopOnClose := context.AfterFunc(e.ctx, cancel)
	defer stopOnClose()
	errorsFromLoops := make(chan error, 2)
	go func() {
		defer e.wg.Done()
		for {
			packet, err := device.Receive(runCtx)
			if err != nil {
				if runCtx.Err() == nil {
					errorsFromLoops <- fmt.Errorf("receive tunnel packet: %w", err)
				}
				return
			}
			if _, _, err := e.InjectCaptured(packet); err != nil && !errors.Is(err, ErrUnsupportedPacket) && !errors.Is(err, ErrUnknownFlow) && !errors.Is(err, ErrVirtualPortsFull) && !errors.Is(err, ErrFlowCapacity) {
				errorsFromLoops <- fmt.Errorf("inject captured packet: %w", err)
				return
			}
		}
	}()
	go func() {
		defer e.wg.Done()
		for {
			packet, _, err := e.ReadReply(runCtx)
			if err != nil {
				if runCtx.Err() == nil && !errors.Is(err, ErrUnsupportedPacket) && !errors.Is(err, ErrUnknownFlow) {
					errorsFromLoops <- fmt.Errorf("read stack reply: %w", err)
					return
				}
				if runCtx.Err() != nil {
					return
				}
				continue
			}
			if err := device.Send(packet); err != nil {
				if errors.Is(err, ErrTunnelBackpressure) {
					e.backpressureDrops.Add(1)
					continue
				}
				errorsFromLoops <- fmt.Errorf("send tunnel reply: %w", err)
				return
			}
		}
	}()
	go func() {
		defer e.wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case now := <-ticker.C:
				e.flows.ExpireIdle(now, 30*time.Second, udpIdleLimit)
			}
		}
	}()
	select {
	case err := <-errorsFromLoops:
		cancel()
		return err
	case <-runCtx.Done():
		return runCtx.Err()
	}
}

func (e *ForwardingStack) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = e.CloseContext(ctx)
}

// CloseContext stops admissions before waiting, so late TCP callbacks cannot add
// workers after a shutdown waiter has completed. The caller controls drain time.
func (e *ForwardingStack) CloseContext(ctx context.Context) error {
	e.close.Do(func() {
		e.workMu.Lock()
		e.closing = true
		e.cancel()
		e.workMu.Unlock()
		go func() {
			e.stack.Close()
			e.link.Close()
			e.wg.Wait()
			close(e.closed)
		}()
	})
	select {
	case <-e.closed:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("transport drain: %w", ctx.Err())
	}
}

func (e *ForwardingStack) SetEventSink(sink EventSink) {
	e.sinkMu.Lock()
	e.eventSink = sink
	e.sinkMu.Unlock()
}

func (e *ForwardingStack) Stats() TransportStats {
	e.workMu.Lock()
	tcpCount, udpCount := e.activeTCP, e.activeUDP
	e.workMu.Unlock()
	return TransportStats{CapturedPackets: e.capturedPackets.Load(), ReplyPackets: e.replyPackets.Load(), UnsupportedDrops: e.unsupportedDrops.Load(), UnknownFlowDrops: e.unknownFlowDrops.Load(), CapacityDrops: e.capacityDrops.Load(), BackpressureDrops: e.backpressureDrops.Load(), ActiveTCP: tcpCount, ActiveUDP: udpCount}
}

func (e *ForwardingStack) beginFlow(protocol uint8) bool {
	e.workMu.Lock()
	defer e.workMu.Unlock()
	if e.closing {
		return false
	}
	if protocol == protocolTCP {
		if e.activeTCP >= e.tcpLimit {
			e.capacityDrops.Add(1)
			return false
		}
		e.activeTCP++
	} else {
		if e.activeUDP >= e.udpLimit {
			e.capacityDrops.Add(1)
			return false
		}
		e.activeUDP++
	}
	e.wg.Add(1)
	return true
}

func (e *ForwardingStack) endFlow(protocol uint8) {
	e.workMu.Lock()
	if protocol == protocolTCP {
		e.activeTCP--
	} else {
		e.activeUDP--
	}
	e.workMu.Unlock()
	e.wg.Done()
}

func (e *ForwardingStack) emit(event EngineEvent) {
	e.sinkMu.RLock()
	sink := e.eventSink
	e.sinkMu.RUnlock()
	if sink == nil {
		return
	}
	if event.At.IsZero() {
		event.At = time.Now()
	}
	sink(event)
}

func (e *ForwardingStack) handleTCP(request *tcp.ForwarderRequest) {
	id := request.ID()
	flow, ok := e.flows.LookupVirtual(protocolTCP, id.LocalPort)
	if !ok || !requestMatchesFlow(id.RemoteAddress, id.RemotePort, flow) {
		request.Complete(true)
		return
	}
	if !e.beginFlow(protocolTCP) {
		request.Complete(true)
		e.flows.Retire(flow.Key, 2*time.Second)
		return
	}
	managed := false
	defer func() {
		if !managed {
			e.endFlow(protocolTCP)
			e.flows.Retire(flow.Key, 2*time.Second)
		}
	}()
	e.flows.MarkManaged(flow.Key)
	upstream, lease, err := e.openUpstream(flow, "tcp")
	if err != nil {
		request.Complete(true)
		return
	}
	var queue waiter.Queue
	endpoint, tcpErr := request.CreateEndpoint(&queue)
	if tcpErr != nil {
		request.Complete(true)
		upstream.Close()
		lease.Release()
		return
	}
	request.Complete(false)
	application := gonet.NewTCPConn(&queue, endpoint)
	managed = true
	go e.bridgeTCP(flow, application, upstream, lease)
}

func (e *ForwardingStack) handleUDP(request *udp.ForwarderRequest) bool {
	id := request.ID()
	flow, ok := e.flows.LookupVirtual(protocolUDP, id.LocalPort)
	if !ok || !requestMatchesFlow(id.RemoteAddress, id.RemotePort, flow) {
		return false
	}
	if !e.beginFlow(protocolUDP) {
		e.flows.Remove(flow.Key)
		return false
	}
	managed := false
	defer func() {
		if !managed {
			e.endFlow(protocolUDP)
			e.flows.Remove(flow.Key)
		}
	}()
	e.flows.MarkManaged(flow.Key)
	upstream, lease, err := e.openUpstream(flow, "udp")
	if err != nil {
		return false
	}
	var queue waiter.Queue
	endpoint, tcpErr := request.CreateEndpoint(&queue)
	if tcpErr != nil {
		upstream.Close()
		lease.Release()
		return false
	}
	application := gonet.NewUDPConn(&queue, endpoint)
	managed = true
	go e.bridgeUDP(flow, application, upstream, lease)
	return true
}

func requestMatchesFlow(remoteAddress tcpip.Address, remotePort uint16, flow Flow) bool {
	remote, ok := netip.AddrFromSlice(remoteAddress.AsSlice())
	return ok && remote == flow.Key.Source && remotePort == flow.Key.SourcePort
}

func (e *ForwardingStack) openUpstream(flow Flow, transport string) (net.Conn, *Lease, error) {
	excluded := make(map[uint32]struct{})
	var lastErr error
	address := net.JoinHostPort(flow.Key.Destination.String(), strconv.Itoa(int(flow.Key.DestinationPort)))
	if flow.Key.Destination.Is6() {
		transport += "6"
	} else {
		transport += "4"
	}
	for attempts := 0; attempts < len(e.scheduler.Snapshots()); attempts++ {
		lease, err := e.scheduler.AcquireExcludingForFamily(excluded, flow.Key.Destination.Is6())
		if err != nil {
			break
		}
		link := lease.Link()
		ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
		connection, dialErr := e.dial(ctx, link.Index, transport, address)
		cancel()
		if dialErr == nil {
			if strings.HasPrefix(transport, "tcp") {
				e.emit(EngineEvent{Level: "info", Category: "flow-start", Message: "TCP flow assigned to " + link.Name, LinkIndex: link.Index})
			}
			return connection, lease, nil
		}
		if strings.HasPrefix(transport, "tcp") {
			e.emit(EngineEvent{Level: "warning", Category: "link-dial-failed", Message: "TCP connection failed on " + link.Name + "; trying another available link", LinkIndex: link.Index})
		}
		lastErr = dialErr
		excluded[link.Index] = struct{}{}
		lease.Release()
	}
	if lastErr == nil {
		lastErr = ErrNoHealthyLink
	}
	return nil, nil, lastErr
}

func dialOnInterface(ctx context.Context, interfaceIndex uint32, network, address string) (net.Conn, error) {
	dialer, err := NewLinkDialer(interfaceIndex, 10*time.Second)
	if err != nil {
		return nil, err
	}
	return dialer.DialContext(ctx, network, address)
}

func (e *ForwardingStack) bridgeTCP(flow Flow, application, upstream net.Conn, lease *Lease) {
	defer e.endFlow(protocolTCP)
	defer func() { application.Close(); upstream.Close(); e.flows.Retire(flow.Key, tcpRetireGrace) }()
	defer lease.Release()
	stopOnShutdown := context.AfterFunc(e.ctx, func() {
		application.Close()
		upstream.Close()
	})
	defer stopOnShutdown()
	refreshDeadline := func() {
		deadline := time.Now().Add(e.tcpIdleTimeout)
		_ = application.SetDeadline(deadline)
		_ = upstream.SetDeadline(deadline)
	}
	refreshDeadline()

	type copyResult struct {
		sent uint64
		recv uint64
		err  error
	}
	results := make(chan copyResult, 2)
	go func() {
		n, err := io.Copy(flowByteWriter{destination: upstream, report: func(bytes uint64) { lease.Report(bytes, 0); refreshDeadline() }}, application)
		if closer, ok := upstream.(interface{ CloseWrite() error }); ok {
			_ = closer.CloseWrite()
		}
		results <- copyResult{sent: uint64(n), err: err}
	}()
	go func() {
		n, err := io.Copy(flowByteWriter{destination: application, report: func(bytes uint64) { lease.Report(0, bytes); refreshDeadline() }}, upstream)
		if closer, ok := application.(interface{ CloseWrite() error }); ok {
			_ = closer.CloseWrite()
		}
		results <- copyResult{recv: uint64(n), err: err}
	}()
	first := <-results
	if first.err != nil {
		_ = application.Close()
		_ = upstream.Close()
	}
	// A peer may half-close and never finish its remaining direction. Bound that
	// drain independently of the general idle timeout.
	timer := time.NewTimer(e.halfCloseTimeout)
	defer timer.Stop()
	var second copyResult
	select {
	case second = <-results:
	case <-timer.C:
		application.Close()
		upstream.Close()
		second = <-results
	}
	bytesSent := first.sent + second.sent
	bytesReceived := first.recv + second.recv
	e.emit(EngineEvent{Level: "info", Category: "flow-end", Message: "TCP flow ended", LinkIndex: lease.LinkIndex(), BytesSent: bytesSent, BytesReceived: bytesReceived})
}

func (e *ForwardingStack) bridgeUDP(flow Flow, application *gonet.UDPConn, upstream net.Conn, lease *Lease) {
	defer e.endFlow(protocolUDP)
	defer application.Close()
	defer upstream.Close()
	defer lease.Release()
	defer e.flows.Remove(flow.Key)
	stopOnShutdown := context.AfterFunc(e.ctx, func() {
		application.Close()
		upstream.Close()
	})
	defer stopOnShutdown()

	results := make(chan struct{}, 2)
	var sentTotal, receivedTotal atomic.Uint64
	refreshDeadline := func() {
		deadline := time.Now().Add(e.udpIdleTimeout)
		_ = application.SetDeadline(deadline)
		_ = upstream.SetDeadline(deadline)
	}
	refreshDeadline()
	go func() {
		buffer := make([]byte, 65535)
		for {
			n, err := application.Read(buffer)
			if err != nil {
				break
			}
			written, err := upstream.Write(buffer[:n])
			sentTotal.Add(uint64(written))
			lease.Report(uint64(written), 0)
			if written > 0 {
				refreshDeadline()
			}
			if err != nil {
				break
			}
		}
		results <- struct{}{}
	}()
	go func() {
		buffer := make([]byte, 65535)
		for {
			n, err := upstream.Read(buffer)
			if err != nil {
				break
			}
			written, err := application.Write(buffer[:n])
			receivedTotal.Add(uint64(written))
			lease.Report(0, uint64(written))
			if written > 0 {
				refreshDeadline()
			}
			if err != nil {
				break
			}
		}
		results <- struct{}{}
	}()
	<-results
	_ = application.Close()
	_ = upstream.Close()
	<-results
	sent, received := sentTotal.Load(), receivedTotal.Load()
	if sent+received >= 64*1024 {
		e.emit(EngineEvent{Level: "info", Category: "flow-end", Message: "Large UDP flow ended", LinkIndex: lease.LinkIndex(), BytesSent: sent, BytesReceived: received})
	}
}

type flowByteWriter struct {
	destination io.Writer
	report      func(uint64)
}

func (w flowByteWriter) Write(data []byte) (int, error) {
	count, err := w.destination.Write(data)
	if count > 0 {
		w.report(uint64(count))
	}
	return count, err
}
