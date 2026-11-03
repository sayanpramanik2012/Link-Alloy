package engine

import (
	"errors"
	"math"
	"sort"
	"sync"
	"time"
)

var ErrNoHealthyLink = errors.New("no eligible network link is available")

type Link struct {
	Index          uint32  `json:"index"`
	Name           string  `json:"name"`
	Weight         float64 `json:"weight"`
	Available      bool    `json:"available"`
	IPv4Available  bool    `json:"ipv4Available"`
	IPv6Available  bool    `json:"ipv6Available"`
	FamiliesKnown  bool    `json:"familiesKnown"`
	Health         string  `json:"health"`
	LastProbeError string  `json:"lastProbeError,omitempty"`
	Disabled       bool    `json:"-"`
}
type LinkSnapshot struct {
	Link
	Enabled           bool    `json:"enabled"`
	ActiveConnections int     `json:"activeConnections"`
	BytesSent         uint64  `json:"bytesSent"`
	BytesReceived     uint64  `json:"bytesReceived"`
	SendBytesPerSec   float64 `json:"sendBytesPerSec"`
	RecvBytesPerSec   float64 `json:"recvBytesPerSec"`
}

const rateBuckets = 16
const rateBucketDuration = 250 * time.Millisecond
const rateWindow = 2 * time.Second

type trafficBucket struct {
	tick           int64
	sent, received uint64
}
type linkState struct {
	link                 Link
	active               int
	deficit              float64
	bytesSent, bytesRecv uint64
	buckets              [rateBuckets]trafficBucket
}

// Scheduler uses explicitly configured weights as a target, then uses the
// recent byte rate of Link Alloy flows to keep new connections away from a
// saturated link. This matters for launchers that open several long-lived
// connections: a connection-count-only scheduler can place a large transfer on
// one link while the other link is idle. Existing connections remain pinned to
// their selected link; only new connections can be balanced here.
type Scheduler struct {
	mu      sync.Mutex
	links   map[uint32]*linkState
	order   []uint32
	lastTie uint32
}

