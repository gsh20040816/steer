// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// These are sing-box's derived DNS addresses (TUN address + 1). The compiler
// deliberately leaves dns_address unset so its automatic DNS handler is used.
var SystemDNSServers = []string{"198.18.0.2", "fdfe:dcba:9876::2"}

const (
	DynamicStoreDNSKey = "State:/Network/Service/com.steer.dns/DNS"
	scutilBinary       = "/usr/sbin/scutil"
)

type GlobalDNSSettings interface {
	SetGlobal(context.Context, []string) error
	ClearGlobal(context.Context) error
}

// DynamicStoreDNSSettings registers an ephemeral supplemental DNS service in
// macOS's SCDynamicStore with an empty match domain and highest priority.
// Physical interfaces keep their DHCP/manual scoped DNS so captive portal
// detection (captiveagent) and Wi-Fi broken-backhaul probes remain healthy.
type DynamicStoreDNSSettings struct{ Runner InputRunner }

func (s DynamicStoreDNSSettings) SetGlobal(ctx context.Context, servers []string) error {
	current, exists, err := s.read(ctx)
	if err != nil {
		return err
	}
	if exists && dynamicStoreDNSMatches(current, servers) {
		return nil
	}
	var script strings.Builder
	script.WriteString("d.init\n")
	script.WriteString("d.add ServerAddresses * ")
	script.WriteString(strings.Join(servers, " "))
	script.WriteString("\n")
	script.WriteString("d.add SupplementalMatchDomains * \"\"\n")
	script.WriteString("d.add SupplementalMatchOrders * # 1\n")
	script.WriteString("set " + DynamicStoreDNSKey + "\n")
	script.WriteString("quit\n")
	output, err := s.Runner.OutputWithInput(ctx, script.String(), scutilBinary)
	if err != nil {
		return fmt.Errorf("set dynamic store DNS: %w", err)
	}
	if err := checkScutilMutationError(string(output)); err != nil {
		return fmt.Errorf("set dynamic store DNS: %w", err)
	}
	updated, exists, err := s.read(ctx)
	if err != nil {
		return err
	}
	if !exists || !dynamicStoreDNSMatches(updated, servers) {
		return errors.New("dynamic store DNS update did not take effect")
	}
	return nil
}

func (s DynamicStoreDNSSettings) ClearGlobal(ctx context.Context) error {
	_, exists, err := s.read(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	script := "remove " + DynamicStoreDNSKey + "\nquit\n"
	output, err := s.Runner.OutputWithInput(ctx, script, scutilBinary)
	if err != nil {
		return fmt.Errorf("clear dynamic store DNS: %w", err)
	}
	if err := checkScutilMutationError(string(output)); err != nil {
		return fmt.Errorf("clear dynamic store DNS: %w", err)
	}
	_, exists, err = s.read(ctx)
	if err != nil {
		return err
	}
	if exists {
		return errors.New("dynamic store DNS entry remained after removal")
	}
	return nil
}

type dynamicStoreDNSState struct {
	servers []string
	domains []string
	orders  []string
}

func (s DynamicStoreDNSSettings) read(ctx context.Context) (dynamicStoreDNSState, bool, error) {
	script := "show " + DynamicStoreDNSKey + "\nquit\n"
	output, err := s.Runner.OutputWithInput(ctx, script, scutilBinary)
	if err != nil {
		return dynamicStoreDNSState{}, false, fmt.Errorf("read dynamic store DNS: %w", err)
	}
	text := string(output)
	if strings.Contains(text, "No such key") {
		return dynamicStoreDNSState{}, false, nil
	}
	if err := checkScutilMutationError(text); err != nil {
		return dynamicStoreDNSState{}, false, fmt.Errorf("read dynamic store DNS: %w", err)
	}
	if !strings.Contains(text, "<dictionary>") {
		return dynamicStoreDNSState{}, false, fmt.Errorf("unexpected scutil output reading dynamic store DNS: %s", strings.TrimSpace(text))
	}
	return parseDynamicStoreDNS(text), true, nil
}

func checkScutilMutationError(output string) error {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "No such key" {
			continue
		}
		if strings.Contains(trimmed, "Permission denied") ||
			strings.Contains(trimmed, "error") ||
			strings.Contains(trimmed, "Error") ||
			strings.Contains(trimmed, "Failed") {
			return errors.New(trimmed)
		}
	}
	return nil
}

