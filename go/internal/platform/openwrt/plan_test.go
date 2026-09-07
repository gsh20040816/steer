// SPDX-License-Identifier: GPL-3.0-or-later

package openwrt

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/gsh20040816/steer/go/internal/compiler"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

func TestPlanOwnsOpenWrtResourcesAndCompilerTarget(t *testing.T) {
	value := model.Intent{
		LocalProxies: []model.LocalProxy{{Enabled: true, ListenPort: 20000}},
		Rules:        []model.Rule{{Enabled: true, SourceMACAddress: []string{"02:00:00:00:00:10"}}},
	}
	plan := NewPlan(value)
	target := plan.CompilerTarget()
	encoded, _ := json.Marshal(target)
	if len(target.Inbounds) != 2 || !strings.Contains(string(encoded), `"auto_redirect":true`) {
		t.Fatalf("compiler target omitted OpenWrt native inbounds: %s", encoded)
	}
	tun := target.Inbounds[0].(map[string]any)
	if tun["dns_mode"] != "hijack" {
		t.Fatalf("OpenWrt TUN must enable native DNS hijacking: %#v", tun)
	}
	for _, retired := range []string{`"type":"tproxy"`, `"mac_bindings"`, `"steer-mac-"`} {
		if strings.Contains(string(encoded), retired) {
			t.Fatalf("compiler target retained pre-1.14 MAC shim %q: %s", retired, encoded)
		}
	}
}

func TestOpenWrtCompilerTargetUsesSharedLocalProxyResolve(t *testing.T) {
	value, err := Decode(strings.NewReader(minimalConfig))
	if err != nil {
		t.Fatal(err)
	}
	value.LocalProxies = []model.LocalProxy{{ID: "local", Enabled: true, Protocol: "http", Listen: "127.0.0.1", ListenPort: 1090}}
	value.Rules = append([]model.Rule{{ID: "local", Enabled: true, DNSProfile: "direct_dns", Route: "direct", Inbound: []string{"local"}}}, value.Rules...)
	bundle := compiler.Compile(value, compiler.Options{Target: NewPlan(value).CompilerTarget()})
	rules := bundle.SingBox["route"].(map[string]any)["rules"].([]any)
	if len(rules) < 4 || rules[0].(map[string]any)["action"] != "hijack-dns" ||
		rules[1].(map[string]any)["action"] != "sniff" || rules[2].(map[string]any)["action"] != "resolve" ||
		!reflect.DeepEqual(rules[2].(map[string]any)["inbound"], []string{"steer-tun", "steer-local-local"}) ||
		rules[3].(map[string]any)["outbound"] != "steer-route-direct" {
		t.Fatalf("OpenWrt target lost shared sniff/resolve/route semantics: %#v", rules)
	}
}
