// SPDX-License-Identifier: GPL-3.0-or-later
package intent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func validIntent() Intent {
	return Intent{
		Main: Main{ID: "main", SchemaVersion: SchemaVersion, Enabled: true, LogLevel: "warn",
			ProbeDirectURL: "https://direct.example/", ProbeProxyURL: "https://proxy.example/", SpeedtestProxyURL: "https://speed.example/"},
		Bootstrap:   Bootstrap{ID: "bootstrap", Protocol: "udp", Server: "1.1.1.1", ServerPort: 53, Strategy: "prefer_ipv4"},
		Nodes:       []Node{{ID: "proxy", Enabled: true, Type: "vless", Server: "proxy.example", ServerPort: 443, NodeCredentials: NodeCredentials{UUID: "00000000-0000-4000-8000-000000000001"}, NodeTransport: NodeTransport{PacketEncoding: "xudp"}}},
		Routes:      []Route{{ID: "direct", Enabled: true, Kind: "direct"}, {ID: "proxy_route", Enabled: true, Kind: "single", Node: "proxy"}, {ID: "block", Enabled: true, Kind: "block"}},
		DNSProfiles: []DNSProfile{{ID: "dns", Enabled: true, Protocol: "https", Server: "1.1.1.1", ServerPort: 443, TLSServerName: "one.one.one.one", Path: "/dns-query"}},
		Rules:       []Rule{{ID: "proxy_rule", Enabled: true, DNSProfile: "dns", Route: "proxy_route", DomainMatch: []string{"domain:example.com"}}, {ID: "default", Enabled: true, Default: true, DNSProfile: "dns", Route: "direct"}},
	}
}

func TestValidateRepresentativeIntent(t *testing.T) {
	validation := Validate(validIntent())
	if !validation.OK {
		t.Fatalf("unexpected validation errors: %#v", validation.Errors)
	}
}

