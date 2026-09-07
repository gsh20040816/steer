// SPDX-License-Identifier: GPL-3.0-or-later

package linux

import (
	"fmt"
	"strings"
)

// RenderFirewall retains only the local-destination exception: LAN/container
// and host queries to this host cannot rely on TUN routing. sing-box owns all other
// port-53 interception through dns_mode=hijack + auto_redirect.
func RenderFirewall(plan Plan) string {
	lines := []string{
		"table inet steer {",
		"\tchain dns_prerouting {",
		"\t\ttype nat hook prerouting priority dstnat - 2; policy accept;",
		fmt.Sprintf("\t\tiifname \"%s\" return", plan.Resources.TunInterface),
		fmt.Sprintf("\t\tmeta mark 0x%x counter return", plan.Resources.AutoRedirectOutputMark),
		"\t\tfib daddr type != local return",
		fmt.Sprintf("\t\tmeta nfproto ipv4 meta l4proto { tcp, udp } th dport 53 counter redirect to :%d", plan.Resources.DNSPort),
		fmt.Sprintf("\t\tmeta nfproto ipv6 meta l4proto { tcp, udp } th dport 53 counter redirect to :%d", plan.Resources.DNSPort6),
		"\t}",
		"\tchain dns_output {",
		"\t\ttype nat hook output priority mangle - 2; policy accept;",
		fmt.Sprintf("\t\tmeta mark 0x%x counter return", plan.Resources.AutoRedirectOutputMark),
		fmt.Sprintf("\t\toifname \"%s\" return", plan.Resources.TunInterface),
		"\t\tfib daddr type != local return",
		fmt.Sprintf("\t\tmeta nfproto ipv4 meta l4proto { tcp, udp } th dport 53 counter dnat ip to 127.0.0.1:%d", plan.Resources.DNSPort),
		fmt.Sprintf("\t\tmeta nfproto ipv6 meta l4proto { tcp, udp } th dport 53 counter dnat ip6 to [::1]:%d", plan.Resources.DNSPort6),
		"\t}",
		"\tchain dns_postrouting {",
		"\t\ttype nat hook postrouting priority srcnat - 2; policy accept;",
		fmt.Sprintf("\t\tmeta nfproto ipv4 meta l4proto { tcp, udp } ip daddr 127.0.0.1 th dport %d counter snat ip to 127.0.0.1", plan.Resources.DNSPort),
		fmt.Sprintf("\t\tmeta nfproto ipv6 meta l4proto { tcp, udp } ip6 daddr ::1 th dport %d counter snat ip6 to ::1", plan.Resources.DNSPort6),
		"\t}",
		"\tchain dns_input {",
		"\t\ttype filter hook input priority filter; policy accept;",
		fmt.Sprintf("\t\tmeta l4proto { tcp, udp } th dport { %d, %d } ct status dnat counter accept", plan.Resources.DNSPort, plan.Resources.DNSPort6),
		fmt.Sprintf("\t\tmeta l4proto { tcp, udp } th dport { %d, %d } counter reject", plan.Resources.DNSPort, plan.Resources.DNSPort6),
		"\t}",
		"}",
		"",
	}
	return strings.Join(lines, "\n")
}
