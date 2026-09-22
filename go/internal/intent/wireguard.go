// SPDX-License-Identifier: GPL-3.0-or-later
package intent

import (
	"encoding/base64"
	"fmt"
	"net/netip"
)

// WireGuardTunnel owns one device and its cryptokey routing table. Access is
// deliberately separate from ordinary outbound rules and defaults to deny.
type WireGuardTunnel struct {
	EndpointStrategy string            `json:"endpoint_strategy,omitempty"`
	ID               string            `json:"id"`
	Enabled          bool              `json:"enabled"`
	Name             string            `json:"name,omitempty"`
	Address          []string          `json:"address"`
	PrivateKey       string            `json:"private_key"`
	ListenPort       int               `json:"listen_port,omitempty"`
	MTU              int               `json:"mtu,omitempty"`
	Peers            []WireGuardPeer   `json:"peers"`
	Access           []WireGuardAccess `json:"access,omitempty"`
}
type WireGuardPeer struct {
	PublicKey           string   `json:"public_key"`
	PreSharedKey        string   `json:"pre_shared_key,omitempty"`
	Server              string   `json:"server,omitempty"`
	ServerPort          int      `json:"server_port,omitempty"`
	AllowedIPs          []string `json:"allowed_ips"`
	PersistentKeepalive int      `json:"persistent_keepalive,omitempty"`
}
type WireGuardAccess struct {
	SourceIPCIDR []string `json:"source_ip_cidr"`
	Network      string   `json:"network"`
	Port         int      `json:"port"`
	Target       string   `json:"target"`
	TargetPort   int      `json:"target_port"`
}

