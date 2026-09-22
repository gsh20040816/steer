// SPDX-License-Identifier: GPL-3.0-or-later

package compiler

import (
	"encoding/json"
	"net/netip"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

type bypassPacket struct {
	domain, ip, source, mac, network, inbound string
	port                                      int
}

// Evaluate only the emitted native predicate vocabulary. The real sing-box
// namespace integration test separately verifies engine and socket behavior.
func matchesBypass(t *testing.T, value map[string]any, p bypassPacket) bool {
	t.Helper()
	matched := true
	if value["type"] == "logical" {
		matched = value["mode"] == "and"
		for _, child := range value["rules"].([]any) {
			yes := matchesBypass(t, child.(map[string]any), p)
			if value["mode"] == "and" {
				matched = matched && yes
			} else {
				matched = matched || yes
			}
		}
	} else {
		for key, raw := range value {
			predicate := false
			switch key {
			case "action":
				continue
			case "ip_version":
				predicate = netip.MustParseAddr(p.ip).Is6() == (raw.(int) == 6)
			case "port":
				predicate = slices.Contains(raw.([]int), p.port)
			case "network":
				predicate = slices.Contains(raw.([]string), p.network)
			case "inbound":
				predicate = slices.Contains(raw.([]string), p.inbound)
			case "source_mac_address":
				predicate = slices.Contains(raw.([]string), strings.ToLower(p.mac))
			case "domain":
				predicate = slices.Contains(raw.([]string), p.domain)
			case "domain_suffix":
				for _, suffix := range raw.([]string) {
					predicate = predicate || p.domain == suffix || strings.HasSuffix(p.domain, "."+suffix)
				}
			case "domain_regex":
				for _, pattern := range raw.([]string) {
					predicate = predicate || regexp.MustCompile(pattern).MatchString(p.domain)
				}
			case "ip_cidr", "source_ip_cidr":
				address := p.ip
				if key == "source_ip_cidr" {
					address = p.source
				}
				for _, cidr := range raw.([]string) {
					predicate = predicate || netip.MustParsePrefix(cidr).Contains(netip.MustParseAddr(address))
				}
			default:
				t.Fatalf("unexpected predicate %q", key)
			}
			matched = matched && predicate
		}
	}
	if value["invert"] == true {
		matched = !matched
	}
	return matched
}

func TestDirectBypassPreservesOrderedPolicy(t *testing.T) {
	directDomain := model.Rule{Route: "direct", DomainMatch: []string{"domain:direct.test"}}
	proxyDomain := model.Rule{Route: "proxy", DomainMatch: []string{"domain:proxy.test"}}
	proxyProtocol := model.Rule{Route: "proxy", Protocol: []string{"bittorrent"}}
	directIP := model.Rule{Route: "direct", IPMatch: []string{"2001:db8::/32"}}
	base := bypassPacket{domain: "direct.test", ip: "2001:db8::2", source: "2001:db8:1::2", network: "tcp", inbound: "steer-tun", port: 443}
	for _, tc := range []struct {
		name, mode string
		rules      []model.Rule
		change     func(*bypassPacket)
		want       bool
	}{
		{"mapped direct", "dns", []model.Rule{proxyDomain, directDomain}, nil, true},
		{"earlier proxy wins", "dns", []model.Rule{proxyDomain, directIP}, func(p *bypassPacket) { p.domain = "proxy.test" }, false},
		{"missing domain blocks later IP", "dns", []model.Rule{proxyDomain, directIP}, func(p *bypassPacket) { p.domain = "" }, false},
		{"known domain excludes proxy for IP", "dns", []model.Rule{proxyDomain, directIP}, nil, true},
		{"static ignores reverse mapping", "static", []model.Rule{proxyDomain, directIP}, nil, false},
		{"same direct unknown is harmless", "static", []model.Rule{directDomain, directIP}, func(p *bypassPacket) { p.domain = "" }, true},
		{"same direct protocol unknown is harmless", "static", []model.Rule{{Route: "direct", Protocol: []string{"bittorrent"}}, directIP}, nil, true},
		{"protocol barrier", "dns", []model.Rule{proxyProtocol, directDomain}, nil, false},
		{"compound protocol disproved by port", "dns", []model.Rule{{Route: "proxy", Protocol: []string{"bittorrent"}, Port: []int{6881}}, directDomain}, nil, true},
		{"compound protocol still possible", "dns", []model.Rule{{Route: "proxy", Protocol: []string{"bittorrent"}, Port: []int{443}}, directDomain}, nil, false},
		{"UDP proxy irrelevant for TCP", "dns", []model.Rule{{Route: "proxy", Network: []string{"udp"}, Protocol: []string{"quic"}}, directDomain}, nil, true},
		{"reject barrier", "dns", []model.Rule{{Route: "block", DomainMatch: []string{"domain:direct.test"}}, directDomain}, nil, false},
		{"direct before proxy", "dns", []model.Rule{directDomain, proxyDomain}, nil, true},
		{"default direct respects barrier", "dns", []model.Rule{proxyDomain, {Route: "direct", Default: true}}, func(p *bypassPacket) { p.domain = "" }, false},
		{"all direct default", "static", []model.Rule{directDomain, {Route: "direct", Default: true}}, func(p *bypassPacket) { p.domain = "" }, true},
		{"local proxy rule irrelevant", "dns", []model.Rule{{Route: "proxy", Inbound: []string{"local"}}, directDomain}, nil, true},
		{"unknown MAC proxy blocks", "dns", []model.Rule{{Route: "proxy", SourceMACAddress: []string{"02:00:00:00:00:01"}}, directDomain}, nil, false},
		{"MAC direct matches", "static", []model.Rule{directDomain, {Route: "direct", SourceMACAddress: []string{"02:AA:00:00:00:01"}}}, func(p *bypassPacket) { p.mac = "02:aa:00:00:00:01" }, true},
		{"missing MAC cannot direct", "static", []model.Rule{{Route: "direct", SourceMACAddress: []string{"02:00:00:00:00:01"}}}, nil, false},
		{"UDP bypass", "dns", []model.Rule{directDomain}, func(p *bypassPacket) { p.network = "udp" }, true},
		{"no implicit network restriction", "static", []model.Rule{{Route: "direct", Default: true}}, func(p *bypassPacket) { p.network = "icmp"; p.port = 0 }, true},
		{"UDP direct matches UDP", "static", []model.Rule{{Route: "direct", Network: []string{"udp"}}}, func(p *bypassPacket) { p.network = "udp" }, true},
		{"UDP direct excludes TCP", "static", []model.Rule{{Route: "direct", Network: []string{"udp"}}}, nil, false},
		{"TCP direct excludes UDP", "static", []model.Rule{{Route: "direct", Network: []string{"tcp"}}}, func(p *bypassPacket) { p.network = "udp" }, false},
		{"TCP UDP direct matches UDP", "static", []model.Rule{{Route: "direct", Network: []string{"tcp", "udp"}}}, func(p *bypassPacket) { p.network = "udp" }, true},
		{"UDP proxy wins for UDP", "dns", []model.Rule{{Route: "proxy", Network: []string{"udp"}}, directDomain}, func(p *bypassPacket) { p.network = "udp" }, false},
		{"UDP protocol barrier for UDP", "dns", []model.Rule{{Route: "proxy", Network: []string{"udp"}, Protocol: []string{"quic"}}, directDomain}, func(p *bypassPacket) { p.network = "udp" }, false},
		{"TCP proxy irrelevant for UDP", "dns", []model.Rule{{Route: "proxy", Network: []string{"tcp"}}, directDomain}, func(p *bypassPacket) { p.network = "udp" }, true},
		{"UDP reject wins", "static", []model.Rule{{Route: "block", Network: []string{"udp"}}, {Route: "direct", Default: true}}, func(p *bypassPacket) { p.network = "udp" }, false},
		{"UDP MAC direct", "static", []model.Rule{{Route: "direct", SourceMACAddress: []string{"02:AA:00:00:00:01"}}}, func(p *bypassPacket) { p.network = "udp"; p.mac = "02:aa:00:00:00:01" }, true},
		{"no UDP DNS bypass", "static", []model.Rule{{Route: "direct", Default: true}}, func(p *bypassPacket) { p.network = "udp"; p.port = 53 }, false},
		{"no IPv4 bypass", "dns", []model.Rule{directDomain}, func(p *bypassPacket) { p.ip = "192.0.2.2" }, false},
		{"no local proxy bypass", "dns", []model.Rule{directDomain}, func(p *bypassPacket) { p.inbound = "steer-local-local" }, false},
		{"no DNS bypass", "static", []model.Rule{{Route: "direct", Default: true}}, func(p *bypassPacket) { p.port = 53 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			intent := representativeIntent()
			intent.Main.DirectBypass = tc.mode
			intent.Rules = tc.rules
			for i := range intent.Rules {
				intent.Rules[i].Enabled = true
			}
			packet := base
			if tc.change != nil {
				tc.change(&packet)
			}
			rules := compileDirectBypass(intent, Target{BypassInboundTags: []string{"steer-tun"}})
			got := false
			for _, raw := range rules {
				rule := raw.(map[string]any)
				if _, ok := rule["outbound"]; ok {
					t.Fatal("pre-match rule has an L4 fallback")
				}
				got = got || matchesBypass(t, rule, packet)
			}
			if got != tc.want {
				t.Fatalf("bypass=%v want %v; rules=%#v", got, tc.want, rules)
			}
		})
	}
}

func TestBypassOptInAndFallback(t *testing.T) {
	intent := representativeIntent()
	options := testOptions()
	baseline := Compile(intent, options)
	options.Target.BypassInboundTags = []string{"steer-tun"}
	if got := Compile(intent, options); got.RuntimeDigest != baseline.RuntimeDigest {
		t.Fatal("platform capability changed default behavior")
	}
	intent.Main.DirectBypass = "off"
	if got := Compile(intent, options); got.RuntimeDigest != baseline.RuntimeDigest {
		t.Fatal("explicit off changed runtime")
	}
	intent.Main.DirectBypass = "dns"
	unsupported := options
	unsupported.Target.BypassInboundTags = nil
	if got := Compile(intent, unsupported); got.RuntimeDigest != baseline.RuntimeDigest {
		t.Fatal("unsupported target emits bypass")
	}
	got := Compile(intent, options)
	if got.RuntimeDigest == baseline.RuntimeDigest {
		t.Fatal("bypass did not change runtime digest")
	}
	wantRules := baseline.SingBox["route"].(map[string]any)["rules"].([]any)
	var kept []any
	seenSniff := false
	for _, raw := range got.SingBox["route"].(map[string]any)["rules"].([]any) {
		rule := raw.(map[string]any)
		if rule["action"] == "sniff" {
			seenSniff = true
		}
		if rule["action"] == "bypass" && rule["outbound"] == nil {
			if seenSniff {
				t.Fatal("bypass emitted after sniff")
			}
			continue
		}
		kept = append(kept, raw)
	}
	if !reflect.DeepEqual(wantRules, kept) {
		t.Fatal("full fallback rules changed")
	}
	if !reflect.DeepEqual(baseline.SingBox["dns"], got.SingBox["dns"]) {
		t.Fatal("DNS transport projection changed")
	}
	// The default rule is semantically final regardless of its slice position.
	intent.Rules = []model.Rule{{Enabled: true, Default: true, Route: "direct"}, {Enabled: true, Route: "proxy", Protocol: []string{"tls"}}}
	if len(compileDirectBypass(intent, options.Target)) != 0 {
		t.Fatal("default bypass escaped earlier protocol barrier")
	}
	intent.Rules[1].Enabled = false
	if len(compileDirectBypass(intent, options.Target)) != 1 {
		t.Fatal("disabled barrier participates in plan")
	}
	a, _ := json.Marshal(Compile(intent, options))
	b, _ := json.Marshal(Compile(intent, options))
	if string(a) != string(b) {
		t.Fatal("non-deterministic bypass plan")
	}
}
