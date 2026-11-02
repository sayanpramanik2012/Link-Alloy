package engine

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type packetTestDevice struct {
	input  chan []byte
	output chan []byte
	send   func([]byte) error
}

func newPacketTestDevice() *packetTestDevice {
	return &packetTestDevice{input: make(chan []byte, 8), output: make(chan []byte, 8)}
}
func (*packetTestDevice) InterfaceName() string              { return "in-memory-test" }
func (*packetTestDevice) InterfaceLUID() uint64              { return 1 }
func (*packetTestDevice) InterfaceIndexes() (uint32, uint32) { return 1, 1 }
func (*packetTestDevice) Close() error                       { return nil }
func (d *packetTestDevice) Receive(ctx context.Context) ([]byte, error) {
	select {
	case p := <-d.input:
		return p, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (d *packetTestDevice) Send(p []byte) error {
	if d.send != nil {
		if err := d.send(p); err != nil {
			return err
		}
	}
	d.output <- append([]byte(nil), p...)
	return nil
}

func newHardeningForwarder(t *testing.T, scheduler *Scheduler, dial UpstreamDialer) *ForwardingStack {
	t.Helper()
	f, err := NewForwardingStack(netip.MustParseAddr("198.18.0.1"), netip.MustParseAddr("fd00:6c69:6e6b::1"), testFlowTable(t), scheduler, dial)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)
	return f
}

func waitForCondition(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true before deadline")
}

func TestPacketPumpDropsBackpressureWithoutStopping(t *testing.T) {
	f := newHardeningForwarder(t, NewScheduler(nil), nil)
	d := newPacketTestDevice()
	var sends atomic.Int32
	d.send = func([]byte) error {
		if sends.Add(1) == 1 {
			return ErrTunnelBackpressure
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.RunTunnel(ctx, d) }()
	client, remote := netip.MustParseAddr("198.18.0.2"), netip.MustParseAddr("203.0.113.80")
	d.input <- testTCPPacket4(client, remote, 55100, 443, 0x02)
	d.input <- testTCPPacket4(client, remote, 55101, 443, 0x02)
	select {
	case <-d.output:
	case err := <-done:
		t.Fatalf("packet pump stopped: %v", err)
	case <-ctx.Done():
		t.Fatal("no reply after a transient queue drop")
	}
	if got := f.Stats().BackpressureDrops; got != 1 {
		t.Fatalf("backpressure drops=%d, want 1", got)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("packet pump did not stop")
	}
}

func TestPacketPumpPropagatesPermanentDeviceFailure(t *testing.T) {
	f := newHardeningForwarder(t, NewScheduler(nil), nil)
	d := newPacketTestDevice()
	fatalErr := errors.New("device disconnected")
	d.send = func([]byte) error { return fatalErr }
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	d.input <- testTCPPacket4(netip.MustParseAddr("198.18.0.2"), netip.MustParseAddr("203.0.113.80"), 55100, 443, 0x02)
	if err := f.RunTunnel(ctx, d); !errors.Is(err, fatalErr) {
		t.Fatalf("RunTunnel error=%v, want permanent device failure", err)
	}
}

func TestExhaustedTCPMappingsDoNotStopEstablishedUDPTransfer(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	serverPort := server.LocalAddr().(*net.UDPAddr).Port
	go func() {
		buf := make([]byte, 64)
		for {
			n, peer, err := server.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = server.WriteToUDP(buf[:n], peer)
		}
	}()
	s := NewScheduler([]Link{{Index: 77, Name: "Test link", Weight: 1, Available: true}})
	f := newHardeningForwarder(t, s, func(ctx context.Context, _ uint32, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	})
	client, remote := netip.MustParseAddr("198.18.0.2"), netip.MustParseAddr("127.0.0.1")
	packet := testUDPPacket4(client, remote, 55124, uint16(serverPort), []byte("still alive"))
	if _, _, err := f.InjectCaptured(packet); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := f.ReadReply(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= lastVirtualPort-firstVirtualPort; i++ {
		if _, _, _, err := f.flows.RewriteOutbound(testTCPPacket4(client, remote, uint16(10000+i), 443, 0x02)); err != nil {
			t.Fatalf("fill NAT table: %v", err)
		}
	}
	d := newPacketTestDevice()
	d.input <- testTCPPacket4(client, remote, 60001, 443, 0x02)
	d.input <- packet
	done := make(chan error, 1)
	go func() { done <- f.RunTunnel(ctx, d) }()
	select {
	case reply := <-d.output:
		if string(reply[28:]) != "still alive" {
			t.Fatalf("reply=%q", reply[28:])
		}
	case err := <-done:
		t.Fatalf("capacity exhaustion stopped pump: %v", err)
	case <-ctx.Done():
		t.Fatal("established UDP flow stopped after TCP NAT exhaustion")
	}
	if f.Stats().CapacityDrops == 0 {
		t.Fatal("capacity rejection was not counted")
	}
	cancel()
	<-done
}

func TestFailedTCPSetupRetainsResetMappingThenReclaimsIt(t *testing.T) {
	f := newHardeningForwarder(t, NewScheduler(nil), nil)
	flow, _, err := f.InjectCaptured(testTCPPacket4(netip.MustParseAddr("198.18.0.2"), netip.MustParseAddr("203.0.113.80"), 55123, 443, 0x02))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err := f.ReadReply(ctx); err != nil {
		t.Fatal(err)
	}
	waitForCondition(t, func() bool {
		mapping, ok := f.flows.LookupVirtual(protocolTCP, flow.VirtualPort)
		return ok && !mapping.RetireAt.IsZero()
	})
	if got := f.flows.ExpireIdle(time.Now().Add(3*time.Second), time.Hour, time.Hour); got != 1 {
		t.Fatalf("reclaimed mappings=%d, want 1", got)
	}
	if got := f.Stats().ActiveTCP; got != 0 {
		t.Fatalf("active TCP=%d after failed setup", got)
	}
}

func TestTransportAdmissionAndBoundedShutdown(t *testing.T) {
	f := newHardeningForwarder(t, NewScheduler(nil), nil)
	f.tcpLimit = 2
	f.udpLimit = 1
	if !f.beginFlow(protocolTCP) || !f.beginFlow(protocolTCP) || f.beginFlow(protocolTCP) {
		t.Fatal("TCP quota not enforced")
	}
	if !f.beginFlow(protocolUDP) || f.beginFlow(protocolUDP) {
		t.Fatal("UDP quota not enforced")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := f.CloseContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CloseContext=%v, want bounded timeout", err)
	}
	if f.beginFlow(protocolTCP) {
		t.Fatal("shutdown admitted another flow")
	}
	f.endFlow(protocolTCP)
	f.endFlow(protocolTCP)
	f.endFlow(protocolUDP)
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := f.CloseContext(ctx2); err != nil {
		t.Fatal(err)
	}
	stats := f.Stats()
	if stats.ActiveTCP != 0 || stats.ActiveUDP != 0 {
		t.Fatalf("resources retained: %#v", stats)
	}
}

func TestTCPIdleAndHalfCloseReleaseResources(t *testing.T) {
	for _, halfClose := range []bool{false, true} {
		t.Run(strconv.FormatBool(halfClose), func(t *testing.T) {
			s := NewScheduler([]Link{{Index: 77, Name: "Test link", Weight: 1, Available: true}})
			f := newHardeningForwarder(t, s, nil)
			f.tcpIdleTimeout = 30 * time.Millisecond
			f.halfCloseTimeout = 30 * time.Millisecond
			if halfClose {
				f.tcpIdleTimeout = time.Second
			}
			_, flow, _, err := f.flows.RewriteOutbound(testTCPPacket4(netip.MustParseAddr("198.18.0.2"), netip.MustParseAddr("203.0.113.80"), 55123, 443, 0x02))
			if err != nil {
				t.Fatal(err)
			}
			f.flows.MarkManaged(flow.Key)
			lease, err := s.Acquire()
			if err != nil {
				t.Fatal(err)
			}
			app, appPeer := net.Pipe()
			up, upPeer := net.Pipe()
			defer appPeer.Close()
			defer upPeer.Close()
			if !f.beginFlow(protocolTCP) {
				t.Fatal("flow not admitted")
			}
			go f.bridgeTCP(flow, app, up, lease)
			if halfClose {
				appPeer.Close()
			}
			waitForCondition(t, func() bool { return f.Stats().ActiveTCP == 0 })
			if got := s.Snapshots()[0].ActiveConnections; got != 0 {
				t.Fatalf("lease retained after idle: %d", got)
			}
			mapping, ok := f.flows.LookupVirtual(protocolTCP, flow.VirtualPort)
			if !ok || mapping.RetireAt.IsZero() {
				t.Fatal("closed TCP mapping was not retired for FIN/RST grace")
			}
		})
	}
}

func TestConcurrentAdmissionAndShutdown(t *testing.T) {
	f := newHardeningForwarder(t, NewScheduler(nil), nil)
	var workers sync.WaitGroup
	for range 100 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if f.beginFlow(protocolUDP) {
				defer f.endFlow(protocolUDP)
				time.Sleep(time.Millisecond)
			}
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := f.CloseContext(ctx); err != nil {
		t.Fatal(err)
	}
	workers.Wait()
	if got := f.Stats().ActiveUDP; got != 0 {
		t.Fatalf("shutdown left %d UDP workers", got)
	}
}

func TestCloseDuringUnacknowledgedTCPHandshake(t *testing.T) {
	s := NewScheduler([]Link{{Index: 77, Name: "Test link", Weight: 1, Available: true}})
	upstream, peer := net.Pipe()
	defer peer.Close()
	f := newHardeningForwarder(t, s, func(context.Context, uint32, string, string) (net.Conn, error) { return upstream, nil })
	_, _, err := f.InjectCaptured(testTCPPacket4(netip.MustParseAddr("198.18.0.2"), netip.MustParseAddr("203.0.113.80"), 55123, 443, 0x02))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err := f.ReadReply(ctx); err != nil {
		t.Fatal(err)
	}
	// No ACK is injected; shutdown must abort the handshake and its upstream.
	closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
	defer closeCancel()
	if err := f.CloseContext(closeCtx); err != nil {
		t.Fatal(err)
	}
	if got := f.Stats().ActiveTCP; got != 0 {
		t.Fatalf("pending handshake retained %d workers", got)
	}
}

func TestRejectedSecondPacketPumpDoesNotCancelFirst(t *testing.T) {
	f := newHardeningForwarder(t, NewScheduler(nil), nil)
	d := newPacketTestDevice()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.RunTunnel(ctx, d) }()
	waitForCondition(t, func() bool { f.workMu.Lock(); defer f.workMu.Unlock(); return f.running })
	if err := f.RunTunnel(ctx, newPacketTestDevice()); err == nil {
		t.Fatal("second packet pump admitted")
	}
	d.input <- testTCPPacket4(netip.MustParseAddr("198.18.0.2"), netip.MustParseAddr("203.0.113.80"), 55123, 443, 0x02)
	select {
	case <-d.output:
	case err := <-done:
		t.Fatalf("first pump stopped: %v", err)
	case <-ctx.Done():
		t.Fatal("first pump stopped responding")
	}
	cancel()
	<-done
}

func TestManagedFlowSurvivesNATIdleSweeperUntilRetired(t *testing.T) {
	table := testFlowTable(t)
	_, flow, _, err := table.RewriteOutbound(testTCPPacket4(netip.MustParseAddr("198.18.0.2"), netip.MustParseAddr("203.0.113.80"), 55123, 443, 0x02))
	if err != nil {
		t.Fatal(err)
	}
	table.MarkManaged(flow.Key)
	if got := table.ExpireIdle(time.Now().Add(48*time.Hour), time.Second, time.Second); got != 0 {
		t.Fatal("NAT sweep removed a live transport mapping")
	}
	table.Retire(flow.Key, time.Second)
	if got := table.ExpireIdle(time.Now().Add(2*time.Second), time.Hour, time.Hour); got != 1 {
		t.Fatal("retired mapping was not reclaimed")
	}
}

func FuzzFlowTablePacketParsing(f *testing.F) {
	f.Add([]byte{})
	f.Add(testTCPPacket4(netip.MustParseAddr("198.18.0.2"), netip.MustParseAddr("203.0.113.80"), 55123, 443, 0x02))
	f.Add(testUDPPacket4(netip.MustParseAddr("198.18.0.2"), netip.MustParseAddr("203.0.113.80"), 55124, 53, []byte("dns")))
	f.Fuzz(func(t *testing.T, packet []byte) {
		table := testFlowTable(t)
		_, _, _, _ = table.RewriteOutbound(packet)
		_, _, _ = table.RewriteInbound(packet)
	})
}
