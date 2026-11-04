//go:build windows

package engine

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var tcpOwnerTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

type flowOwner struct {
	PID            uint32
	Name, Instance string
}

// A snapshot is refreshed on admission, at most every 100 ms per family.
// Failures leave attribution unknown; they never prevent forwarding.
type ownerResolver struct {
	mu   sync.Mutex
	at   [2]time.Time
	rows [2]map[FlowKey]uint32
}

func (r *ownerResolver) resolve(flow Flow) flowOwner {
	if flow.Key.Protocol != protocolTCP {
		return flowOwner{}
	}
	family := 0
	af := uint32(windows.AF_INET)
	if flow.Key.Destination.Is6() {
		family, af = 1, windows.AF_INET6
	}
	r.mu.Lock()
	if time.Since(r.at[family]) >= 100*time.Millisecond {
		r.rows[family] = readTCPOwners(af)
		r.at[family] = time.Now()
	}
	pid := r.rows[family][flow.Key]
	// A SYN can arrive after the cached snapshot. Resolve a miss once against
	// a fresh table so a burst is not split into known/unknown cohorts merely
	// because the connections opened in the same cache interval.
	if pid == 0 {
		r.rows[family] = readTCPOwners(af)
		r.at[family] = time.Now()
		pid = r.rows[family][flow.Key]
	}
	r.mu.Unlock()
	if pid == 0 {
		return flowOwner{}
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return flowOwner{PID: pid}
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil {
		return flowOwner{PID: pid}
	}
	owner := flowOwner{PID: pid, Instance: fmt.Sprintf("%d:%d:%d", pid, created.HighDateTime, created.LowDateTime)}
	name := make([]uint16, 32768)
	size := uint32(len(name))
	if windows.QueryFullProcessImageName(h, 0, &name[0], &size) == nil {
		owner.Name = filepath.Base(windows.UTF16ToString(name[:size]))
	}
	return owner
}

func readTCPOwners(af uint32) map[FlowKey]uint32 {
	var size uint32
	// TCP_TABLE_OWNER_PID_ALL = 5. Size and retry count are bounded.
	tcpOwnerTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, uintptr(af), 5, 0)
	for attempt := 0; attempt < 3 && size >= 4 && size <= 8<<20; attempt++ {
		data := make([]byte, size)
		result, _, _ := tcpOwnerTable.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&size)), 0, uintptr(af), 5, 0)
		runtime.KeepAlive(data)
		if result == uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
			continue
		}
		if result != 0 {
			return nil
		}
		return parseTCPOwners(data, af == windows.AF_INET6)
	}
	return nil
}

func parseTCPOwners(data []byte, ipv6 bool) map[FlowKey]uint32 {
	out := make(map[FlowKey]uint32)
	if len(data) < 4 {
		return out
	}
	count := uint64(binary.LittleEndian.Uint32(data[:4]))
	stride := 24
	if ipv6 {
		stride = 56
	}
	if count > uint64((len(data)-4)/stride) {
		return out
	}
	for i := 0; i < int(count); i++ {
		row := data[4+i*stride : 4+(i+1)*stride]
		key := FlowKey{Protocol: protocolTCP}
		var state, pid uint32
		if ipv6 {
			key.Source = netip.AddrFrom16([16]byte(row[:16]))
			key.Destination = netip.AddrFrom16([16]byte(row[24:40]))
			key.SourcePort = binary.BigEndian.Uint16(row[20:22])
			key.DestinationPort = binary.BigEndian.Uint16(row[44:46])
			state = binary.LittleEndian.Uint32(row[48:52])
			pid = binary.LittleEndian.Uint32(row[52:56])
		} else {
			key.Source = netip.AddrFrom4([4]byte(row[4:8]))
			key.Destination = netip.AddrFrom4([4]byte(row[12:16]))
			key.SourcePort = binary.BigEndian.Uint16(row[8:10])
			key.DestinationPort = binary.BigEndian.Uint16(row[16:18])
			state = binary.LittleEndian.Uint32(row[:4])
			pid = binary.LittleEndian.Uint32(row[20:24])
		}
		if state >= 3 && state <= 8 {
			out[key] = pid
		}
	}
	return out
}

func cohortFor(flow Flow, owner flowOwner) string {
	identity := owner.Instance
	if identity == "" {
		identity = "unknown"
	}
	return fmt.Sprintf("%s:%d:%d:%t", identity, flow.Key.Protocol, flow.Key.DestinationPort, flow.Key.Destination.Is6())
}
