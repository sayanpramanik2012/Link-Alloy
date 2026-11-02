package engine

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"
)

const (
	protocolTCP      = 6
	protocolUDP      = 17
	firstVirtualPort = 40000
	lastVirtualPort  = 60000
)

var (
	ErrUnsupportedPacket = errors.New("packet protocol or format is not supported by the current engine")
	ErrUnknownFlow       = errors.New("packet does not belong to a known flow")
	ErrVirtualPortsFull  = errors.New("virtual flow port range is exhausted")
)

type FlowKey struct {
	Protocol        uint8
	Source          netip.Addr
	SourcePort      uint16
	Destination     netip.Addr
	DestinationPort uint16
}

type Flow struct {
	Key         FlowKey
	VirtualPort uint16
	LastSeen    time.Time
	Managed     bool
	RetireAt    time.Time
}

type virtualFlowKey struct {
	protocol uint8
	port     uint16
}

// FlowTable performs the small, explicit address translation needed to present
// captured application connections to Link Alloy's user-space transport stack.
// It does not open sockets or alter the Windows route table.
type FlowTable struct {
	mu          sync.Mutex
	virtualIPv4 netip.Addr
	virtualIPv6 netip.Addr
	nextPort    uint16
	byOriginal  map[FlowKey]*Flow
	byVirtual   map[virtualFlowKey]*Flow
}

func NewFlowTable(virtualIPv4, virtualIPv6 netip.Addr) (*FlowTable, error) {
	if !virtualIPv4.Is4() || !virtualIPv6.Is6() || virtualIPv6.Is4In6() {
		return nil, errors.New("the flow table requires one IPv4 and one IPv6 virtual address")
	}
	return &FlowTable{
		virtualIPv4: virtualIPv4,
		virtualIPv6: virtualIPv6,
		nextPort:    firstVirtualPort,
		byOriginal:  make(map[FlowKey]*Flow),
		byVirtual:   make(map[virtualFlowKey]*Flow),
	}, nil
}

// RewriteOutbound changes a captured app packet's destination to the local
// virtual endpoint. The returned flow retains the original destination for the
// upstream dialer. TCP flows must begin with SYN; UDP creates a flow on its
// first datagram.
func (t *FlowTable) RewriteOutbound(packet []byte) ([]byte, Flow, bool, error) {
	view, err := parsePacket(packet)
	if err != nil {
		return nil, Flow{}, false, err
	}
	if view.protocol != protocolTCP && view.protocol != protocolUDP {
		return nil, Flow{}, false, ErrUnsupportedPacket
	}
	key := FlowKey{
		Protocol:        view.protocol,
		Source:          view.source,
		SourcePort:      binary.BigEndian.Uint16(packet[view.transportOffset : view.transportOffset+2]),
		Destination:     view.destination,
		DestinationPort: binary.BigEndian.Uint16(packet[view.transportOffset+2 : view.transportOffset+4]),
	}

	t.mu.Lock()
	flow := t.byOriginal[key]
	created := false
	if flow == nil {
		if view.protocol == protocolTCP && packet[view.transportOffset+13]&0x02 == 0 {
			t.mu.Unlock()
			return nil, Flow{}, false, ErrUnknownFlow
		}
		port, allocErr := t.allocatePortLocked(view.protocol)
		if allocErr != nil {
			t.mu.Unlock()
			return nil, Flow{}, false, allocErr
		}
		flow = &Flow{Key: key, VirtualPort: port, LastSeen: time.Now()}
		t.byOriginal[key] = flow
		t.byVirtual[virtualFlowKey{protocol: key.Protocol, port: port}] = flow
		created = true
	} else {
		flow.LastSeen = time.Now()
	}
	flowCopy := *flow
	t.mu.Unlock()

	rewritten := append([]byte(nil), packet[:view.packetLength]...)
	virtualAddress := t.virtualAddressFor(view.ipVersion)
	if err := replaceAddress(rewritten, view.destinationOffset, virtualAddress); err != nil {
		return nil, Flow{}, false, err
	}
	binary.BigEndian.PutUint16(rewritten[view.transportOffset+2:view.transportOffset+4], flowCopy.VirtualPort)
	recomputeChecksums(rewritten, view)
	return rewritten, flowCopy, created, nil
}

