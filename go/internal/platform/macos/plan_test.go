// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"encoding/json"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/gsh20040816/steer/go/internal/compiler"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

func TestPlanStaticallyCapturesPrivateNetworksAndUsesPort53DNSCapture(t *testing.T) {
	plan := NewPlan(model.Intent{})
	if plan.SchemaVersion != 3 {
		t.Fatalf("unexpected static macOS plan schema: %d", plan.SchemaVersion)
	}
	target := plan.CompilerTarget()
	if len(target.Inbounds) != 1 || target.DNSCapture.Mode != compiler.DNSCaptureTUNPort53Hijack {
		t.Fatalf("unexpected macOS target: %#v", target)
	}
	if len(target.DNSCapture.InboundTags) != 1 || target.DNSCapture.InboundTags[0] != "steer-tun" {
		t.Fatalf("unexpected DNS inbound tags: %#v", target.DNSCapture)
	}
	tun := target.Inbounds[0].(map[string]any)
	if tun["auto_route"] != true {
		t.Fatal("macOS launchd runtime must let sing-box own auto_route")
	}
	if tun["dns_mode"] != "hijack" {
		t.Fatal("macOS must enable native DNS hijacking")
	}
	if _, exists := tun["auto_redirect"]; exists {
		t.Fatal("macOS target must not use Linux auto_redirect")
	}
	if len(tun["route_exclude_address"].([]string)) == 0 {
		t.Fatal("macOS TUN must preserve non-global routes")
	}
	excluded := tun["route_exclude_address"].([]string)
	for _, prefix := range excluded {
		if prefix == "198.18.0.0/15" {
			t.Fatal("macOS must not exclude the IPv4 subnet containing the system-stack peer")
		}
	}
	for _, private := range privateRouteAddress {
		if prefixCoveredByAny(private, excluded) {
			t.Fatalf("static private range %s remained excluded from TUN capture: %#v", private, excluded)
		}
	}
	for _, preserved := range []string{"127.0.0.0/8", "169.254.0.0/16", "192.0.2.0/24", "fe80::/10", "ff00::/8"} {
		if !contains(excluded, preserved) {
			t.Fatalf("non-private exclusion %s was lost: %#v", preserved, excluded)
		}
	}
	routeAddress := tun["route_address"].([]string)
	for _, required := range defaultRouteAddress {
		if !contains(routeAddress, required) {
			t.Fatalf("TUN route_address is missing %s: %#v", required, routeAddress)
		}
	}
	if !reflect.DeepEqual(routeAddress, defaultRouteAddress) {
		t.Fatalf("unnecessary private routes: %#v", routeAddress)
	}
	if !reflect.DeepEqual(target.DirectRouteAddress, privateRouteAddress) {
		t.Fatalf("static private Direct rule drifted: %#v", target.DirectRouteAddress)
	}
	globalOnLink := "2001:4860:4860::/64"
	if prefixCoveredByAny(globalOnLink, excluded) || prefixCoveredByAny(globalOnLink, target.DirectRouteAddress) {
		t.Fatalf("global IPv6 on-link prefix was treated as special private traffic: %s", globalOnLink)
	}
}

func TestPlanDoesNotBroadenScopedIPv6LinkLocalCapture(t *testing.T) {
	resolver := netip.MustParseAddr("fe80::1%en0")
	if resolver.Zone() != "en0" {
		t.Fatalf("test resolver lost its real interface scope: %s", resolver)
	}
	target := NewPlan(model.Intent{}).CompilerTarget()
	tun := target.Inbounds[0].(map[string]any)
	if !prefixContainsAddress("fe80::/10", resolver.WithZone("")) ||
		!contains(tun["route_exclude_address"].([]string), "fe80::/10") {
		t.Fatalf("scoped link-local resolver is no longer covered by the explicit link-local exclusion: %s", resolver)
	}
	for _, prefix := range target.DirectRouteAddress {
		if prefixContainsAddress(prefix, resolver.WithZone("")) {
			t.Fatalf("scoped resolver %s was broadened into private Direct range %s", resolver, prefix)
		}
	}
}