func parseDynamicStoreDNS(output string) dynamicStoreDNSState {
	var state dynamicStoreDNSState
	section := ""
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "}" {
			section = ""
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "ServerAddresses : <array>"):
			section = "servers"
		case strings.HasPrefix(trimmed, "SupplementalMatchDomains : <array>"):
			section = "domains"
		case strings.HasPrefix(trimmed, "SupplementalMatchOrders : <array>"):
			section = "orders"
		case section != "" && strings.Contains(trimmed, " : "):
			_, value, _ := strings.Cut(trimmed, " : ")
			value = strings.TrimSpace(value)
			switch section {
			case "servers":
				state.servers = append(state.servers, value)
			case "domains":
				state.domains = append(state.domains, value)
			case "orders":
				state.orders = append(state.orders, value)
			}
		case section != "" && strings.HasSuffix(trimmed, " :"):
			if section == "domains" {
				state.domains = append(state.domains, "")
			}
		}
	}
	return state
}

func dynamicStoreDNSMatches(state dynamicStoreDNSState, servers []string) bool {
	return slices.Equal(state.servers, servers) &&
		slices.Equal(state.domains, []string{""}) &&
		slices.Equal(state.orders, []string{"1"})
}

type DNSService struct {
	Name     string
	Servers  []string
	Disabled bool
}

type DNSSettings interface {
	Services(context.Context) (map[string]DNSService, error)
	SetServers(context.Context, string, DNSService, []string) error
}

// NetworkDNSSettings uses service UUIDs for identity, and networksetup only for
// changing ServerAddresses. Search domains and VPN/supplemental DNS stay intact.
type NetworkDNSSettings struct{ Runner Runner }