// RewriteInbound restores the original remote source address on a reply packet
// leaving the user-space stack for the application.
func (t *FlowTable) RewriteInbound(packet []byte) ([]byte, Flow, error) {
	view, err := parsePacket(packet)
	if err != nil {
		return nil, Flow{}, err
	}
	if view.protocol != protocolTCP && view.protocol != protocolUDP {
		return nil, Flow{}, ErrUnsupportedPacket
	}
	virtualPort := binary.BigEndian.Uint16(packet[view.transportOffset : view.transportOffset+2])
	t.mu.Lock()
	flow := t.byVirtual[virtualFlowKey{protocol: view.protocol, port: virtualPort}]
	if flow == nil || flow.Key.Source != view.destination || flow.Key.SourcePort != binary.BigEndian.Uint16(packet[view.transportOffset+2:view.transportOffset+4]) {
		t.mu.Unlock()
		return nil, Flow{}, ErrUnknownFlow
	}
	flow.LastSeen = time.Now()
	flowCopy := *flow
	t.mu.Unlock()

	rewritten := append([]byte(nil), packet[:view.packetLength]...)
	if err := replaceAddress(rewritten, view.sourceOffset, flowCopy.Key.Destination); err != nil {
		return nil, Flow{}, err
	}
	binary.BigEndian.PutUint16(rewritten[view.transportOffset:view.transportOffset+2], flowCopy.Key.DestinationPort)
	recomputeChecksums(rewritten, view)
	return rewritten, flowCopy, nil
}

func (t *FlowTable) LookupVirtual(protocol uint8, port uint16) (Flow, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	flow := t.byVirtual[virtualFlowKey{protocol: protocol, port: port}]
	if flow == nil {
		return Flow{}, false
	}
	return *flow, true
}

func (t *FlowTable) Remove(key FlowKey) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	flow := t.byOriginal[key]
	if flow == nil {
		return false
	}
	delete(t.byOriginal, key)
	delete(t.byVirtual, virtualFlowKey{protocol: key.Protocol, port: flow.VirtualPort})
	return true
}

// MarkManaged transfers expiration responsibility to the transport bridge.
// Active bridges close their own sockets on idle and then retire the mapping.
func (t *FlowTable) MarkManaged(key FlowKey) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if flow := t.byOriginal[key]; flow != nil {
		flow.Managed = true
		flow.RetireAt = time.Time{}
	}
}

// Retire keeps the reverse mapping briefly so queued TCP FIN/RST packets and
// their acknowledgments can finish before a virtual port is reused.
func (t *FlowTable) Retire(key FlowKey, grace time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if flow := t.byOriginal[key]; flow != nil {
		flow.Managed = false
		flow.RetireAt = time.Now().Add(grace)
	}
}

func (t *FlowTable) ExpireIdle(now time.Time, tcpIdle, udpIdle time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	removed := 0
	for key, flow := range t.byOriginal {
		if flow.Managed {
			continue
		}
		if !flow.RetireAt.IsZero() {
			if now.Before(flow.RetireAt) {
				continue
			}
			delete(t.byOriginal, key)
			delete(t.byVirtual, virtualFlowKey{protocol: key.Protocol, port: flow.VirtualPort})
			removed++
			continue
		}
		idleLimit := tcpIdle
		if key.Protocol == protocolUDP {
			idleLimit = udpIdle
		}
		if now.Sub(flow.LastSeen) < idleLimit {
			continue
		}
		delete(t.byOriginal, key)
		delete(t.byVirtual, virtualFlowKey{protocol: key.Protocol, port: flow.VirtualPort})
		removed++
	}
	return removed
}