func TestListenersOverlapAddressFamilies(t *testing.T) {
	tests := []struct {
		name      string
		first     Listener
		second    Listener
		dualStack bool
		want      bool
	}{
		{"same address", Listener{Address: "127.0.0.1", Port: 1080}, Listener{Address: "127.0.0.1", Port: 1080}, false, true},
		{"IPv4 wildcard and specific", Listener{Address: "0.0.0.0", Port: 1080}, Listener{Address: "192.0.2.1", Port: 1080}, false, true},
		{"IPv6 wildcard and specific", Listener{Address: "::", Port: 1080}, Listener{Address: "2001:db8::1", Port: 1080}, false, true},
		{"dual-stack IPv6 wildcard and IPv4", Listener{Address: "::", Port: 1080}, Listener{Address: "127.0.0.1", Port: 1080}, true, true},
		{"IPv6-only wildcard and IPv4", Listener{Address: "::", Port: 1080}, Listener{Address: "127.0.0.1", Port: 1080}, false, false},
		{"IPv4 wildcard and IPv6", Listener{Address: "0.0.0.0", Port: 1080}, Listener{Address: "::1", Port: 1080}, true, false},
		{"different IPv4 specifics", Listener{Address: "127.0.0.1", Port: 1080}, Listener{Address: "127.0.0.2", Port: 1080}, true, false},
		{"different IPv6 specifics", Listener{Address: "::1", Port: 1080}, Listener{Address: "::2", Port: 1080}, true, false},
		{"IPv4 mapped address", Listener{Address: "::ffff:127.0.0.1", Port: 1080}, Listener{Address: "127.0.0.1", Port: 1080}, false, true},
		{"different ports", Listener{Address: "0.0.0.0", Port: 1080}, Listener{Address: "127.0.0.1", Port: 1081}, true, false},
		{"invalid address", Listener{Address: "localhost", Port: 1080}, Listener{Address: "127.0.0.1", Port: 1080}, true, false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := ListenersOverlap(testCase.first, testCase.second, testCase.dualStack); got != testCase.want {
				t.Fatalf("ListenersOverlap(%#v, %#v, %v) = %v, want %v", testCase.first, testCase.second, testCase.dualStack, got, testCase.want)
			}
			if got := ListenersOverlap(testCase.second, testCase.first, testCase.dualStack); got != testCase.want {
				t.Fatalf("ListenersOverlap is asymmetric: reverse = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestValidateListenerCollisionsAreObjectIssues(t *testing.T) {
	value := validIntent()
	value.LocalProxies = []LocalProxy{{
		ID: "local", Enabled: true, Protocol: "mixed", Listen: "127.0.0.1", ListenPort: 1053,
	}}
	validation := ValidateWithOptions(value, ValidationOptions{
		ReservedListeners:     []Listener{{Address: "0.0.0.0", Port: 1053, Owner: "platform DNS"}},
		IPv6WildcardDualStack: true,
	})
	if validation.OK {
		t.Fatalf("reserved wildcard listener collision was accepted: %#v", validation)
	}
	for _, issue := range validation.Errors {
		if issue.Code == "PORT_COLLISION" && issue.ObjectType == "local_proxy" && issue.ObjectID == "local" && issue.Option == "listen_port" {
			return
		}
	}
	t.Fatalf("object-level collision issue is missing: %#v", validation.Errors)
}

func TestValidateAllowsNonOverlappingSpecificListeners(t *testing.T) {
	value := validIntent()
	value.LocalProxies = []LocalProxy{
		{ID: "first", Enabled: true, Protocol: "mixed", Listen: "127.0.0.1", ListenPort: 1080},
		{ID: "second", Enabled: true, Protocol: "mixed", Listen: "127.0.0.2", ListenPort: 1080},
	}
	if validation := Validate(value); !validation.OK {
		t.Fatalf("distinct specific listener addresses were rejected: %#v", validation.Errors)
	}
}

func TestValidateAllowsHTTPSubscriptionURL(t *testing.T) {
	intent := validIntent()
	intent.Subscriptions = []Subscription{{ID: "feed", Enabled: true, URL: "http://192.168.1.2/subscription"}}
	if validation := Validate(intent); !validation.OK {
		t.Fatalf("ordinary HTTP subscription URL was rejected: %#v", validation.Errors)
	}
}

func TestLocalProxyAddressFixtureMatchesBackendValidation(t *testing.T) {
	type fixtureCase struct {
		Name                 string `json:"name"`
		Listen               string `json:"listen"`
		Classification       string `json:"classification"`
		AllowUnauthenticated bool   `json:"allow_unauthenticated"`
	}
	var fixtures struct {
		SchemaVersion int           `json:"schema_version"`
		Cases         []fixtureCase `json:"cases"`
	}
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "ui", "local-proxy-listen-fixtures.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &fixtures); err != nil {
		t.Fatal(err)
	}
	if fixtures.SchemaVersion != 1 || len(fixtures.Cases) == 0 {
		t.Fatalf("invalid local proxy fixture header: %#v", fixtures)
	}

	for _, fixture := range fixtures.Cases {
		t.Run(fixture.Name, func(t *testing.T) {
			value := validIntent()
			value.LocalProxies = []LocalProxy{{
				ID: "entry", Enabled: true, Protocol: "mixed", Listen: fixture.Listen, ListenPort: 1080,
			}}
			validation := Validate(value)
			classification := "loopback"
			if hasIssue(validation, "INVALID_LISTEN_ADDRESS") {
				classification = "invalid"
			} else if hasIssue(validation, "LOCAL_PROXY_AUTH_REQUIRED") {
				classification = "non_loopback"
			}
			if classification != fixture.Classification {
				t.Fatalf("classification=%q, want %q; errors=%#v", classification, fixture.Classification, validation.Errors)
			}
			if validation.OK != fixture.AllowUnauthenticated {
				t.Fatalf("unauthenticated result=%v, want %v; errors=%#v", validation.OK, fixture.AllowUnauthenticated, validation.Errors)
			}

			value.LocalProxies[0].Username = "user"
			value.LocalProxies[0].Password = "secret"
			authenticated := Validate(value)
			if authenticated.OK != (fixture.Classification != "invalid") {
				t.Fatalf("authenticated result=%v; errors=%#v", authenticated.OK, authenticated.Errors)
			}
		})
	}
}

func TestValidateNodeRejectsControlCharacters(t *testing.T) {
	node := validIntent().Nodes[0]
	node.Name = "bad\nname"
	validation := ValidateNode(node)
	if validation.OK || !hasIssue(validation, "CONTROL_CHARACTER") {
		t.Fatalf("node control character was accepted: %#v", validation.Errors)
	}
}

func TestValidateNodeAllowsMultilinePrivateKey(t *testing.T) {
	node := validIntent().Nodes[0]
	node.Type = "ssh"
	node.Server = "ssh.example"
	node.ServerPort = 22
	node.Username = "root"
	node.Password = ""
	node.PrivateKey = "-----BEGIN OPENSSH PRIVATE KEY-----\nkey-material\n-----END OPENSSH PRIVATE KEY-----\n"
	validation := ValidateNode(node)
	if !validation.OK {
		t.Fatalf("multiline private key was rejected: %#v", validation.Errors)
	}
	node.PrivateKey = "key\tmaterial"
	validation = ValidateNode(node)
	if validation.OK || !hasIssue(validation, "CONTROL_CHARACTER") {
		t.Fatalf("tab in private key was accepted: %#v", validation.Errors)
	}
}

func TestValidateRequiresEveryHTTPSProbe(t *testing.T) {
	intent := validIntent()
	intent.Main.ProbeDirectURL = ""
	if validation := Validate(intent); validation.OK || !hasIssue(validation, "REQUIRED_PROBE_URL") {
		t.Fatalf("missing required probe was accepted: %#v", validation.Errors)
	}
}

func TestValidateRouteDetourGraph(t *testing.T) {
	intent := validIntent()
	intent.Routes = append(intent.Routes,
		Route{ID: "front", Enabled: true, Kind: "single", Node: "proxy", Detour: "ingress"},
		Route{ID: "ingress", Enabled: true, Kind: "single", Node: "proxy"},
	)
	intent.Routes[1].Detour = "front"
	if validation := Validate(intent); !validation.OK {
		t.Fatalf("valid route detour chain was rejected: %#v", validation.Errors)
	}

	intent.Routes[4].Detour = "proxy_route"
	validation := Validate(intent)
	if validation.OK || !hasIssue(validation, "ROUTE_DETOUR_CYCLE") {
		t.Fatalf("indirect route detour cycle was accepted: %#v", validation.Errors)
	}
	for _, issue := range validation.Errors {
		if issue.Code == "ROUTE_DETOUR_CYCLE" && !strings.Contains(issue.Message, "proxy_route -> front -> ingress -> proxy_route") {
			t.Fatalf("cycle error omitted the complete path: %#v", issue)
		}
	}
}

func TestValidateRejectsInvalidRouteDetourTargets(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Intent)
		code string
	}{
		{"missing", func(intent *Intent) { intent.Routes[1].Detour = "missing" }, "DANGLING_DETOUR"},
		{"direct", func(intent *Intent) { intent.Routes[1].Detour = "direct" }, "INVALID_DETOUR_KIND"},
		{"disabled", func(intent *Intent) {
			intent.Routes = append(intent.Routes, Route{ID: "disabled_route", Enabled: false, Kind: "single", Node: "proxy"})
			intent.Routes[1].Detour = "disabled_route"
		}, "DISABLED_DETOUR"},
		{"self", func(intent *Intent) { intent.Routes[1].Detour = "proxy_route" }, "ROUTE_DETOUR_CYCLE"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			intent := validIntent()
			testCase.edit(&intent)
			validation := Validate(intent)
			if validation.OK || !hasIssue(validation, testCase.code) {
				t.Fatalf("invalid detour target was accepted: %#v", validation.Errors)
			}
		})
	}
}

