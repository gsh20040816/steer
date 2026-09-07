// SPDX-License-Identifier: GPL-3.0-or-later

package uispec

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

func TestContractCoversEveryNodeType(t *testing.T) {
	contract := ContractValue()
	if contract.SchemaVersion != SchemaVersion || contract.CanonicalSchema != 9 {
		t.Fatalf("unexpected schema contract: %#v", contract)
	}
	seen := map[string]bool{}
	for _, nodeType := range contract.NodeTypes {
		if seen[nodeType.Value] {
			t.Fatalf("duplicate node type %q", nodeType.Value)
		}
		seen[nodeType.Value] = true
		fields := contract.FieldsForNodeType(nodeType.Value)
		if len(fields) == 0 {
			t.Fatalf("node type %q has no UI fields", nodeType.Value)
		}
		keys := map[string]bool{}
		for _, field := range fields {
			if keys[field.Key] {
				t.Fatalf("node type %q repeats field %q", nodeType.Value, field.Key)
			}
			keys[field.Key] = true
		}
		for _, required := range []string{"enabled", "name"} {
			if !keys[required] {
				t.Fatalf("node type %q misses %q", nodeType.Value, required)
			}
		}
		if nodeType.Value == "tor" {
			if keys["server"] || keys["server_port"] {
				t.Fatalf("Tor must not expose a remote endpoint")
			}
		} else if !keys["server"] || !keys["server_port"] {
			t.Fatalf("remote node type %q misses endpoint fields", nodeType.Value)
		}
	}
	for _, expected := range nodeTypeValues() {
		if !seen[expected] {
			t.Fatalf("node type %q is absent", expected)
		}
	}
}

func TestPlatformCapabilitiesAreExplicit(t *testing.T) {
	capabilities := ContractValue().PlatformCapabilities
	for _, platform := range []string{"openwrt", "linux", "macos"} {
		if _, exists := capabilities[platform]; !exists {
			t.Fatalf("missing platform capability %q", platform)
		}
	}
	if capabilities["macos"].SourceMAC {
		t.Fatal("macOS must not advertise source-MAC support")
	}
	if capabilities["macos"].SourceMACReason == "" {
		t.Fatal("macOS source-MAC capability must explain the stable platform boundary")
	}
}

func TestReferenceAndRuleStageContractsAreExplicit(t *testing.T) {
	contract := ContractValue()
	want := []CollectionReference{
		{TargetCollection: "nodes", SourceCollection: "routes", SourceObjectType: "route", Field: "node"},
		{TargetCollection: "routes", SourceCollection: "rules", SourceObjectType: "rule", Field: "route"},
		{TargetCollection: "routes", SourceCollection: "routes", SourceObjectType: "route", Field: "detour"},
		{TargetCollection: "dns_profiles", SourceCollection: "rules", SourceObjectType: "rule", Field: "dns_profile"},
		{TargetCollection: "local_proxies", SourceCollection: "rules", SourceObjectType: "rule", Field: "inbound", Multiple: true},
	}
	if !reflect.DeepEqual(contract.CollectionReferences, want) {
		t.Fatalf("collection reference contract drifted: %#v", contract.CollectionReferences)
	}
	if !reflect.DeepEqual(contract.RuleConnectionOnlyFields, []string{"ip_match", "network", "protocol", "port"}) {
		t.Fatalf("connection-only rule fields drifted: %#v", contract.RuleConnectionOnlyFields)
	}
}

func TestSubscriptionCreationDefaultIsShared(t *testing.T) {
	if value := ContractValue().SubscriptionUpdateIntervalDefault; value != "6h" {
		t.Fatalf("unexpected subscription update interval default %q", value)
	}
}

