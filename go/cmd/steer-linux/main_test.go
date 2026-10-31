// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestGeoCatalogLoadsPackageManifest(t *testing.T) {
	root := t.TempDir()
	seedDirectory := writeWebSeed(t, root)
	if err := runGeoCatalog([]string{"-kind", "geosite", "-seed-dir", seedDirectory}); err != nil {
		t.Fatalf("geo-catalog did not use the package manifest: %v", err)
	}
}
