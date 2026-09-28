// SPDX-License-Identifier: GPL-3.0-or-later

package compiler

import (
	"slices"
	"strings"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

// DirectMACExclusions selects devices whose entire transparent traffic is
// Direct. Linux adapters lower these to TUN exclude_mac_address, before Docker
// masquerading can erase the source identity of UDP entering the TUN.
// Never promote a conditional rule or override an earlier possible non-Direct
// winner. Explicit local-proxy rules remain in the normal rule engine.
func DirectMACExclusions(intent model.Intent) []string {
	routes := indexRoutes(intent.Routes)
	var earlier []model.Rule
	var result []string
	for _, rule := range intent.Rules {
		if !rule.Enabled || rule.Default {
			continue
		}
		route := routes[rule.Route]
		if route.Enabled && route.Kind == "direct" && len(rule.SourceMACAddress) > 0 &&
			len(rule.Inbound)+len(rule.DomainMatch)+len(rule.IPMatch)+len(rule.SourceIPCIDR)+len(rule.Network)+len(rule.Protocol)+len(rule.Port) == 0 && !rule.AllowedIPs {
			for _, address := range rule.SourceMACAddress {
				mac := strings.ToLower(address)
				blocked := false
				for _, prior := range earlier {
					if len(prior.Inbound) > 0 {
						continue
					}
					if len(prior.SourceMACAddress) > 0 && !slices.ContainsFunc(prior.SourceMACAddress, func(value string) bool { return strings.EqualFold(value, mac) }) {
						continue
					}
					blocked = true
					break
				}
				if !blocked {
					result = append(result, mac)
				}
			}
		}
		if !route.Enabled || route.Kind != "direct" {
			earlier = append(earlier, rule)
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}
