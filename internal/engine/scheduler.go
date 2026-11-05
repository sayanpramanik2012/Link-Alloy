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
	bytesSent, bytesRecv uint64
	buckets              [rateBuckets]trafficBucket
}

// Scheduler spreads new connections within each application/service cohort.
// Byte counters are telemetry, not capacity estimates. Atomic weighted rotation
// prevents a burst of opens from all chasing the same apparently idle link.
type Scheduler struct {
	mu      sync.Mutex
	links   map[uint32]*linkState
	order   []uint32
	lastTie uint32
	cohorts map[string]*allocationCohort
}

type allocationCohort struct {
	credits  map[uint32]float64
	eligible map[uint32]float64
	used     time.Time
}

func NewScheduler(links []Link) *Scheduler {
	s := &Scheduler{links: make(map[uint32]*linkState), cohorts: make(map[string]*allocationCohort)}
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
	return s.acquireCohort("default", excluded, ipv6)
}

func (s *Scheduler) acquireCohort(key string, excluded map[uint32]struct{}, ipv6 *bool) (*Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A fallback is part of the same connection, not a new allocation. Select
	// an alternate without changing the cohort's credits or eligibility history.
	if len(excluded) > 0 {
		var alternate *linkState
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
			if alternate == nil || l.Weight > alternate.link.Weight {
				alternate = st
			}
		}
		if alternate == nil {
			return nil, ErrNoHealthyLink
		}
		alternate.active++
		return &Lease{scheduler: s, index: alternate.link.Index, state: alternate}, nil
	}
	group := s.cohorts[key]
	if group == nil {
		if len(s.cohorts) >= 1024 {
			var oldest string
			var at time.Time
			for k, c := range s.cohorts {
				if at.IsZero() || c.used.Before(at) {
					oldest, at = k, c.used
				}
			}
			delete(s.cohorts, oldest)
		}
		group = &allocationCohort{credits: make(map[uint32]float64)}
		s.cohorts[key] = group
	}
	group.used = time.Now()
	var chosen *linkState
	eligibleWeight := 0.0
	eligible := make(map[uint32]float64)
	candidates := make([]*linkState, 0, len(s.order))
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
		eligible[idx] = l.Weight
		candidates = append(candidates, st)
	}
	// Preserve this group's rotation unless its own eligible adapter set or
	// weights changed. IPv6-only probe changes cannot reset IPv4 allocations.
	changed := len(eligible) != len(group.eligible)
	for idx, weight := range eligible {
		if group.eligible[idx] != weight {
			changed = true
			break
		}
	}
	if changed {
		group.credits = make(map[uint32]float64)
		group.eligible = eligible
	}
	for _, st := range candidates {
		idx, l := st.link.Index, st.link
		group.credits[idx] += l.Weight
		if chosen == nil || group.credits[idx] > group.credits[chosen.link.Index] || (group.credits[idx] == group.credits[chosen.link.Index] && s.lastTie == chosen.link.Index && idx != s.lastTie) {
			chosen = st
		}
	}
	if chosen == nil {
		return nil, ErrNoHealthyLink
	}
	group.credits[chosen.link.Index] -= eligibleWeight
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
	scheduler   *Scheduler
	index       uint32
	state       *linkState
	once        sync.Once
	flowID      uint64
	diagnostics *flowRegistry
}

func (l *Lease) LinkIndex() uint32 { return l.index }
func (l *Lease) Link() Link {
	l.scheduler.mu.Lock()
	defer l.scheduler.mu.Unlock()
	return l.state.link
}
func (l *Lease) Report(sent, received uint64) {
	l.reportAt(time.Now(), sent, received)
	if l.diagnostics != nil {
		l.diagnostics.report(l.flowID, sent, received)
	}
}
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
		if l.diagnostics != nil {
			l.diagnostics.end(l.flowID, "completed")
		}
		l.scheduler.mu.Lock()
		defer l.scheduler.mu.Unlock()
		if l.state.active > 0 {
			l.state.active--
		}
	})
}