func TestValidateRejectsBrokenReferencesAndOldSchema(t *testing.T) {
	intent := validIntent()
	intent.Main.SchemaVersion = 3
	intent.Rules[0].Route = "missing"
	validation := Validate(intent)
	if validation.OK || !hasIssue(validation, "UNSUPPORTED_SCHEMA") || !hasIssue(validation, "DANGLING_ROUTE") {
		t.Fatalf("missing strict failures: %#v", validation.Errors)
	}
}

func TestDisabledObjectsRemainStagingOnly(t *testing.T) {
	intent := validIntent()
	intent.Nodes = append(intent.Nodes, Node{ID: "unfinished", Enabled: false, Type: "unknown"})
	intent.Rules = append(intent.Rules[:1], append([]Rule{{ID: "disabled", Enabled: false, DNSProfile: "missing", Route: "missing"}}, intent.Rules[1:]...)...)
	validation := Validate(intent)
	if !validation.OK {
		t.Fatalf("disabled objects must not enter semantic validation: %#v", validation.Errors)
	}
}

func TestGlobalDNSCacheFeaturesValidateOn114(t *testing.T) {
	intent := validIntent()
	intent.Main.DNSCachePersist = true
	intent.Main.DNSOptimisticCache = true
	validation := Validate(intent)
	if !validation.OK {
		t.Fatalf("unexpected result: %#v", validation)
	}
}

