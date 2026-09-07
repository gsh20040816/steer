// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

type dnsRouteRunner struct{ device, ifconfig string }

func (r dnsRouteRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	if strings.HasSuffix(name, "ifconfig") {
		return []byte(r.ifconfig), nil
	}
	return []byte("  interface: " + r.device + "\n"), nil
}

func TestDNSReadinessRequiresOwnedLocalTUNRoute(t *testing.T) {
	for _, test := range []struct {
		name, device, ifconfig string
		good                   bool
	}{
		{"ready", "utun8", "utun8: flags=8051\n\tinet 198.18.0.1 --> 198.18.0.1 netmask 0xfffffffc\n\tinet6 fdfe:dcba:9876::1 prefixlen 126\n", true},
		{"router-can-answer-DNS", "en0", "en0: flags=8863\n\tinet 10.0.0.2 netmask 0xffffff00\n", false},
		{"unrelated-VPN", "utun7", "utun7: flags=8051\n\tinet 10.0.0.2 netmask 0xffffff00\nutun8: flags=8051\n\tinet 198.18.0.1 netmask 0xfffffffc\n\tinet6 fdfe:dcba:9876::1 prefixlen 126\n", false},
		{"IPv6-not-ready", "utun8", "utun8: flags=8051\n\tinet 198.18.0.1 netmask 0xfffffffc\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := NewBackend(dnsRouteRunner{test.device, test.ifconfig}, model.Intent{}, BackendOptions{})
			if (backend.checkDNSRouting(context.Background()) == nil) != test.good {
				t.Fatal("incorrect DNS route readiness")
			}
		})
	}
}

func TestSupervisorRestoresDNSOnCoreExitAndCancellation(t *testing.T) {
	for _, stop := range []string{"core-exit", "cancel"} {
		t.Run(stop, func(t *testing.T) {
			root := t.TempDir()
			binary := filepath.Join(root, "core")
			// Wait for a test-controlled file to simulate an unexpected core exit.
			script := "#!/bin/sh\nwhile [ ! -f '" + root + "/stop' ]; do sleep 0.05; done\nexit 7\n"
			if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			settings := &fakeDNSSettings{services: map[string]DNSService{"wifi": {Name: "Wi-Fi"}}}
			manager := DNSManager{StateDirectory: filepath.Join(root, "state"), Settings: settings}
			backend := NewBackend(nil, model.Intent{}, BackendOptions{RunDirectory: root, StateDirectory: manager.StateDirectory, SingBoxBinary: binary})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			systemChecked := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- backend.runSupervised(ctx, filepath.Join(root, "sing-box.json"), manager,
					func(context.Context) error { return nil }, func(context.Context) error { close(systemChecked); return nil })
			}()
			select {
			case <-systemChecked:
			case <-ctx.Done():
				t.Fatal("DNS takeover did not start")
			}
			if stop == "core-exit" {
				if err := os.WriteFile(filepath.Join(root, "stop"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if stop == "core-exit" && err == nil {
					t.Fatal("lost core exit failure")
				}
			case <-time.After(6 * time.Second):
				t.Fatal("supervisor did not stop")
			}
			if len(settings.services["wifi"].Servers) != 0 {
				t.Fatal("DNS was not restored")
			}
			if _, err := os.Stat(filepath.Join(root, "dns-ready.json")); !os.IsNotExist(err) {
				t.Fatal("stale healthy marker")
			}
		})
	}
}
