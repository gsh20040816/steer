// SPDX-License-Identifier: GPL-3.0-or-later
package compiler

import (
	"encoding/json"
	model "github.com/gsh20040816/steer/go/internal/intent"
	"os"
	"strings"
	"testing"
)

func wgIntent() model.Intent {
	v := representativeIntent()
	v.WireGuardTunnels = []model.WireGuardTunnel{{ID: "home", Enabled: true, Address: []string{"10.77.0.2/32"}, PrivateKey: "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", Peers: []model.WireGuardPeer{{Server: "wg.example.com", ServerPort: 51820, PublicKey: "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=", AllowedIPs: []string{"10.0.0.0/8", "fd00::/8"}}}, Access: []model.WireGuardAccess{{SourceIPCIDR: []string{"10.77.0.1/32"}, Network: "tcp", Port: 22, Target: "127.0.0.1", TargetPort: 2222}}}}
	v.Routes = append(v.Routes, model.Route{ID: "wg-route", Enabled: true, Kind: "wireguard", Tunnel: "home"})
	v.Rules = append([]model.Rule{{ID: "wg-rule", Enabled: true, AllowedIPs: true, Route: "wg-route", DNSProfile: "public"}}, v.Rules...)
	return v
}
func TestWireGuardIsolationAndOrdering(t *testing.T) {
	v := wgIntent()
	opts := testOptions()
	opts.Target.DirectRouteAddress = []string{"10.0.0.0/8"}
	v.Main.DirectBypass = "static"
	opts.Target.BypassInboundTags = []string{"steer-tun"}
	out := Compile(v, opts)
	if valid := model.Validate(v); !valid.OK {
		t.Fatalf("invalid fixture: %+v", valid.Errors)
	}
	b, _ := json.Marshal(out.SingBox)
	text := string(b)
	if !strings.Contains(text, `"endpoints"`) || !strings.Contains(text, `"with_wireguard"`) && !strings.Contains(strings.Join(out.RequiredCapabilities, ","), "with_wireguard") {
		t.Fatal(text)
	}
	rules := out.SingBox["route"].(map[string]any)["rules"].([]any)
	allow := rules[0].(map[string]any)
	deny := rules[1].(map[string]any)
	if allow["override_address"] != "127.0.0.1" || allow["override_port"] != 2222 || deny["action"] != "reject" {
		t.Fatalf("inbound access escaped: %+v", rules[:2])
	}
	business, private := -1, -1
	for i, raw := range rules {
		r := raw.(map[string]any)
		if r["outbound"] == "steer-route-wg-route" {
			business = i
		}
		if r["outbound"] == "steer-route-direct" && r["ip_cidr"] != nil {
			private = i
		}
	}
	if business < 0 || private <= business {
		t.Fatalf("private Direct precedes WireGuard: %s", text)
	}
	if len(v.Rules[0].IPMatch) != 0 {
		t.Fatal("compiler mutated saved intent")
	}
	for _, raw := range out.SingBox["dns"].(map[string]any)["rules"].([]any) {
		r := raw.(map[string]any)
		if r["server"] == "steer-dns-public-via-wg-route" {
			t.Fatal("IP-only AllowedIPs rule changed DNS routing")
		}
	}
	v.WireGuardTunnels[0].Peers[0].AllowedIPs = []string{"10.99.0.0/16"}
	next := Compile(v, opts)
	data, _ := json.Marshal(next.SingBox)
	if !strings.Contains(string(data), "10.99.0.0/16") {
		t.Fatal("AllowedIPs did not update")
	}
	if path := os.Getenv("STEER_WG_FIXTURE"); path != "" {
		if e := os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