func TestAllFrozenDNSTransportsValidate(t *testing.T) {
	for _, protocol := range []string{"udp", "tcp", "tls", "https", "quic", "h3"} {
		t.Run(protocol, func(t *testing.T) {
			intent := validIntent()
			intent.DNSProfiles[0].Protocol = protocol
			if protocol == "udp" || protocol == "tcp" {
				intent.DNSProfiles[0].TLSServerName = ""
			}
			if protocol != "https" && protocol != "h3" {
				intent.DNSProfiles[0].Path = ""
			}
			if validation := Validate(intent); !validation.OK {
				t.Fatalf("%s rejected: %#v", protocol, validation.Errors)
			}
		})
	}
}

func TestRejectDNSOptionsOutsideProtocolMatrix(t *testing.T) {
	testCases := []struct {
		name     string
		protocol string
		option   string
		apply    func(*DNSProfile)
	}{
		{name: "udp TLS name", protocol: "udp", option: "tls_server_name", apply: func(profile *DNSProfile) { profile.TLSServerName = "dns.example" }},
		{name: "udp insecure", protocol: "udp", option: "insecure", apply: func(profile *DNSProfile) { profile.Insecure = true }},
		{name: "tcp insecure", protocol: "tcp", option: "insecure", apply: func(profile *DNSProfile) { profile.Insecure = true }},
		{name: "udp HTTP path", protocol: "udp", option: "path", apply: func(profile *DNSProfile) { profile.Path = "/dns-query" }},
		{name: "tcp TLS name", protocol: "tcp", option: "tls_server_name", apply: func(profile *DNSProfile) { profile.TLSServerName = "dns.example" }},
		{name: "tcp HTTP path", protocol: "tcp", option: "path", apply: func(profile *DNSProfile) { profile.Path = "/dns-query" }},
		{name: "DoT HTTP path", protocol: "tls", option: "path", apply: func(profile *DNSProfile) { profile.Path = "/dns-query" }},
		{name: "DoQ HTTP path", protocol: "quic", option: "path", apply: func(profile *DNSProfile) { profile.Path = "/dns-query" }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			intent := validIntent()
			profile := &intent.DNSProfiles[0]
			profile.Protocol = testCase.protocol
			profile.TLSServerName = ""
			profile.Path = ""
			profile.Insecure = false
			if oneOf(testCase.protocol, "tls", "quic") {
				profile.TLSServerName = "dns.example"
			}
			testCase.apply(profile)
			validation := Validate(intent)
			if validation.OK || !hasIssueForOption(validation, "UNSUPPORTED_DNS_OPTION", testCase.option) {
				t.Fatalf("unsupported %s option was accepted: %#v", testCase.protocol, validation.Errors)
			}
		})
	}
}

