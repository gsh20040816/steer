// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"path/filepath"

	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/probe"
)

func ReadDiagnostics(configPath string, options BackendOptions) probe.Diagnostics {
	options = normalizeBackendOptions(options)
	identity, warnings := readProbeIdentity(configPath, options)
	latest := probe.ReadLatestProbeResults(options.StateDirectory, identity)
	diagnostics := probe.Diagnostics{Warnings: append(warnings, latest.Warnings...)}
	if identity.ActiveGeneration != "" {
		if current, err := runtimePaths(options.RunDirectory, "").LoadCurrent(); err == nil {
			diagnostics.DNSCapture = probe.InspectDNSCapture(
				"tun_port53_hijack", current.GenerationID,
				filepath.Join(options.RunDirectory, "generations", current.Directory, "sing-box.json"), "",
			)
			return diagnostics
		}
	}
	diagnostics.DNSCapture = probe.InspectDNSCapture("tun_port53_hijack", "", "", "")
	return diagnostics
}

func ReadLatestProbeResults(configPath string, options BackendOptions) probe.LatestProbeResults {
	options = normalizeBackendOptions(options)
	identity, warnings := readProbeIdentity(configPath, options)
	results := probe.ReadLatestProbeResults(options.StateDirectory, identity)
	results.Warnings = append(warnings, results.Warnings...)
	return results
}

func readProbeIdentity(configPath string, options BackendOptions) (probe.Identity, []string) {
	identity := probe.Identity{}
	warnings := []string{}
	if value, _, err := SavedStore(configPath, options.StateDirectory).LoadSaved(); err == nil {
		identity.SavedDigest = compiler.IntentDigest(value)
	} else {
		warnings = append(warnings, "the Saved configuration identity is unavailable")
	}
	if current, err := runtimePaths(options.RunDirectory, "").LoadCurrent(); err == nil {
		identity.ActiveGeneration = current.GenerationID
		identity.ActiveDigest = current.IntentDigest
	}
	return identity, warnings
}

func SaveTestReport(options BackendOptions, report TestReport) error {
	options = normalizeBackendOptions(options)
	return probe.SaveReport(options.StateDirectory, report)
}

func SaveTestFailure(configPath string, options BackendOptions, scope, objectID, kind string, testErr error) error {
	options = normalizeBackendOptions(options)
	identity, _ := readProbeIdentity(configPath, options)
	report := probe.BindReportIdentity(probe.FailureReport(scope, objectID, kind, testErr), identity)
	return probe.SaveReport(options.StateDirectory, report)
}
