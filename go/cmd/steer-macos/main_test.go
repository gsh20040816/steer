// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

func TestRunVersion(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"version"}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != version {
		t.Fatalf("unexpected version output: %q", output.String())
	}
}

func TestRunRequiresExplicitPaths(t *testing.T) {
	for _, args := range [][]string{
		{"validate", "unexpected"}, {"compile", "unexpected"}, {"prepare", "unexpected"},
		{"parse-nodes"}, {"export-node"}, {"control"}, {"_state", "unexpected"}, {"_control", "unexpected"},
	} {
		if err := run(args, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("expected explicit path error for %v", args)
		}
	}
}

func TestRunUIStateReportsSavedCountsAndPendingApply(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	value := model.Intent{
		Main: model.Main{
			ID: "main", SchemaVersion: model.SchemaVersion, Enabled: true, LogLevel: "warn",
			ProbeDirectURL: "https://direct.example/", ProbeProxyURL: "https://proxy.example/",
			SpeedtestProxyURL: "https://speed.example/",
		},
		Bootstrap:     model.Bootstrap{ID: "bootstrap", Protocol: "udp", Server: "1.1.1.1", ServerPort: 53, Strategy: "prefer_ipv4"},
		Nodes:         []model.Node{{ID: "node", Enabled: true, Type: "socks", Server: "127.0.0.1", ServerPort: 1080}},
		Subscriptions: []model.Subscription{{ID: "feed", Enabled: false, URL: "https://example.com/feed", UpdateInterval: "6h"}},
		Routes:        []model.Route{{ID: "direct", Enabled: true, Kind: "direct"}},
		DNSProfiles:   []model.DNSProfile{{ID: "dns", Enabled: true, Protocol: "udp", Server: "1.1.1.1", ServerPort: 53}},
		Rules:         []model.Rule{{ID: "default", Enabled: true, Default: true, DNSProfile: "dns", Route: "direct"}},
	}
	writeConfig := func() {
		file, err := os.Create(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := model.EncodeJSON(file, value); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	readState := func() macUILifecycleState {
		var output bytes.Buffer
		err := run([]string{
			"_state", "--config", configPath, "--run-dir", filepath.Join(root, "run"),
			"--state-dir", filepath.Join(root, "state"), "--geodata", filepath.Join(root, "geodata"),
		}, &output, &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
		var state macUILifecycleState
		if err := json.Unmarshal(output.Bytes(), &state); err != nil {
			t.Fatalf("decode UI state: %v\n%s", err, output.String())
		}
		return state
	}
	writeConfig()
	state := readState()
	if !state.Saved.Available || !state.Saved.Enabled || !state.Saved.Validation.OK ||
		state.Saved.Counts.Nodes != 1 || state.Saved.Counts.Subscriptions != 1 || !state.PendingApply || state.Active.GenerationID != "" {
		t.Fatalf("unexpected enabled Saved lifecycle: %#v", state)
	}
	value.Main.Enabled = false
	writeConfig()
	state = readState()
	if state.Saved.Enabled || state.PendingApply {
		t.Fatalf("disabled Saved configuration manufactured pending Apply: %#v", state)
	}
}

func TestRunParseNodes(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "nodes.txt")
	if err := os.WriteFile(inputPath, []byte("socks://user:pass@127.0.0.1:1080#Local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"parse-nodes", "--input", inputPath}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"nodes"`, `"name": "Local"`, `"type": "socks"`} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("parsed node output is missing %s:\n%s", expected, output.String())
		}
	}
}

func TestRunExportNode(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "node.json")
	node := model.Node{
		ID: "edge", Enabled: true, Name: "Edge", Type: "vless", Server: "proxy.example", ServerPort: 443,
		NodeCredentials: model.NodeCredentials{UUID: "00000000-0000-4000-8000-000000000001"},
		NodeTLS:         model.NodeTLS{TLSServerName: "edge.example"},
	}
	content, err := json.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inputPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"export-node", "--input", inputPath}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var result struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.URI, "vless://") || !strings.Contains(result.URI, "Edge") {
		t.Fatalf("unexpected exported node link: %s", output.String())
	}
}

func TestRunCompileEmitsMacOSTarget(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	file, err := os.Create(configPath)
	if err != nil {
		t.Fatal(err)
	}
	value := model.Intent{
		Main: model.Main{
			ID: "main", SchemaVersion: model.SchemaVersion, Enabled: true, LogLevel: "warn",
			ProbeDirectURL: "https://direct.example/", ProbeProxyURL: "https://proxy.example/",
			SpeedtestProxyURL: "https://speed.example/", DNSCacheCapacity: 4096,
		},
		Bootstrap:   model.Bootstrap{ID: "bootstrap", Protocol: "udp", Server: "1.1.1.1", ServerPort: 53, Strategy: "prefer_ipv4"},
		Routes:      []model.Route{{ID: "direct", Enabled: true, Kind: "direct"}},
		DNSProfiles: []model.DNSProfile{{ID: "dns", Enabled: true, Protocol: "udp", Server: "1.1.1.1", ServerPort: 53}},
		Rules:       []model.Rule{{ID: "default", Enabled: true, Default: true, DNSProfile: "dns", Route: "direct"}},
	}
	if err := model.EncodeJSON(file, value); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"compile", "--config", configPath, "--state-dir", filepath.Join(root, "state")}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"action": "hijack-dns"`, `"port": [
            53
          ]`, `"auto_route": true`} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("compiled macOS output is missing %s:\n%s", expected, output.String())
		}
	}
	if strings.Contains(output.String(), `"auto_redirect"`) {
		t.Fatalf("compiled macOS output contains Linux auto_redirect:\n%s", output.String())
	}
}
