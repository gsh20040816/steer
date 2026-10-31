// SPDX-License-Identifier: GPL-3.0-or-later

package linux

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gsh20040816/steer/go/internal/compiler"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

func TestProbeOverviewUsesSavedIntentWithoutActiveGeneration(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "linux", "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	value, err := model.DecodeJSON(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	value.Main.Enabled = false
	value.Main.ProbeProxyURL = server.URL

	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	var encoded bytes.Buffer
	if err := model.EncodeJSON(&encoded, value); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := ProbeOverviewWithState(
		context.Background(), configPath, filepath.Join(root, "run"), filepath.Join(root, "state"), "proxy", server.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.SavedDigest != compiler.IntentDigest(value) || report.ActiveGeneration != "" {
		t.Fatalf("disabled Saved overview probe = %#v", report)
	}
}

func TestTemporaryProbePreservesBootstrapAndMarksEveryDialSocket(t *testing.T) {
	bootstrap := model.Bootstrap{Protocol: "udp", Server: "1.1.1.1", ServerPort: 53, Strategy: "prefer_ipv4"}
	config, err := temporaryProbeConfig(bootstrap, []any{map[string]any{"type": "direct", "tag": "route"}}, "route", 12345)
	if err != nil {
		t.Fatal(err)
	}
	outbound := config["outbounds"].([]any)[0].(map[string]any)
	if outbound["routing_mark"] != AutoRedirectOutputMark {
		t.Fatalf("temporary outbound is not bypass-marked: %#v", outbound)
	}
	dns := config["dns"].(map[string]any)["servers"].([]any)[0].(map[string]any)
	if dns["routing_mark"] != AutoRedirectOutputMark {
		t.Fatalf("temporary DNS is not bypass-marked: %#v", dns)
	}
	if dns["type"] != bootstrap.Protocol || dns["server"] != bootstrap.Server || dns["server_port"] != bootstrap.ServerPort {
		t.Fatalf("temporary DNS does not preserve bootstrap: %#v", dns)
	}
	resolver := config["route"].(map[string]any)["default_domain_resolver"].(map[string]any)
	if resolver["server"] != "steer-dns-bootstrap" || resolver["strategy"] != bootstrap.Strategy {
		t.Fatalf("temporary default resolver does not preserve bootstrap strategy: %#v", resolver)
	}
}

func TestTemporaryProbeResolvesNodeDomainStrategyThroughBootstrap(t *testing.T) {
	bootstrap := model.Bootstrap{Protocol: "udp", Server: "1.1.1.1", ServerPort: 53, Strategy: "prefer_ipv6"}
	value := model.Intent{
		Nodes: []model.Node{
			{ID: "front", Enabled: true, Type: "socks", Server: "front.example", ServerPort: 1080},
			{ID: "osa", Enabled: true, Type: "socks", Server: "osa.example", ServerPort: 1080, DomainStrategy: "prefer_ipv4"},
		},
		Routes: []model.Route{
			{ID: "front", Enabled: true, Kind: "single", Node: "front"},
			{ID: "osa", Enabled: true, Kind: "single", Node: "osa", Detour: "front"},
		},
	}
	for name, outbounds := range map[string][]any{
		"node":  {compiler.CompileNodeOutbound(value.Nodes[1])},
		"route": compiler.CompileRouteChainOutbounds(value, "osa"),
	} {
		config, err := temporaryProbeConfig(bootstrap, outbounds, "probe", 12345)
		if err != nil {
			t.Fatal(err)
		}
		servers := map[string]bool{}
		for _, raw := range config["dns"].(map[string]any)["servers"].([]any) {
			servers[raw.(map[string]any)["tag"].(string)] = true
		}
		pinned := 0
		for _, raw := range config["outbounds"].([]any) {
			resolver, exists := raw.(map[string]any)["domain_resolver"].(map[string]any)
			if !exists {
				continue
			}
			if !servers[resolver["server"].(string)] || resolver["strategy"] != "prefer_ipv4" {
				t.Fatalf("%s probe outbound resolves through an undefined server or strategy: %#v", name, resolver)
			}
			pinned++
		}
		if pinned != 1 {
			t.Fatalf("%s probe must pin exactly the osa node, got %d: %#v", name, pinned, config["outbounds"])
		}
	}
}
