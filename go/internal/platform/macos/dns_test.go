// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"testing"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

type fakeDNSSettings struct {
	services    map[string]DNSService
	failID      string
	beforeWrite func()
}

func (s *fakeDNSSettings) Services(context.Context) (map[string]DNSService, error) {
	return s.services, nil
}
func (s *fakeDNSSettings) SetServers(_ context.Context, id string, service DNSService, servers []string) error {
	if s.beforeWrite != nil {
		s.beforeWrite()
	}
	if id == s.failID {
		return errors.New("injected write failure")
	}
	service.Servers = slices.Clone(servers)
	s.services[id] = service
	return nil
}

func TestDNSLeaseRestoresAutomaticAndManualByUUID(t *testing.T) {
	settings := &fakeDNSSettings{services: map[string]DNSService{
		"wifi": {Name: "Wi-Fi"}, "ethernet": {Name: "Ethernet", Servers: []string{"1.1.1.1", "2606:4700:4700::1111"}},
	}}
	m := DNSManager{StateDirectory: t.TempDir(), Settings: settings}
	settings.beforeWrite = func() {
		if _, err := os.Stat(filepath.Join(m.StateDirectory, "dns-restore.json")); err != nil {
			t.Fatal("DNS changed before journal was saved")
		}
	}
	ctx := context.Background()
	if err := m.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	service := settings.services["wifi"]
	service.Name = "Renamed Wi-Fi"
	settings.services["wifi"] = service
	settings.services["new"] = DNSService{Name: "USB Ethernet"}
	if err := m.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	// Recovery uses the durable journal, not the original manager instance.
	if err := (DNSManager{StateDirectory: m.StateDirectory, Settings: settings}).Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if len(settings.services["wifi"].Servers) != 0 || len(settings.services["new"].Servers) != 0 {
		t.Fatal("automatic DNS was not restored")
	}
	if !slices.Equal(settings.services["ethernet"].Servers, []string{"1.1.1.1", "2606:4700:4700::1111"}) {
		t.Fatal("manual DNS was not restored")
	}
}

func TestDNSLeaseRelinquishesUserChangesAndSkipsDisabled(t *testing.T) {
	settings := &fakeDNSSettings{services: map[string]DNSService{"wifi": {Name: "Wi-Fi"}, "off": {Name: "Disabled", Disabled: true}}}
	m := DNSManager{StateDirectory: t.TempDir(), Settings: settings}
	ctx := context.Background()
	if err := m.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	s := settings.services["wifi"]
	s.Servers = []string{"9.9.9.9"}
	settings.services["wifi"] = s
	for range 2 {
		if err := m.Acquire(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(settings.services["wifi"].Servers, []string{"9.9.9.9"}) || len(settings.services["off"].Servers) != 0 {
		t.Fatal("overwrote unowned DNS")
	}
}

func TestDNSPartialFailureAndFailedRestoreRemainRecoverable(t *testing.T) {
	settings := &fakeDNSSettings{services: map[string]DNSService{"a": {Name: "A"}, "b": {Name: "B"}}, failID: "b"}
	m := DNSManager{StateDirectory: t.TempDir(), Settings: settings}
	ctx := context.Background()
	if err := m.Acquire(ctx); err == nil {
		t.Fatal("expected partial acquisition error")
	}
	settings.failID = "a"
	if err := m.Restore(ctx); err == nil {
		t.Fatal("expected restore error")
	}
	if _, err := os.Stat(filepath.Join(m.StateDirectory, "dns-restore.json")); err != nil {
		t.Fatal("lost recovery journal")
	}
	settings.failID = ""
	if err := m.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if len(settings.services["a"].Servers) != 0 {
		t.Fatal("partial takeover was not undone")
	}
}

type dnsReadRunner struct{ data []byte }

func (r dnsReadRunner) Output(context.Context, string, ...string) ([]byte, error) { return r.data, nil }

func TestDNSPreferencesHandlesDataAndExcludesVPN(t *testing.T) {
	runner := dnsReadRunner{[]byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>NetworkServices</key><dict>
<key>wifi</key><dict><key>UserDefinedName</key><string>Wi-Fi</string><key>Interface</key><dict><key>Type</key><string>IEEE80211</string><key>Data</key><data>AA==</data></dict><key>DNS</key><dict><key>ServerAddresses</key><array><string>1.1.1.1</string></array></dict></dict>
<key>vpn</key><dict><key>UserDefinedName</key><string>VPN</string><key>Interface</key><dict><key>Type</key><string>PPP</string></dict></dict>
<key>off</key><dict><key>UserDefinedName</key><string>Ethernet</string><key>__INACTIVE__</key><integer>1</integer><key>Interface</key><dict><key>Type</key><string>Ethernet</string></dict></dict>
</dict></dict></plist>`)}
	services, err := (NetworkDNSSettings{Runner: runner}).Services(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 2 || !services["off"].Disabled || !slices.Equal(services["wifi"].Servers, []string{"1.1.1.1"}) {
		t.Fatalf("unexpected services: %#v", services)
	}
}

func TestReadSystemDNSPreferences(t *testing.T) {
	if os.Getenv("STEER_TEST_SYSTEM_DNS_READ") != "1" {
		t.Skip("opt-in read-only macOS integration check")
	}
	services, err := (NetworkDNSSettings{Runner: ExecRunner{}}).Services(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(services) == 0 {
		t.Fatal("no physical network services found")
	}
	t.Logf("read %d physical network services without changing DNS", len(services))
}

func TestSystemDNSAddressesFollowNativeDerivation(t *testing.T) {
	plan := NewPlan(model.Intent{})
	for i, prefix := range plan.Resources.TunAddresses {
		if netip.MustParsePrefix(prefix).Addr().Next().String() != SystemDNSServers[i] {
			t.Fatal("system DNS address diverged from sing-box's derived address")
		}
	}
}

func TestDefaultDNSVerificationRejectsOnlyScopedMatch(t *testing.T) {
	for _, good := range []bool{false, true} {
		output := "DNS configuration\nresolver #1\n  nameserver[0] : 1.1.1.1\nresolver #2\n  nameserver[0] : 198.18.0.2\n  nameserver[1] : fdfe:dcba:9876::2\n"
		if good {
			output = "DNS configuration\nresolver #1\n  nameserver[0] : 198.18.0.2\n  nameserver[1] : fdfe:dcba:9876::2\nresolver #2\n"
		}
		backend := NewBackend(dnsReadRunner{[]byte(output)}, model.Intent{}, BackendOptions{})
		if (backend.checkSystemDNS(context.Background()) == nil) != good {
			t.Fatalf("incorrect default DNS check for good=%v", good)
		}
	}
}
