// SPDX-License-Identifier: GPL-3.0-or-later

package uispec

import (
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	model "github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/subscription"
)

func TestContractCoversEveryNodeType(t *testing.T) {
	contract := ContractValue()
	if contract.SchemaVersion != SchemaVersion || contract.CanonicalSchema != model.SchemaVersion || contract.CreationDefaults["main"]["schema_version"] != model.SchemaVersion {
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
	if capabilities["macos"].DirectBypass || !capabilities["linux"].DirectBypass || !capabilities["openwrt"].DirectBypass {
		t.Fatal("kernel bypass must be limited to Linux auto_redirect platforms")
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
		{TargetCollection: "wireguard_tunnels", SourceCollection: "routes", SourceObjectType: "route", Field: "tunnel"},
		{TargetCollection: "nodes", SourceCollection: "routes", SourceObjectType: "route", Field: "node"},
		{TargetCollection: "routes", SourceCollection: "rules", SourceObjectType: "rule", Field: "route"},
		{TargetCollection: "routes", SourceCollection: "routes", SourceObjectType: "route", Field: "detour"},
		{TargetCollection: "dns_profiles", SourceCollection: "rules", SourceObjectType: "rule", Field: "dns_profile"},
		{TargetCollection: "local_proxies", SourceCollection: "rules", SourceObjectType: "rule", Field: "inbound", Multiple: true},
	}
	if !reflect.DeepEqual(contract.CollectionReferences, want) {
		t.Fatalf("collection reference contract drifted: %#v", contract.CollectionReferences)
	}
	if !reflect.DeepEqual(contract.RuleConnectionOnlyFields, []string{"allowed_ips", "ip_match", "network", "protocol", "port"}) {
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

func TestNodeDomainStrategyFieldFollowsBootstrapChoices(t *testing.T) {
	contract := ContractValue()
	index := -1
	for position, field := range contract.NodeFields {
		if field.Key == "domain_strategy" {
			index = position
		}
	}
	if index < 1 || contract.NodeFields[index-1].Key != "server_port" {
		t.Fatalf("domain_strategy must follow server_port in the shared field order: %#v", contract.NodeFields)
	}
	field := contract.NodeFields[index]
	if field.Label != "Domain resolution strategy" || field.ZhCN != "域名解析策略" || field.Control != "select" ||
		field.Section != "general" || field.Default != nil || len(field.RequiredTypes) != 0 || field.When != nil {
		t.Fatalf("unexpected domain_strategy field: %#v", field)
	}
	if !reflect.DeepEqual(field.Types, allExcept("tor")) {
		t.Fatalf("domain_strategy must apply to every node type with a server: %#v", field.Types)
	}
	want := append([]Choice{{Value: "", Label: "Follow default", ZhCN: "跟随默认"}}, contract.BootstrapStrategies...)
	if !reflect.DeepEqual(field.Options, want) {
		t.Fatalf("domain_strategy options must reuse the bootstrap strategy choices: %#v", field.Options)
	}
	wording := map[string]string{"prefer_ipv4": "优先 IPv4", "prefer_ipv6": "优先 IPv6", "ipv4_only": "仅 IPv4", "ipv6_only": "仅 IPv6"}
	for _, choice := range contract.BootstrapStrategies {
		if choice.ZhCN != wording[choice.Value] {
			t.Fatalf("strategy %q wording drifted: %#v", choice.Value, choice)
		}
	}
	for _, choice := range field.Options {
		node := model.Node{ID: "node", Enabled: true, Type: "socks", Server: "proxy.example", ServerPort: 1080, DomainStrategy: choice.Value}
		if validation := model.ValidateNode(node); !validation.OK {
			t.Fatalf("domain_strategy choice %q is rejected by the backend: %#v", choice.Value, validation.Errors)
		}
	}
}

func TestRepresentativeNodeFromEveryGeneratedFormValidates(t *testing.T) {
	uuid1 := "00000000-0000-4000-8000-000000000001"
	uuid2 := "00000000-0000-4000-8000-000000000002"
	cases := []model.Node{
		{Type: "socks", Server: "proxy.example", ServerPort: 1080, DomainStrategy: "prefer_ipv4"},
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

func TestSubscriptionNodeAndApplyResultContractsMatchTheBackend(t *testing.T) {
	contract := ContractValue()
	if !contract.SubscriptionNodes.ReadOnly || contract.SubscriptionNodes.ManualCopyField != "manual_copy" {
		t.Fatalf("unexpected subscription node contract: %#v", contract.SubscriptionNodes)
	}
	for _, action := range []string{"enable", "disable", "clean", "copy_to_manual"} {
		text := contract.SubscriptionNodes.Actions[action]
		if text.Label == "" || text.ZhCN == "" {
			t.Fatalf("subscription node action %q has no shared wording", action)
		}
	}
	if contract.SubscriptionNodes.Actions["copy_to_manual"].ZhCN != "复制到我的节点" {
		t.Fatal("copy action wording drifted")
	}
	nodes := contract.SubscriptionNodes
	codes := append([]string(nil), nodes.ErrorCodes...)
	texts := []string{}
	for code := range nodes.Errors {
		texts = append(texts, code)
	}
	sort.Strings(codes)
	sort.Strings(texts)
	if !reflect.DeepEqual(codes, texts) {
		t.Fatalf("every subscription node error code needs exactly one shared text: codes %v, texts %v", codes, texts)
	}
	for owner, values := range map[string]map[string]UIText{"errors": nodes.Errors, "validation_issues": nodes.ValidationIssues, "messages": nodes.Messages} {
		for key, text := range values {
			if strings.TrimSpace(text.Label) == "" || text.ZhCN == "" {
				t.Fatalf("subscription_nodes.%s.%s has no shared wording", owner, key)
			}
			mentionsRoutes := strings.Contains(text.Label, nodes.RoutesPlaceholder)
			if mentionsRoutes != strings.Contains(text.ZhCN, nodes.RoutesPlaceholder) || mentionsRoutes != (key == subscription.ErrorNodeStillReferenced) {
				t.Fatalf("subscription_nodes.%s.%s uses the routes placeholder inconsistently: %#v", owner, key, text)
			}
		}
	}
	if nodes.RoutesPlaceholder != "{routes}" || nodes.Errors[subscription.ErrorNodeStillReferenced].ZhCN != "仍有路由在使用这个节点（{routes}），请先修改这些路由" ||
		nodes.Messages["read_only_note"].ZhCN != "订阅节点只读，复制到我的节点后可以编辑" {
		t.Fatalf("subscription node wording drifted: %#v", nodes)
	}
	for _, key := range []string{"read_only_note", "load_failed", "empty_group", "stale_marker", "cleaned", "list_separator"} {
		if _, exists := nodes.Messages[key]; !exists {
			t.Fatalf("subscription_nodes.messages.%s is missing", key)
		}
	}
	config := model.Intent{Nodes: []model.Node{{ID: "copied", NodeSource: model.NodeSource{SourceSubscription: "feed"}}}}
	issues := model.ConfigNodeIssues(config)
	if len(issues) != 1 || nodes.ValidationIssues[issues[0].Code].ZhCN == "" || len(nodes.ValidationIssues) != 1 {
		t.Fatalf("validation issue texts do not match the backend: %#v / %#v", issues, nodes.ValidationIssues)
	}
	encoded, err := json.Marshal(map[string]any{"ok": true, "activated": true, "restarted": false})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]bool
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if contract.ApplyResult.RestartedField != "restarted" || !reflect.DeepEqual(contract.ApplyResult.UnchangedNoticeWhen, result) ||
		contract.ApplyResult.UnchangedNotice.ZhCN != "运行配置没有变化，未重启" {
		t.Fatalf("unexpected Apply result contract: %#v", contract.ApplyResult)
	}
}