func TestAllSingBox113ProxyOutboundsValidate(t *testing.T) {
	cases := []struct {
		name string
		node Node
	}{
		{"socks", Node{Type: "socks", Server: "proxy.example", ServerPort: 1080}},
		{"http", Node{Type: "http", Server: "proxy.example", ServerPort: 8080}},
		{"shadowsocks", Node{Type: "shadowsocks", Server: "proxy.example", ServerPort: 8388, NodeCredentials: NodeCredentials{Password: "secret"}, NodeProtocol: NodeProtocol{Method: "aes-256-gcm"}}},
		{"vmess", Node{Type: "vmess", Server: "proxy.example", ServerPort: 443, NodeCredentials: NodeCredentials{UUID: "00000000-0000-4000-8000-000000000001"}, NodeProtocol: NodeProtocol{Security: "auto"}}},
		{"hysteria", Node{Type: "hysteria", Server: "proxy.example", ServerPort: 443, NodeCredentials: NodeCredentials{Password: "secret"}, NodeProtocol: NodeProtocol{UpMbps: 100, DownMbps: 100}, NodeTLS: NodeTLS{TLSServerName: "proxy.example"}}},
		{"shadowtls", Node{Type: "shadowtls", Server: "proxy.example", ServerPort: 443, NodeCredentials: NodeCredentials{Password: "secret"}, NodeProtocol: NodeProtocol{Version: 3}, NodeTLS: NodeTLS{TLSServerName: "proxy.example"}}},
		{"tuic", Node{Type: "tuic", Server: "proxy.example", ServerPort: 443, NodeCredentials: NodeCredentials{UUID: "00000000-0000-4000-8000-000000000002", Password: "secret"}, NodeTLS: NodeTLS{TLSServerName: "proxy.example"}}},
		{"anytls", Node{Type: "anytls", Server: "proxy.example", ServerPort: 443, NodeCredentials: NodeCredentials{Password: "secret"}, NodeTLS: NodeTLS{TLSServerName: "proxy.example"}}},
		{"naive", Node{Type: "naive", Server: "proxy.example", ServerPort: 443, NodeCredentials: NodeCredentials{Username: "user", Password: "secret"}, NodeTLS: NodeTLS{TLSServerName: "proxy.example"}}},
		{"ssh", Node{Type: "ssh", Server: "proxy.example", ServerPort: 22, NodeCredentials: NodeCredentials{Username: "root", Password: "secret"}}},
		{"tor", Node{Type: "tor", NodeProtocol: NodeProtocol{ExecutablePath: "/usr/bin/tor"}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			intent := validIntent()
			testCase.node.ID, testCase.node.Enabled = "proxy", true
			intent.Nodes[0] = testCase.node
			if validation := Validate(intent); !validation.OK {
				t.Fatalf("%s rejected: %#v", testCase.name, validation.Errors)
			}
		})
	}
}

func TestTUICAllowsEmptyPasswordAndValidatesALPN(t *testing.T) {
	intent := validIntent()
	intent.Nodes[0] = Node{ID: "proxy", Enabled: true, Type: "tuic", Server: "proxy.example", ServerPort: 443,
		NodeCredentials: NodeCredentials{UUID: "00000000-0000-4000-8000-000000000001"},
		NodeTLS:         NodeTLS{TLSServerName: "proxy.example", ALPN: []string{"h3", "h2"}}}
	if validation := Validate(intent); !validation.OK {
		t.Fatalf("TUIC with empty password and ALPN was rejected: %#v", validation.Errors)
	}
	for _, alpn := range [][]string{{""}, {"h3,h2"}, {strings.Repeat("a", 256)}} {
		intent.Nodes[0].ALPN = alpn
		validation := Validate(intent)
		if validation.OK || !hasIssueForOption(validation, "INVALID_ALPN", "alpn") {
			t.Fatalf("invalid ALPN was accepted: %#v", validation)
		}
	}
}

func TestRejectVMessTransportInOutboundNetwork(t *testing.T) {
	intent := validIntent()
	intent.Nodes[0] = Node{ID: "proxy", Enabled: true, Type: "vmess", Server: "proxy.example", ServerPort: 443,
		NodeCredentials: NodeCredentials{UUID: "00000000-0000-4000-8000-000000000001"},
		NodeTransport:   NodeTransport{Network: "ws", Transport: "ws", TransportPath: "/ws"},
		NodeProtocol:    NodeProtocol{Security: "auto"}}
	validation := Validate(intent)
	if validation.OK || !hasIssue(validation, "INVALID_NODE_NETWORK") {
		t.Fatalf("VMess transport leaked into outbound network without rejection: %#v", validation.Errors)
	}
}

func TestRejectVLESSURISecurityLeakingIntoCanonicalModel(t *testing.T) {
	intent := validIntent()
	intent.Nodes[0] = Node{ID: "proxy", Enabled: true, Type: "vless", Server: "proxy.example", ServerPort: 443,
		NodeCredentials: NodeCredentials{UUID: "00000000-0000-4000-8000-000000000001"},
		NodeProtocol:    NodeProtocol{Security: "tls"},
		NodeTLS:         NodeTLS{TLSServerName: "proxy.example"}}
	validation := Validate(intent)
	if validation.OK || !hasIssue(validation, "UNSUPPORTED_NODE_OPTION") {
		t.Fatalf("VLESS URI security leaked into canonical model: %#v", validation.Errors)
	}
}

