package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	maxLogEntries      = 500
	linkHealthPeriod   = 15 * time.Second
	linkHealthTimeout  = 8 * time.Second
	linkHealthFailures = 2
)

type RuntimeStatus struct {
	State      string         `json:"state"`
	Since      time.Time      `json:"since,omitempty"`
	LastError  string         `json:"lastError,omitempty"`
	Links      []LinkSnapshot `json:"links"`
	Transport  TransportStats `json:"transport"`
	LogDropped uint64         `json:"logDropped"`
	LogError   string         `json:"logError,omitempty"`
	Flows      []FlowSnapshot `json:"flows"`
}
type tunnelForwarder interface {
	RunTunnel(context.Context, TunnelDevice) error
	CloseContext(context.Context) error
	SetEventSink(EventSink)
	Stats() TransportStats
}
type tunnelSession struct {
	device    TunnelDevice
	forwarder tunnelForwarder
	scheduler *Scheduler
	cancel    context.CancelFunc
	done      chan struct{}
	result    error
}
type TunnelController struct {
	mu               sync.Mutex
	state            string
	since            time.Time
	lastError        string
	routeManager     RouteManager
	session          *tunnelSession
	startCancel      context.CancelFunc
	startDone        chan struct{}
	logMu            sync.RWMutex
	logs             []EngineEvent
	logger           *eventLogger
	preferences      map[string]linkPreference
	deviceFactory    func() (TunnelDevice, error)
	discover         func(context.Context, string) ([]Link, error)
	forwarderFactory func(*FlowTable, *Scheduler) (tunnelForwarder, error)
	listLinks        func() ([]Link, error)
	probe            func(context.Context, uint32, bool) error
	healthPeriod     time.Duration
}

