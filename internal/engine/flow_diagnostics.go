package engine

import (
	"net"
	"sort"
	"sync"
	"time"
)

// Flow diagnostics stay in memory. Event logs contain only the flow ID and
// interface, never process names, endpoint addresses, or application payloads.
type FlowSnapshot struct {
	ID               uint64    `json:"id"`
	ProcessID        uint32    `json:"processId"`
	ProcessName      string    `json:"processName"`
	Protocol         string    `json:"protocol"`
	LocalPort        uint16    `json:"localPort"`
	RemotePort       uint16    `json:"remotePort"`
	LinkIndex        uint32    `json:"linkIndex"`
	UpstreamLocal    string    `json:"upstreamLocal"`
	Started          time.Time `json:"started"`
	State            string    `json:"state"`
	DialMilliseconds int64     `json:"dialMilliseconds"`
	BytesSent        uint64    `json:"bytesSent"`
	BytesReceived    uint64    `json:"bytesReceived"`
	RecvBytesPerSec  float64   `json:"recvBytesPerSec"`
	SendBytesPerSec  float64   `json:"sendBytesPerSec"`
}

type diagnosticFlow struct {
	FlowSnapshot
	buckets [rateBuckets]trafficBucket
}
type flowRegistry struct {
	mu    sync.Mutex
	next  uint64
	flows map[uint64]*diagnosticFlow
}

func (r *flowRegistry) begin(flow Flow, owner flowOwner, protocol string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.flows == nil {
		r.flows = make(map[uint64]*diagnosticFlow)
	}
	// Active records are limited by transport admissions; keep at most 256
	// completed records as well, so rapid connection churn cannot grow memory.
	if len(r.flows) >= maxTCPFlows+maxUDPFlows+256 {
		var oldest uint64
		for id, f := range r.flows {
			if f.State != "active" && f.State != "dialing" && (oldest == 0 || id < oldest) {
				oldest = id
			}
		}
		if oldest != 0 {
			delete(r.flows, oldest)
		}
	}
	r.next++
	r.flows[r.next] = &diagnosticFlow{FlowSnapshot: FlowSnapshot{ID: r.next, ProcessID: owner.PID, ProcessName: owner.Name, Protocol: protocol, LocalPort: flow.Key.SourcePort, RemotePort: flow.Key.DestinationPort, Started: time.Now(), State: "dialing"}}
	return r.next
}

func (r *flowRegistry) connected(id uint64, link uint32, conn net.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if f := r.flows[id]; f != nil {
		f.LinkIndex = link
		f.State = "active"
		f.DialMilliseconds = time.Since(f.Started).Milliseconds()
		if conn.LocalAddr() != nil {
			f.UpstreamLocal = conn.LocalAddr().String()
		}
	}
}
func (r *flowRegistry) report(id uint64, sent, recv uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if f := r.flows[id]; f != nil {
		f.BytesSent += sent
		f.BytesReceived += recv
		tick := time.Now().UnixNano() / int64(rateBucketDuration)
		b := &f.buckets[tick%rateBuckets]
		if b.tick != tick {
			*b = trafficBucket{tick: tick}
		}
		b.sent += sent
		b.received += recv
	}
}
func (r *flowRegistry) end(id uint64, state string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if f := r.flows[id]; f != nil {
		f.State = state
	}
	// Prune oldest completed records without removing active records.
	ended := make([]uint64, 0)
	for id, f := range r.flows {
		if f.State != "active" && f.State != "dialing" {
			ended = append(ended, id)
		}
	}
	if len(ended) > 256 {
		sort.Slice(ended, func(i, j int) bool { return ended[i] < ended[j] })
		for _, id := range ended[:len(ended)-256] {
			delete(r.flows, id)
		}
	}
}
func (r *flowRegistry) snapshots() []FlowSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]FlowSnapshot, 0, len(r.flows))
	tick := time.Now().UnixNano() / int64(rateBucketDuration)
	for _, f := range r.flows {
		v := f.FlowSnapshot
		if v.State == "active" {
			for _, b := range f.buckets {
				if b.tick > tick-int64(rateWindow/rateBucketDuration) && b.tick <= tick {
					v.RecvBytesPerSec += float64(b.received) / rateWindow.Seconds()
					v.SendBytesPerSec += float64(b.sent) / rateWindow.Seconds()
				}
			}
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		ai, aj := out[i].State == "active", out[j].State == "active"
		if ai != aj {
			return ai
		}
		if out[i].RecvBytesPerSec != out[j].RecvBytesPerSec {
			return out[i].RecvBytesPerSec > out[j].RecvBytesPerSec
		}
		return out[i].ID > out[j].ID
	})
	if len(out) > 256 {
		out = out[:256]
	}
	return out
}