func (t *FlowTable) allocatePortLocked(protocol uint8) (uint16, error) {
	for attempts := 0; attempts <= lastVirtualPort-firstVirtualPort; attempts++ {
		port := t.nextPort
		t.nextPort++
		if t.nextPort > lastVirtualPort {
			t.nextPort = firstVirtualPort
		}
		if _, used := t.byVirtual[virtualFlowKey{protocol: protocol, port: port}]; !used {
			return port, nil
		}
	}
	return 0, ErrVirtualPortsFull
}

func (t *FlowTable) virtualAddressFor(version uint8) netip.Addr {
	if version == 4 {
		return t.virtualIPv4
	}
	return t.virtualIPv6
}

type packetView struct {
	ipVersion         uint8
	protocol          uint8
	source            netip.Addr
	destination       netip.Addr
	sourceOffset      int
	destinationOffset int
	transportOffset   int
	packetLength      int
	ipHeaderLength    int
}

func parsePacket(packet []byte) (packetView, error) {
	if len(packet) == 0 {
		return packetView{}, ErrUnsupportedPacket
	}
	switch packet[0] >> 4 {
	case 4:
		return parseIPv4(packet)
	case 6:
		return parseIPv6(packet)
	default:
		return packetView{}, ErrUnsupportedPacket
	}
}

func parseIPv4(packet []byte) (packetView, error) {
	if len(packet) < 20 {
		return packetView{}, ErrUnsupportedPacket
	}
	ihl := int(packet[0]&0x0f) * 4
	length := int(binary.BigEndian.Uint16(packet[2:4]))
	if ihl < 20 || length < ihl+8 || length > len(packet) {
		return packetView{}, ErrUnsupportedPacket
	}
	fragment := binary.BigEndian.Uint16(packet[6:8])
	if fragment&0x3fff != 0 {
		return packetView{}, ErrUnsupportedPacket
	}
	protocol := packet[9]
	transportOffset := ihl
	if err := validateTransport(packet, protocol, transportOffset, length); err != nil {
		return packetView{}, err
	}
	var sourceBytes, destinationBytes [4]byte
	copy(sourceBytes[:], packet[12:16])
	copy(destinationBytes[:], packet[16:20])
	return packetView{
		ipVersion:         4,
		protocol:          protocol,
		source:            netip.AddrFrom4(sourceBytes),
		destination:       netip.AddrFrom4(destinationBytes),
		sourceOffset:      12,
		destinationOffset: 16,
		transportOffset:   transportOffset,
		packetLength:      length,
		ipHeaderLength:    ihl,
	}, nil
}

func parseIPv6(packet []byte) (packetView, error) {
	if len(packet) < 40 {
		return packetView{}, ErrUnsupportedPacket
	}
	length := 40 + int(binary.BigEndian.Uint16(packet[4:6]))
	if length > len(packet) {
		return packetView{}, ErrUnsupportedPacket
	}
	nextHeader := packet[6]
	transportOffset := 40
	for {
		switch nextHeader {
		case protocolTCP, protocolUDP:
			if err := validateTransport(packet, nextHeader, transportOffset, length); err != nil {
				return packetView{}, err
			}
			var sourceBytes, destinationBytes [16]byte
			copy(sourceBytes[:], packet[8:24])
			copy(destinationBytes[:], packet[24:40])
			return packetView{
				ipVersion:         6,
				protocol:          nextHeader,
				source:            netip.AddrFrom16(sourceBytes),
				destination:       netip.AddrFrom16(destinationBytes),
				sourceOffset:      8,
				destinationOffset: 24,
				transportOffset:   transportOffset,
				packetLength:      length,
				ipHeaderLength:    40,
			}, nil
		case 0, 43, 60:
			if transportOffset+2 > length {
				return packetView{}, ErrUnsupportedPacket
			}
			extensionLength := (int(packet[transportOffset+1]) + 1) * 8
			if extensionLength < 8 || transportOffset+extensionLength > length {
				return packetView{}, ErrUnsupportedPacket
			}
			nextHeader = packet[transportOffset]
			transportOffset += extensionLength
		case 44, 50, 51:
			return packetView{}, ErrUnsupportedPacket
		default:
			return packetView{}, ErrUnsupportedPacket
		}
	}
}

