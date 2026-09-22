// SPDX-License-Identifier: GPL-3.0-or-later

package platform_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/platform/linux"
	"github.com/gsh20040816/steer/go/internal/platform/openwrt"
)

// Export fixtures produced by the actual compiler and each Linux platform
// target. The namespace test uses hosts DNS and a local SOCKS stub only.
func TestExportDirectBypassFixtures(t *testing.T) {
	root := os.Getenv("STEER_BYPASS_FIXTURE_DIR")
	if root == "" {
		t.Skip("set STEER_BYPASS_FIXTURE_DIR for isolated bypass integration")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	value := intent.Intent{
		Main:         intent.Main{DirectBypass: "dns", LogLevel: "debug"},
		Bootstrap:    intent.Bootstrap{Protocol: "udp", Server: "1.1.1.1", ServerPort: 53, Strategy: "prefer_ipv6"},
		Routes:       []intent.Route{{ID: "direct", Enabled: true, Kind: "direct"}, {ID: "proxy", Enabled: true, Kind: "single", Node: "proxy"}, {ID: "block", Enabled: true, Kind: "block"}},
		Nodes:        []intent.Node{{ID: "proxy", Enabled: true, Type: "socks", Server: "2001:4860:77:2::2", ServerPort: 19080}},
		DNSProfiles:  []intent.DNSProfile{{ID: "dns", Enabled: true, Protocol: "udp", Server: "1.1.1.1", ServerPort: 53}},
		LocalProxies: []intent.LocalProxy{{ID: "local", Enabled: true, Protocol: "http", Listen: "::", ListenPort: 19090}},
		Rules: []intent.Rule{
			{ID: "private", Route: "direct", DomainMatch: []string{"domain:lan"}},
			{ID: "management", Route: "direct", Port: []int{18081}},
			{ID: "mac", Route: "direct", SourceMACAddress: []string{"02:00:00:00:00:10"}, Port: []int{18085}},
			{ID: "proxy_domain", Route: "proxy", DomainMatch: []string{"geosite:test-proxy"}},
			{ID: "reject_domain", Route: "block", DomainMatch: []string{"domain:reject.test"}},
			{ID: "protocol", Route: "proxy", Protocol: []string{"http"}, Port: []int{18083}},
			{ID: "udp", Route: "proxy", Network: []string{"udp"}, Protocol: []string{"quic"}, Port: []int{18084}},
			{ID: "direct_domain", Route: "direct", DomainMatch: []string{"geosite:test-direct", "full:extra.test"}},
			{ID: "direct_ip", Route: "direct", IPMatch: []string{"geoip:test-direct"}},
			{ID: "default", Default: true, Route: "proxy"},
		},
	}
	for i := range value.Rules {
		value.Rules[i].Enabled = true
		value.Rules[i].DNSProfile = "dns"
	}
	for _, platform := range []string{"linux", "openwrt"} {
		target := linux.NewPlan(value).CompilerTarget()
		if platform == "openwrt" {
			target = openwrt.NewPlan(value).CompilerTarget()
		}
		for _, mode := range []string{"off", "static", "dns"} {
			value.Main.DirectBypass = mode
			doc := compiler.Compile(value, compiler.Options{Target: target, StateDirectory: "/run/steer-bypass"}).SingBox
			// Replace only the external resolver transports, retaining generated
			// DNS routing and reverse-mapping options.
			dns := doc["dns"].(map[string]any)
			predefined := map[string]any{
				"mapped.direct.test":   []string{"2001:4860:77:3::10"},
				"mapped.proxy.test":    []string{"2001:4860:77:3::11"},
				"protocol.direct.test": []string{"2001:4860:77:3::12"},
				"mapped.other.test":    []string{"2001:4860:77:3::13"},
				"mapped.reject.test":   []string{"2001:4860:77:3::14"},
			}
			var servers []any
			for _, raw := range dns["servers"].([]any) {
				servers = append(servers, map[string]any{"type": "hosts", "tag": raw.(map[string]any)["tag"], "predefined": predefined})
			}
			dns["servers"] = servers
			// Inline equivalents exercise the same rule_set predicates without
			// requiring downloads, seed files or real third-party classifications.
			doc["route"].(map[string]any)["rule_set"] = []any{
				map[string]any{"type": "inline", "tag": "steer-geosite-test-proxy", "rules": []any{map[string]any{"domain_suffix": []string{"proxy.test"}}}},
				map[string]any{"type": "inline", "tag": "steer-geosite-test-direct", "rules": []any{map[string]any{"domain_suffix": []string{"direct.test"}}}},
				map[string]any{"type": "inline", "tag": "steer-geoip-test-direct", "rules": []any{map[string]any{"ip_cidr": []string{"2001:4860:77:3::/64"}}}},
			}
			data, err := json.MarshalIndent(doc, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, platform+"-"+mode+".json"), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}
