// SPDX-License-Identifier: GPL-3.0-or-later
package openwrt

import (
	"strings"
	"testing"
)

func TestRenderMinimalDNSShim(t *testing.T) {
	plan := Plan{Resources: Resources{DNSPort: 1053, AutoRedirectOutputMark: 0x2024}}
	config := RenderFirewall(plan)
	for _, required := range []string{"meta nfproto ipv4 meta l4proto { tcp, udp } th dport 53 counter redirect to :1053", "dnat ip6 to fdfe:dcba:9876::2", "dnat ip to 127.0.0.1:1053", "dnat ip6 to [::1]:1053", "snat ip to 127.0.0.1", "snat ip6 to ::1", "udp dport 123"} {
		if !strings.Contains(config, required) {
			t.Fatalf("missing %q:\n%s", required, config)
		}
	}
	if strings.Count(config, "fib daddr type != local return") != 2 {
		t.Fatal("both external DNS hooks must be restricted to local destinations")
	}
}
