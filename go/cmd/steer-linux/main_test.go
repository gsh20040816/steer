// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	linuxplatform "github.com/gsh20040816/steer/go/internal/platform/linux"
)

func TestUsageUsesInstalledProductName(t *testing.T) {
	message := usage().Error()
	if !strings.HasPrefix(message, "usage: steer ") || strings.Contains(message, "steer-linux") {
		t.Fatalf("Linux usage exposes the source target name: %s", message)
	}
	if err := runSubscription(nil); err == nil || !strings.HasPrefix(err.Error(), "usage: steer subscription ") {
		t.Fatalf("subscription usage does not use installed product name: %v", err)
	}
}

func TestRunServiceDisabledConfigurationExitsCleanly(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	if _, err := linuxplatform.SavedStore(configPath, filepath.Join(filepath.Dir(configPath), "state")).Save(webTestIntent(), ""); err != nil {
		t.Fatal(err)
	}
	nftPath := filepath.Join(root, "nft")
	if err := os.WriteFile(nftPath, []byte("#!/bin/sh\nprintf '%s' '{\"nftables\":[]}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := runService([]string{
		"-config", configPath,
		"-run-dir", filepath.Join(root, "run"),
		"-state-dir", filepath.Join(root, "state"),
		"-nft", nftPath,
	}); err != nil {
		t.Fatalf("disabled service entered a failure path: %v", err)
	}
}

func TestRunServiceLoadsFirewallOncePerStart(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	value := webTestIntent()
	value.Main.Enabled = true
	if _, err := linuxplatform.SavedStore(configPath, filepath.Join(root, "state")).Save(value, ""); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "nft.log")
	nftPath := filepath.Join(root, "nft")
	nftScript := "#!/bin/sh\necho \"$*\" >> '" + logPath + "'\n[ \"$1\" = -j ] && printf '%s' '{\"nftables\":[]}'\nexit 0\n"
	if err := os.WriteFile(nftPath, []byte(nftScript), 0o700); err != nil {
		t.Fatal(err)
	}
	// sing-box removes itself after the configuration check, so the final
	// exec fails and returns here instead of replacing the test process.
	singBoxPath := filepath.Join(root, "sing-box")
	singBoxScript := "#!/bin/sh\ncase \"$1\" in\nversion) printf 'sing-box version 1.14.0-rc.1\\nTags: with_quic,with_utls\\n' ;;\ncheck) rm -f \"$0\" ;;\nesac\n"
	if err := os.WriteFile(singBoxPath, []byte(singBoxScript), 0o700); err != nil {
		t.Fatal(err)
	}
	args := []string{
		"-config", configPath,
		"-run-dir", filepath.Join(root, "run"),
		"-state-dir", filepath.Join(root, "state"),
		"-nft", nftPath,
		"-sing-box", singBoxPath,
	}
	loads := func() int {
		log, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, line := range strings.Split(string(log), "\n") {
			if strings.HasPrefix(line, "-f ") {
				count++
			}
		}
		return count
	}

	if err := runService(args); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("cold start did not reach the sing-box exec: %v", err)
	}
	if count := loads(); count != 1 {
		t.Fatalf("cold start loaded the nftables shim %d times", count)
	}
	if err := runService(args); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("restart did not reach the sing-box exec: %v", err)
	}
	if count := loads(); count != 2 {
		t.Fatalf("restart of the current generation did not reload the nftables shim once: %d loads in total", count)
	}
}

func TestGeoCatalogLoadsPackageManifest(t *testing.T) {
	root := t.TempDir()
	seedDirectory := writeWebSeed(t, root)
	if err := runGeoCatalog([]string{"-kind", "geosite", "-seed-dir", seedDirectory}); err != nil {
		t.Fatalf("geo-catalog did not use the package manifest: %v", err)
	}
}
