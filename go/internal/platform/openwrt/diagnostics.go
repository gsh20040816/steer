// SPDX-License-Identifier: GPL-3.0-or-later

package openwrt

import (
	"os"
	"path/filepath"

	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/generation"
	"github.com/gsh20040816/steer/go/internal/probe"
)

func ReadDiagnostics(configPath, runDirectory, stateDirectory string) probe.Diagnostics {
	identity, warnings := readProbeIdentity(configPath, runDirectory)
	latest := probe.ReadLatestProbeResults(stateDirectory, identity)
	diagnostics := probe.Diagnostics{Warnings: append(warnings, latest.Warnings...)}
	if runDirectory == "" {
		runDirectory = "/run/steer"
	}
	currentPath := filepath.Join(runDirectory, "current")
	diagnostics.DNSCapture = probe.InspectDNSCapture(
		"native_hijack_with_local_shim", identity.ActiveGeneration,
		filepath.Join(currentPath, "sing-box.json"), filepath.Join(currentPath, "firewall.nft"),
	)
	return diagnostics
}

func ReadLatestProbeResults(configPath, runDirectory, stateDirectory string) probe.LatestProbeResults {
	identity, warnings := readProbeIdentity(configPath, runDirectory)
	results := probe.ReadLatestProbeResults(stateDirectory, identity)
	results.Warnings = append(warnings, results.Warnings...)
	return results
}

func readProbeIdentity(configPath, runDirectory string) (probe.Identity, []string) {
	identity := probe.Identity{}
	warnings := []string{}
	if config, err := os.ReadFile(configPath); err == nil {
		if value, decodeErr := DecodeBytes(config); decodeErr == nil {
			identity.SavedDigest = compiler.IntentDigest(value)
		} else {
			warnings = append(warnings, "the Saved configuration identity is unavailable")
		}
	} else {
		warnings = append(warnings, "the Saved configuration identity is unavailable")
	}
	if runDirectory == "" {
		runDirectory = "/run/steer"
	}
	currentPath := filepath.Join(runDirectory, "current")
	if value, err := generation.ReadIntent(currentPath); err == nil {
		identity.ActiveDigest = compiler.IntentDigest(value)
		identity.ActiveGeneration = currentGenerationID(currentPath)
	}
	return identity, warnings
}

func currentGenerationID(currentPath string) string {
	info, err := os.Lstat(currentPath)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(currentPath)
	if err != nil {
		return ""
	}
	return filepath.Base(resolved)
}
