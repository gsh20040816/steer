// SPDX-License-Identifier: GPL-3.0-or-later

package linux

import (
	"github.com/gsh20040816/steer/go/internal/compiler"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

const (
	TunInterface           = "steer0"
	DNSPort                = 1053
	DNSPort6               = 1054
	TunTable               = 2022
	TunPriority            = 9000
	TunFallbackPriority    = 32768
	AutoRedirectInputMark  = 0x2023
	AutoRedirectOutputMark = 0x2024
	AutoRedirectResetMark  = 0x2025
	AutoRedirectNFQueue    = 100
)

var nonGlobalIPv4 = []string{
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16",
	"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
}

var nonGlobalIPv6 = []string{
	"::/128", "::1/128", "::ffff:0:0/96", "64:ff9b:1::/48", "100::/64", "100:0:0:1::/64",
	"2001:db8::/32", "2002::/16", "3fff::/20", "5f00::/16", "fc00::/7", "fe80::/10", "ff00::/8",
}

type Plan struct {
	SchemaVersion int       `json:"schema_version"`
	Resources     Resources `json:"resources"`
}

type Resources struct {
	TunInterface           string   `json:"tun_interface"`
	TunAddresses           []string `json:"tun_addresses"`
	DNSPort                int      `json:"dns_port"`
	DNSPort6               int      `json:"dns_port6"`
	TunTable               int      `json:"tun_table"`
	TunPriority            int      `json:"tun_priority"`
	TunFallbackPriority    int      `json:"tun_fallback_priority"`
	AutoRedirectInputMark  int      `json:"auto_redirect_input_mark"`
	AutoRedirectOutputMark int      `json:"auto_redirect_output_mark"`
	AutoRedirectResetMark  int      `json:"auto_redirect_reset_mark"`
	AutoRedirectNFQueue    int      `json:"auto_redirect_nfqueue"`
}

func NewPlan(_ model.Intent) Plan {
	return Plan{SchemaVersion: 1, Resources: Resources{
		TunInterface: TunInterface,
		TunAddresses: []string{"198.18.0.1/30", "fdfe:dcba:9876::1/126"},
		DNSPort:      DNSPort, DNSPort6: DNSPort6, TunTable: TunTable, TunPriority: TunPriority,
		TunFallbackPriority: TunFallbackPriority, AutoRedirectInputMark: AutoRedirectInputMark,
		AutoRedirectOutputMark: AutoRedirectOutputMark, AutoRedirectResetMark: AutoRedirectResetMark,
		AutoRedirectNFQueue: AutoRedirectNFQueue,
	}}
}

func (plan Plan) CompilerTarget() compiler.Target {
	inbounds := []any{
		map[string]any{
			"type": "tun", "tag": "steer-tun", "interface_name": plan.Resources.TunInterface,
			"address": plan.Resources.TunAddresses, "mtu": 9000, "dns_mode": "hijack", "auto_route": true,
			"strict_route": true, "auto_redirect": true, "stack": "system",
			"iproute2_table_index": plan.Resources.TunTable, "iproute2_rule_index": plan.Resources.TunPriority,
			"auto_redirect_iproute2_fallback_rule_index": plan.Resources.TunFallbackPriority,
			"auto_redirect_input_mark":                   plan.Resources.AutoRedirectInputMark,
			"auto_redirect_output_mark":                  plan.Resources.AutoRedirectOutputMark,
			"auto_redirect_reset_mark":                   plan.Resources.AutoRedirectResetMark,
			"auto_redirect_nfqueue":                      plan.Resources.AutoRedirectNFQueue,
			"route_exclude_address":                      append(append([]string{}, nonGlobalIPv4...), nonGlobalIPv6...),
		},
		map[string]any{
			"type": "direct", "tag": "steer-dns4", "listen": "0.0.0.0", "listen_port": plan.Resources.DNSPort,
			"network": []string{"tcp", "udp"},
		},
		map[string]any{
			"type": "direct", "tag": "steer-dns6", "listen": "::", "listen_port": plan.Resources.DNSPort6,
			"network": []string{"tcp", "udp"},
		},
	}
	return compiler.Target{
		Inbounds: inbounds, DNSInboundTags: []string{"steer-dns4", "steer-dns6"}, SniffInboundTags: []string{"steer-tun"},
		RequiredCapabilities: []string{"tun", "auto_route", "auto_redirect"},
	}
}