func TestValidateVLESSRealityRequiresCompleteFields(t *testing.T) {
	tests := []struct {
		name           string
		tls            NodeTLS
		missingOptions []string
	}{
		{name: "unconfigured"},
		{name: "ordinary TLS", tls: NodeTLS{TLSServerName: "proxy.example"}},
		{name: "public key only", tls: NodeTLS{RealityPublicKey: "public-key"}, missingOptions: []string{"tls_server_name", "reality_short_id"}},
		{name: "short ID only", tls: NodeTLS{RealityShortID: "0123456789abcdef"}, missingOptions: []string{"tls_server_name", "reality_public_key"}},
		{name: "TLS name and public key", tls: NodeTLS{TLSServerName: "proxy.example", RealityPublicKey: "public-key"}, missingOptions: []string{"reality_short_id"}},
		{name: "TLS name and short ID", tls: NodeTLS{TLSServerName: "proxy.example", RealityShortID: "0123456789abcdef"}, missingOptions: []string{"reality_public_key"}},
		{name: "public key and short ID", tls: NodeTLS{RealityPublicKey: "public-key", RealityShortID: "0123456789abcdef"}, missingOptions: []string{"tls_server_name"}},
		{name: "complete", tls: NodeTLS{TLSServerName: "proxy.example", RealityPublicKey: "public-key", RealityShortID: "0123456789abcdef"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := validIntent().Nodes[0]
			node.NodeTLS = test.tls
			validation := ValidateNode(node)
			var missingOptions []string
			for _, issue := range validation.Errors {
				if issue.Code == "INCOMPLETE_REALITY" {
					if issue.ObjectType != "node" || issue.ObjectID != node.ID {
						t.Fatalf("Reality error has the wrong location: %#v", issue)
					}
					missingOptions = append(missingOptions, issue.Option)
				}
			}
			if !reflect.DeepEqual(missingOptions, test.missingOptions) {
				t.Fatalf("unexpected missing Reality fields: got %v, want %v; errors=%#v", missingOptions, test.missingOptions, validation.Errors)
			}
			if validation.OK != (len(test.missingOptions) == 0) {
				t.Fatalf("unexpected validation result: %#v", validation)
			}
		})
	}
}

func TestValidateDefaultVLESSTransportSemantics(t *testing.T) {
	for _, transport := range []string{"", "tcp", "raw"} {
		t.Run("transport_"+transport, func(t *testing.T) {
			node := validIntent().Nodes[0]
			node.Transport = transport
			node.NodeTLS = NodeTLS{
				TLSServerName: "proxy.example", RealityPublicKey: "public-key",
				RealityShortID: "0123456789abcdef",
			}
			if validation := ValidateNode(node); !validation.OK {
				t.Fatalf("default TCP / Raw transport was rejected: %#v", validation.Errors)
			}
		})
	}

	for _, test := range []struct {
		transport string
		path      string
		service   string
		code      string
	}{
		{transport: "unsupported", code: "UNSUPPORTED_TRANSPORT"},
		{transport: "ws", code: "REQUIRED"},
		{transport: "grpc", code: "REQUIRED"},
	} {
		node := validIntent().Nodes[0]
		node.Transport, node.TransportPath, node.ServiceName = test.transport, test.path, test.service
		validation := ValidateNode(node)
		if validation.OK || !hasIssue(validation, test.code) {
			t.Fatalf("%s transport lost its validation guard: %#v", test.transport, validation.Errors)
		}
	}
}

func TestPinnedStaleSubscriptionNodeWarns(t *testing.T) {
	intent := validIntent()
	intent.Nodes[0].PinnedStale = true
	validation := Validate(intent)
	if !validation.OK || !hasWarning(validation, "SUBSCRIPTION_NODE_STALE") {
		t.Fatalf("stale subscription node must remain usable but warn: %#v", validation)
	}
}

