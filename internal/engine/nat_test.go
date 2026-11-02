package engine

import (
	"encoding/binary"
	"net/netip"
	"testing"
	"time"
)

func TestFlowTableRewritesIPv4TCPBothDirections(t *testing.T) {
	table := testFlowTable(t)
	originalSource := netip.MustParseAddr("192.168.0.15")
	originalDestination := netip.MustParseAddr("203.0.113.80")
	packet := testTCPPacket4(originalSource, originalDestination, 51001, 443, 0x02)
	rewritten, flow, created, err := table.RewriteOutbound(packet)
	if err != nil {
		t.Fatal(err)
	}
	if !created || flow.Key.Destination != originalDestination {
		t.Fatalf("new flow = %v, destination = %s", created, flow.Key.Destination)
	}
	if got := netip.AddrFrom4([4]byte(rewritten[16:20])); got != netip.MustParseAddr("198.18.0.1") {
		t.Fatalf("rewritten destination = %s", got)
	}
	if got := binary.BigEndian.Uint16(rewritten[22:24]); got != flow.VirtualPort {
		t.Fatalf("rewritten destination port = %d, want %d", got, flow.VirtualPort)
	}
	assertIPv4Checksums(t, rewritten)

	reply := testTCPPacket4(netip.MustParseAddr("198.18.0.1"), originalSource, flow.VirtualPort, 51001, 0x12)
	restored, restoredFlow, err := table.RewriteInbound(reply)
	if err != nil {
		t.Fatal(err)
	}
	if restoredFlow.Key != flow.Key {
		t.Fatalf("restored flow = %#v, want %#v", restoredFlow.Key, flow.Key)
	}
	if got := netip.AddrFrom4([4]byte(restored[12:16])); got != originalDestination {
		t.Fatalf("restored source = %s, want %s", got, originalDestination)
	}
	if got := binary.BigEndian.Uint16(restored[20:22]); got != 443 {
		t.Fatalf("restored source port = %d, want 443", got)
	}
	assertIPv4Checksums(t, restored)

	// A retransmitted SYN maps to the same virtual endpoint.
	retry, retryFlow, created, err := table.RewriteOutbound(packet)
	if err != nil {
		t.Fatal(err)
	}
	if created || retryFlow.VirtualPort != flow.VirtualPort {
		t.Fatalf("retransmission created=%v virtualPort=%d, want existing %d", created, retryFlow.VirtualPort, flow.VirtualPort)
	}
	assertIPv4Checksums(t, retry)
}

func TestFlowTableKeepsDistinctDestinationsSeparate(t *testing.T) {
	table := testFlowTable(t)
	source := netip.MustParseAddr("192.168.0.15")
	first, _, _, err := table.RewriteOutbound(testTCPPacket4(source, netip.MustParseAddr("203.0.113.10"), 52000, 443, 0x02))
	if err != nil {
		t.Fatal(err)
	}
	second, _, _, err := table.RewriteOutbound(testTCPPacket4(source, netip.MustParseAddr("203.0.113.11"), 52000, 443, 0x02))
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(first[22:24]) == binary.BigEndian.Uint16(second[22:24]) {
		t.Fatal("different original destinations received the same virtual port")
	}
}

func TestFlowTableRewritesIPv4UDPAndExpiresIdleFlow(t *testing.T) {
	table := testFlowTable(t)
	source := netip.MustParseAddr("192.168.0.15")
	destination := netip.MustParseAddr("1.1.1.1")
	packet := testUDPPacket4(source, destination, 53000, 53, []byte("dns request"))
	rewritten, flow, created, err := table.RewriteOutbound(packet)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first UDP datagram did not create a flow")
	}
	assertIPv4Checksums(t, rewritten)

	reply := testUDPPacket4(netip.MustParseAddr("198.18.0.1"), source, flow.VirtualPort, 53000, []byte("dns reply"))
	restored, _, err := table.RewriteInbound(reply)
	if err != nil {
		t.Fatal(err)
	}
	if got := netip.AddrFrom4([4]byte(restored[12:16])); got != destination {
		t.Fatalf("restored UDP source = %s, want %s", got, destination)
	}
	assertIPv4Checksums(t, restored)

	removed := table.ExpireIdle(time.Now().Add(time.Minute), 2*time.Minute, 30*time.Second)
	if removed != 1 {
		t.Fatalf("expired flows = %d, want 1", removed)
	}
}

func TestFlowTableRewritesIPv6TCP(t *testing.T) {
	table := testFlowTable(t)
	source := netip.MustParseAddr("2001:db8::15")
	destination := netip.MustParseAddr("2001:db8::80")
	packet := testTCPPacket6(source, destination, 54000, 443, 0x02)
	rewritten, flow, _, err := table.RewriteOutbound(packet)
	if err != nil {
		t.Fatal(err)
	}
	if got := netip.AddrFrom16([16]byte(rewritten[24:40])); got != netip.MustParseAddr("fd00:6c69:6e6b::1") {
		t.Fatalf("rewritten IPv6 destination = %s", got)
	}
	assertIPv6TransportChecksum(t, rewritten, 6, 40)

	reply := testTCPPacket6(netip.MustParseAddr("fd00:6c69:6e6b::1"), source, flow.VirtualPort, 54000, 0x12)
	restored, _, err := table.RewriteInbound(reply)
	if err != nil {
		t.Fatal(err)
	}
	if got := netip.AddrFrom16([16]byte(restored[8:24])); got != destination {
		t.Fatalf("restored IPv6 source = %s, want %s", got, destination)
	}
	assertIPv6TransportChecksum(t, restored, 6, 40)
}

