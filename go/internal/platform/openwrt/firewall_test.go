// SPDX-License-Identifier: GPL-3.0-or-later
package openwrt

import (
	"strings"
	"testing"
)

func TestRenderMinimalDNSShim(t *testing.T) {
	plan := Plan{Resources: Resources{DNSPort: 1053, AutoRedirectOutputMark: 0x2024}}
	config, err := RenderFirewall(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"meta nfproto ipv4 meta l4proto { tcp, udp } th dport 53 counter redirect to :1053", "dnat ip6 to fdfe:dcba:9876::2", "dnat ip to 127.0.0.1:1053", "dnat ip6 to [::1]:1053", "snat ip to 127.0.0.1", "snat ip6 to ::1", "udp dport 123"} {
		if !strings.Contains(config, required) {
			t.Fatalf("missing %q:\n%s", required, config)
		}
	}
	if strings.Count(config, "fib daddr type != local return") != 2 {
		t.Fatal("both external DNS hooks must be restricted to local destinations")
	}
	for _, retired := range []string{"ether saddr", "tproxy to", "mac_tproxy", "0x2026", "redirect to :20001"} {
		if strings.Contains(config, retired) {
			t.Fatalf("firewall retained pre-1.14 MAC shim %q:\n%s", retired, config)
		}
	}
	if strings.Contains(config, "managed_devices") || strings.Contains(config, "iifname @") {
		t.Fatalf("firewall still contains managed-zone scoping:\n%s", config)
	}
}

func TestRenderFirewallDoesNotRequireResolvedDevices(t *testing.T) {
	if _, err := RenderFirewall(Plan{}); err != nil {
		t.Fatal(err)
	}
}
