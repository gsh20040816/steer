// SPDX-License-Identifier: GPL-3.0-or-later
package compiler

import (
	"reflect"
	"testing"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

func TestWireGuardICMPPreMatch(t *testing.T) {
	for _, tc := range []struct {
		name, ip, network, defaultRoute, action, outbound string
	}{
		{"public IPv4", "1.1.1.1", "icmp", "proxy", "bypass", "steer-route-direct"},
		{"public IPv6", "2605:52c0:1:c8b:8894:b7ff:fe59:6f8e", "icmp", "proxy", "bypass", "steer-route-direct"},
		{"WireGuard IPv4", "10.77.0.1", "icmp", "proxy", "route", "steer-route-wg-route"},
		{"WireGuard IPv6", "fd00::1", "icmp", "proxy", "route", "steer-route-wg-route"},
		{"earlier proxy wins", "10.1.0.1", "icmp", "proxy", "bypass", "steer-route-direct"},
		{"block wins", "10.2.0.1", "icmp", "proxy", "reject", ""},
		{"WireGuard default", "1.1.1.1", "icmp", "wg-route", "route", "steer-route-wg-route"},
		{"block default", "1.1.1.1", "icmp", "block", "reject", ""},
		{"TCP unchanged", "10.77.0.1", "tcp", "proxy", "", ""},
		{"UDP unchanged", "10.77.0.1", "udp", "proxy", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := wgIntent()
			v.Routes = append(v.Routes, model.Route{ID: "block", Enabled: true, Kind: "block"})
			v.Rules = []model.Rule{
				{Enabled: true, Route: "proxy", IPMatch: []string{"10.1.0.0/16"}},
				{Enabled: true, Route: "block", IPMatch: []string{"10.2.0.0/16"}},
				{Enabled: false, Route: "block", IPMatch: []string{"0.0.0.0/0"}},
				{Enabled: true, Route: "wg-route", AllowedIPs: true},
				{Enabled: true, Default: true, Route: tc.defaultRoute},
			}
			rules := Compile(v, testOptions()).SingBox["route"].(map[string]any)["rules"].([]any)
			for _, raw := range rules {
				rule := raw.(map[string]any)
				if rule["action"] == "sniff" {
					break
				}
				predicate := map[string]any{}
				for k, value := range rule {
					if k != "outbound" && k != "override_address" && k != "override_port" {
						switch v := value.(type) {
						case string:
							if k == "network" {
								value = []string{v}
							}
						case int:
							if k == "port" {
								value = []int{v}
							}
						}
						predicate[k] = value
					}
				}
				if !matchesBypass(t, predicate, bypassPacket{ip: tc.ip, source: "198.18.0.1", network: tc.network, inbound: "steer-tun"}) {
					continue
				}
				outbound, _ := rule["outbound"].(string)
				if rule["action"] != tc.action || outbound != tc.outbound {
					t.Fatalf("unexpected pre-match action: %#v", rule)
				}
				return
			}
			if tc.action != "" {
				t.Fatal("ICMP reached sniff without a terminal L3 action")
			}
		})
	}
}

func TestICMPPolicyDoesNotDependOnUnusedWireGuard(t *testing.T) {
	v := representativeIntent()
	without := compileICMPRules(v, testOptions().Target)
	v.WireGuardTunnels = wgIntent().WireGuardTunnels
	with := compileICMPRules(v, testOptions().Target)
	if !reflect.DeepEqual(without, with) {
		t.Fatal("an unrelated endpoint changed ICMP policy")
	}
}

func TestICMPDirectUsesPlatformTUNHostAddresses(t *testing.T) {
	v := representativeIntent()
	v.Rules = []model.Rule{{Enabled: true, Default: true, Route: "direct"}}
	target := Target{BypassInboundTags: []string{"test-tun"}, TUNAddresses: []string{"198.19.7.5/30", "fd12:3456::5/126"}}
	rules := compileICMPRules(v, target)
	for _, tc := range []struct{ source, action string }{
		{"198.19.7.5", "route"}, {"fd12:3456::5", "route"},
		{"198.19.7.6", "bypass"}, {"fd12:3456::6", "bypass"}, {"2001:4860::1", "bypass"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			for _, raw := range rules {
				r := raw.(map[string]any)
				predicate := map[string]any{}
				for k, val := range r {
					if k != "outbound" {
						predicate[k] = val
					}
				}
				if matchesBypass(t, predicate, bypassPacket{ip: "1.1.1.1", source: tc.source, network: "icmp", inbound: "test-tun"}) {
					if r["action"] != tc.action {
						t.Fatalf("source %s: got %v, want %s", tc.source, r["action"], tc.action)
					}
					return
				}
			}
			t.Fatal("missing terminal ICMP action")
		})
	}
}
