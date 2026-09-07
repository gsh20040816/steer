// SPDX-License-Identifier: GPL-3.0-or-later
package openwrt

// Platform probe tests cover OpenWrt intent and runtime integration.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/generation"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

func TestSaveTestReport(t *testing.T) {
	stateDirectory := t.TempDir()
	report := TestReport{
		Scope:    "nodes",
		ObjectID: "node_a",
		Kind:     "download",
		OK:       true,
		TestedAt: time.Date(2026, time.August, 22, 4, 5, 6, 0, time.UTC),
		Results: []TestResult{{
			URL:                  "https://speed.example/",
			OK:                   true,
			Status:               http.StatusOK,
			DownloadMilliseconds: 250,
			DownloadedBytes:      1024,
		}},
	}
	if err := saveTestReport(stateDirectory, report); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDirectory, "logs", "tests", "nodes", "node_a", "download.json")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved TestReport
	if err := json.Unmarshal(content, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Scope != report.Scope || saved.ObjectID != report.ObjectID || !saved.OK || !saved.TestedAt.Equal(report.TestedAt) || len(saved.Results) != 1 {
		t.Fatalf("unexpected saved test report: %#v", saved)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("test report mode = %o, want 600", info.Mode().Perm())
	}
}

func TestNodeAndRouteTestsRejectDisabledObjects(t *testing.T) {
	config := minimalConfig + `
config node 'disabled_node'
	option enabled '0'
	option type 'socks'
	option server '192.0.2.10'
	option server_port '1080'

config route 'disabled_route'
	option enabled '0'
	option kind 'single'
	option node 'disabled_node'
`
	configPath := filepath.Join(t.TempDir(), "steer")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SpeedTestNode(context.Background(), configPath, t.TempDir(), "/definitely/missing/sing-box", "disabled_node", false); err == nil {
		t.Fatal("disabled node test was accepted")
	}
	if _, err := SpeedTestRoute(context.Background(), configPath, t.TempDir(), "/definitely/missing/sing-box", "disabled_route", false); err == nil {
		t.Fatal("disabled route test was accepted")
	}
}

func TestReadLatestProbeResultsReturnsBackendDTO(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "steer")
	stateDirectory := filepath.Join(root, "state")
	if err := os.WriteFile(configPath, []byte(minimalConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveTestReport(stateDirectory, TestReport{
		Scope: "overview", Kind: "direct", OK: true, TestedAt: time.Now(), Results: []TestResult{},
	}); err != nil {
		t.Fatal(err)
	}
	results := ReadLatestProbeResults(configPath, filepath.Join(root, "run"), stateDirectory)
	if len(results.Results) != 1 || results.Results[0].Kind != "direct" || !results.Results[0].Stale {
		t.Fatalf("unexpected latest probe results: %#v", results)
	}
}

func TestReadDiagnosticsInspectsCurrentGenerationDNSCaptureArtifacts(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "steer")
	runDirectory := filepath.Join(root, "run")
	if err := os.WriteFile(configPath, []byte(minimalConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := DecodeBytes([]byte(minimalConfig))
	if err != nil {
		t.Fatal(err)
	}
	plan := NewPlan(value)
	compiled := compiler.Compile(value, compiler.Options{Target: plan.CompilerTarget()})
	candidate, err := generation.Create(filepath.Join(runDirectory, "generations"), value, compiled.SingBox)
	if err != nil {
		t.Fatal(err)
	}
	firewall, err := RenderFirewall(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(candidate.Directory, "firewall.nft"), []byte(firewall), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(candidate.Directory, filepath.Join(runDirectory, "current")); err != nil {
		t.Fatal(err)
	}

	diagnostics := ReadDiagnostics(configPath, runDirectory, filepath.Join(root, "state"))
	if !diagnostics.DNSCapture.Configured || diagnostics.DNSCapture.ActiveGeneration != filepath.Base(candidate.Directory) ||
		diagnostics.DNSCapture.Mode != "native_hijack_with_local_shim" {
		t.Fatalf("current OpenWrt DNS capture artifacts were not diagnosed: %#v", diagnostics.DNSCapture)
	}
}

func TestTemporaryProbeConfigPreservesBootstrapAndMarksDialSockets(t *testing.T) {
	original := map[string]any{"type": "socks", "tag": "route-proxy", "server": "192.0.2.1", "server_port": 1080}
	bootstrap := model.Bootstrap{Protocol: "udp", Server: "1.1.1.1", ServerPort: 53, Strategy: "prefer_ipv4"}
	config, err := temporaryProbeConfig(bootstrap, []any{original}, "route-proxy", 12345)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := original["routing_mark"]; exists {
		t.Fatal("temporary probe mutated compiler output")
	}
	dns := config["dns"].(map[string]any)["servers"].([]any)[0].(map[string]any)
	if dns["type"] != bootstrap.Protocol || dns["server"] != bootstrap.Server || dns["server_port"] != bootstrap.ServerPort {
		t.Fatalf("temporary DNS does not preserve bootstrap: %#v", dns)
	}
	if dns["routing_mark"] != AutoRedirectOutputMark {
		t.Fatalf("temporary DNS routing mark = %#v, want %#x", dns["routing_mark"], AutoRedirectOutputMark)
	}
	resolver := config["route"].(map[string]any)["default_domain_resolver"].(map[string]any)
	if resolver["server"] != "steer-dns-bootstrap" || resolver["strategy"] != bootstrap.Strategy {
		t.Fatalf("temporary default resolver does not preserve bootstrap strategy: %#v", resolver)
	}
	for _, value := range config["outbounds"].([]any) {
		outbound := value.(map[string]any)
		if outbound["routing_mark"] != AutoRedirectOutputMark {
			t.Fatalf("temporary outbound routing mark = %#v, want %#x", outbound["routing_mark"], AutoRedirectOutputMark)
		}
	}
}
