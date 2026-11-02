package main

import (
	"context"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sayanpramanik2012/Link-Alloy/internal/engine"
)

type AdapterSummary struct {
	Index     int    `json:"index"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	KindLabel string `json:"kindLabel"`
	IsUp      bool   `json:"isUp"`
	MTU       int    `json:"mtu"`
}

type NetworkService struct {
	mu           sync.RWMutex
	tunnel       *engine.TunnelController
	startupError string
}

type TunnelLinkStatus struct {
	Index             uint32  `json:"index"`
	Name              string  `json:"name"`
	Weight            float64 `json:"weight"`
	Available         bool    `json:"available"`
	Enabled           bool    `json:"enabled"`
	Health            string  `json:"health"`
	LastProbeError    string  `json:"lastProbeError,omitempty"`
	IPv4Available     bool    `json:"ipv4Available"`
	IPv6Available     bool    `json:"ipv6Available"`
	ActiveConnections int     `json:"activeConnections"`
	BytesSent         uint64  `json:"bytesSent"`
	BytesReceived     uint64  `json:"bytesReceived"`
	SendBytesPerSec   float64 `json:"sendBytesPerSec"`
	RecvBytesPerSec   float64 `json:"recvBytesPerSec"`
}

type TunnelStatus struct {
	State      string                `json:"state"`
	Since      time.Time             `json:"since,omitempty"`
	LastError  string                `json:"lastError,omitempty"`
	Links      []TunnelLinkStatus    `json:"links"`
	Transport  engine.TransportStats `json:"transport"`
	LogDropped uint64                `json:"logDropped"`
	LogError   string                `json:"logError,omitempty"`
}

type TunnelLogEntry struct {
	At            time.Time `json:"at"`
	Level         string    `json:"level"`
	Category      string    `json:"category"`
	Message       string    `json:"message"`
	LinkIndex     uint32    `json:"linkIndex,omitempty"`
	BytesSent     uint64    `json:"bytesSent,omitempty"`
	BytesReceived uint64    `json:"bytesReceived,omitempty"`
}

func NewNetworkService() *NetworkService {
	controller := engine.NewTunnelController(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	recoveryErr := controller.RecoverStale(ctx)
	service := &NetworkService{tunnel: controller}
	if recoveryErr != nil {
		service.startupError = recoveryErr.Error()
	}
	return service
}

// ListAdapters reads interface metadata only; it never changes routes, proxies, or traffic.
func (*NetworkService) ListAdapters() ([]AdapterSummary, error) {
	interfaces, err := engine.UpstreamInterfaces()
	if err != nil {
		return nil, err
	}

	adapters := make([]AdapterSummary, 0, len(interfaces))
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 || strings.EqualFold(iface.Name, "Link Alloy Tunnel") {
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

func (s *NetworkService) StartTunnel() error {
	err := s.tunnel.Start(context.Background())
	s.mu.Lock()
	if err != nil {
		s.startupError = err.Error()
	} else {
		s.startupError = ""
	}
	s.mu.Unlock()
	return err
}

func (s *NetworkService) StopTunnel() error {
	return s.tunnel.Stop()
}

func (s *NetworkService) Shutdown() error {
	err := s.tunnel.Stop()
	s.tunnel.CloseLogs()
	return err
}

func (s *NetworkService) ConfigureLink(index uint32, enabled bool, weight float64) error {
	return s.tunnel.ConfigureLink(index, enabled, weight)
}

func (s *NetworkService) GetTunnelStatus() TunnelStatus {
	status := s.tunnel.Status()
	s.mu.RLock()
	startupError := s.startupError
	s.mu.RUnlock()
	if startupError != "" && status.State == "stopped" {
		status.State = "error"
		status.LastError = startupError
	}
	result := TunnelStatus{State: status.State, Since: status.Since, LastError: status.LastError, Transport: status.Transport, LogDropped: status.LogDropped, LogError: status.LogError}
	result.Links = make([]TunnelLinkStatus, 0, len(status.Links))
	for _, link := range status.Links {
		result.Links = append(result.Links, TunnelLinkStatus{
			Index: link.Index, Name: link.Name, Weight: link.Weight, Available: link.Available,
			Enabled: link.Enabled, Health: link.Health, LastProbeError: link.LastProbeError, IPv4Available: link.IPv4Available, IPv6Available: link.IPv6Available,
			ActiveConnections: link.ActiveConnections, BytesSent: link.BytesSent, BytesReceived: link.BytesReceived,
			SendBytesPerSec: link.SendBytesPerSec, RecvBytesPerSec: link.RecvBytesPerSec,
		})
	}
	return result
}

func (s *NetworkService) GetTunnelLogs() []TunnelLogEntry {
	entries := s.tunnel.Logs(100)
	result := make([]TunnelLogEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, TunnelLogEntry{
			At: entry.At, Level: entry.Level, Category: entry.Category, Message: entry.Message,
			LinkIndex: entry.LinkIndex, BytesSent: entry.BytesSent, BytesReceived: entry.BytesReceived,
		})
	}
	return result
}

func (s *NetworkService) GetLogFilePath() (string, error) {
	return s.tunnel.LogFilePath()
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