func NewTunnelController(r RouteManager) *TunnelController {
	if r == nil {
		r = NewRouteManager()
	}
	c := &TunnelController{state: "stopped", routeManager: r}
	c.deviceFactory = func() (TunnelDevice, error) { return NewTunnelDevice("") }
	c.discover = discoverLinks
	c.listLinks = enumerateLinks
	c.probe = ProbeLinkFamily
	c.healthPeriod = linkHealthPeriod
	c.forwarderFactory = func(f *FlowTable, s *Scheduler) (tunnelForwarder, error) {
		_, _, v4, v6 := defaultTunnelAddresses()
		return NewForwardingStack(v4, v6, f, s, nil)
	}
	c.loadRecentLogs()
	c.logger = newEventLogger()
	c.loadPreferences()
	return c
}
func (c *TunnelController) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.startCancel != nil || c.state == "starting" || c.state == "running" || c.state == "stopping" {
		c.mu.Unlock()
		return errors.New("tunnel is already active or changing state")
	}
	startup, cancel := context.WithTimeout(ctx, 45*time.Second)
	done := make(chan struct{})
	c.startCancel = cancel
	c.startDone = done
	c.state = "starting"
	c.lastError = ""
	c.mu.Unlock()
	defer func() { cancel(); c.mu.Lock(); c.startCancel = nil; close(done); c.mu.Unlock() }()
	c.addLog(EngineEvent{Level: "info", Category: "tunnel", Message: "Preparing the Windows tunnel"})
	if err := c.routeManager.Recover(startup); err != nil {
		return c.failStart(err, nil, nil)
	}
	if err := startup.Err(); err != nil {
		return c.failStart(err, nil, nil)
	}
	device, err := c.deviceFactory()
	if err != nil {
		return c.failStart(err, nil, nil)
	}
	links, err := c.discover(startup, device.InterfaceName())
	if err != nil {
		return c.failStart(err, device, nil)
	}
	c.applyPreferences(links)
	eligible := false
	for _, l := range links {
		if l.Available && !l.Disabled {
			eligible = true
		}
	}
	if !eligible {
		return c.failStart(errors.New("enable at least one verified network link before starting"), device, nil)
	}
	s := NewScheduler(links)
	_, _, v4, v6 := defaultTunnelAddresses()
	flows, err := NewFlowTable(v4, v6)
	if err != nil {
		return c.failStart(err, device, nil)
	}
	f, err := c.forwarderFactory(flows, s)
	if err != nil {
		return c.failStart(err, device, nil)
	}
	f.SetEventSink(c.addLog)
	if err := startup.Err(); err != nil {
		return c.failStart(err, device, f)
	}
	if err := c.routeManager.Install(startup, device); err != nil {
		return c.failStart(err, device, f)
	}
	if err := startup.Err(); err != nil {
		return c.failStart(err, device, f)
	}
	runCtx, runCancel := context.WithCancel(context.Background())
	session := &tunnelSession{device: device, forwarder: f, scheduler: s, cancel: runCancel, done: make(chan struct{})}
	c.mu.Lock()
	c.session = session
	c.state = "running"
	c.since = time.Now()
	c.mu.Unlock()
	c.addLog(EngineEvent{Level: "info", Category: "tunnel", Message: "Tunnel active for supported IPv4 and IPv6 traffic"})
	go c.runSession(session, runCtx)
	return nil
}
func (c *TunnelController) RecoverStale(ctx context.Context) error {
	return c.routeManager.Recover(ctx)
}
func (c *TunnelController) Stop() error {
	c.mu.Lock()
	if c.startCancel != nil {
		cancel, done := c.startCancel, c.startDone
		c.state = "stopping"
		cancel()
		c.mu.Unlock()
		select {
		case <-done:
			return c.Stop()
		case <-time.After(50 * time.Second):
			return errors.New("startup cancellation timed out; recovery is required")
		}
	}
	s := c.session
	if s == nil {
		c.mu.Unlock()
		return nil
	}
	c.state = "stopping"
	c.mu.Unlock()
	s.cancel()
	select {
	case <-s.done:
		return s.result
	case <-time.After(40 * time.Second):
		return errors.New("cleanup deadline exceeded; use recovery before reconnecting")
	}
}
func (c *TunnelController) Status() RuntimeStatus {
	c.mu.Lock()
	st := RuntimeStatus{State: c.state, Since: c.since, LastError: c.lastError}
	s := c.session
	c.mu.Unlock()
	if c.logger != nil {
		st.LogDropped = c.logger.dropped.Load()
		if e := c.logger.lastError.Load(); e != nil {
			st.LogError = e.(string)
		}
	}
	if s != nil {
		st.Links = s.scheduler.Snapshots()
		st.Transport = s.forwarder.Stats()
		if source, ok := s.forwarder.(interface{ FlowSnapshots() []FlowSnapshot }); ok {
			st.Flows = source.FlowSnapshots()
		}
	} else {
		ifs, e := UpstreamInterfaces()
		if e == nil {
			links := []Link{}
			for _, i := range ifs {
				links = append(links, Link{Index: uint32(i.Index), Name: i.Name, Weight: 1, Health: "idle"})
			}
			c.applyPreferences(links)
			st.Links = NewScheduler(links).Snapshots()
		}
	}
	return st
}
func (c *TunnelController) runSession(s *tunnelSession, ctx context.Context) {
	healthDone := make(chan struct{})
	go func() { defer close(healthDone); c.monitorLinks(ctx, s.scheduler) }()
	runErr := s.forwarder.RunTunnel(ctx, s.device)
	s.cancel()
	// Restore routing before waiting on any potentially stalled transport worker.
	cleanup, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	routeErr := c.routeManager.Stop(cleanup)
	cancel()
	drain, cancelDrain := context.WithTimeout(context.Background(), 5*time.Second)
	drainErr := s.forwarder.CloseContext(drain)
	cancelDrain()
	deviceErr := s.device.Close()
	select {
	case <-healthDone:
	case <-time.After(time.Second):
	}
	result := errors.Join(routeErr, drainErr, deviceErr)
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		result = errors.Join(runErr, result)
	}
	s.result = result
	c.mu.Lock()
	c.session = nil
	c.since = time.Time{}
	if result != nil {
		c.state = "error"
		c.lastError = result.Error()
	} else {
		c.state = "stopped"
		c.lastError = ""
	}
	c.mu.Unlock()
	if result != nil {
		c.addLog(EngineEvent{Level: "error", Category: "route-cleanup", Message: result.Error()})
	} else {
		c.addLog(EngineEvent{Level: "info", Category: "route-cleanup", Message: "Tunnel stopped; Windows routes restored"})
	}
	close(s.done)
}
func (c *TunnelController) failStart(err error, d TunnelDevice, f tunnelForwarder) error {
	cleanup, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	if d != nil {
		err = errors.Join(err, c.routeManager.Stop(cleanup))
	}
	cancel()
	if f != nil {
		drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = errors.Join(err, f.CloseContext(drain))
		cancel()
	}
	if d != nil {
		err = errors.Join(err, d.Close())
	}
	c.mu.Lock()
	c.state = "error"
	c.lastError = err.Error()
	c.session = nil
	c.mu.Unlock()
	c.addLog(EngineEvent{Level: "error", Category: "tunnel-start", Message: err.Error()})
	return err
}
func (c *TunnelController) monitorLinks(ctx context.Context, s *Scheduler) {
	ticker := time.NewTicker(c.healthPeriod)
	defer ticker.Stop()
	v4health, v6health := linkHealthTracker{}, linkHealthTracker{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			links, err := c.listLinks()
			if err != nil {
				c.addLog(EngineEvent{Level: "warning", Category: "link-health", Message: "Could not refresh network adapters"})
				continue
			}
			previous := map[uint32]LinkSnapshot{}
			for _, old := range s.Snapshots() {
				previous[old.Index] = old
			}
			for i := range links {
				if old, ok := previous[links[i].Index]; ok && old.Name == links[i].Name && old.Health != "disconnected" && old.Health != "removed" {
					links[i] = old.Link
				} else {
					links[i].Health = "probing"
				}
			}
			c.applyPreferences(links)
			s.Reconcile(links)
			type result struct {
				link   LinkSnapshot
				v4, v6 error
			}
			results := make(chan result, len(links))
			var wg sync.WaitGroup
			for _, l := range s.Snapshots() {
				if l.Health == "disconnected" || l.Health == "removed" {
					continue
				}
				wg.Add(1)
				go func(l LinkSnapshot) {
					defer wg.Done()
					p, cancel := context.WithTimeout(ctx, linkHealthTimeout)
					defer cancel()
					var a, b error
					var pair sync.WaitGroup
					pair.Add(2)
					go func() { defer pair.Done(); a = c.probe(p, l.Index, false) }()
					go func() { defer pair.Done(); b = c.probe(p, l.Index, true) }()
					pair.Wait()
					results <- result{l, a, b}
				}(l)
			}
			go func() { wg.Wait(); close(results) }()
			for r := range results {
				if ctx.Err() != nil {
					return
				}
				v4, _, _ := v4health.observe(r.link.Index, r.link.IPv4Available, r.v4 == nil)
				v6, _, _ := v6health.observe(r.link.Index, r.link.IPv6Available, r.v6 == nil)
				if errors.Is(r.v4, ErrIPFamilyUnavailable) {
					v4 = false
				}
				if errors.Is(r.v6, ErrIPFamilyUnavailable) {
					v6 = false
				}
				health := "healthy"
				detail := ""
				if (r.v4 != nil && !errors.Is(r.v4, ErrIPFamilyUnavailable)) || (r.v6 != nil && !errors.Is(r.v6, ErrIPFamilyUnavailable)) {
					health = "degraded"
					detail = "One or more IP-family probes missed; existing flows may continue"
				}
				if !v4 && !v6 {
					health = "unavailable"
					detail = "No IP family passed the repeated connectivity checks"
				}
				s.SetHealth(r.link.Index, v4, v6, health, detail)
				if health != r.link.Health {
					c.addLog(EngineEvent{Level: "info", Category: "link-health", LinkIndex: r.link.Index, Message: fmt.Sprintf("%s: %s", r.link.Name, health)})
				}
			}
		}
	}
}
func enumerateLinks() ([]Link, error) {
	ifs, err := UpstreamInterfaces()
	if err != nil {
		return nil, err
	}
	links := []Link{}
	for _, i := range ifs {
		if i.Flags&1 == 0 {
			continue
		}
		links = append(links, Link{Index: uint32(i.Index), Name: i.Name, Weight: 1, FamiliesKnown: true, Health: "probing"})
	}
	return links, nil
}
func discoverLinks(ctx context.Context, _ string) ([]Link, error) {
	links, err := enumerateLinks()
	if err != nil {
		return nil, err
	}
	var wg sync.WaitGroup
	for i := range links {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var pair sync.WaitGroup
			pair.Add(2)
			p, cancel := context.WithTimeout(ctx, linkHealthTimeout)
			defer cancel()
			go func() { defer pair.Done(); links[i].IPv4Available = ProbeLinkFamily(p, links[i].Index, false) == nil }()
			go func() { defer pair.Done(); links[i].IPv6Available = ProbeLinkFamily(p, links[i].Index, true) == nil }()
			pair.Wait()
			links[i].Available = links[i].IPv4Available || links[i].IPv6Available
			if links[i].Available {
				links[i].Health = "healthy"
			} else {
				links[i].Health = "unavailable"
			}
		}(i)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	for _, l := range links {
		if l.Available {
			return links, nil
		}
	}
	return links, errors.New("no supported upstream has verified connectivity")
}
