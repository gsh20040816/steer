// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

type fakeScutilRunner struct {
	entry     string
	setCalls  int
	delCalls  int
	denyWrite bool
}

func (r *fakeScutilRunner) OutputWithInput(_ context.Context, input, _ string, _ ...string) ([]byte, error) {
	switch {
	case strings.HasPrefix(input, "show "):
		if r.entry == "" {
			return []byte("  No such key\n"), nil
		}
		return []byte(r.entry), nil
	case strings.HasPrefix(input, "d.init\n"):
		r.setCalls++
		if r.denyWrite {
			return []byte("  Permission denied\n"), nil
		}
		if !strings.Contains(input, "d.add SupplementalMatchDomains * \"\"\n") ||
			!strings.Contains(input, "d.add SupplementalMatchOrders * # 1\n") ||
			!strings.Contains(input, "set "+DynamicStoreDNSKey+"\n") {
			return nil, errors.New("unexpected scutil set script: " + input)
		}
		r.entry = `<dictionary> {
  ServerAddresses : <array> {
    0 : 198.18.0.2
    1 : fdfe:dcba:9876::2
  }
  SupplementalMatchDomains : <array> {
    0 : 
  }
  SupplementalMatchOrders : <array> {
    0 : 1
  }
}
`
		return nil, nil
	case strings.HasPrefix(input, "remove "):
		r.delCalls++
		if r.denyWrite {
			return []byte("  Permission denied\n"), nil
		}
		r.entry = ""
		return nil, nil
	default:
		return nil, errors.New("unexpected scutil input: " + input)
	}
}

func TestDynamicStoreDNSAcquireIdempotentAndClear(t *testing.T) {
	runner := &fakeScutilRunner{}
	settings := DynamicStoreDNSSettings{Runner: runner}
	ctx := context.Background()

	if err := settings.SetGlobal(ctx, SystemDNSServers); err != nil {
		t.Fatal(err)
	}
	if runner.setCalls != 1 {
		t.Fatalf("expected 1 set call, got %d", runner.setCalls)
	}
	// Repeating Acquire must not rewrite SCDynamicStore when already matching.
	if err := settings.SetGlobal(ctx, SystemDNSServers); err != nil {
		t.Fatal(err)
	}
	if runner.setCalls != 1 {
		t.Fatalf("expected idempotent SetGlobal, got %d set calls", runner.setCalls)
	}
	if err := settings.ClearGlobal(ctx); err != nil {
		t.Fatal(err)
	}
	if runner.delCalls != 1 {
		t.Fatalf("expected 1 remove call, got %d", runner.delCalls)
	}
	// Clearing an absent key must be a no-op.
	if err := settings.ClearGlobal(ctx); err != nil {
		t.Fatal(err)
	}
	if runner.delCalls != 1 {
		t.Fatalf("expected idempotent ClearGlobal, got %d remove calls", runner.delCalls)
	}

	runner.denyWrite = true
	if err := settings.SetGlobal(ctx, SystemDNSServers); err == nil {
		t.Fatal("expected error on Permission denied")
	}
}

func TestDNSManagerMigratesLegacyJournalWhenUsingDynamicStore(t *testing.T) {
	stateDir := t.TempDir()
	legacy := &fakeDNSSettings{services: map[string]DNSService{
		"wifi": {Name: "Wi-Fi"},
	}}
	ctx := context.Background()
	// Simulate an older Steer version having mutated preferences.plist and saved dns-restore.json.
	if err := (DNSManager{StateDirectory: stateDir, Settings: legacy}).Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(legacy.services["wifi"].Servers, SystemDNSServers) {
		t.Fatal("legacy setup did not write managed servers")
	}

	scutil := &fakeScutilRunner{}
	manager := DNSManager{
		StateDirectory: stateDir,
		Settings:       legacy,
		Global:         DynamicStoreDNSSettings{Runner: scutil},
	}
	if err := manager.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	if len(legacy.services["wifi"].Servers) != 0 {
		t.Fatalf("expected legacy Wi-Fi DNS to be restored to DHCP, got %v", legacy.services["wifi"].Servers)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "dns-restore.json")); !os.IsNotExist(err) {
		t.Fatal("expected legacy dns-restore.json to be removed after migration")
	}
	if scutil.setCalls != 1 || scutil.entry == "" {
		t.Fatal("expected DynamicStore DNS to be installed")
	}
	if err := manager.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if scutil.entry != "" {
		t.Fatal("expected DynamicStore DNS to be cleared on restore")
	}
}
