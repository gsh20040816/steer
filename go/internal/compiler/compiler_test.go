// SPDX-License-Identifier: GPL-3.0-or-later
package compiler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

func representativeIntent() model.Intent {
	return model.Intent{
		Main: model.Main{ID: "main", SchemaVersion: model.SchemaVersion, Enabled: true, LogLevel: "warn",
			ProbeDirectURL: "https://www.baidu.com/", ProbeProxyURL: "https://www.google.com/generate_204", SpeedtestProxyURL: "https://speed.cloudflare.com/__down?bytes=1000000",
			DNSCacheCapacity: 4096, DNSCachePersist: true, DNSOptimisticCache: true},
		Bootstrap:    model.Bootstrap{ID: "bootstrap", Protocol: "udp", Server: "1.1.1.1", ServerPort: 53, Strategy: "prefer_ipv4"},
		Nodes:        []model.Node{{ID: "node", Enabled: true, Type: "vless", Server: "node.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{UUID: "00000000-0000-4000-8000-000000000001"}, NodeTransport: model.NodeTransport{Flow: "xtls-rprx-vision", PacketEncoding: "xudp"}, NodeTLS: model.NodeTLS{TLSServerName: "www.example.com", RealityPublicKey: "fixture", RealityShortID: "0123456789abcdef", UTLSFingerprint: "chrome"}}},
		Routes:       []model.Route{{ID: "direct", Enabled: true, Kind: "direct"}, {ID: "proxy", Enabled: true, Kind: "single", Node: "node"}, {ID: "block", Enabled: true, Kind: "block"}},
		DNSProfiles:  []model.DNSProfile{{ID: "public", Enabled: true, Protocol: "https", Server: "1.1.1.1", ServerPort: 443, TLSServerName: "one.one.one.one", Path: "/dns-query"}},
		LocalProxies: []model.LocalProxy{{ID: "local", Enabled: true, Protocol: "mixed", Listen: "127.0.0.1", ListenPort: 1090}},
		Rules: []model.Rule{
			{ID: "mac", Enabled: true, DNSProfile: "public", Route: "direct", SourceMACAddress: []string{"02:00:00:00:00:10"}},
			{ID: "service", Enabled: true, DNSProfile: "public", Route: "proxy", DomainMatch: []string{"domain:example.com", "geosite:category-example"}, Network: []string{"udp"}, Protocol: []string{"quic"}, Port: []int{443}},
			{ID: "local_rule", Enabled: true, DNSProfile: "public", Route: "proxy", Inbound: []string{"local"}},
			{ID: "default", Enabled: true, Default: true, DNSProfile: "public", Route: "direct"},
		},
	}
}

func testOptions() Options {
	return Options{StateDirectory: "/var/lib/steer", Target: Target{
		Inbounds: []any{
			map[string]any{"type": "tun", "tag": "steer-tun", "address": []string{"198.18.0.1/30", "fdfe:dcba:9876::1/126"}, "dns_mode": "disabled", "auto_route": true, "auto_redirect": true},
			map[string]any{"type": "direct", "tag": "steer-dns", "listen": "::", "listen_port": 1053},
		},
		DNSInboundTags:       []string{"steer-dns"},
		SniffInboundTags:     []string{"steer-tun"},
		RequiredCapabilities: []string{"tun", "auto_route", "auto_redirect"},
	}}
}

func TestCompilePathIsolationAndProjection(t *testing.T) {
	bundle := Compile(representativeIntent(), testOptions())
	dns := bundle.SingBox["dns"].(map[string]any)
	if len(dns["servers"].([]any)) != 3 {
		t.Fatalf("DNS paths are not isolated by route: %#v", dns["servers"])
	}
	if _, exists := dns["independent_cache"]; exists || dns["optimistic"] != true {
		t.Fatalf("sing-box 1.14 DNS cache options are wrong: %#v", dns)
	}
	route := bundle.SingBox["route"].(map[string]any)
	sets := route["rule_set"].([]any)
	if len(sets) != 1 {
		t.Fatalf("Geo rule-sets were not grouped: %#v", sets)
	}
	remote := sets[0].(map[string]any)
	if remote["type"] != "remote" || remote["url"] != "https://gsh20040816.github.io/steer/geodata/latest/rules/{tag}.srs" || remote["initial_path"] != "/usr/share/steer/geodata-seed/rules/{tag}.srs" {
		t.Fatalf("unexpected remote Geo rule-set: %#v", remote)
	}
	cache := bundle.SingBox["experimental"].(map[string]any)["cache_file"].(map[string]any)
	if cache["path"] != "/var/lib/steer/cache.db" || cache["store_dns"] != true {
		t.Fatalf("unexpected persistent cache: %#v", cache)
	}
	dnsRules := dns["rules"].([]any)
	routeRules := route["rules"].([]any)
	encodedDNS, _ := json.Marshal(dnsRules)
	encodedRoute, _ := json.Marshal(routeRules)
	if strings.Contains(string(encodedDNS), `"port":`) || !strings.Contains(string(encodedRoute), `"port":[443]`) {
		t.Fatalf("DNS and Route projections diverged incorrectly\nDNS=%s\nRoute=%s", encodedDNS, encodedRoute)
	}
	if !strings.Contains(string(encodedDNS), "steer-dns-public-via-proxy") {
		t.Fatalf("proxy DNS path missing: %s", encodedDNS)
	}
	if strings.Contains(string(encodedDNS), `"strategy"`) {
		t.Fatalf("DNS rules retained the deprecated route-action strategy: %s", encodedDNS)
	}
}

func TestCompileLocalProxyDomainsUseRuleDNSProfile(t *testing.T) {
	for _, protocol := range []string{"socks", "http", "mixed"} {
		for _, routeID := range []string{"direct", "proxy"} {
			t.Run(protocol+"_via_"+routeID, func(t *testing.T) {
				intent := representativeIntent()
				intent.LocalProxies[0].Protocol = protocol
				intent.Rules = []model.Rule{
					{ID: "local_rule", Enabled: true, DNSProfile: "public", Route: routeID, Inbound: []string{"local"}},
					{ID: "default", Enabled: true, Default: true, DNSProfile: "public", Route: "direct"},
				}

				bundle := Compile(intent, testOptions())
				assertLocalProxyInboundProtocol(t, bundle, protocol)
				assertLocalProxyResolveOrder(t, bundle, routeID)
				assertLocalProxyDNSPath(t, bundle, routeID)
			})
		}
	}
}

func assertLocalProxyInboundProtocol(t *testing.T, bundle Output, protocol string) {
	t.Helper()
	for _, raw := range bundle.SingBox["inbounds"].([]any) {
		inbound := raw.(map[string]any)
		if inbound["tag"] == "steer-local-local" {
			if inbound["type"] != protocol {
				t.Fatalf("local proxy protocol changed: %#v", inbound)
			}
			return
		}
	}
	t.Fatal("compiled local proxy inbound is missing")
}

func assertLocalProxyResolveOrder(t *testing.T, bundle Output, routeID string) {
	t.Helper()
	rules := bundle.SingBox["route"].(map[string]any)["rules"].([]any)
	sniffIndex, resolveIndex, routeIndex, resolveCount := -1, -1, -1, 0
	for index, raw := range rules {
		rule := raw.(map[string]any)
		switch rule["action"] {
		case "sniff":
			if reflect.DeepEqual(rule["inbound"], []string{"steer-tun", "steer-local-local"}) {
				sniffIndex = index
			}
		case "resolve":
			resolveCount++
			resolveIndex = index
			if !reflect.DeepEqual(rule["inbound"], []string{"steer-tun", "steer-local-local"}) {
				t.Fatalf("resolve is not restricted to sniffed inbounds: %#v", rule)
			}
			if _, exists := rule["server"]; exists {
				t.Fatalf("resolve bypasses DNS routing instead of selecting a rule DNS Profile: %#v", rule)
			}
		case "route":
			if rule["outbound"] == routeTag(routeID) && reflect.DeepEqual(rule["inbound"], []string{"steer-local-local"}) {
				routeIndex = index
			}
		}
	}
	if resolveCount != 1 {
		t.Fatalf("expected one native resolve action, got %d: %#v", resolveCount, rules)
	}
	if sniffIndex < 0 || resolveIndex != sniffIndex+1 || routeIndex <= resolveIndex {
		t.Fatalf("local proxy must sniff, resolve through DNS rules, then route: %#v", rules)
	}
}

func assertLocalProxyDNSPath(t *testing.T, bundle Output, routeID string) {
	t.Helper()
	expectedTag := dnsPathTag("public", routeID)
	dns := bundle.SingBox["dns"].(map[string]any)
	matchedRule := false
	for _, raw := range dns["rules"].([]any) {
		rule := raw.(map[string]any)
		if reflect.DeepEqual(rule["inbound"], []string{"steer-local-local"}) {
			if rule["action"] != "route" || rule["server"] != expectedTag {
				t.Fatalf("local proxy DNS rule selected the wrong path: %#v", rule)
			}
			matchedRule = true
		}
	}
	if !matchedRule {
		t.Fatal("local proxy DNS rule is missing")
	}

	matchedServer := false
	for _, raw := range dns["servers"].([]any) {
		server := raw.(map[string]any)
		if server["tag"] != expectedTag {
			continue
		}
		matchedServer = true
		if routeID == "proxy" && server["detour"] != routeTag("proxy") {
			t.Fatalf("proxy DNS path lost its Route detour: %#v", server)
		}
		if routeID == "direct" {
			if _, exists := server["detour"]; exists {
				t.Fatalf("Direct DNS path unexpectedly has a detour: %#v", server)
			}
		}
	}
	if !matchedServer {
		t.Fatalf("DNS server path %q is missing: %#v", expectedTag, dns["servers"])
	}
}

func TestCompileUsesStrategyOnlyForInternalDomainResolution(t *testing.T) {
	intent := representativeIntent()
	intent.Bootstrap.Strategy = "prefer_ipv6"
	intent.DNSProfiles = append(intent.DNSProfiles, model.DNSProfile{
		ID: "hostname", Enabled: true, Protocol: "udp", Server: "resolver.example",
		ServerPort: 53,
	})
	intent.Rules = append(intent.Rules[:len(intent.Rules)-1],
		model.Rule{ID: "hostname", Enabled: true, DNSProfile: "hostname", Route: "direct", DomainMatch: []string{"domain:resolver.test"}},
		intent.Rules[len(intent.Rules)-1],
	)
	bundle := Compile(intent, testOptions())
	dns := bundle.SingBox["dns"].(map[string]any)
	if dns["strategy"] != nil {
		t.Fatalf("client DNS unexpectedly received a global address strategy: %#v", dns)
	}
	defaultResolver := bundle.SingBox["route"].(map[string]any)["default_domain_resolver"].(map[string]any)
	if defaultResolver["strategy"] != "prefer_ipv6" {
		t.Fatalf("route domain resolver lost bootstrap strategy: %#v", defaultResolver)
	}
	for _, raw := range dns["rules"].([]any) {
		if rule := raw.(map[string]any); rule["strategy"] != nil {
			t.Fatalf("DNS rule retained deprecated per-query strategy: %#v", rule)
		}
	}
	foundResolver := false
	for _, raw := range dns["servers"].([]any) {
		server := raw.(map[string]any)
		if server["tag"] != "steer-dns-hostname-via-direct" {
			continue
		}
		resolver := server["domain_resolver"].(map[string]any)
		if resolver["strategy"] != "prefer_ipv6" {
			t.Fatalf("DNS server domain resolver lost its independent strategy: %#v", server)
		}
		foundResolver = true
	}
	if !foundResolver {
		t.Fatal("hostname DNS server path was not compiled")
	}
}

func TestCompileNativeMACRulesAndNoForbiddenFeatures(t *testing.T) {
	bundle := Compile(representativeIntent(), testOptions())
	encoded, _ := json.Marshal(bundle.SingBox)
	text := string(encoded)
	for _, forbidden := range []string{"fakeip", "udp/443", "smartdns", "serve_expired", "steer-mac-tproxy", "steer-mac-dns"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("forbidden feature %q in %s", forbidden, text)
		}
	}
	if !strings.Contains(text, `"auto_redirect":true`) || !strings.Contains(text, `"auto_route":true`) {
		t.Fatalf("TUN main path missing: %s", text)
	}
	if !strings.Contains(text, `"address":["198.18.0.1/30","fdfe:dcba:9876::1/126"]`) {
		t.Fatalf("unexpected TUN addresses: %s", text)
	}
	dnsRules, _ := json.Marshal(bundle.SingBox["dns"].(map[string]any)["rules"])
	routeRules, _ := json.Marshal(bundle.SingBox["route"].(map[string]any)["rules"])
	for name, rules := range map[string][]byte{"DNS": dnsRules, "Route": routeRules} {
		if !strings.Contains(string(rules), `"source_mac_address":["02:00:00:00:00:10"]`) {
			t.Fatalf("%s rules do not use sing-box 1.14 native source MAC matching: %s", name, rules)
		}
	}
}

func TestCompileDoesNotHijackICMP(t *testing.T) {
	bundle := Compile(representativeIntent(), testOptions())
	rules := bundle.SingBox["route"].(map[string]any)["rules"].([]any)
	icmp := icmpBypassRule(rules)
	if icmp == nil {
		t.Fatalf("ICMP must leave the capture path before sniff: %#v", rules)
	}
	if icmp["action"] != "bypass" || !reflect.DeepEqual(icmp["network"], []string{"icmp"}) || icmp["outbound"] != "steer-route-direct" {
		t.Fatalf("ICMP bypass is wrong: %#v", icmp)
	}
	if _, exists := icmp["inbound"]; exists {
		t.Fatalf("ICMP bypass must apply to auto_redirect pre-match, not only TUN: %#v", icmp)
	}
	if icmpIndex(rules) >= sniffIndex(rules) {
		t.Fatalf("ICMP bypass must run before sniff: %#v", rules)
	}
}

func TestCompileKeepsDedicatedDNSHijackBoundary(t *testing.T) {
	bundle := Compile(representativeIntent(), testOptions())
	route := bundle.SingBox["route"].(map[string]any)
	rules := route["rules"].([]any)
	first := rules[0].(map[string]any)
	if first["action"] != "hijack-dns" {
		t.Fatalf("DNS capture boundary lost: %#v", first)
	}
	inbounds := first["inbound"].([]string)
	if len(inbounds) != 1 || inbounds[0] != "steer-dns" {
		t.Fatalf("hijack-dns is not restricted to the dedicated DNS inbound: %#v", first)
	}
	if strings.Contains(string(mustJSON(first)), "steer-tun") {
		t.Fatalf("general TUN inbound was connected directly to hijack-dns: %#v", first)
	}
}

func TestCompileTUNPort53DNSCaptureIsExplicit(t *testing.T) {
	options := testOptions()
	options.Target.DNSInboundTags = nil
	options.Target.DNSCapture = DNSCapture{Mode: DNSCaptureTUNPort53Hijack, InboundTags: []string{"steer-tun"}}
	bundle := Compile(representativeIntent(), options)
	rules := bundle.SingBox["route"].(map[string]any)["rules"].([]any)
	first := rules[0].(map[string]any)
	if first["action"] != "hijack-dns" || !reflect.DeepEqual(first["port"], []uint16{53}) || !reflect.DeepEqual(first["network"], []string{"tcp", "udp"}) {
		t.Fatalf("TUN port-53 capture was not explicit: %#v", first)
	}
	if _, exists := first["source_port"]; exists {
		t.Fatalf("TUN DNS capture matched the reusable source port instead of destination port 53: %#v", first)
	}
}

func TestCompileDNSCaptureNoneDoesNotInventHijack(t *testing.T) {
	options := testOptions()
	options.Target.DNSInboundTags = nil
	options.Target.DNSCapture = DNSCapture{Mode: DNSCaptureNone, InboundTags: []string{"steer-dns"}, TUNInboundTags: []string{"steer-tun"}}
	bundle := Compile(representativeIntent(), options)
	rules := bundle.SingBox["route"].(map[string]any)["rules"].([]any)
	if strings.Contains(string(mustJSON(rules)), `"action":"hijack-dns"`) {
		t.Fatalf("DNS capture none emitted hijack-dns: %#v", rules)
	}
}

func mustJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func TestCompileDirectDNSWithoutDetourAndBlockAsReject(t *testing.T) {
	intent := representativeIntent()
	blockRule := model.Rule{ID: "blocked", Enabled: true, DNSProfile: "public", Route: "block", DomainMatch: []string{"domain:blocked.example"}}
	intent.Rules = append(intent.Rules[:len(intent.Rules)-1], blockRule, intent.Rules[len(intent.Rules)-1])
	bundle := Compile(intent, testOptions())
	dns := bundle.SingBox["dns"].(map[string]any)
	servers, _ := json.Marshal(dns["servers"])
	if strings.Contains(string(servers), `"tag":"steer-dns-bootstrap","detour"`) || strings.Contains(string(servers), `"tag":"steer-dns-public-via-direct","detour"`) {
		t.Fatalf("direct DNS transport has an invalid direct detour: %s", servers)
	}
	if !strings.Contains(string(servers), `"detour":"steer-route-proxy"`) {
		t.Fatalf("proxy DNS transport lost its fixed route: %s", servers)
	}
	rules, _ := json.Marshal(dns["rules"])
	if !strings.Contains(string(rules), `"domain_suffix":["blocked.example"],"action":"reject"`) && !strings.Contains(string(rules), `"action":"reject","domain_suffix":["blocked.example"]`) {
		t.Fatalf("block DNS projection is not a reject action: %s", rules)
	}
	routeRules, _ := json.Marshal(bundle.SingBox["route"].(map[string]any)["rules"])
	if !strings.Contains(string(routeRules), `"domain_suffix":["blocked.example"],"action":"reject"`) && !strings.Contains(string(routeRules), `"action":"reject","domain_suffix":["blocked.example"]`) {
		t.Fatalf("block traffic projection is not a reject action: %s", routeRules)
	}
	outbounds, _ := json.Marshal(bundle.SingBox["outbounds"])
	if strings.Contains(string(outbounds), `"type":"block"`) || strings.Contains(string(outbounds), `"tag":"steer-route-block"`) {
		t.Fatalf("deprecated block outbound was emitted: %s", outbounds)
	}
}

func TestCompileRejectDoesNotWidenDNSProjection(t *testing.T) {
	testCases := []struct {
		name          string
		connectionSet func(*model.Rule)
		expectedRoute string
	}{
		{name: "port", connectionSet: func(rule *model.Rule) { rule.Port = []int{8443} }, expectedRoute: `"port":[8443]`},
		{name: "protocol", connectionSet: func(rule *model.Rule) { rule.Protocol = []string{"tls"} }, expectedRoute: `"protocol":["tls"]`},
		{name: "network", connectionSet: func(rule *model.Rule) { rule.Network = []string{"tcp"} }, expectedRoute: `"network":["tcp"]`},
		{name: "ip_match", connectionSet: func(rule *model.Rule) { rule.IPMatch = []string{"203.0.113.0/24"} }, expectedRoute: `"ip_cidr":["203.0.113.0/24"]`},
		{name: "geoip", connectionSet: func(rule *model.Rule) { rule.IPMatch = []string{"geoip:private"} }, expectedRoute: `"rule_set":["steer-geoip-private"]`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			intent := representativeIntent()
			blockRule := model.Rule{
				ID: "conditional_block", Enabled: true, DNSProfile: "public", Route: "block",
				DomainMatch: []string{"domain:conditional-block.example"},
			}
			testCase.connectionSet(&blockRule)
			intent.Rules = append(intent.Rules[:len(intent.Rules)-1], blockRule, intent.Rules[len(intent.Rules)-1])

			bundle := Compile(intent, testOptions())
			dnsRules := string(mustJSON(bundle.SingBox["dns"].(map[string]any)["rules"]))
			if strings.Contains(dnsRules, "conditional-block.example") {
				t.Fatalf("conditional reject widened into a DNS reject: %s", dnsRules)
			}

			var compiledRoute string
			for _, raw := range bundle.SingBox["route"].(map[string]any)["rules"].([]any) {
				routeRule := raw.(map[string]any)
				encoded := string(mustJSON(routeRule))
				if routeRule["action"] == "reject" && strings.Contains(encoded, "conditional-block.example") {
					compiledRoute = encoded
					break
				}
			}
			if compiledRoute == "" || !strings.Contains(compiledRoute, testCase.expectedRoute) {
				t.Fatalf("connection-stage reject lost its exact match: %s", compiledRoute)
			}
		})
	}
}

