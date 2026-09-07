// SPDX-License-Identifier: GPL-3.0-or-later

package linux

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadDiagnosticsInspectsCurrentGenerationDNSCaptureArtifacts(t *testing.T) {
	root := t.TempDir()
	runDirectory := filepath.Join(root, "run")
	configPath := filepath.Join(root, "config.json")
	value := validIntent()
	value.Main.Enabled = true
	activeDirectory := prepareCurrentStatusGeneration(t, runDirectory, value)
	if err := os.WriteFile(filepath.Join(activeDirectory, "firewall.nft"), []byte(RenderFirewall(NewPlan(value))), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (IntentStore{Path: configPath}).Save(value, ""); err != nil {
		t.Fatal(err)
	}

	diagnostics := ReadDiagnostics(configPath, runDirectory, filepath.Join(root, "state"))
	if !diagnostics.DNSCapture.Configured || diagnostics.DNSCapture.ActiveGeneration != filepath.Base(activeDirectory) ||
		diagnostics.DNSCapture.Mode != "native_hijack_with_local_shim" {
		t.Fatalf("current Linux DNS capture artifacts were not diagnosed: %#v", diagnostics.DNSCapture)
	}
}
