// SPDX-License-Identifier: GPL-3.0-or-later

package validation_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gsh20040816/steer/go/internal/geodata"
	model "github.com/gsh20040816/steer/go/internal/intent"
	linuxplatform "github.com/gsh20040816/steer/go/internal/platform/linux"
	macosplatform "github.com/gsh20040816/steer/go/internal/platform/macos"
	openwrtplatform "github.com/gsh20040816/steer/go/internal/platform/openwrt"
)

func TestGeoCategoryValidationIsConsistentAcrossPlatforms(t *testing.T) {
	seedDirectory := writeManifest(t)
	validators := platformValidators(seedDirectory)

	t.Run("unknown selector", func(t *testing.T) {
		value := platformIntent()
		value.Rules = append([]model.Rule{{
			ID: "unknown", Enabled: true, DNSProfile: "dns", Route: "direct",
			DomainMatch: []string{"geosite:not-installed"},
		}}, value.Rules...)
		for name, validate := range validators {
			t.Run(name, func(t *testing.T) {
				validation := validate(value)
				issue, ok := findIssue(validation, geodata.ErrorCategoryNotFound)
				if validation.OK || !ok {
					t.Fatalf("unknown selector was accepted: %#v", validation.Errors)
				}
				if issue.ObjectType != "rule" || issue.ObjectID != "unknown" || issue.Option != "domain_match" {
					t.Fatalf("unknown selector issue lost its object location: %#v", issue)
				}
			})
		}
	})

	t.Run("installed selectors and attribute", func(t *testing.T) {
		value := platformIntent()
		value.Rules = append([]model.Rule{{
			ID: "installed", Enabled: true, DNSProfile: "dns", Route: "direct",
			DomainMatch: []string{"geosite:cn@ads"}, IPMatch: []string{"geoip:cn"},
		}}, value.Rules...)
		for name, validate := range validators {
			t.Run(name, func(t *testing.T) {
				if validation := validate(value); !validation.OK {
					t.Fatalf("installed selectors were rejected: %#v", validation.Errors)
				}
			})
		}
	})
}

func TestUnreadableManifestHasAStableDistinctIssueAcrossPlatforms(t *testing.T) {
	missingDirectory := filepath.Join(t.TempDir(), "missing")
	value := platformIntent()
	value.Rules = append([]model.Rule{{
		ID: "geo", Enabled: true, DNSProfile: "dns", Route: "direct",
		IPMatch: []string{"geoip:cn"},
	}}, value.Rules...)
	for name, validate := range platformValidators(missingDirectory) {
		t.Run(name, func(t *testing.T) {
			validation := validate(value)
			issue, ok := findIssue(validation, geodata.ErrorManifestInvalid)
			if validation.OK || !ok {
				t.Fatalf("missing manifest did not produce %s: %#v", geodata.ErrorManifestInvalid, validation.Errors)
			}
			if _, unknown := findIssue(validation, geodata.ErrorCategoryNotFound); unknown {
				t.Fatalf("missing manifest was misreported as an unknown category: %#v", validation.Errors)
			}
			if issue.ObjectType != "rule" || issue.ObjectID != "geo" || issue.Option != "ip_match" {
				t.Fatalf("manifest issue lost its object location: %#v", issue)
			}
		})
	}
}

func TestReachableWarningGroupsAreConsistentAcrossPlatforms(t *testing.T) {
	value := platformIntent()
	value.Nodes = []model.Node{
		{ID: "used", Enabled: true, Type: "vless", Server: "used.example", ServerPort: 443,
			NodeCredentials: model.NodeCredentials{UUID: "00000000-0000-4000-8000-000000000001"}, NodeTLS: model.NodeTLS{Insecure: true}},
		{ID: "unused", Enabled: true, Type: "vless", Server: "unused.example", ServerPort: 443,
			NodeCredentials: model.NodeCredentials{UUID: "00000000-0000-4000-8000-000000000002"}, NodeTLS: model.NodeTLS{Insecure: true}},
	}
	value.Routes = append(value.Routes, model.Route{ID: "proxy", Enabled: true, Kind: "single", Node: "used"})
	value.Rules = append([]model.Rule{{
		ID: "proxy_rule", Enabled: true, DNSProfile: "dns", Route: "proxy", DomainMatch: []string{"domain:example.com"},
	}}, value.Rules...)
	for name, validate := range platformValidators(writeManifest(t)) {
		t.Run(name, func(t *testing.T) {
			validation := validate(value)
			if !validation.OK || len(validation.Warnings) != 1 || validation.Warnings[0].ObjectID != "used" {
				t.Fatalf("platform warning reachability drifted: %#v", validation)
			}
			if len(validation.WarningGroups) != 1 || validation.WarningGroups[0].Count != 1 || validation.WarningGroups[0].Destination != "nodes" {
				t.Fatalf("platform warning grouping drifted: %#v", validation.WarningGroups)
			}
		})
	}
}