func NewScheduler(links []Link) *Scheduler {
	s := &Scheduler{links: make(map[uint32]*linkState)}
	s.Reconcile(links)
	return s
}
func validWeight(w float64) bool { return w >= 0.25 && w <= 16 && !math.IsNaN(w) && !math.IsInf(w, 0) }
func (s *Scheduler) Reconcile(links []Link) {
	s.mu.Lock()
	defer s.mu.Unlock()
	present := make(map[uint32]bool)
	for _, l := range links {
		if l.Index == 0 || l.Name == "" {
			continue
		}
		present[l.Index] = true
		if !validWeight(l.Weight) {
			l.Weight = 1
		}
		if old := s.links[l.Index]; old != nil && old.link.Name == l.Name {
			l.Weight = old.link.Weight
			l.Disabled = old.link.Disabled
			old.link = l
		} else if old == nil || old.active == 0 {
			s.links[l.Index] = &linkState{link: l}
		} else {
			old.link.Available = false
			old.link.Health = "removed"
		}
	}
	for idx, st := range s.links {
		if !present[idx] {
			st.link.Available = false
			st.link.Health = "disconnected"
			if st.active == 0 {
				delete(s.links, idx)
			}
		}
	}
	s.order = s.order[:0]
	for idx := range s.links {
		s.order = append(s.order, idx)
	}
	sort.Slice(s.order, func(i, j int) bool { return s.order[i] < s.order[j] })
}
func (s *Scheduler) Configure(index uint32, enabled bool, weight float64) error {
	if !validWeight(weight) {
		return errors.New("relative weight must be between 0.25 and 16")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.links[index]
	if st == nil {
		return errors.New("network link is no longer present")
	}
	st.link.Disabled = !enabled
	st.link.Weight = weight
	return nil
}
func (s *Scheduler) Acquire() (*Lease, error) { return s.acquire(nil, nil) }
func (s *Scheduler) AcquireExcluding(excluded map[uint32]struct{}) (*Lease, error) {
	return s.acquire(excluded, nil)
}
func (s *Scheduler) AcquireExcludingForFamily(excluded map[uint32]struct{}, ipv6 bool) (*Lease, error) {
	return s.acquire(excluded, &ipv6)
}
func (s *Scheduler) acquire(excluded map[uint32]struct{}, ipv6 *bool) (*Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tick := time.Now().UnixNano() / int64(rateBucketDuration)
	type candidate struct {
		state       *linkState
		weight      float64
		activeShare float64
		rateShare   float64
		pressure    float64
	}
	candidates := make([]candidate, 0, len(s.order))
	var chosen *linkState
	chosenPressure := math.Inf(1)
	eligibleWeight := 0.0
	totalRate := 0.0
	totalActive := 0
	for _, idx := range s.order {
		st := s.links[idx]
		l := st.link
		if !l.Available || l.Disabled {
			continue
		}
		if _, skip := excluded[idx]; skip {
			continue
		}
		if ipv6 != nil && l.FamiliesKnown && ((*ipv6 && !l.IPv6Available) || (!*ipv6 && !l.IPv4Available)) {
			continue
		}
		eligibleWeight += l.Weight
		sent, received := s.sampleAtLocked(st, tick)
		rate := (float64(sent) + float64(received)) / rateWindow.Seconds()
		totalRate += rate
		totalActive += st.active
		candidates = append(candidates, candidate{state: st, weight: l.Weight, rateShare: rate})
	}
	if len(candidates) == 0 {
		return nil, ErrNoHealthyLink
	}
	// Keep the original deficit scheduler as the deterministic fallback when
	// there is no recent traffic to balance. Once traffic exists, compare each
	// link's observed share with its configured target share. The active-flow
	// share is a small tie-breaker that prevents repeatedly assigning new flows
	// to the same idle link before its first bytes are reported.
	for i := range candidates {
		c := &candidates[i]
		c.state.deficit += c.weight
		if totalRate > 0 {
			c.rateShare = (c.rateShare / totalRate) / (c.weight / eligibleWeight)
		} else {
			c.rateShare = 0
		}
		if totalActive > 0 {
			c.activeShare = (float64(c.state.active) / float64(totalActive)) / (c.weight / eligibleWeight)
		}
		c.pressure = c.rateShare*0.8 + c.activeShare*0.2
	}
	for _, c := range candidates {
		if chosen == nil || c.pressure < chosenPressure || (c.pressure == chosenPressure && c.state.deficit > chosen.deficit) || (c.pressure == chosenPressure && c.state.deficit == chosen.deficit && s.lastTie == chosen.link.Index && c.state.link.Index != s.lastTie) {
			chosen = c.state
			chosenPressure = c.pressure
		}
	}
	chosen.deficit -= eligibleWeight
	chosen.active++
	s.lastTie = chosen.link.Index
	return &Lease{scheduler: s, index: chosen.link.Index, state: chosen}, nil
}

func (s *Scheduler) sampleAtLocked(st *linkState, tick int64) (uint64, uint64) {
	var sent, received uint64
	for _, b := range st.buckets {
		if b.tick > tick-int64(rateWindow/rateBucketDuration) && b.tick <= tick {
			sent += b.sent
			received += b.received
		}
	}
	return sent, received
}

func (s *Scheduler) SetAvailable(index uint32, available bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.links[index]
	if st == nil {
		return false
	}
	st.link.Available = available
	return true
}
func (s *Scheduler) SetHealth(index uint32, v4, v6 bool, health, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.links[index]; st != nil {
		st.link.Available = v4 || v6
		st.link.IPv4Available = v4
		st.link.IPv6Available = v6
		st.link.FamiliesKnown = true
		st.link.Health = health
		st.link.LastProbeError = detail
	}
}
func (s *Scheduler) Snapshots() []LinkSnapshot { return s.snapshotsAt(time.Now()) }
func (s *Scheduler) snapshotsAt(now time.Time) []LinkSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]LinkSnapshot, 0, len(s.order))
	tick := now.UnixNano() / int64(rateBucketDuration)
	for _, idx := range s.order {
		st := s.links[idx]
		sent, recv := s.sampleAtLocked(st, tick)
		health := st.link.Health
		if health == "" {
			if st.link.Available {
				health = "healthy"
			} else {
				health = "unavailable"
			}
		}
		l := st.link
		l.Health = health
		out = append(out, LinkSnapshot{Link: l, Enabled: !l.Disabled, ActiveConnections: st.active, BytesSent: st.bytesSent, BytesReceived: st.bytesRecv, SendBytesPerSec: float64(sent) / rateWindow.Seconds(), RecvBytesPerSec: float64(recv) / rateWindow.Seconds()})
	}
	return out
}

type Lease struct {
	scheduler *Scheduler
	index     uint32
	state     *linkState
	once      sync.Once
}

func (l *Lease) LinkIndex() uint32 { return l.index }
func (l *Lease) Link() Link {
	l.scheduler.mu.Lock()
	defer l.scheduler.mu.Unlock()
	return l.state.link
}
func (l *Lease) Report(sent, received uint64) { l.reportAt(time.Now(), sent, received) }
func (l *Lease) reportAt(now time.Time, sent, received uint64) {
	if sent == 0 && received == 0 {
		return
	}
	l.scheduler.mu.Lock()
	defer l.scheduler.mu.Unlock()
	st := l.state
	st.bytesSent += sent
	st.bytesRecv += received
	tick := now.UnixNano() / int64(rateBucketDuration)
	b := &st.buckets[tick%rateBuckets]
	if b.tick != tick {
		*b = trafficBucket{tick: tick}
	}
	b.sent += sent
	b.received += received
}
func (l *Lease) Release() {
	l.once.Do(func() {
		l.scheduler.mu.Lock()
		defer l.scheduler.mu.Unlock()
		if l.state.active > 0 {
			l.state.active--
		}
	})
}
