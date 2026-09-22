// SPDX-License-Identifier: GPL-3.0-or-later
package macos

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"time"
)

// Endpoint status is DNS/control-plane state, not proof of a WG handshake.
type WireGuardPeerStatus struct {
	Tunnel    string    `json:"tunnel"`
	Server    string    `json:"server"`
	Address   string    `json:"address,omitempty"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}
type wireGuardStatus struct {
	Config  string                `json:"config"`
	Peers   []WireGuardPeerStatus `json:"peers"`
	Reloads int                   `json:"reloads"`
}
type wireGuardPeerRuntime struct {
	options  map[string]any
	status   WireGuardPeerStatus
	strategy string
	cacheKey string
}
type wireGuardRuntime struct {
	cachePath                   string
	cache                       map[string]string
	config                      map[string]any
	path, directory, statusPath string
	status                      wireGuardStatus
	peers                       []wireGuardPeerRuntime
}

// Never edit the immutable generation or the user's saved Endpoint hostname.
func prepareWireGuardRuntime(config, runDirectory string) (*wireGuardRuntime, error) {
	data, err := os.ReadFile(config)
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err = json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	runtime := &wireGuardRuntime{config: document, status: wireGuardStatus{Config: config}, statusPath: filepath.Join(runDirectory, "wireguard-status.json")}
	runtime.cachePath = filepath.Join(runDirectory, "wireguard-endpoints.json")
	runtime.cache = map[string]string{}
	if data, e := os.ReadFile(runtime.cachePath); e == nil && len(data) <= 1<<20 {
		_ = json.Unmarshal(data, &runtime.cache)
	}
	if runtime.cache == nil {
		runtime.cache = map[string]string{}
	}
	endpoints, _ := document["endpoints"].([]any)
	for _, raw := range endpoints {
		ep, _ := raw.(map[string]any)
		if ep["type"] != "wireguard" {
			continue
		}
		tag, _ := ep["tag"].(string)
		resolver, _ := ep["domain_resolver"].(map[string]any)
		strategy, _ := resolver["strategy"].(string)
		peers, _ := ep["peers"].([]any)
		for _, rawPeer := range peers {
			p, _ := rawPeer.(map[string]any)
			host, _ := p["address"].(string)
			if host == "" {
				continue
			}
			status := WireGuardPeerStatus{Tunnel: tag, Server: host}
			if ip, e := netip.ParseAddr(host); e == nil {
				status.Address = ip.String()
			}
			cacheKey := fmt.Sprintf("%s|%s|%v|%v", tag, host, p["port"], p["public_key"])
			if status.Address == "" {
				if cached, e := netip.ParseAddr(runtime.cache[cacheKey]); e == nil && (strategy != "ipv4_only" || cached.Is4()) && (strategy != "ipv6_only" || cached.Is6()) {
					p["address"] = cached.String()
					status.Address = cached.String()
				}
			}
			runtime.peers = append(runtime.peers, wireGuardPeerRuntime{options: p, status: status, strategy: strategy, cacheKey: cacheKey})
		}
	}
	if len(runtime.peers) == 0 {
		return nil, nil
	}
	if err = os.MkdirAll(runDirectory, 0700); err != nil {
		return nil, err
	}
	runtime.directory, err = os.MkdirTemp(runDirectory, "wireguard-")
	if err != nil {
		return nil, err
	}
	runtime.path = filepath.Join(runtime.directory, "sing-box.json")
	data, err = json.MarshalIndent(runtime.config, "", "  ")
	if err != nil {
		runtime.close()
		return nil, err
	}
	if err = atomicWriteMode(runtime.path, data, 0600); err != nil {
		runtime.close()
		return nil, err
	}
	return runtime, nil
}
func (r *wireGuardRuntime) close() { _ = os.RemoveAll(r.directory); _ = os.Remove(r.statusPath) }
func chooseWireGuardAddress(addresses []netip.Addr, current, strategy string) string {
	// Retain a still-advertised address rather than flapping with DNS ordering.
	for _, ip := range addresses {
		if ip.String() == current {
			return current
		}
	}
	for _, ip := range addresses {
		if (strategy == "prefer_ipv6" && ip.Is6()) || (strategy != "prefer_ipv6" && ip.Is4()) {
			return ip.String()
		}
	}
	if len(addresses) > 0 {
		return addresses[0].String()
	}
	return ""
}
func (r *wireGuardRuntime) refresh(ctx context.Context, lookup func(context.Context, string, string) ([]netip.Addr, error), reload func() error) {
	changed := false
	previous := make([]any, len(r.peers))
	previousStatus := make([]WireGuardPeerStatus, len(r.peers))
	for i := range r.peers {
		p := &r.peers[i]
		previous[i] = p.options["address"]
		previousStatus[i] = p.status
		p.status.CheckedAt = time.Now().UTC()
		if _, e := netip.ParseAddr(p.status.Server); e == nil {
			continue
		}
		network := "ip"
		if p.strategy == "ipv4_only" {
			network = "ip4"
		}
		if p.strategy == "ipv6_only" {
			network = "ip6"
		}
		addresses, err := lookup(ctx, network, p.status.Server)
		if err != nil {
			p.status.Error = "Endpoint DNS lookup failed; keeping previous address"
			continue
		}
		chosen := chooseWireGuardAddress(addresses, p.status.Address, p.strategy)
		if chosen == "" {
			p.status.Error = "Endpoint has no usable DNS address"
			continue
		}
		p.status.Error = ""
		p.status.Address = chosen
		if p.options["address"] != chosen {
			p.options["address"] = chosen
			changed = true
		}
	}
	if changed {
		data, err := json.MarshalIndent(r.config, "", "  ")
		if err == nil {
			err = atomicWriteMode(r.path, data, 0600)
		}
		if err == nil {
			err = reload()
		}
		if err != nil {
			for i := range r.peers {
				r.peers[i].options["address"] = previous[i]
				r.peers[i].status = previousStatus[i]
				r.peers[i].status.Error = "Endpoint reload failed; keeping previous configuration"
			}
			if data, e := json.Marshal(r.config); e == nil {
				_ = atomicWriteMode(r.path, data, 0600)
			}
		} else {
			r.status.Reloads++
		}
	}
	r.cache = map[string]string{}
	for _, p := range r.peers {
		if p.status.Address != "" {
			r.cache[p.cacheKey] = p.status.Address
		}
	}
	if data, e := json.Marshal(r.cache); e == nil {
		_ = atomicWriteMode(r.cachePath, data, 0600)
	}
	r.status.Peers = nil
	for _, p := range r.peers {
		r.status.Peers = append(r.status.Peers, p.status)
	}
	if data, e := json.Marshal(r.status); e == nil {
		_ = atomicWriteMode(r.statusPath, data, 0644)
	}
}
func (r *wireGuardRuntime) monitor(ctx context.Context, reload func() error) {
	// The compiler pins these exact domains to bootstrap with cache disabled.
	// Query through Steer's own DNS entrance; no OS scoped resolver is involved.
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, net.JoinHostPort(SystemDNSServers[0], "53"))
	}}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			lookup := func(ctx context.Context, network, host string) ([]netip.Addr, error) {
				c, cancel := context.WithTimeout(ctx, 4*time.Second)
				defer cancel()
				return resolver.LookupNetIP(c, network, host)
			}
			r.refresh(ctx, lookup, reload)
			timer.Reset(60 * time.Second)
		}
	}
}
func (backend *Backend) readWireGuardStatus(config string) []WireGuardPeerStatus {
	data, err := os.ReadFile(filepath.Join(backend.options.RunDirectory, "wireguard-status.json"))
	if err != nil {
		return nil
	}
	var status wireGuardStatus
	if json.Unmarshal(data, &status) != nil || status.Config != config {
		return nil
	}
	return status.Peers
}
