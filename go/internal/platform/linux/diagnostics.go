// SPDX-License-Identifier: GPL-3.0-or-later

package linux

import (
	"path/filepath"

	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/probe"
)

func ReadDiagnostics(configPath, runDirectory, stateDirectory string) probe.Diagnostics {
	identity, warnings := readProbeIdentity(configPath, runDirectory)
	latest := probe.ReadLatestProbeResults(stateDirectory, identity)
	diagnostics := probe.Diagnostics{Warnings: append(warnings, latest.Warnings...)}
	if identity.ActiveGeneration != "" {
		_, resolved, _, _, err := readCurrentIdentity(BackendOptions{RunDirectory: runDirectory})
		if err == nil {
			diagnostics.DNSCapture = probe.InspectDNSCapture(
				"native_hijack_with_local_shim", identity.ActiveGeneration, filepath.Join(resolved, "sing-box.json"), filepath.Join(resolved, "firewall.nft"),
			)
			return diagnostics
		}
	}
	diagnostics.DNSCapture = probe.InspectDNSCapture("native_hijack_with_local_shim", "", "", "")
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
	if value, _, err := (IntentStore{Path: configPath}).Load(); err == nil {
		identity.SavedDigest = compiler.IntentDigest(value)
	} else {
		warnings = append(warnings, "the Saved configuration identity is unavailable")
	}
	if generationID, _, _, activeIdentity, err := readCurrentIdentity(BackendOptions{RunDirectory: runDirectory}); err == nil {
		identity.ActiveGeneration = generationID
		identity.ActiveDigest = activeIdentity.IntentDigest
	}
	return identity, warnings
}