func (t WireGuardTunnel) AllowedPrefixes() []string {
	seen := map[string]bool{}
	var result []string
	for _, p := range t.Peers {
		for _, raw := range p.AllowedIPs {
			if prefix, err := netip.ParsePrefix(raw); err == nil {
				raw = prefix.Masked().String()
			}
			if !seen[raw] {
				result = append(result, raw)
				seen[raw] = true
			}
		}
	}
	return result
}
func validWGKey(s string) bool {
	b, e := base64.StdEncoding.DecodeString(s)
	if e != nil || len(b) != 32 {
		return false
	}
	for _, v := range b {
		if v != 0 {
			return true
		}
	}
	return false
}
func validateWireGuard(v Intent, register func(string, string), issue, warn issueFn) {
	tunnels := map[string]WireGuardTunnel{}
	ports := map[int]string{}
	for _, t := range v.WireGuardTunnels {
		register("wireguard_tunnel", t.ID)
		tunnels[t.ID] = t
		if !t.Enabled {
			continue
		}
		bad := func(field, msg string) { issue("INVALID_WIREGUARD", "wireguard_tunnel", t.ID, field, msg) }
		if t.EndpointStrategy != "" && !validStrategy(t.EndpointStrategy) {
			bad("endpoint_strategy", "invalid endpoint address strategy")
		}
		if len(t.Peers) > 32 {
			bad("peers", "at most 32 peers per tunnel")
		}
		if !validWGKey(t.PrivateKey) {
			bad("private_key", "WireGuard private key must be a nonzero 32-byte base64 key")
		}
		if len(t.Address) == 0 {
			bad("address", "at least one tunnel address is required")
		}
		for _, raw := range t.Address {
			p, e := netip.ParsePrefix(raw)
			if e != nil || p.Addr().IsUnspecified() || p.Addr().IsMulticast() || p.Addr().IsLoopback() {
				bad("address", "invalid tunnel address")
			} else if p.Bits() != p.Addr().BitLen() {
				bad("address", "use host prefixes (/32 or /128) to prevent sing-box mapping an entire subnet to localhost")
			}
		}
		if t.MTU != 0 && (t.MTU < 1280 || t.MTU > 9000) {
			bad("mtu", "MTU must be 1280..9000")
		}
		if t.ListenPort < 0 || t.ListenPort > 65535 {
			bad("listen_port", "listen port must be 0..65535")
		}
		if t.ListenPort > 0 {
			if other, ok := ports[t.ListenPort]; ok {
				bad("listen_port", "listen port already used by "+other)
			}
			ports[t.ListenPort] = t.ID
		}
		if len(t.Peers) == 0 {
			bad("peers", "at least one peer is required")
		}
		prefixes := map[string]int{}
		keys := map[string]bool{}
		for n, p := range t.Peers {
			if !validWGKey(p.PublicKey) || keys[p.PublicKey] {
				bad("peers", "invalid or duplicate peer public key")
			}
			keys[p.PublicKey] = true
			if p.PreSharedKey != "" && !validWGKey(p.PreSharedKey) {
				bad("peers", "invalid peer preshared key")
			}
			if p.Server != "" {
				if !validHost(p.Server) || p.ServerPort < 1 || p.ServerPort > 65535 {
					bad("peers", "invalid peer endpoint")
				}
			} else if p.ServerPort != 0 || t.ListenPort == 0 {
				bad("peers", "a peer without an endpoint requires a tunnel listen port")
			}
			if p.PersistentKeepalive < 0 || p.PersistentKeepalive > 65535 {
				bad("peers", "keepalive must be 0..65535 seconds")
			}
			if len(p.AllowedIPs) == 0 {
				bad("peers", "peer AllowedIPs cannot be empty")
			}
			for _, raw := range p.AllowedIPs {
				prefix, e := netip.ParsePrefix(raw)
				if e != nil {
					bad("peers", "invalid AllowedIPs prefix")
					continue
				}
				key := prefix.Masked().String()
				if prior, ok := prefixes[key]; ok && prior != n {
					bad("peers", "same AllowedIPs prefix belongs to multiple peers")
				}
				prefixes[key] = n
			}
		}
		for _, a := range t.Access {
			if len(a.SourceIPCIDR) == 0 {
				bad("access", "remote access requires explicit source prefixes")
			}
			for _, raw := range a.SourceIPCIDR {
				if _, e := netip.ParsePrefix(raw); e != nil {
					bad("access", "invalid access source prefix")
				}
			}
			if a.Network != "tcp" && a.Network != "udp" {
				bad("access", "access network must be tcp or udp")
			}
			if a.Port < 1 || a.Port > 65535 || a.TargetPort < 1 || a.TargetPort > 65535 {
				bad("access", "access ports must be 1..65535")
			}
			target, e := netip.ParseAddr(a.Target)
			if e != nil || !target.IsLoopback() {
				bad("access", "local service target must be a loopback IP")
			}
		}
	}
	routes := map[string]Route{}
	for _, r := range v.Routes {
		routes[r.ID] = r
		if !r.Enabled {
			continue
		}
		if r.Kind == "wireguard" {
			t, ok := tunnels[r.Tunnel]
			if !ok || !t.Enabled {
				issue("DANGLING_WIREGUARD", "route", r.ID, "tunnel", "WireGuard route requires an enabled tunnel")
			}
		} else if r.Tunnel != "" {
			issue("UNEXPECTED_TUNNEL", "route", r.ID, "tunnel", "only WireGuard routes accept a tunnel")
		}
	}
	for _, r := range v.Rules {
		if !r.Enabled || !r.AllowedIPs {
			continue
		}
		route := routes[r.Route]
		if route.Kind != "wireguard" || len(r.IPMatch) > 0 || r.Default {
			issue("INVALID_ALLOWED_IPS_RULE", "rule", r.ID, "allowed_ips", "AllowedIPs requires a WireGuard route, no IP match and a non-default rule")
		}
		for _, prefix := range tunnels[route.Tunnel].AllowedPrefixes() {
			if prefix == "0.0.0.0/0" || prefix == "::/0" {
				warn("WIREGUARD_DEFAULT_ROUTE", "rule", r.ID, "allowed_ips", fmt.Sprintf("AllowedIPs %s captures remaining traffic of this address family", prefix))
			}
		}
	}
	// Cross-tunnel selection follows the user's rule order, not longest prefix.
	earlier := []Rule{}
	for _, r := range v.Rules {
		if !r.Enabled || !r.AllowedIPs {
			continue
		}
		tunnel := routes[r.Route].Tunnel
		for _, prev := range earlier {
			other := routes[prev.Route].Tunnel
			if other == tunnel {
				continue
			}
			overlap := false
			for _, a := range tunnels[tunnel].AllowedPrefixes() {
				pa, ea := netip.ParsePrefix(a)
				if ea != nil {
					continue
				}
				for _, b := range tunnels[other].AllowedPrefixes() {
					pb, eb := netip.ParsePrefix(b)
					if eb == nil && pa.Overlaps(pb) {
						overlap = true
					}
				}
			}
			if overlap {
				warn("WIREGUARD_ROUTE_OVERLAP", "rule", r.ID, "allowed_ips", "AllowedIPs overlap an earlier WireGuard rule; normal first-match rule order applies")
			}
		}
		earlier = append(earlier, r)
	}

}