func TestFlowTableRejectsNonInitialTCPWithoutMapping(t *testing.T) {
	table := testFlowTable(t)
	packet := testTCPPacket4(netip.MustParseAddr("192.168.0.15"), netip.MustParseAddr("203.0.113.80"), 55000, 443, 0x10)
	if _, _, _, err := table.RewriteOutbound(packet); err != ErrUnknownFlow {
		t.Fatalf("RewriteOutbound() error = %v, want ErrUnknownFlow", err)
	}
}

func testFlowTable(t *testing.T) *FlowTable {
	t.Helper()
	table, err := NewFlowTable(netip.MustParseAddr("198.18.0.1"), netip.MustParseAddr("fd00:6c69:6e6b::1"))
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func testTCPPacket4(source, destination netip.Addr, sourcePort, destinationPort uint16, flags byte) []byte {
	return testTCPPacket4WithPayload(source, destination, sourcePort, destinationPort, 0, 0, flags, nil)
}

func testTCPPacket4WithPayload(source, destination netip.Addr, sourcePort, destinationPort uint16, sequence, acknowledgment uint32, flags byte, payload []byte) []byte {
	packet := make([]byte, 40+len(payload))
	packet[0] = 0x45
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	packet[8], packet[9] = 64, protocolTCP
	src, dst := source.As4(), destination.As4()
	copy(packet[12:16], src[:])
	copy(packet[16:20], dst[:])
	binary.BigEndian.PutUint16(packet[20:22], sourcePort)
	binary.BigEndian.PutUint16(packet[22:24], destinationPort)
	binary.BigEndian.PutUint32(packet[24:28], sequence)
	binary.BigEndian.PutUint32(packet[28:32], acknowledgment)
	packet[32], packet[33], packet[34], packet[35] = 0x50, flags, 0x20, 0x00
	copy(packet[40:], payload)
	recomputeChecksums(packet, packetView{ipVersion: 4, protocol: protocolTCP, transportOffset: 20, packetLength: len(packet), ipHeaderLength: 20})
	return packet
}

func testUDPPacket4(source, destination netip.Addr, sourcePort, destinationPort uint16, payload []byte) []byte {
	packet := make([]byte, 28+len(payload))
	packet[0] = 0x45
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	packet[8], packet[9] = 64, protocolUDP
	src, dst := source.As4(), destination.As4()
	copy(packet[12:16], src[:])
	copy(packet[16:20], dst[:])
	binary.BigEndian.PutUint16(packet[20:22], sourcePort)
	binary.BigEndian.PutUint16(packet[22:24], destinationPort)
	binary.BigEndian.PutUint16(packet[24:26], uint16(8+len(payload)))
	copy(packet[28:], payload)
	recomputeChecksums(packet, packetView{ipVersion: 4, protocol: protocolUDP, transportOffset: 20, packetLength: len(packet), ipHeaderLength: 20})
	return packet
}

func testTCPPacket6(source, destination netip.Addr, sourcePort, destinationPort uint16, flags byte) []byte {
	packet := make([]byte, 60)
	packet[0] = 0x60
	binary.BigEndian.PutUint16(packet[4:6], uint16(len(packet)-40))
	packet[6], packet[7] = protocolTCP, 64
	src, dst := source.As16(), destination.As16()
	copy(packet[8:24], src[:])
	copy(packet[24:40], dst[:])
	binary.BigEndian.PutUint16(packet[40:42], sourcePort)
	binary.BigEndian.PutUint16(packet[42:44], destinationPort)
	packet[52], packet[53], packet[54], packet[55] = 0x50, flags, 0x20, 0x00
	recomputeChecksums(packet, packetView{ipVersion: 6, protocol: protocolTCP, transportOffset: 40, packetLength: len(packet)})
	return packet
}

func assertIPv4Checksums(t *testing.T, packet []byte) {
	t.Helper()
	ihl := int(packet[0]&0x0f) * 4
	if got := internetChecksum(packet[:ihl]); got != 0 {
		t.Fatalf("IPv4 header checksum = %#x, want 0", got)
	}
	view, err := parseIPv4(packet)
	if err != nil {
		t.Fatal(err)
	}
	transport := packet[view.transportOffset:view.packetLength]
	var pseudo []byte
	if view.protocol == protocolTCP || binary.BigEndian.Uint16(transport[6:8]) != 0 {
		pseudo = make([]byte, 12)
		copy(pseudo[:4], packet[12:16])
		copy(pseudo[4:8], packet[16:20])
		pseudo[9] = view.protocol
		binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(transport)))
	}
	if pseudo != nil {
		if got := internetChecksum(pseudo, transport); got != 0 {
			t.Fatalf("IPv4 transport checksum = %#x, want 0", got)
		}
	}
}

func assertIPv6TransportChecksum(t *testing.T, packet []byte, protocol uint8, offset int) {
	t.Helper()
	transport := packet[offset:]
	pseudo := make([]byte, 40)
	copy(pseudo[:16], packet[8:24])
	copy(pseudo[16:32], packet[24:40])
	binary.BigEndian.PutUint32(pseudo[32:36], uint32(len(transport)))
	pseudo[39] = protocol
	if got := internetChecksum(pseudo, transport); got != 0 {
		t.Fatalf("IPv6 transport checksum = %#x, want 0", got)
	}
}
