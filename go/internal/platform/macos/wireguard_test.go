// SPDX-License-Identifier: GPL-3.0-or-later
package macos

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

func TestWireGuardRefreshKeepsSavedDomainAndLastGoodAddress(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "saved.json")
	original := []byte(`{"endpoints":[{"type":"wireguard","tag":"steer-wg-home","domain_resolver":{"strategy":"ipv6_only"},"private_key":"secret","peers":[{"address":"wg.example.com","port":51820}]}]}`)
	if e := os.WriteFile(config, original, 0600); e != nil {
		t.Fatal(e)
	}
	r, e := prepareWireGuardRuntime(config, root)
	if e != nil {
		t.Fatal(e)
	}
	defer r.close()
	address := "2001:db8::1"
	var lookupError error
	reloads := 0
	lookup := func(ctx context.Context, network, host string) ([]netip.Addr, error) {
		if network != "ip6" || host != "wg.example.com" {
			t.Fatal("lost configured family or host")
		}
		if lookupError != nil {
			return nil, lookupError
		}
		return []netip.Addr{netip.MustParseAddr(address)}, nil
	}
	reload := func() error { reloads++; return nil }
	r.refresh(context.Background(), lookup, reload)
	r.refresh(context.Background(), lookup, reload)
	if reloads != 1 {
		t.Fatal("unchanged DNS reloaded core")
	}
	lookupError = errors.New("temporary error")
	r.refresh(context.Background(), lookup, reload)
	if reloads != 1 || r.peers[0].status.Address != address || r.peers[0].status.Error == "" {
		t.Fatal("lookup failure lost working endpoint")
	}
	lookupError = nil
	address = "2001:db8::2"
	r.refresh(context.Background(), lookup, reload)
	if reloads != 2 || r.peers[0].status.Address != address {
		t.Fatal("DDNS update not applied")
	}
	saved, _ := os.ReadFile(config)
	if string(saved) != string(original) {
		t.Fatal("saved generation mutated")
	}
	status, _ := os.ReadFile(r.statusPath)
	var decoded map[string]any
	if e = json.Unmarshal(status, &decoded); e != nil {
		t.Fatal(e)
	}
	for _, v := range []string{r.path, r.directory} {
		st, e := os.Stat(v)
		if e != nil || st.Mode().Perm()&0077 != 0 {
			t.Fatal("runtime secrets are not private")
		}
	}
	address = "2001:db8::3"
	r.refresh(context.Background(), lookup, func() error { return errors.New("process exited") })
	next, e := prepareWireGuardRuntime(config, root)
	if e != nil {
		t.Fatal(e)
	}
	defer next.close()
	if next.peers[0].status.Address != "2001:db8::2" || next.peers[0].options["address"] != "2001:db8::2" {
		t.Fatal("restart lost cached endpoint")
	}
	if r.peers[0].status.Address != "2001:db8::2" {
		t.Fatal("failed reload advanced applied state")
	}
}
func TestWireGuardDNSOrderingDoesNotFlap(t *testing.T) {
	a := []netip.Addr{netip.MustParseAddr("2001:db8::2"), netip.MustParseAddr("2001:db8::1")}
	if chooseWireGuardAddress(a, "2001:db8::1", "prefer_ipv6") != "2001:db8::1" {
		t.Fatal("flapped")
	}
}
