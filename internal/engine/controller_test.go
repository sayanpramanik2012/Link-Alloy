package engine

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type controllerRoutes struct {
	installed atomic.Bool
	restored  atomic.Bool
	install   func(context.Context) error
}

func (r *controllerRoutes) Recover(context.Context) error { return nil }
func (r *controllerRoutes) Install(ctx context.Context, _ TunnelDevice) error {
	if r.install != nil {
		return r.install(ctx)
	}
	r.installed.Store(true)
	return nil
}
func (r *controllerRoutes) Stop(context.Context) error {
	r.restored.Store(true)
	r.installed.Store(false)
	return nil
}

type controllerDevice struct{}

func (controllerDevice) InterfaceName() string                     { return "test" }
func (controllerDevice) InterfaceLUID() uint64                     { return 1 }
func (controllerDevice) InterfaceIndexes() (uint32, uint32)        { return 1, 1 }
func (controllerDevice) Receive(c context.Context) ([]byte, error) { <-c.Done(); return nil, c.Err() }
func (controllerDevice) Send([]byte) error                         { return nil }
func (controllerDevice) Close() error                              { return nil }

type controllerForwarder struct {
	routes  *controllerRoutes
	started chan struct{}
}

func (f *controllerForwarder) RunTunnel(c context.Context, _ TunnelDevice) error {
	close(f.started)
	<-c.Done()
	return c.Err()
}
func (f *controllerForwarder) CloseContext(context.Context) error {
	if !f.routes.restored.Load() {
		return errors.New("drain ran before route recovery")
	}
	return nil
}
func (*controllerForwarder) SetEventSink(EventSink) {}
func (*controllerForwarder) Stats() TransportStats  { return TransportStats{} }
func TestStopCancelsStartupAndWaitsForRollback(t *testing.T) {
	r := &controllerRoutes{}
	c := NewTunnelController(r)
	defer c.CloseLogs()
	c.deviceFactory = func() (TunnelDevice, error) { return controllerDevice{}, nil }
	entered := make(chan struct{})
	c.discover = func(ctx context.Context, _ string) ([]Link, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	start := make(chan error, 1)
	go func() { start <- c.Start(context.Background()) }()
	<-entered
	if e := c.Stop(); e != nil {
		t.Fatal(e)
	}
	if e := <-start; !errors.Is(e, context.Canceled) {
		t.Fatalf("startup result %v", e)
	}
	if r.installed.Load() || !r.restored.Load() {
		t.Fatal("stop returned before rollback")
	}
}
func TestStopRestoresRoutesBeforeWorkerDrain(t *testing.T) {
	r := &controllerRoutes{}
	c := NewTunnelController(r)
	defer c.CloseLogs()
	c.deviceFactory = func() (TunnelDevice, error) { return controllerDevice{}, nil }
	c.discover = func(context.Context, string) ([]Link, error) {
		return []Link{{Index: 1, Name: "test", Weight: 1, Available: true}}, nil
	}
	f := &controllerForwarder{routes: r, started: make(chan struct{})}
	c.forwarderFactory = func(*FlowTable, *Scheduler) (tunnelForwarder, error) { return f, nil }
	if e := c.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	select {
	case <-f.started:
	case <-time.After(time.Second):
		t.Fatal("pump not started")
	}
	if e := c.Stop(); e != nil {
		t.Fatal(e)
	}
	if !r.restored.Load() || c.Status().State != "stopped" {
		t.Fatal("cleanup incomplete")
	}
}
