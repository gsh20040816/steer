// SPDX-License-Identifier: GPL-3.0-or-later

package platform_test

import (
	"encoding/json"
	"github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/platform/linux"
	"github.com/gsh20040816/steer/go/internal/platform/macos"
	"github.com/gsh20040816/steer/go/internal/platform/openwrt"
	"os"
	"path/filepath"
	"testing"
)

func TestExportNativeDNSFixtures(t *testing.T) {
	root := os.Getenv("STEER_DNS_FIXTURE_DIR")
	if root == "" {
		t.Skip("set STEER_DNS_FIXTURE_DIR to export isolated integration fixtures")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"linux", "openwrt", "macos"} {
		p := linux.NewPlan(intent.Intent{})
		target := p.CompilerTarget()
		firewall := linux.RenderFirewall(p)
		if platform == "openwrt" {
			p := openwrt.NewPlan(intent.Intent{})
			target = p.CompilerTarget()
			firewall, _ = openwrt.RenderFirewall(p)
		}
		if platform == "macos" {
			target = macos.NewPlan(intent.Intent{}).CompilerTarget()
			firewall = ""
		}
		dnsRule := map[string]any{"inbound": target.DNSInboundTags, "action": "hijack-dns"}
		if platform == "macos" {
			dnsRule = map[string]any{"inbound": []string{"steer-tun"}, "network": []string{"tcp", "udp"}, "port": []int{53}, "action": "hijack-dns"}
		}
		doc := map[string]any{"log": map[string]any{"level": "debug"}, "inbounds": target.Inbounds,
			"dns":       map[string]any{"servers": []any{map[string]any{"type": "hosts", "tag": "test", "predefined": map[string]any{"steer.test": []string{"203.0.113.7"}}}}},
			"outbounds": []any{map[string]any{"type": "direct", "tag": "direct", "bind_interface": "lo"}},
			"route": map[string]any{"auto_detect_interface": true, "rules": []any{
				dnsRule,
				map[string]any{"port": 19000, "action": "route", "outbound": "direct", "override_address": "127.0.0.1", "override_port": 19000},
			}, "final": "direct"},
		}
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, platform+".json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, platform+".nft"), []byte(firewall), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
