// SPDX-License-Identifier: GPL-3.0-or-later
package capability

import "testing"

func TestParseBuildAndTags(t *testing.T) {
	report := Parse("sing-box version 1.14.0-rc.1\n\nEnvironment: go1.26 linux/amd64\nTags: with_quic,with_utls\n", []string{"tun", "dns_quic", "with_utls"})
	if !report.OK || report.Version != "1.14.0-rc.1" {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestParseRejectsMissingTagButDoesNotPinVersion(t *testing.T) {
	report := Parse("sing-box version 1.14.0-beta.1\nTags: with_quic\n", []string{"with_utls"})
	if report.OK || len(report.Errors) != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	for _, version := range []string{"1.13.99", "1.14.0-alpha.50", "1.14.0-beta.1", "1.14.0-rc.1", "1.15.0", "2.0.0"} {
		output := "sing-box version " + version + "\nTags: with_quic,with_utls\n"
		if report := Parse(output, nil); !report.OK || report.Version != version {
			t.Fatalf("version was incorrectly rejected before native config check: %s: %#v", version, report)
		}
	}
}

func TestUserspaceWireGuardRequiresBothBuildTags(t *testing.T) {
	for _, tags := range []string{"with_gvisor", "with_wireguard", "with_wireguard,with_gvisor"} {
		result := Parse("sing-box version 1.14.1\nTags: "+tags+"\n", []string{"with_wireguard", "with_gvisor"})
		if result.OK != (tags == "with_wireguard,with_gvisor") {
			t.Fatalf("unexpected capabilities: %+v", result)
		}
	}
}