func TestCreationDefaultsAndAutomaticIDPolicyAreShared(t *testing.T) {
	contract := ContractValue()
	if !contract.IDPolicy.AutoGenerate || contract.IDPolicy.MaxLength != 32 {
		t.Fatalf("unexpected ID policy: %#v", contract.IDPolicy)
	}
	pattern, err := regexp.Compile(contract.IDPolicy.Pattern)
	if err != nil {
		t.Fatal(err)
	}
	for collection, prefix := range contract.IDPolicy.CollectionPrefixes {
		if !pattern.MatchString(prefix + "-abc123") {
			t.Errorf("%s prefix %q cannot generate a valid ID", collection, prefix)
		}
	}
	for collection, required := range contract.CreationRequiredFields {
		defaults, exists := contract.CreationDefaults[collection]
		if !exists {
			t.Errorf("%s has required fields but no creation defaults", collection)
			continue
		}
		for _, field := range required {
			if field == "id" {
				if _, exists := contract.IDPolicy.CollectionPrefixes[collection]; !exists {
					t.Errorf("%s requires id but has no ID prefix", collection)
				}
				continue
			}
			if _, exists := defaults[field]; !exists {
				t.Errorf("%s creation defaults do not materialize %q", collection, field)
			}
		}
	}
	if got := contract.CreationDefaults["dns_profiles"]; got["protocol"] != "udp" || got["server_port"] != 53 {
		t.Fatalf("DNS creation must make the stable UDP/53 choice: %#v", got)
	}
	if got := contract.CreationDefaults["nodes"]; got["type"] != "socks" || got["server_port"] != 1080 {
		t.Fatalf("Node creation defaults drifted: %#v", got)
	}
	if got := contract.CreationDefaults["local_proxies"]; got["protocol"] != "mixed" || got["listen_port"] != 1090 {
		t.Fatalf("Local Proxy creation defaults drifted: %#v", got)
	}
}

func TestDNSBoundariesAndSubscriptionInventoryAreExplicit(t *testing.T) {
	contract := ContractValue()
	for _, platform := range []string{"linux", "openwrt", "macos"} {
		boundary := contract.DNSBoundaries[platform]
		if boundary.CaptureMode == "" || boundary.CaptureScope == "" || len(boundary.Exclusions) == 0 ||
			boundary.BootstrapBoundary == "" || boundary.EncryptedDNSBoundary == "" || boundary.DiagnosticBoundary == "" {
			t.Errorf("%s DNS boundary is incomplete: %#v", platform, boundary)
		}
	}
	if contract.SubscriptionInventory.ChangesActiveGeneration ||
		contract.SubscriptionInventory.UnreferencedNodes != "removed" ||
		contract.SubscriptionInventory.StaleReferencedNodes != "preserved" || contract.SubscriptionInventory.Notice == "" {
		t.Fatalf("subscription inventory semantics drifted: %#v", contract.SubscriptionInventory)
	}

}

func TestHighFrequencyInputFormatsAreShared(t *testing.T) {
	formats := ContractValue().InputFormats
	probe := formats["probe_url"]
	if probe.Kind != "url" || !probe.Absolute || !probe.ForbidCredentials || !probe.ForbidFragment || !reflect.DeepEqual(probe.Schemes, []string{"https"}) {
		t.Fatalf("probe URL format drifted: %#v", probe)
	}
	subscription := formats["subscription_url"]
	if subscription.Kind != "url" || !subscription.Absolute || !reflect.DeepEqual(subscription.Schemes, []string{"http", "https"}) {
		t.Fatalf("subscription URL format drifted: %#v", subscription)
	}
	if duration := formats["positive_duration"]; duration.Kind != "duration" || !duration.Positive || duration.Pattern != `^[1-9][0-9]*(ms|s|m|h)$` {
		t.Fatalf("duration format drifted: %#v", duration)
	}
	if path := formats["dns_http_path"]; path.Kind != "string" || path.Prefix != "/" {
		t.Fatalf("DNS path format drifted: %#v", path)
	}
}

func TestDNSProtocolFieldAndPortMatrix(t *testing.T) {
	expected := map[string]struct {
		fields []string
		port   int
	}{
		"udp":   {fields: []string{}, port: 53},
		"tcp":   {fields: []string{}, port: 53},
		"tls":   {fields: []string{"tls_server_name", "insecure"}, port: 853},
		"https": {fields: []string{"tls_server_name", "path", "insecure"}, port: 443},
		"quic":  {fields: []string{"tls_server_name", "insecure"}, port: 853},
		"h3":    {fields: []string{"tls_server_name", "path", "insecure"}, port: 443},
	}
	protocols := ContractValue().DNSProtocols
	if len(protocols) != len(expected) {
		t.Fatalf("DNS protocol matrix has %d entries, want %d", len(protocols), len(expected))
	}
	for _, protocol := range protocols {
		want, exists := expected[protocol.Value]
		if !exists {
			t.Fatalf("unexpected DNS protocol %q", protocol.Value)
		}
		if !reflect.DeepEqual(protocol.Fields, want.fields) || protocol.DefaultPort != want.port {
			t.Fatalf("DNS protocol %q matrix drifted: %#v", protocol.Value, protocol)
		}
		wantRequired := []string{}
		if len(protocol.Fields) > 0 {
			wantRequired = []string{"tls_server_name"}
		}
		if !reflect.DeepEqual(protocol.RequiredFields, wantRequired) {
			t.Fatalf("DNS protocol %q required fields drifted: %#v", protocol.Value, protocol.RequiredFields)
		}
	}
}