func TestRuntimeWarningsOnlyCoverReachableEntities(t *testing.T) {
	t.Run("unused insecure node is filtered", func(t *testing.T) {
		value := validIntent()
		value.Nodes = append(value.Nodes, Node{
			ID: "unused", Enabled: true, Type: "vless", Server: "unused.example", ServerPort: 443,
			NodeCredentials: NodeCredentials{UUID: "00000000-0000-4000-8000-000000000002"},
			NodeTLS:         NodeTLS{Insecure: true},
		})
		validation := Validate(value)
		if warningForObject(validation, "INSECURE_TLS", "unused") {
			t.Fatalf("unused node warning entered the runtime result: %#v", validation.Warnings)
		}
	})

	t.Run("rule route and detour keep insecure nodes", func(t *testing.T) {
		value := validIntent()
		value.Nodes[0].Insecure = true
		value.Nodes = append(value.Nodes, Node{
			ID: "front_node", Enabled: true, Type: "vless", Server: "front.example", ServerPort: 443,
			NodeCredentials: NodeCredentials{UUID: "00000000-0000-4000-8000-000000000002"},
			NodeTLS:         NodeTLS{Insecure: true},
		})
		value.Routes = append(value.Routes, Route{ID: "front", Enabled: true, Kind: "single", Node: "front_node"})
		value.Routes[1].Detour = "front"
		validation := Validate(value)
		for _, id := range []string{"proxy", "front_node"} {
			if !warningForObject(validation, "INSECURE_TLS", id) {
				t.Fatalf("reachable node %q lost its warning: %#v", id, validation.Warnings)
			}
		}
		if len(validation.WarningGroups) != 1 || validation.WarningGroups[0].Count != 2 || validation.WarningGroups[0].Destination != "nodes" {
			t.Fatalf("reachable node warnings were not safely grouped: %#v", validation.WarningGroups)
		}
	})

	t.Run("unused insecure DNS profile is filtered", func(t *testing.T) {
		value := validIntent()
		value.DNSProfiles = append(value.DNSProfiles, DNSProfile{
			ID: "unused_dns", Enabled: true, Protocol: "https", Server: "dns.example", ServerPort: 443,
			TLSServerName: "dns.example", Path: "/dns-query", Insecure: true,
		})
		validation := Validate(value)
		if warningForObject(validation, "INSECURE_TLS", "unused_dns") {
			t.Fatalf("unused DNS warning entered the runtime result: %#v", validation.Warnings)
		}
		value.Rules[0].DNSProfile = "unused_dns"
		validation = Validate(value)
		if !warningForObject(validation, "INSECURE_TLS", "unused_dns") {
			t.Fatalf("reachable DNS warning was filtered: %#v", validation.Warnings)
		}
	})

	t.Run("disabled Steer has no entity runtime warnings", func(t *testing.T) {
		value := validIntent()
		value.Main.Enabled = false
		value.Nodes[0].Insecure = true
		value.DNSProfiles[0].Insecure = true
		validation := Validate(value)
		if len(validation.Warnings) != 0 || len(validation.WarningGroups) != 0 {
			t.Fatalf("disabled Steer exposed runtime warnings: %#v", validation)
		}
	})

	t.Run("strict errors are unchanged for unreachable entities", func(t *testing.T) {
		value := validIntent()
		value.Nodes = append(value.Nodes, Node{ID: "unused", Enabled: true, Type: "vless", Server: "bad host", ServerPort: 0})
		validation := Validate(value)
		if validation.OK || !hasIssue(validation, "INVALID_SERVER") || !hasIssue(validation, "INVALID_PORT") || !hasIssue(validation, "REQUIRED") {
			t.Fatalf("unreachable entity errors were weakened: %#v", validation.Errors)
		}
	})
}

