// SPDX-License-Identifier: GPL-3.0-or-later
package intent

import "testing"

func TestWireGuardRejectsAmbiguousPeersAndUnsafeAccess(t *testing.T) {
	key := "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	base := WireGuardTunnel{ID: "wg", Enabled: true, PrivateKey: key, Address: []string{"10.20.0.2/32"}, Peers: []WireGuardPeer{{PublicKey: key, Server: "vpn.example", ServerPort: 51820, AllowedIPs: []string{"10.0.0.0/8"}}}}
	for _, tt := range []struct {
		name   string
		change func(*WireGuardTunnel)
	}{
		{"subnet", func(t *WireGuardTunnel) { t.Address = []string{"10.20.0.2/24"} }},
		{"key", func(t *WireGuardTunnel) { t.PrivateKey = "secret" }},
		{"duplicate", func(t *WireGuardTunnel) {
			p := t.Peers[0]
			p.PublicKey = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="
			t.Peers = append(t.Peers, p)
		}},
		{"open source", func(t *WireGuardTunnel) {
			t.Access = []WireGuardAccess{{Network: "tcp", Port: 22, Target: "127.0.0.1", TargetPort: 22}}
		}},
		{"forwarding", func(t *WireGuardTunnel) {
			t.Access = []WireGuardAccess{{SourceIPCIDR: []string{"10.0.0.1/32"}, Network: "tcp", Port: 22, Target: "192.168.1.1", TargetPort: 22}}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := base
			tt.change(&v)
			var count int
			validateWireGuard(Intent{WireGuardTunnels: []WireGuardTunnel{v}}, func(string, string) {}, func(a, b, c, d, e string) { count++ }, func(a, b, c, d, e string) {})
			if count == 0 {
				t.Fatal("accepted invalid tunnel")
			}
		})
	}
}