func TestNodeFieldsCoverCanonicalUserOptions(t *testing.T) {
	canonical := map[string]bool{}
	collectJSONFields(reflect.TypeOf(model.Node{}), canonical)
	for _, internal := range []string{"id", "type", "source_subscription", "source_fingerprint", "pinned_stale"} {
		delete(canonical, internal)
	}
	covered := map[string]bool{}
	for _, field := range ContractValue().NodeFields {
		covered[field.Key] = true
	}
	for key := range canonical {
		if !covered[key] {
			t.Errorf("canonical node field %q has no shared UI specification", key)
		}
	}
}

func TestRepresentativeNodeFromEveryGeneratedFormValidates(t *testing.T) {
	uuid1 := "00000000-0000-4000-8000-000000000001"
	uuid2 := "00000000-0000-4000-8000-000000000002"
	cases := []model.Node{
		{Type: "socks", Server: "proxy.example", ServerPort: 1080},
		{Type: "http", Server: "proxy.example", ServerPort: 8080},
		{Type: "shadowsocks", Server: "proxy.example", ServerPort: 8388, NodeCredentials: model.NodeCredentials{Password: "secret"}, NodeProtocol: model.NodeProtocol{Method: "aes-256-gcm"}},
		{Type: "vmess", Server: "proxy.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{UUID: uuid1}, NodeProtocol: model.NodeProtocol{Security: "auto"}},
		{Type: "vless", Server: "proxy.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{UUID: uuid1}, NodeTransport: model.NodeTransport{PacketEncoding: "xudp"}},
		{Type: "trojan", Server: "proxy.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{Password: "secret"}, NodeTLS: model.NodeTLS{TLSServerName: "proxy.example"}},
		{Type: "hysteria", Server: "proxy.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{Password: "secret"}, NodeProtocol: model.NodeProtocol{UpMbps: 100, DownMbps: 100}, NodeTLS: model.NodeTLS{TLSServerName: "proxy.example"}},
		{Type: "hysteria2", Server: "proxy.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{Password: "secret"}, NodeTLS: model.NodeTLS{TLSServerName: "proxy.example"}},
		{Type: "shadowtls", Server: "proxy.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{Password: "secret"}, NodeProtocol: model.NodeProtocol{Version: 3}, NodeTLS: model.NodeTLS{TLSServerName: "proxy.example"}},
		{Type: "tuic", Server: "proxy.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{UUID: uuid2, Password: "secret"}, NodeTLS: model.NodeTLS{TLSServerName: "proxy.example"}},
		{Type: "anytls", Server: "proxy.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{Password: "secret"}, NodeTLS: model.NodeTLS{TLSServerName: "proxy.example"}},
		{Type: "naive", Server: "proxy.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{Username: "user", Password: "secret"}, NodeTLS: model.NodeTLS{TLSServerName: "proxy.example"}},
		{Type: "ssh", Server: "proxy.example", ServerPort: 22, NodeCredentials: model.NodeCredentials{Username: "root", Password: "secret"}},
		{Type: "tor", NodeProtocol: model.NodeProtocol{ExecutablePath: "/usr/local/bin/tor"}},
	}
	contract := ContractValue()
	for index := range cases {
		node := &cases[index]
		node.ID, node.Name, node.Enabled = "node-"+node.Type, node.Type, true
		t.Run(node.Type, func(t *testing.T) {
			if validation := model.ValidateNode(*node); !validation.OK {
				t.Fatalf("representative node is rejected: %#v", validation.Errors)
			}
			fields := map[string]bool{}
			for _, field := range contract.FieldsForNodeType(node.Type) {
				fields[field.Key] = true
			}
			encoded, err := json.Marshal(node)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := json.Unmarshal(encoded, &document); err != nil {
				t.Fatal(err)
			}
			for key, value := range document {
				if key == "id" || key == "type" || reflect.ValueOf(value).IsZero() {
					continue
				}
				if !fields[key] {
					t.Errorf("representative value %q is not editable from the generated form", key)
				}
			}
		})
	}
}

func collectJSONFields(value reflect.Type, fields map[string]bool) {
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		if field.Anonymous {
			collectJSONFields(field.Type, fields)
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			fields[name] = true
		}
	}
}
