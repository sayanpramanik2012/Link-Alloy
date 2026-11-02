package engine

import (
	"context"
	"errors"
)

type TunnelDevice interface {
	InterfaceName() string
	InterfaceLUID() uint64
	InterfaceIndexes() (ipv4, ipv6 uint32)
	Receive(context.Context) ([]byte, error)
	Send([]byte) error
	Close() error
}

var ErrTunnelDeviceUnavailable = errors.New("the Windows tunnel device is unavailable on this platform")

// ErrTunnelBackpressure means one reply could not be queued in the bounded
// adapter ring. TCP can retransmit it; a transient full ring does not stop TUN.
var ErrTunnelBackpressure = errors.New("the tunnel send queue is temporarily full")
