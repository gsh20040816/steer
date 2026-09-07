// SPDX-License-Identifier: GPL-3.0-or-later

package linux

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gsh20040816/steer/go/internal/compiler"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

func TestLinuxPlanCapturesHostAndForwardedTraffic(t *testing.T) {
	plan := NewPlan(model.Intent{})
	if plan.Resources.TunInterface != "steer0" || plan.Resources.DNSPort != 1053 || plan.Resources.DNSPort6 != 1054 {
		t.Fatalf("unexpected Linux resources: %#v", plan.Resources)
	}
	target := plan.CompilerTarget()
	if len(target.Inbounds) != 3 || len(target.DNSInboundTags) != 2 {
		t.Fatalf("Linux target has unexpected resources: %#v", target)
	}
	dns4 := target.Inbounds[1].(map[string]any)
	dns6 := target.Inbounds[2].(map[string]any)
	if dns4["listen"] != "0.0.0.0" || dns4["listen_port"] != 1053 || dns6["listen"] != "::" || dns6["listen_port"] != 1054 {
		t.Fatalf("Linux DNS listeners do not cover redirected IPv4 and IPv6 traffic: %#v %#v", dns4, dns6)
	}
	tun := target.Inbounds[0].(map[string]any)
	if tun["dns_mode"] != "hijack" {
		t.Fatalf("Linux TUN must enable native DNS hijacking: %#v", tun)
	}
	if _, restricted := tun["include_interface"]; restricted {
		t.Fatal("Linux TUN unexpectedly restricts interception to a host interface")
	}
	if target.RequiredCapabilities == nil || strings.Join(target.RequiredCapabilities, ",") != "tun,auto_route,auto_redirect" {
		t.Fatalf("unexpected capability contract: %#v", target.RequiredCapabilities)
	}
}

func TestLinuxCompilerTargetUsesSharedLocalProxyResolve(t *testing.T) {
	value := validIntent()
	value.LocalProxies = []model.LocalProxy{{ID: "local", Enabled: true, Protocol: "socks", Listen: "127.0.0.1", ListenPort: 1090}}
	value.Rules = append([]model.Rule{{ID: "local", Enabled: true, DNSProfile: "public", Route: "direct", Inbound: []string{"local"}}}, value.Rules...)
	bundle := compiler.Compile(value, compiler.Options{Target: NewPlan(value).CompilerTarget()})
	rules := bundle.SingBox["route"].(map[string]any)["rules"].([]any)
	if len(rules) < 4 || rules[0].(map[string]any)["action"] != "hijack-dns" ||
		rules[1].(map[string]any)["action"] != "sniff" || rules[2].(map[string]any)["action"] != "resolve" ||
		!reflect.DeepEqual(rules[2].(map[string]any)["inbound"], []string{"steer-tun", "steer-local-local"}) ||
		rules[3].(map[string]any)["outbound"] != "steer-route-direct" {
		t.Fatalf("Linux target lost shared sniff/resolve/route semantics: %#v", rules)
	}
}

func TestLinuxFirewallCapturesHostAndForwardedDNS(t *testing.T) {
	text := RenderFirewall(NewPlan(model.Intent{}))
	if !strings.Contains(text, "hook output") || !strings.Contains(text, "hook prerouting") || strings.Count(text, "fib daddr type != local return") != 2 {
		t.Fatalf("Linux firewall does not cover host and forwarded DNS:\n%s", text)
	}
	for _, required := range []string{
		"meta nfproto ipv4 meta l4proto { tcp, udp } th dport 53 counter redirect to :1053",
		"meta nfproto ipv6 meta l4proto { tcp, udp } th dport 53 counter redirect to :1054",
		"iifname \"steer0\" return",
		"meta mark 0x2024 counter return",
		"dnat ip to 127.0.0.1:1053",
		"dnat ip6 to [::1]:1054",
		"snat ip to 127.0.0.1",
		"snat ip6 to ::1",
		"th dport { 1053, 1054 } ct status dnat counter accept",
		"th dport { 1053, 1054 } counter reject",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Linux DNS firewall is missing %q:\n%s", required, text)
		}
	}
}
