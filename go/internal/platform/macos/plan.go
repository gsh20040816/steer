// SPDX-License-Identifier: GPL-3.0-or-later

// Package macos contains the launchd platform contract for the supported
// macOS runtime. The Swift GUI remains a frontend: launchd starts the external
// sing-box binary and this package owns only its deterministic TUN plan.
package macos

import (
	"github.com/gsh20040816/steer/go/internal/compiler"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

const TunMTU = 9000

const DefaultGeoDataDirectory = "/Library/Application Support/Steer/geodata-seed"

var defaultRouteAddress = []string{"0.0.0.0/1", "128.0.0.0/1", "::/1", "8000::/1"}

var privateRouteAddress = []string{
	"10.0.0.0/8", "100.64.0.0/10", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7",
}

var routeExcludeIPv4 = []string{
	"0.0.0.0/8", "127.0.0.0/8", "169.254.0.0/16", "192.0.2.0/24", "192.88.99.0/24",
	"198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
}

var routeExcludeIPv6 = []string{
	"::/128", "::1/128", "::ffff:0:0/96", "64:ff9b:1::/48", "100::/64", "100:0:0:1::/64",
	"2001:db8::/32", "2002::/16", "3fff::/20", "5f00::/16", "fe80::/10", "ff00::/8",
}

type Plan struct {
	SchemaVersion int       `json:"schema_version"`
	Resources     Resources `json:"resources"`
}

type Resources struct {
	TunAddresses []string `json:"tun_addresses"`
}

func NewPlan(_ model.Intent) Plan {
	return Plan{SchemaVersion: 3, Resources: Resources{
		TunAddresses: []string{"198.18.0.1/30", "fdfe:dcba:9876::1/126"},
	}}
}

// CompilerTarget is the supported no-Apple-Developer runtime path. sing-box
// owns the Darwin utun device and auto_route; macOS does not use Linux's
// auto_redirect, nftables, or pf. System DNS is managed by the supervisor;
// extra private routes are unnecessary. Private packets that do enter TUN
// are sent Direct after the explicit port-53 rule.
func (plan Plan) CompilerTarget() compiler.Target {
	routes := append([]string{}, defaultRouteAddress...)
	excluded := append(append([]string{}, routeExcludeIPv4...), routeExcludeIPv6...)
	return compiler.Target{
		Inbounds: []any{
			map[string]any{
				"type": "tun", "tag": "steer-tun", "address": plan.Resources.TunAddresses,
				"mtu": TunMTU, "dns_mode": "hijack", "auto_route": true, "stack": "system",
				"route_address": routes, "route_exclude_address": excluded,
			},
		},
		DNSCapture:           compiler.DNSCapture{Mode: compiler.DNSCaptureTUNPort53Hijack, InboundTags: []string{"steer-tun"}},
		SniffInboundTags:     []string{"steer-tun"},
		DirectRouteAddress:   append([]string{}, privateRouteAddress...),
		RequiredCapabilities: []string{"tun", "auto_route"},
	}
}

func (plan Plan) CompilerOptions(stateDirectory string, geoDataDirectory ...string) compiler.Options {
	seedDirectory := DefaultGeoDataDirectory
	if len(geoDataDirectory) > 0 && geoDataDirectory[0] != "" {
		seedDirectory = geoDataDirectory[0]
	}
	return compiler.Options{StateDirectory: stateDirectory, GeoDataDirectory: seedDirectory, Target: plan.CompilerTarget()}
}