func TestCompileDefaultBlockAsFinalRejectRule(t *testing.T) {
	intent := representativeIntent()
	intent.Rules[len(intent.Rules)-1].Route = "block"
	bundle := Compile(intent, testOptions())
	route := bundle.SingBox["route"].(map[string]any)
	rules := route["rules"].([]any)
	last := rules[len(rules)-1].(map[string]any)
	if !reflect.DeepEqual(last, map[string]any{"action": "reject"}) {
		t.Fatalf("default Block did not become a final reject action: %#v", rules)
	}
	if route["final"] != "steer-route-direct" {
		t.Fatalf("unreachable sing-box final must reference the valid Direct outbound: %#v", route)
	}
	dnsRules := bundle.SingBox["dns"].(map[string]any)["rules"].([]any)
	if !reflect.DeepEqual(dnsRules[len(dnsRules)-1], map[string]any{"action": "reject"}) {
		t.Fatalf("default Block DNS did not become a final reject action: %#v", dnsRules)
	}
}

func TestCompileIsDeterministic(t *testing.T) {
	first := Compile(representativeIntent(), testOptions())
	second := Compile(representativeIntent(), testOptions())
	if !reflect.DeepEqual(first, second) {
		t.Fatal("same intent produced different bundle")
	}
}

func TestRuntimeDigestIgnoresInventoryOnlyChanges(t *testing.T) {
	base := representativeIntent()
	first := Compile(base, testOptions())

	inventoryOnly := base
	inventoryOnly.Subscriptions = []model.Subscription{{ID: "feed", Enabled: true, URL: "https://feed.example/sub"}}
	inventoryOnly.Nodes = append(append([]model.Node{}, base.Nodes...), model.Node{
		ID: "unused", Enabled: true, Type: "socks", Server: "unused.example", ServerPort: 1080,
	})
	second := Compile(inventoryOnly, testOptions())
	if first.IntentDigest == second.IntentDigest {
		t.Fatal("canonical digest did not include subscription inventory")
	}
	if first.RuntimeDigest != second.RuntimeDigest {
		t.Fatalf("inventory-only change altered runtime digest: %s != %s", first.RuntimeDigest, second.RuntimeDigest)
	}

	runtimeChange := base
	runtimeChange.Main.LogLevel = "info"
	third := Compile(runtimeChange, testOptions())
	if first.RuntimeDigest == third.RuntimeDigest {
		t.Fatal("runtime-affecting change did not alter runtime digest")
	}

	probeChange := base
	probeChange.Main.ProbeDirectURL = "https://new-probe.example/"
	fourth := Compile(probeChange, testOptions())
	if first.RuntimeDigest == fourth.RuntimeDigest {
		t.Fatal("active probe change did not alter runtime digest")
	}
}