func validateTransport(packet []byte, protocol uint8, offset, packetLength int) error {
	transportLength := packetLength - offset
	switch protocol {
	case protocolTCP:
		if transportLength < 20 {
			return ErrUnsupportedPacket
		}
		headerLength := int(packet[offset+12]>>4) * 4
		if headerLength < 20 || headerLength > transportLength {
			return ErrUnsupportedPacket
		}
	case protocolUDP:
		if transportLength < 8 {
			return ErrUnsupportedPacket
		}
		udpLength := int(binary.BigEndian.Uint16(packet[offset+4 : offset+6]))
		if udpLength < 8 || udpLength > transportLength {
			return ErrUnsupportedPacket
		}
	default:
		return ErrUnsupportedPacket
	}
	return nil
}

func replaceAddress(packet []byte, offset int, address netip.Addr) error {
	if address.Is4() && offset+4 <= len(packet) {
		value := address.As4()
		copy(packet[offset:offset+4], value[:])
		return nil
	}
	if address.Is6() && !address.Is4In6() && offset+16 <= len(packet) {
		value := address.As16()
		copy(packet[offset:offset+16], value[:])
		return nil
	}
	return fmt.Errorf("address family mismatch for %s", address)
}

func recomputeChecksums(packet []byte, view packetView) {
	transport := packet[view.transportOffset:view.packetLength]
	checksumOffset := 16
	preserveZeroUDP := false
	if view.protocol == protocolUDP {
		checksumOffset = 6
		preserveZeroUDP = view.ipVersion == 4 && binary.BigEndian.Uint16(transport[checksumOffset:checksumOffset+2]) == 0
	}
	if !preserveZeroUDP {
		transport[checksumOffset] = 0
		transport[checksumOffset+1] = 0
		var checksum uint16
		if view.ipVersion == 4 {
			pseudo := make([]byte, 12)
			copy(pseudo[0:4], packet[12:16])
			copy(pseudo[4:8], packet[16:20])
			pseudo[9] = view.protocol
			binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(transport)))
			checksum = internetChecksum(pseudo, transport)
		} else {
			pseudo := make([]byte, 40)
			copy(pseudo[0:16], packet[8:24])
			copy(pseudo[16:32], packet[24:40])
			binary.BigEndian.PutUint32(pseudo[32:36], uint32(len(transport)))
			pseudo[39] = view.protocol
			checksum = internetChecksum(pseudo, transport)
		}
		if view.protocol == protocolUDP && checksum == 0 {
			checksum = 0xffff
		}
		binary.BigEndian.PutUint16(transport[checksumOffset:checksumOffset+2], checksum)
	}

	if view.ipVersion == 4 {
		packet[10] = 0
		packet[11] = 0
		binary.BigEndian.PutUint16(packet[10:12], internetChecksum(packet[:view.ipHeaderLength]))
	}
}

func internetChecksum(parts ...[]byte) uint16 {
	var sum uint32
	var pending byte
	hasPending := false
	for _, part := range parts {
		i := 0
		if hasPending && len(part) > 0 {
			sum += uint32(pending)<<8 | uint32(part[0])
			i = 1
			hasPending = false
		}
		for ; i+1 < len(part); i += 2 {
			sum += uint32(part[i])<<8 | uint32(part[i+1])
		}
		if i < len(part) {
			pending = part[i]
			hasPending = true
		}
	}
	if hasPending {
		sum += uint32(pending) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}
