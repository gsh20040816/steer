// SPDX-License-Identifier: GPL-3.0-or-later
package compiler

import model "github.com/gsh20040816/steer/go/internal/intent"

func wireGuardTag(id string) string { return "steer-wg-" + id }

// Expand before both normal routing and pre-match proof construction. A proof
// must never drop a dynamic AllowedIPs condition and bypass WireGuard traffic.
func expandWireGuardRules(intent model.Intent) model.Intent {
	tunnels := map[string]model.WireGuardTunnel{}
	for _, t := range intent.WireGuardTunnels {
		tunnels[t.ID] = t
	}
	routes := indexRoutes(intent.Routes)
	intent.Rules = append([]model.Rule(nil), intent.Rules...)
	for n, r := range intent.Rules {
		if r.AllowedIPs {
			intent.Rules[n].IPMatch = tunnels[routes[r.Route].Tunnel].AllowedPrefixes()
		}
	}
	return intent
}
func compileWireGuard(intent model.Intent) (endpoints, access []any) {
	direct := routeTag(enabledDirectRouteID(intent))
	for _, t := range intent.WireGuardTunnels {
		if !t.Enabled {
			continue
		}
		peers := []any{}
		for _, p := range t.Peers {
			peer := map[string]any{"public_key": p.PublicKey, "allowed_ips": p.AllowedIPs}
			if p.Server != "" {
				peer["address"] = p.Server
				peer["port"] = p.ServerPort
			}
			if p.PreSharedKey != "" {
				peer["pre_shared_key"] = p.PreSharedKey
			}
			if p.PersistentKeepalive != 0 {
				peer["persistent_keepalive_interval"] = p.PersistentKeepalive
			}
			peers = append(peers, peer)
		}
		strategy := t.EndpointStrategy
		if strategy == "" {
			strategy = intent.Bootstrap.Strategy
		}
		ep := map[string]any{"type": "wireguard", "tag": wireGuardTag(t.ID), "system": false, "address": t.Address, "private_key": t.PrivateKey, "peers": peers, "domain_resolver": map[string]any{"server": "steer-dns-bootstrap", "strategy": strategy}}
		if t.ListenPort > 0 {
			ep["listen_port"] = t.ListenPort
		}
		if t.MTU > 0 {
			ep["mtu"] = t.MTU
		}
		endpoints = append(endpoints, ep)
		// Endpoint local destinations have already been mapped to loopback by
		// sing-box. Never grant these privileges to application/TUN traffic.
		for _, a := range t.Access {
			access = append(access, map[string]any{"inbound": []string{wireGuardTag(t.ID)}, "source_ip_cidr": a.SourceIPCIDR, "ip_cidr": []string{"127.0.0.1/32", "::1/128"}, "network": a.Network, "port": a.Port, "action": "route", "outbound": direct, "override_address": a.Target, "override_port": a.TargetPort})
		}
		access = append(access, map[string]any{"inbound": []string{wireGuardTag(t.ID)}, "action": "reject"})
	}
	return
}