func TestCompileRoutePrivateOutboundsAndDetour(t *testing.T) {
	intent := representativeIntent()
	intent.Routes = append(intent.Routes, model.Route{ID: "chained", Enabled: true, Kind: "single", Node: "node", Detour: "proxy"})
	bundle := Compile(intent, testOptions())
	outbounds := bundle.SingBox["outbounds"].([]any)
	var proxy, chained map[string]any
	for _, raw := range outbounds {
		outbound := raw.(map[string]any)
		switch outbound["tag"] {
		case "steer-route-proxy":
			proxy = outbound
		case "steer-route-chained":
			chained = outbound
		}
	}
	if proxy == nil || chained == nil {
		t.Fatalf("route-private outbounds are missing: %#v", outbounds)
	}
	if proxy["type"] != "vless" || proxy["detour"] != nil {
		t.Fatalf("base route outbound is wrong: %#v", proxy)
	}
	if chained["type"] != "vless" || chained["detour"] != "steer-route-proxy" {
		t.Fatalf("chained route outbound lost detour: %#v", chained)
	}
	encoded, _ := json.Marshal(outbounds)
	if strings.Contains(string(encoded), "steer-node-node") || strings.Contains(string(encoded), `"type":"selector"`) {
		t.Fatalf("legacy global node selector leaked into route-private compilation: %s", encoded)
	}
	chain := CompileRouteChainOutbounds(intent, "chained")
	chainJSON, _ := json.Marshal(chain)
	if len(chain) != 2 || !strings.Contains(string(chainJSON), `"tag":"steer-route-proxy"`) || !strings.Contains(string(chainJSON), `"tag":"steer-route-chained"`) {
		t.Fatalf("route test did not retain the complete target chain: %s", chainJSON)
	}
	if strings.Contains(string(chainJSON), `"tag":"steer-route-direct"`) || strings.Contains(string(chainJSON), `"tag":"steer-route-block"`) {
		t.Fatalf("route test included unrelated outbounds: %s", chainJSON)
	}
}