func (s NetworkDNSSettings) Services(ctx context.Context) (map[string]DNSService, error) {
	data, err := s.Runner.Output(ctx, "/usr/bin/plutil", "-convert", "xml1", "-o", "-", "/Library/Preferences/SystemConfiguration/preferences.plist")
	if err != nil {
		return nil, fmt.Errorf("read network service preferences: %w", err)
	}
	// Real preferences contain CFData/CFDate (for example interface metadata),
	// which plutil cannot convert to JSON. Decode the XML plist instead.
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	var root any
	for {
		token, tokenErr := decoder.Token()
		if tokenErr != nil {
			return nil, tokenErr
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "dict" {
			root, err = readPlistValue(decoder, start)
			break
		}
	}
	if err != nil {
		return nil, err
	}
	data, err = json.Marshal(root)
	if err != nil {
		return nil, err
	}
	var prefs struct {
		NetworkServices map[string]struct {
			Name      string `json:"UserDefinedName"`
			Inactive  any    `json:"__INACTIVE__"`
			Interface struct{ Type string }
			DNS       struct{ ServerAddresses []string }
		}
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return nil, err
	}
	result := make(map[string]DNSService)
	for id, service := range prefs.NetworkServices {
		if service.Interface.Type != "Ethernet" && service.Interface.Type != "IEEE80211" {
			continue
		}
		if service.Name == "" {
			continue
		}
		result[id] = DNSService{Name: service.Name, Servers: service.DNS.ServerAddresses, Disabled: service.Inactive == float64(1) || service.Inactive == true}
	}
	return result, nil
}

func readPlistValue(d *xml.Decoder, start xml.StartElement) (any, error) {
	if start.Name.Local != "dict" && start.Name.Local != "array" {
		var text string
		if err := d.DecodeElement(&text, &start); err != nil {
			return nil, err
		}
		switch start.Name.Local {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "integer":
			return strconv.ParseInt(text, 10, 64)
		default:
			return text, nil
		}
	}
	dict := map[string]any{}
	array := []any{}
	key := ""
	for {
		token, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch token := token.(type) {
		case xml.StartElement:
			if token.Name.Local == "key" {
				if err := d.DecodeElement(&key, &token); err != nil {
					return nil, err
				}
				continue
			}
			value, err := readPlistValue(d, token)
			if err != nil {
				return nil, err
			}
			if start.Name.Local == "dict" {
				dict[key] = value
			} else {
				array = append(array, value)
			}
		case xml.EndElement:
			if start.Name.Local == "dict" {
				return dict, nil
			}
			return array, nil
		}
	}
}

func (s NetworkDNSSettings) SetServers(ctx context.Context, id string, expected DNSService, servers []string) error {
	current, err := s.Services(ctx)
	if err != nil {
		return err
	}
	service, ok := current[id]
	if !ok || service.Name != expected.Name || !slices.Equal(service.Servers, expected.Servers) {
		return fmt.Errorf("network service %s changed during DNS update", id)
	}
	// networksetup addresses services by name: refuse ambiguous names.
	for otherID, other := range current {
		if otherID != id && other.Name == service.Name {
			return fmt.Errorf("ambiguous network service name for %s", id)
		}
	}
	args := append([]string{"-setdnsservers", service.Name}, servers...)
	if len(servers) == 0 {
		args = append(args, "Empty")
	}
	if _, err := s.Runner.Output(ctx, "/usr/sbin/networksetup", args...); err != nil {
		return fmt.Errorf("set DNS for service %s: %w", id, err)
	}
	updated, err := s.Services(ctx)
	if err != nil {
		return err
	}
	actual, ok := updated[id]
	if !ok || !slices.Equal(actual.Servers, servers) {
		return fmt.Errorf("DNS update for service %s did not take effect", id)
	}
	return nil
}

type dnsLease struct {
	Original []string `json:"original"`
	Managed  []string `json:"managed"`
	Released bool     `json:"released,omitempty"`
}

// DNSManager owns macOS system DNS takeover. When Global is configured, it
// manages an ephemeral SCDynamicStore supplemental DNS entry and automatically
// migrates/restores any legacy preferences.plist lease journal left by older
// versions. When Global is nil, it falls back to legacy per-service takeover.
type DNSManager struct {
	StateDirectory string
	Settings       DNSSettings
	Global         GlobalDNSSettings
}

func (m DNSManager) withLock(ctx context.Context, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := os.MkdirAll(m.StateDirectory, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(m.StateDirectory, "dns.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	for {
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return fn(ctx)
}

func (m DNSManager) updateLocked(ctx context.Context, restore bool) error {
	if m.Settings == nil {
		return nil
	}
	path := filepath.Join(m.StateDirectory, "dns-restore.json")
	leases := map[string]dnsLease{}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &leases); err != nil {
			return fmt.Errorf("read DNS recovery journal: %w", err)
		}
		if leases == nil {
			return errors.New("invalid DNS recovery journal")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if restore && len(leases) == 0 {
		return nil
	}
	services, err := m.Settings.Services(ctx)
	if err != nil {
		return err
	}
	save := func() error {
		data, err := json.Marshal(leases)
		if err != nil {
			return err
		}
		return atomicWrite(path, data)
	}
	ids := make([]string, 0, len(services)+len(leases))
	if restore {
		for id := range leases {
			ids = append(ids, id)
		}
	} else {
		for id := range services {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var failures []error
	for _, id := range ids {
		service, exists := services[id]
		lease, owned := leases[id]
		if restore {
			if exists && !lease.Released && slices.Equal(service.Servers, lease.Managed) {
				if err := m.Settings.SetServers(ctx, id, service, lease.Original); err != nil {
					failures = append(failures, err)
					continue
				}
			}
			delete(leases, id)
			if err := save(); err != nil {
				return err
			}
			continue
		}
		if service.Disabled {
			continue
		}
		if owned {
			if lease.Released {
				continue
			}
			if !slices.Equal(service.Servers, lease.Managed) {
				// Do not fight another VPN, administrator or user, including on
				// later network polls. Relinquish ownership until the next start.
				lease.Released = true
				leases[id] = lease
				if err := save(); err != nil {
					return err
				}
			}
			continue
		}
		leases[id] = dnsLease{Original: service.Servers, Managed: slices.Clone(SystemDNSServers)}
		if err := save(); err != nil {
			return err
		}
		if err := m.Settings.SetServers(ctx, id, service, SystemDNSServers); err != nil {
			return err
		}
	}
	if restore && len(leases) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return errors.Join(failures...)
}

func (m DNSManager) Acquire(ctx context.Context) error {
	return m.withLock(ctx, func(ctx context.Context) error {
		if m.Global == nil {
			return m.updateLocked(ctx, false)
		}
		var errs []error
		if err := m.updateLocked(ctx, true); err != nil {
			errs = append(errs, err)
		}
		if err := m.Global.SetGlobal(ctx, SystemDNSServers); err != nil {
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	})
}

func (m DNSManager) Restore(ctx context.Context) error {
	return m.withLock(ctx, func(ctx context.Context) error {
		if m.Global == nil {
			return m.updateLocked(ctx, true)
		}
		var errs []error
		if err := m.Global.ClearGlobal(ctx); err != nil {
			errs = append(errs, err)
		}
		if err := m.updateLocked(ctx, true); err != nil {
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	})
}

func (backend *Backend) DNSManager() DNSManager {
	runner := backend.runner
	if commandRunner, ok := runner.(ExecRunner); ok {
		commandRunner.JoinProcessGroup = true
		runner = commandRunner
	}
	manager := DNSManager{
		StateDirectory: backend.options.StateDirectory,
		Settings:       NetworkDNSSettings{Runner: runner},
	}
	if inputRunner, ok := runner.(InputRunner); ok {
		manager.Global = DynamicStoreDNSSettings{Runner: inputRunner}
	}
	return manager
}

// RecoverDNSIfStopped is also called by the independent control daemon. It
// covers a SIGKILL of the runtime supervisor and recovery after reboot.
func (backend *Backend) RecoverDNSIfStopped(ctx context.Context) error {
	output, err := backend.runner.Output(ctx, backend.options.LaunchctlBinary, "print", "system/"+backend.options.LaunchDaemonLabel)
	if err == nil && launchdOutputIsRunning(string(output)) {
		return nil
	}
	return backend.DNSManager().Restore(ctx)
}
