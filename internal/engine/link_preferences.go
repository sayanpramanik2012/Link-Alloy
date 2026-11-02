package engine

import (
	"encoding/json"
	"errors"
	"path/filepath"
)

type linkPreference struct {
	Enabled bool    `json:"enabled"`
	Weight  float64 `json:"weight"`
}

func (c *TunnelController) loadPreferences() {
	c.preferences = map[string]linkPreference{}
	d, e := ProtectedStateDirectory()
	if e != nil {
		return
	}
	data, e := readProtectedState(filepath.Join(d, "links.json"))
	if e != nil {
		return
	}
	var entries map[string]linkPreference
	if len(data) > 64<<10 || json.Unmarshal(data, &entries) != nil {
		return
	}
	for name, p := range entries {
		if len(name) <= 256 && validWeight(p.Weight) {
			c.preferences[name] = p
		}
	}
}

func (c *TunnelController) applyPreferences(links []Link) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range links {
		if p, ok := c.preferences[links[i].Name]; ok {
			links[i].Weight = p.Weight
			links[i].Disabled = !p.Enabled
		}
	}
}
func (c *TunnelController) ConfigureLink(index uint32, enabled bool, weight float64) error {
	if !validWeight(weight) {
		return errors.New("relative allocation weight must be between 0.25 and 16")
	}
	ifs, e := UpstreamInterfaces()
	if e != nil {
		return e
	}
	name := ""
	for _, i := range ifs {
		if uint32(i.Index) == index {
			name = i.Name
		}
	}
	if name == "" {
		return errors.New("network link is no longer present")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.session
	if s != nil && !enabled {
		others := 0
		for _, l := range s.scheduler.Snapshots() {
			if l.Index != index && l.Available && l.Enabled {
				others++
			}
		}
		if others == 0 {
			return errors.New("keep at least one healthy link enabled while the tunnel is running")
		}
	}
	if e = EnsureProtectedStateDirectory(); e != nil {
		return e
	}
	d, e := ProtectedStateDirectory()
	if e != nil {
		return e
	}
	next := map[string]linkPreference{}
	for n, p := range c.preferences {
		next[n] = p
	}
	next[name] = linkPreference{enabled, weight}
	data, e := json.MarshalIndent(next, "", "  ")
	if e != nil {
		return e
	}
	path := filepath.Join(d, "links.json")
	if e = writeProtectedState(path, data); e != nil {
		return e
	}
	c.preferences = next
	if s != nil {
		return s.scheduler.Configure(index, enabled, weight)
	}
	return nil
}
