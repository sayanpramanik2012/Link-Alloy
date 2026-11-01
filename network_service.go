package main

import (
	"net"
	"sort"
	"strings"
)

type AdapterSummary struct {
	Index     int    `json:"index"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	KindLabel string `json:"kindLabel"`
	IsUp      bool   `json:"isUp"`
	MTU       int    `json:"mtu"`
}

type NetworkService struct{}

// ListAdapters reads interface metadata only; it never changes routes, proxies, or traffic.
func (*NetworkService) ListAdapters() ([]AdapterSummary, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	adapters := make([]AdapterSummary, 0, len(interfaces))
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		kind, label := classifyAdapter(iface.Name)
		adapters = append(adapters, AdapterSummary{
			Index: iface.Index, Name: iface.Name, Kind: kind, KindLabel: label,
			IsUp: iface.Flags&net.FlagUp != 0, MTU: iface.MTU,
		})
	}
	sort.Slice(adapters, func(i, j int) bool { return adapters[i].Index < adapters[j].Index })
	return adapters, nil
}

func classifyAdapter(name string) (string, string) {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "wi-fi"), strings.Contains(lower, "wifi"), strings.Contains(lower, "wireless"):
		return "wifi", "Wi-Fi"
	case strings.Contains(lower, "ethernet"), strings.Contains(lower, "lan"):
		return "ethernet", "Ethernet"
	case strings.Contains(lower, "cellular"), strings.Contains(lower, "mobile"), strings.Contains(lower, "wwan"):
		return "cellular", "Cellular"
	default:
		return "other", "Other adapter"
	}
}