func TestPlatformReservedListenersMatchGeneratedPlans(t *testing.T) {
	tests := []struct {
		name     string
		validate func(model.Intent) model.Validation
		address  string
		port     int
		want     bool
	}{
		{"Linux IPv4 DNS", linuxplatform.Validate, "127.0.0.1", linuxplatform.DNSPort, true},
		{"Linux dual-stack IPv6 DNS", linuxplatform.Validate, "127.0.0.1", linuxplatform.DNSPort6, true},
		{"Linux permits IPv6 on IPv4-only DNS port", linuxplatform.Validate, "::1", linuxplatform.DNSPort, false},
		{"OpenWrt DNS IPv4 overlap", openwrtplatform.Validate, "127.0.0.1", openwrtplatform.DNSPort, true},
		{"OpenWrt DNS IPv6 overlap", openwrtplatform.Validate, "::1", openwrtplatform.DNSPort, true},
		{"macOS has no reserved proxy port", macosplatform.Validate, "127.0.0.1", linuxplatform.DNSPort, false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			value := platformIntent()
			value.LocalProxies = []model.LocalProxy{{
				ID: "local", Enabled: true, Protocol: "mixed", Listen: testCase.address, ListenPort: testCase.port,
			}}
			validation := testCase.validate(value)
			issue, collision := findIssue(validation, "PORT_COLLISION")
			if collision != testCase.want {
				t.Fatalf("collision = %v, want %v: %#v", collision, testCase.want, validation.Errors)
			}
			if collision && (issue.ObjectType != "local_proxy" || issue.ObjectID != "local" || issue.Option != "listen_port") {
				t.Fatalf("listener collision lost its object location: %#v", issue)
			}
		})
	}
}

func TestCanonicalStoresRejectUnknownSelectorsBeforePersistence(t *testing.T) {
	seedDirectory := writeManifest(t)
	value := platformIntent()
	value.Rules = append([]model.Rule{{
		ID: "unknown", Enabled: true, DNSProfile: "dns", Route: "direct",
		DomainMatch: []string{"geosite:not-installed"},
	}}, value.Rules...)

	t.Run("linux", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		_, err := (linuxplatform.IntentStore{Path: path, GeoDataDirectory: seedDirectory, NodeDirectory: filepath.Join(filepath.Dir(path), "nodes")}).Save(value, "")
		var validationErr linuxplatform.ValidationError
		if !errors.As(err, &validationErr) {
			t.Fatalf("save returned %v, want a structured validation error", err)
		}
		if _, ok := findIssue(validationErr.Validation, geodata.ErrorCategoryNotFound); !ok {
			t.Fatalf("save lost the Geo issue: %#v", validationErr.Validation.Errors)
		}
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("invalid Linux candidate was persisted: %v", statErr)
		}
	})

	t.Run("macos", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		store := macosplatform.IntentStore{
			Paths: macosplatform.Paths{ConfigPath: path, StateDirectory: filepath.Join(filepath.Dir(path), "state")}, GeoDataDirectory: seedDirectory,
		}
		_, err := store.Save(value, "")
		var validationErr macosplatform.ValidationError
		if !errors.As(err, &validationErr) {
			t.Fatalf("save returned %v, want a structured validation error", err)
		}
		if _, ok := findIssue(validationErr.Validation, geodata.ErrorCategoryNotFound); !ok {
			t.Fatalf("save lost the Geo issue: %#v", validationErr.Validation.Errors)
		}
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("invalid macOS candidate was persisted: %v", statErr)
		}
	})
}

func platformValidators(seedDirectory string) map[string]func(model.Intent) model.Validation {
	return map[string]func(model.Intent) model.Validation{
		"linux": func(value model.Intent) model.Validation {
			return linuxplatform.ValidateWithGeoDataDirectory(value, seedDirectory)
		},
		"openwrt": func(value model.Intent) model.Validation {
			return openwrtplatform.ValidateWithGeoDataDirectory(value, seedDirectory)
		},
		"macos": func(value model.Intent) model.Validation {
			return macosplatform.ValidateWithGeoDataDirectory(value, seedDirectory)
		},
	}
}

func platformIntent() model.Intent {
	return model.Intent{
		Main: model.Main{
			ID: "main", SchemaVersion: model.SchemaVersion, Enabled: true, LogLevel: "warn",
			ProbeDirectURL: "https://direct.example/", ProbeProxyURL: "https://proxy.example/", SpeedtestProxyURL: "https://speed.example/",
		},
		Bootstrap:   model.Bootstrap{ID: "bootstrap", Protocol: "udp", Server: "1.1.1.1", ServerPort: 53, Strategy: "prefer_ipv4"},
		Routes:      []model.Route{{ID: "direct", Enabled: true, Kind: "direct"}},
		DNSProfiles: []model.DNSProfile{{ID: "dns", Enabled: true, Protocol: "udp", Server: "1.1.1.1", ServerPort: 53}},
		Rules:       []model.Rule{{ID: "default", Enabled: true, Default: true, DNSProfile: "dns", Route: "direct"}},
	}
}

func writeManifest(t *testing.T) string {
	t.Helper()
	seedDirectory := filepath.Join(t.TempDir(), "seed")
	if err := os.MkdirAll(seedDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("0", 64)
	manifest := geodata.Manifest{
		SchemaVersion: geodata.ManifestSchemaVersion,
		Upstream: geodata.UpstreamIdentity{
			Repository: geodata.UpstreamRepository, Version: "test", GeoSiteSHA256: digest, GeoIPSHA256: digest,
		},
		Tools: geodata.ToolIdentity{GeoViewRef: geodata.GeoViewCommit, SingBoxVersion: geodata.SingBoxCompiler},
		Rules: []geodata.Rule{
			{Kind: "geoip", Category: "cn", Tag: "steer-geoip-cn", Path: "rules/steer-geoip-cn.srs", SHA256: digest, Size: 1},
			{Kind: "geosite", Category: "cn@ads", Tag: "steer-geosite-cn@ads", Path: "rules/steer-geosite-cn@ads.srs", SHA256: digest, Size: 1},
		},
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDirectory, "manifest.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	return seedDirectory
}

func findIssue(validation model.Validation, code string) (model.Issue, bool) {
	for _, issue := range validation.Errors {
		if issue.Code == code {
			return issue, true
		}
	}
	return model.Issue{}, false
}