func TestPlanCompilerOutputRetainsDedicatedDNSHijack(t *testing.T) {
	value := model.Intent{
		Main:        model.Main{ID: "main", SchemaVersion: model.SchemaVersion, LogLevel: "warn"},
		Bootstrap:   model.Bootstrap{ID: "bootstrap", Protocol: "udp", Server: "1.1.1.1", ServerPort: 53, Strategy: "prefer_ipv4"},
		Routes:      []model.Route{{ID: "direct", Enabled: true, Kind: "direct"}},
		DNSProfiles: []model.DNSProfile{{ID: "dns", Enabled: true, Protocol: "udp", Server: "1.1.1.1", ServerPort: 53}},
		LocalProxies: []model.LocalProxy{{
			ID: "local", Enabled: true, Protocol: "mixed", Listen: "127.0.0.1", ListenPort: 1090,
		}},
		Rules: []model.Rule{
			{ID: "local", Enabled: true, DNSProfile: "dns", Route: "direct", Inbound: []string{"local"}},
			{ID: "default", Enabled: true, Default: true, DNSProfile: "dns", Route: "direct"},
		},
	}
	plan := NewPlan(value)
	bundle := compiler.Compile(value, plan.CompilerOptions("/tmp/steer-state"))
	encoded, _ := json.Marshal(bundle.SingBox["route"])
	if !strings.Contains(string(encoded), `"action":"hijack-dns"`) || !strings.Contains(string(encoded), `"port":[53]`) {
		t.Fatalf("macOS TUN DNS capture rule is missing: %s", encoded)
	}
	if strings.Contains(string(encoded), `"auto_redirect"`) {
		t.Fatalf("macOS route unexpectedly contains auto_redirect: %s", encoded)
	}
	rules := bundle.SingBox["route"].(map[string]any)["rules"].([]any)
	if len(rules) < 5 {
		t.Fatalf("macOS route rules are incomplete: %#v", rules)
	}
	first := rules[0].(map[string]any)
	if first["action"] != "hijack-dns" || !reflect.DeepEqual(first["port"], []uint16{53}) || !reflect.DeepEqual(first["network"], []string{"tcp", "udp"}) {
		t.Fatalf("macOS DNS hijack must match TCP/UDP destination port 53 exactly: %#v", first)
	}
	if _, exists := first["source_port"]; exists {
		t.Fatalf("macOS DNS hijack accidentally matches the reusable UDP source port: %#v", first)
	}
	direct := rules[1].(map[string]any)
	if direct["action"] != "route" || direct["outbound"] != "steer-route-direct" ||
		!reflect.DeepEqual(direct["ip_cidr"], privateRouteAddress) ||
		!reflect.DeepEqual(direct["inbound"], []string{"steer-tun"}) {
		t.Fatalf("private unicast must route Direct after DNS capture: %#v", rules)
	}
	if rules[2].(map[string]any)["action"] != "sniff" {
		t.Fatalf("sniff must run after DNS and LAN classification: %#v", rules)
	}
	resolve := rules[3].(map[string]any)
	if resolve["action"] != "resolve" || !reflect.DeepEqual(resolve["inbound"], []string{"steer-tun", "steer-local-local"}) {
		t.Fatalf("macOS target lost shared local proxy resolve semantics: %#v", rules)
	}
	if rules[4].(map[string]any)["outbound"] != "steer-route-direct" {
		t.Fatalf("local proxy route must run after domain resolution: %#v", rules)
	}
}

func prefixCoveredByAny(prefix string, values []string) bool {
	target := netip.MustParsePrefix(prefix)
	for _, value := range values {
		candidate := netip.MustParsePrefix(value)
		if candidate.Contains(target.Addr()) && candidate.Bits() <= target.Bits() {
			return true
		}
	}
	return false
}

func prefixContainsAddress(prefix string, address netip.Addr) bool {
	return netip.MustParsePrefix(prefix).Contains(address)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