func TestCompileSingBoxProxyNodeFamilies(t *testing.T) {
	base := representativeIntent()
	base.Nodes = []model.Node{
		{ID: "ss", Enabled: true, Type: "shadowsocks", Server: "ss.example", ServerPort: 8388, NodeCredentials: model.NodeCredentials{Password: "secret"}, NodeProtocol: model.NodeProtocol{Method: "2022-blake3-aes-128-gcm"}},
		{ID: "vmess", Enabled: true, Type: "vmess", Server: "vmess.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{UUID: "00000000-0000-4000-8000-000000000001"}, NodeProtocol: model.NodeProtocol{Security: "auto"}, NodeTransport: model.NodeTransport{Transport: "ws", TransportPath: "/ws"}, NodeTLS: model.NodeTLS{TLSServerName: "vmess.example"}},
		{ID: "tuic", Enabled: true, Type: "tuic", Server: "tuic.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{UUID: "00000000-0000-4000-8000-000000000002", Password: "secret"}, NodeTLS: model.NodeTLS{TLSServerName: "tuic.example"}},
		{ID: "ssh", Enabled: true, Type: "ssh", Server: "ssh.example", ServerPort: 22, NodeCredentials: model.NodeCredentials{Username: "root", Password: "secret"}},
	}
	base.Routes[1].Node = "ss"
	base.Routes = append(base.Routes,
		model.Route{ID: "vmess_route", Enabled: true, Kind: "single", Node: "vmess"},
		model.Route{ID: "tuic_route", Enabled: true, Kind: "single", Node: "tuic"},
		model.Route{ID: "ssh_route", Enabled: true, Kind: "single", Node: "ssh"},
	)
	bundle := Compile(base, testOptions())
	encoded, _ := json.Marshal(bundle.SingBox["outbounds"])
	for _, expected := range []string{`"type":"shadowsocks"`, `"type":"vmess"`, `"type":"tuic"`, `"type":"ssh"`} {
		if !strings.Contains(string(encoded), expected) {
			t.Fatalf("compiled outbounds missing %s: %s", expected, encoded)
		}
	}
}

func TestCompileHTTPFingerprintAlwaysEnablesTLS(t *testing.T) {
	tests := []struct {
		name        string
		credentials model.NodeCredentials
		fingerprint string
		wantTLS     bool
	}{
		{name: "fingerprint only", fingerprint: "chrome", wantTLS: true},
		{name: "fingerprint with authentication", credentials: model.NodeCredentials{Username: "user", Password: "secret"}, fingerprint: "chrome", wantTLS: true},
		{name: "plain HTTP", credentials: model.NodeCredentials{Username: "user", Password: "secret"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := model.Node{
				ID: "http", Enabled: true, Type: "http", Server: "proxy.example", ServerPort: 8080,
				NodeCredentials: test.credentials,
				NodeTLS:         model.NodeTLS{UTLSFingerprint: test.fingerprint},
			}
			if validation := model.ValidateNode(node); !validation.OK {
				t.Fatalf("HTTP node was rejected: %#v", validation.Errors)
			}
			outbound := CompileNodeOutbound(node)
			tls, hasTLS := outbound["tls"].(map[string]any)
			if hasTLS != test.wantTLS {
				t.Fatalf("unexpected TLS projection: %#v", outbound)
			}
			if test.wantTLS {
				if tls["enabled"] != true {
					t.Fatalf("TLS was not enabled: %#v", tls)
				}
				utls, ok := tls["utls"].(map[string]any)
				if !ok || utls["enabled"] != true || utls["fingerprint"] != test.fingerprint {
					t.Fatalf("uTLS fingerprint was not projected: %#v", tls)
				}
			}
			capabilities := requiredCapabilities(model.Intent{Nodes: []model.Node{node}}, Target{})
			if hasString(capabilities, "with_utls") != (test.fingerprint != "") {
				t.Fatalf("uTLS capability does not match TLS projection: %#v", capabilities)
			}
		})
	}
}

func TestCompileVLESSRealityDoesNotSilentlyDowngrade(t *testing.T) {
	tests := []struct {
		name        string
		tls         model.NodeTLS
		wantReality bool
	}{
		{name: "ordinary TLS", tls: model.NodeTLS{TLSServerName: "proxy.example"}},
		{name: "complete Reality", tls: model.NodeTLS{TLSServerName: "proxy.example", RealityPublicKey: "public-key", RealityShortID: "0123456789abcdef"}, wantReality: true},
		{name: "partial Reality stays explicit", tls: model.NodeTLS{TLSServerName: "proxy.example", RealityShortID: "0123456789abcdef"}, wantReality: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := model.Node{
				ID: "vless", Enabled: true, Type: "vless", Server: "proxy.example", ServerPort: 443,
				NodeCredentials: model.NodeCredentials{UUID: "00000000-0000-4000-8000-000000000001"},
				NodeTLS:         test.tls,
			}
			outbound := CompileNodeOutbound(node)
			tls, ok := outbound["tls"].(map[string]any)
			if !ok || tls["enabled"] != true {
				t.Fatalf("VLESS TLS was not generated: %#v", outbound)
			}
			reality, hasReality := tls["reality"].(map[string]any)
			if hasReality != test.wantReality {
				t.Fatalf("unexpected Reality projection: %#v", tls)
			}
			if test.wantReality && reality["enabled"] != true {
				t.Fatalf("Reality was not enabled: %#v", reality)
			}
			if test.name == "complete Reality" && (reality["public_key"] != test.tls.RealityPublicKey || reality["short_id"] != test.tls.RealityShortID) {
				t.Fatalf("complete Reality fields were not projected: %#v", reality)
			}
		})
	}
}

func TestCompileTUICALPN(t *testing.T) {
	node := model.Node{
		ID: "tuic", Enabled: true, Type: "tuic", Server: "example.com", ServerPort: 39823,
		NodeCredentials: model.NodeCredentials{UUID: "00000000-0000-4000-8000-000000000001"},
		NodeTLS:         model.NodeTLS{TLSServerName: "example.com", ALPN: []string{"h3", "h2"}},
	}
	outbound := CompileNodeOutbound(node)
	tls, ok := outbound["tls"].(map[string]any)
	if !ok {
		t.Fatalf("TUIC TLS was not generated: %#v", outbound)
	}
	if !reflect.DeepEqual(tls["alpn"], []string{"h3", "h2"}) {
		t.Fatalf("TUIC ALPN was not projected to sing-box TLS: %#v", tls)
	}
	if _, exists := outbound["password"]; exists {
		t.Fatalf("empty TUIC password must not be fabricated: %#v", outbound)
	}
}

func TestCompileDefaultVLESSTransportsAreEquivalent(t *testing.T) {
	node := model.Node{
		ID: "vless", Enabled: true, Type: "vless", Server: "proxy.example", ServerPort: 443,
		NodeCredentials: model.NodeCredentials{UUID: "00000000-0000-4000-8000-000000000001"},
		NodeTransport:   model.NodeTransport{Flow: "xtls-rprx-vision"},
		NodeTLS: model.NodeTLS{
			TLSServerName: "proxy.example", RealityPublicKey: "public-key",
			RealityShortID: "0123456789abcdef", UTLSFingerprint: "chrome",
		},
	}
	var expected map[string]any
	for _, transport := range []string{"", "tcp", "raw"} {
		node.Transport = transport
		outbound := CompileNodeOutbound(node)
		if _, exists := outbound["transport"]; exists {
			t.Fatalf("%q must compile without a V2Ray transport object: %#v", transport, outbound)
		}
		if expected == nil {
			expected = outbound
		} else if !reflect.DeepEqual(outbound, expected) {
			t.Fatalf("%q transport changed outbound semantics: got %#v, want %#v", transport, outbound, expected)
		}
	}
}

func hasString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func icmpBypassRule(rules []any) map[string]any {
	index := icmpIndex(rules)
	if index < 0 {
		return nil
	}
	return rules[index].(map[string]any)
}

func icmpIndex(rules []any) int {
	for index, raw := range rules {
		rule := raw.(map[string]any)
		if reflect.DeepEqual(rule["network"], []string{"icmp"}) {
			return index
		}
	}
	return -1
}

func sniffIndex(rules []any) int {
	for index, raw := range rules {
		if raw.(map[string]any)["action"] == "sniff" {
			return index
		}
	}
	return -1
}
