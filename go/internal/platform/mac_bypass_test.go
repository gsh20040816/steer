// SPDX-License-Identifier: GPL-3.0-or-later
package platform_test

import (
	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/platform/linux"
	"github.com/gsh20040816/steer/go/internal/platform/openwrt"
	"reflect"
	"strings"
	"testing"
)

func TestWholeMACBypassUsesNativeTUNAndExemptsLocalDNS(t *testing.T) {
	const mac = "02:aa:00:00:00:10"
	value := intent.Intent{Routes: []intent.Route{{ID: "direct", Enabled: true, Kind: "direct"}}, Rules: []intent.Rule{{Enabled: true, Route: "direct", SourceMACAddress: []string{"02:AA:00:00:00:10"}}}}
	for _, mode := range []string{"off", "static", "dns"} {
		value.Main.DirectBypass = mode
		lp, op := linux.NewPlan(value), openwrt.NewPlan(value)
		of := openwrt.RenderFirewall(op)
		for _, tc := range []struct {
			name     string
			target   compiler.Target
			firewall string
		}{{"linux", lp.CompilerTarget(), linux.RenderFirewall(lp)}, {"openwrt", op.CompilerTarget(), of}} {
			tun := tc.target.Inbounds[0].(map[string]any)
			if !reflect.DeepEqual(tun["exclude_mac_address"], []string{mac}) {
				t.Fatalf("%s/%s missing native exclusion: %v", tc.name, mode, tun)
			}
			exclusion := strings.Index(tc.firewall, "ether saddr { "+mac+" } counter return")
			dns := strings.Index(tc.firewall, "th dport 53")
			if exclusion < 0 || exclusion > dns {
				t.Fatalf("%s still intercepts device DNS", tc.name)
			}
			if strings.Contains(tc.firewall, "ct mark set") {
				t.Fatalf("%s implements a custom data bypass", tc.name)
			}
		}
	}
	value.Rules[0].Port = []int{443}
	for _, target := range []compiler.Target{linux.NewPlan(value).CompilerTarget(), openwrt.NewPlan(value).CompilerTarget()} {
		if _, ok := target.Inbounds[0].(map[string]any)["exclude_mac_address"]; ok {
			t.Fatal("conditional direct bypasses entire device")
		}
	}
}