func TestWarningGroupsAreStableBoundedAndCountEntities(t *testing.T) {
	warnings := []Issue{
		{Code: "INSECURE_TLS", ObjectType: "node", ObjectID: "b", Option: "insecure", Message: "raw-b"},
		{Code: "INSECURE_TLS", ObjectType: "node", ObjectID: "a", Option: "insecure", Message: "raw-a"},
		{Code: "INSECURE_TLS", ObjectType: "node", ObjectID: "a", Option: "insecure", Message: "duplicate"},
		{Code: "INSECURE_TLS", ObjectType: "dns_profile", ObjectID: "dns", Option: "insecure", Message: "raw-dns"},
	}
	groups := GroupWarnings(warnings)
	if len(groups) != 2 {
		t.Fatalf("warning groups are unbounded: %#v", groups)
	}
	if groups[0].ObjectType != "dns_profile" || groups[1].ObjectType != "node" || groups[1].Count != 2 {
		t.Fatalf("warning groups are unstable or count duplicate issues: %#v", groups)
	}
	encoded, err := json.Marshal(groups)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"raw-a", "raw-b", "raw-dns", "duplicate", `"object_id"`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("warning group leaked raw detail %q: %s", forbidden, encoded)
		}
	}
}

func TestEveryCurrentWarningCodeHasAProductSummary(t *testing.T) {
	for _, warning := range []Issue{
		{Code: "INSECURE_TLS", ObjectType: "node", Option: "insecure"},
		{Code: "INSECURE_TLS", ObjectType: "dns_profile", Option: "insecure"},
		{Code: "SUBSCRIPTION_NODE_STALE", ObjectType: "node", Option: "pinned_stale"},
		{Code: "DNS_REJECT_PROJECTION_SKIPPED", ObjectType: "rule", Option: "route"},
		{Code: "DNS_PROJECTION_EMPTY", ObjectType: "rule", Option: "dns_profile"},
	} {
		groups := GroupWarnings([]Issue{warning})
		if len(groups) != 1 || groups[0].Summary == "Configuration warning" || groups[0].Destination == "" {
			t.Fatalf("warning %s/%s lacks a product summary: %#v", warning.Code, warning.ObjectType, groups)
		}
	}
}

func TestValidateWarnsWhenDNSRejectProjectionWouldWiden(t *testing.T) {
	intent := validIntent()
	conditionalBlock := Rule{
		ID: "conditional_block", Enabled: true, DNSProfile: "dns", Route: "block",
		DomainMatch: []string{"domain:example.com"},
		IPMatch:     []string{"geoip:private"},
		Network:     []string{"tcp"},
		Protocol:    []string{"tls"},
		Port:        []int{443},
	}
	intent.Rules = append(intent.Rules[:len(intent.Rules)-1], conditionalBlock, intent.Rules[len(intent.Rules)-1])
	validation := Validate(intent)
	if !validation.OK {
		t.Fatalf("conditional block rule was rejected: %#v", validation.Errors)
	}
	var projectionWarning *Issue
	for index := range validation.Warnings {
		if validation.Warnings[index].Code == "DNS_REJECT_PROJECTION_SKIPPED" {
			projectionWarning = &validation.Warnings[index]
			break
		}
	}
	if projectionWarning == nil {
		t.Fatalf("missing skipped DNS reject warning: %#v", validation.Warnings)
	}
	for _, detail := range []string{"ip_match", "network", "protocol", "port", "DNS queries continue to subsequent rules"} {
		if !strings.Contains(projectionWarning.Message, detail) {
			t.Fatalf("warning does not explain %q: %#v", detail, projectionWarning)
		}
	}
}

func hasIssue(validation Validation, code string) bool {
	for _, issue := range validation.Errors {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func hasIssueForOption(validation Validation, code, option string) bool {
	for _, issue := range validation.Errors {
		if issue.Code == code && issue.Option == option {
			return true
		}
	}
	return false
}

func hasWarning(validation Validation, code string) bool {
	for _, issue := range validation.Warnings {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func warningForObject(validation Validation, code, objectID string) bool {
	for _, issue := range validation.Warnings {
		if issue.Code == code && issue.ObjectID == objectID {
			return true
		}
	}
	return false
}

func TestValidateDirectBypassMode(t *testing.T) {
	for _, mode := range []string{"", "off", "static", "dns", "always"} {
		value := validIntent()
		value.Main.DirectBypass = mode
		result := Validate(value)
		found := false
		for _, issue := range result.Errors {
			if issue.Code == "INVALID_DIRECT_BYPASS" {
				found = true
			}
		}
		if found != (mode == "always") {
			t.Fatalf("mode %q: %#v", mode, result.Errors)
		}
	}
}
