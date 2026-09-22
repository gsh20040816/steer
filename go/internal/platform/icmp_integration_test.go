// SPDX-License-Identifier: GPL-3.0-or-later
package platform_test

import (
	"encoding/json"
	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/platform/linux"
	"github.com/gsh20040816/steer/go/internal/platform/macos"
	"github.com/gsh20040816/steer/go/internal/platform/openwrt"
	"os"
	"path/filepath"
	"testing"
)

func TestExportICMPFixtures(t *testing.T) {
	root := os.Getenv("STEER_ICMP_FIXTURE_DIR")
	if root == "" {
		t.Skip("set STEER_ICMP_FIXTURE_DIR for native ICMP integration")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, wg := range []bool{false, true} {
		value := intent.Intent{
			Main:         intent.Main{LogLevel: "debug"},
			Bootstrap:    intent.Bootstrap{Protocol: "udp", Server: "1.1.1.1", ServerPort: 53},
			Routes:       []intent.Route{{ID: "direct", Enabled: true, Kind: "direct"}, {ID: "proxy", Enabled: true, Kind: "single", Node: "proxy"}},
			Nodes:        []intent.Node{{ID: "proxy", Enabled: true, Type: "socks", Server: "192.168.77.2", ServerPort: 19080}},
			DNSProfiles:  []intent.DNSProfile{{ID: "dns", Enabled: true, Protocol: "udp", Server: "1.1.1.1", ServerPort: 53}},
			LocalProxies: []intent.LocalProxy{{ID: "local", Enabled: true, Protocol: "http", Listen: "127.0.0.1", ListenPort: 19090}},
			Rules:        []intent.Rule{{ID: "default", Enabled: true, Default: true, Route: "proxy", DNSProfile: "dns"}},
		}
		suffix := "plain"
		if wg {
			suffix = "wireguard"
			value.WireGuardTunnels = []intent.WireGuardTunnel{{ID: "test", Enabled: true, Address: []string{"10.90.0.1/32"}, PrivateKey: "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", Peers: []intent.WireGuardPeer{{Server: "192.168.77.2", ServerPort: 51820, PublicKey: "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=", AllowedIPs: []string{"10.90.0.0/24"}}}}}
		}
		for _, platform := range []string{"linux", "openwrt", "macos"} {
			target := linux.NewPlan(value).CompilerTarget()
			if platform == "openwrt" {
				target = openwrt.NewPlan(value).CompilerTarget()
			}
			if platform == "macos" {
				target = macos.NewPlan(value).CompilerTarget()
			}
			doc := compiler.Compile(value, compiler.Options{Target: target, StateDirectory: "/run/steer-icmp"}).SingBox
			data, err := json.MarshalIndent(doc, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(root, platform+"-"+suffix+".json"), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}
