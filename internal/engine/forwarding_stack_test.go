package engine

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"strconv"
	"testing"
	"time"
)

func TestForwardingStackResetsWhenNoLinkIsAvailable(t *testing.T) {
	virtualIPv4 := netip.MustParseAddr("198.18.0.1")
	virtualIPv6 := netip.MustParseAddr("fd00:6c69:6e6b::1")
	flows, err := NewFlowTable(virtualIPv4, virtualIPv6)
	if err != nil {
		t.Fatal(err)
	}
	forwarder, err := NewForwardingStack(virtualIPv4, virtualIPv6, flows, NewScheduler(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer forwarder.Close()

	clientIP := netip.MustParseAddr("198.18.0.2")
	remoteIP := netip.MustParseAddr("203.0.113.80")
	flow, created, err := forwarder.InjectCaptured(testTCPPacket4(clientIP, remoteIP, 55123, 443, 0x02))
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first SYN did not create a NAT flow")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	reply, replyFlow, err := forwarder.ReadReply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if replyFlow.Key != flow.Key {
		t.Fatalf("reply flow = %#v, want %#v", replyFlow.Key, flow.Key)
	}
	if got := netip.AddrFrom4([4]byte(reply[12:16])); got != remoteIP {
		t.Fatalf("reply source = %s, want original remote %s", got, remoteIP)
	}
	if got := netip.AddrFrom4([4]byte(reply[16:20])); got != clientIP {
		t.Fatalf("reply destination = %s, want app address %s", got, clientIP)
	}
	if flags := reply[33]; flags&0x04 == 0 {
		t.Fatalf("reply TCP flags = %#x, want RST", flags)
	}
	assertIPv4Checksums(t, reply)
}

func TestForwardingStackCarriesTCPDataThroughLinkDialer(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverAddress := listener.Addr().(*net.TCPAddr)
	remoteIP := netip.MustParseAddr("127.0.0.1")
	clientIP := netip.MustParseAddr("198.18.0.2")
	requestPayload := []byte("ping")
	responsePayload := []byte("pong")
	serverResult := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverResult <- acceptErr
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
		request := make([]byte, len(requestPayload))
		if _, readErr := io.ReadFull(connection, request); readErr != nil {
			serverResult <- readErr
			return
		}
		if !bytes.Equal(request, requestPayload) {
			serverResult <- io.ErrUnexpectedEOF
			return
		}
		_, writeErr := connection.Write(responsePayload)
		serverResult <- writeErr
	}()

	virtualIPv4 := netip.MustParseAddr("198.18.0.1")
	virtualIPv6 := netip.MustParseAddr("fd00:6c69:6e6b::1")
	flows, err := NewFlowTable(virtualIPv4, virtualIPv6)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := NewScheduler([]Link{{Index: 77, Name: "Test link", Weight: 1, Available: true}})
	dial := func(ctx context.Context, interfaceIndex uint32, network, address string) (net.Conn, error) {
		if interfaceIndex != 77 || network != "tcp4" || address != net.JoinHostPort(remoteIP.String(), strconv.Itoa(serverAddress.Port)) {
			t.Errorf("upstream dial = index %d, %s, %s", interfaceIndex, network, address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	forwarder, err := NewForwardingStack(virtualIPv4, virtualIPv6, flows, scheduler, dial)
	if err != nil {
		t.Fatal(err)
	}
	defer forwarder.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	const sourcePort = 55124
	const initialSequence = 1000
	_, _, err = forwarder.InjectCaptured(testTCPPacket4WithPayload(clientIP, remoteIP, sourcePort, uint16(serverAddress.Port), initialSequence, 0, 0x02, nil))
	if err != nil {
		t.Fatal(err)
	}
	synAck, _, err := forwarder.ReadReply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if flags := synAck[33]; flags&0x12 != 0x12 {
		t.Fatalf("first reply TCP flags = %#x, want SYN-ACK", flags)
	}
	serverSequence := binary.BigEndian.Uint32(synAck[24:28])
	acknowledgment := serverSequence + 1
	_, _, err = forwarder.InjectCaptured(testTCPPacket4WithPayload(clientIP, remoteIP, sourcePort, uint16(serverAddress.Port), initialSequence+1, acknowledgment, 0x10, nil))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = forwarder.InjectCaptured(testTCPPacket4WithPayload(clientIP, remoteIP, sourcePort, uint16(serverAddress.Port), initialSequence+1, acknowledgment, 0x18, requestPayload))
	if err != nil {
		t.Fatal(err)
	}

	var received []byte
	for range 20 {
		packet, _, readErr := forwarder.ReadReply(ctx)
		if readErr != nil {
			t.Fatal(readErr)
		}
		headerLength := int(packet[32]>>4) * 4
		payloadOffset := 20 + headerLength
		if payloadOffset < len(packet) {
			received = append(received, packet[payloadOffset:]...)
			if bytes.Contains(received, responsePayload) {
				break
			}
		}
	}
	if !bytes.Contains(received, responsePayload) {
		t.Fatalf("application received %q, want response %q", received, responsePayload)
	}
	select {
	case serverErr := <-serverResult:
		if serverErr != nil {
			t.Fatal(serverErr)
		}
	case <-ctx.Done():
		t.Fatal("loopback server did not finish before deadline")
	}
}

func TestForwardingStackCarriesUDPPacketsThroughLinkDialer(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	serverAddress := server.LocalAddr().(*net.UDPAddr)
	remoteIP := netip.MustParseAddr("127.0.0.1")
	clientIP := netip.MustParseAddr("198.18.0.2")
	requestPayload := []byte("ping")
	responsePayload := []byte("pong")
	serverResult := make(chan error, 1)
	go func() {
		_ = server.SetDeadline(time.Now().Add(5 * time.Second))
		buffer := make([]byte, 64)
		n, peer, readErr := server.ReadFromUDP(buffer)
		if readErr != nil {
			serverResult <- readErr
			return
		}
		if !bytes.Equal(buffer[:n], requestPayload) {
			serverResult <- io.ErrUnexpectedEOF
			return
		}
		_, writeErr := server.WriteToUDP(responsePayload, peer)
		serverResult <- writeErr
	}()

	virtualIPv4 := netip.MustParseAddr("198.18.0.1")
	virtualIPv6 := netip.MustParseAddr("fd00:6c69:6e6b::1")
	flows, err := NewFlowTable(virtualIPv4, virtualIPv6)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := NewScheduler([]Link{{Index: 77, Name: "Test link", Weight: 1, Available: true}})
	dial := func(ctx context.Context, interfaceIndex uint32, network, address string) (net.Conn, error) {
		if interfaceIndex != 77 || network != "udp4" || address != net.JoinHostPort(remoteIP.String(), strconv.Itoa(serverAddress.Port)) {
			t.Errorf("upstream dial = index %d, %s, %s", interfaceIndex, network, address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	forwarder, err := NewForwardingStack(virtualIPv4, virtualIPv6, flows, scheduler, dial)
	if err != nil {
		t.Fatal(err)
	}
	defer forwarder.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, created, err := forwarder.InjectCaptured(testUDPPacket4(clientIP, remoteIP, 56124, uint16(serverAddress.Port), requestPayload))
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first UDP datagram did not create a flow")
	}
	reply, _, err := forwarder.ReadReply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(reply) < 28 || !bytes.Equal(reply[28:], responsePayload) {
		t.Fatalf("application received UDP payload %q, want %q", reply[28:], responsePayload)
	}
	if got := netip.AddrFrom4([4]byte(reply[12:16])); got != remoteIP {
		t.Fatalf("UDP response source = %s, want %s", got, remoteIP)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}
