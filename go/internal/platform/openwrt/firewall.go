// SPDX-License-Identifier: GPL-3.0-or-later
package openwrt

import (
	"fmt"
	"strings"
)

func RenderFirewall(plan Plan) (string, error) {
	var lines []string
	add := func(values ...string) { lines = append(lines, values...) }
	add("table inet steer {",
		"\tchain dns_prerouting {", "\t\ttype nat hook prerouting priority dstnat - 2; policy accept;")
	add("\t\tiifname \"steer0\" return",
		fmt.Sprintf("\t\tmeta mark 0x%x return", plan.Resources.AutoRedirectOutputMark),
	)
	add(
		"\t\tfib daddr type != local return",
		fmt.Sprintf("\t\tmeta l4proto { tcp, udp } th dport 53 counter redirect to :%d", plan.Resources.DNSPort), "\t}",
		"\tchain dns_output {", "\t\ttype nat hook output priority mangle - 2; policy accept;",
		"\t\toifname \"steer0\" return",
		fmt.Sprintf("\t\tmeta mark 0x%x counter return", plan.Resources.AutoRedirectOutputMark),
		"\t\tfib daddr type != local return",
		fmt.Sprintf("\t\tmeta nfproto ipv4 meta l4proto { tcp, udp } th dport 53 counter dnat ip to 127.0.0.1:%d", plan.Resources.DNSPort),
		fmt.Sprintf("\t\tmeta nfproto ipv6 meta l4proto { tcp, udp } th dport 53 counter dnat ip6 to [::1]:%d", plan.Resources.DNSPort), "\t}",
		"\tchain dns_postrouting {", "\t\ttype nat hook postrouting priority srcnat - 2; policy accept;",
		fmt.Sprintf("\t\tmeta nfproto ipv4 meta l4proto { tcp, udp } ip daddr 127.0.0.1 th dport %d counter snat ip to 127.0.0.1", plan.Resources.DNSPort),
		fmt.Sprintf("\t\tmeta nfproto ipv6 meta l4proto { tcp, udp } ip6 daddr ::1 th dport %d counter snat ip6 to ::1", plan.Resources.DNSPort), "\t}",
		"\tchain system_output {", "\t\ttype route hook output priority mangle - 2; policy accept;",
		fmt.Sprintf("\t\tmeta l4proto udp udp dport 123 counter meta mark set 0x%x", plan.Resources.AutoRedirectOutputMark), "\t}")
	add("}", "")
	return strings.Join(lines, "\n"), nil
}
