// SPDX-License-Identifier: GPL-3.0-or-later
package compiler

import (
	"net/netip"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

// ICMP must terminate during L3 pre-match. Falling through to a TCP/UDP
// proxy can make the system TUN stack synthesize echo replies locally.
// WireGuard needs policy routing before the Direct fallback, but TCP/UDP
// proxies cannot carry ICMP. Endpoint ingress ACLs precede these rules.
func compileICMPRules(intent model.Intent, target Target) []any {
	direct := enabledDirectRouteID(intent)
	if direct == "" {
		return nil
	}
	routes := indexRoutes(intent.Routes)
	var localSources []string
	if len(target.BypassInboundTags) > 0 {
		for _, address := range target.TUNAddresses {
			prefix, err := netip.ParsePrefix(address)
			if err == nil {
				localSources = append(localSources, netip.PrefixFrom(prefix.Addr(), prefix.Addr().BitLen()).String())
			}
		}
	}
	var result []any
	appendRule := func(match map[string]any) {
		if match["action"] == "bypass" && len(localSources) > 0 {
			// A locally selected TUN source cannot be routed by the physical
			// network (especially OpenWrt source-specific IPv6 defaults).
			// Forward it through Direct so the core chooses a usable source.
			predicate := map[string]any{}
			for k, v := range match {
				if k != "action" && k != "outbound" {
					predicate[k] = v
				}
			}
			local := combine([]map[string]any{predicate, {"source_ip_cidr": localSources}}, "and")
			local["action"] = "route"
			local["outbound"] = routeTag(direct)
			result = append(result, local)
		}
		result = append(result, match)
	}
	emit := func(rule model.Rule) {
		match := map[string]any{"network": []string{"icmp"}}
		if conditions := compileRuleMatch(rule, false); len(conditions) > 0 {
			match = combine([]map[string]any{match, conditions}, "and")
		}
		switch routes[rule.Route].Kind {
		case "block":
			match["action"] = "reject"
		case "wireguard":
			match["action"] = "route"
			match["outbound"] = routeTag(rule.Route)
		default:
			match["action"] = "bypass"
			match["outbound"] = routeTag(direct)
		}
		appendRule(match)
	}
	var fallback *model.Rule
	for _, rule := range intent.Rules {
		if !rule.Enabled {
			continue
		}
		if rule.Default {
			copy := rule
			fallback = &copy
			continue
		}
		// Local proxy inbounds and transport/application conditions cannot
		// match an ICMP packet. Preserve every applicable L3 predicate.
		if len(rule.Inbound) > 0 || len(rule.Port) > 0 || len(rule.Protocol) > 0 || len(rule.Network) > 0 {
			continue
		}
		emit(rule)
	}
	if fallback != nil {
		emit(*fallback)
		return result
	}
	appendRule(map[string]any{"network": []string{"icmp"}, "action": "bypass", "outbound": routeTag(direct)})
	return result
}
