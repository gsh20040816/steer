// SPDX-License-Identifier: GPL-3.0-or-later
package wireguard

import (
	"strings"
	"testing"
)

const fixture = `[Interface]
PrivateKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=
Address = 10.77.0.2/24, fd77::2/64
DNS = 10.77.0.1
[Peer]
PublicKey = AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=
Endpoint = [2001:db8::1]:51820
AllowedIPs = 10.0.0.0/8, fd00::/8
PersistentKeepalive = 25
`

func TestImportPortableConfig(t *testing.T) {
	v, e := Parse(strings.NewReader(fixture))
	if e != nil {
		t.Fatal(e)
	}
	if v.Tunnel.Address[0] != "10.77.0.2/32" || v.Tunnel.Peers[0].Server != "2001:db8::1" || len(v.Warnings) != 3 {
		t.Fatalf("%+v", v)
	}
}
func TestRejectHooksAndInvalidKeysWithoutLeaking(t *testing.T) {
	for _, f := range []string{fixture + "PostUp = secret-command\n", strings.Replace(fixture, "PrivateKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", "PrivateKey = secret-key", 1), fixture + "Endpoint = bad:port\n"} {
		_, e := Parse(strings.NewReader(f))
		if e == nil {
			t.Fatal("accepted bad config")
		}
		if strings.Contains(e.Error(), "secret") {
			t.Fatal("leaked secret")
		}
	}
}
