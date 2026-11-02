package engine

import "net/netip"

const (
	tunnelIPv4Address = "198.18.0.2"
	stackIPv4Address  = "198.18.0.1"
	tunnelIPv6Address = "fd00:6c69:6e6b::2"
	stackIPv6Address  = "fd00:6c69:6e6b::1"
	tunnelIPv4Prefix  = 15
	tunnelIPv6Prefix  = 64
)

func defaultTunnelAddresses() (hostIPv4, hostIPv6, stackIPv4, stackIPv6 netip.Addr) {
	return netip.MustParseAddr(tunnelIPv4Address), netip.MustParseAddr(tunnelIPv6Address),
		netip.MustParseAddr(stackIPv4Address), netip.MustParseAddr(stackIPv6Address)
}
