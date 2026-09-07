// SPDX-License-Identifier: GPL-3.0-or-later

// Package uispec is the build-time source of truth for fields rendered by the
// three native Steer frontends. It contains user semantics only: transports,
// platform paths and widget layout remain owned by the platform frontends.
package uispec

//go:generate go run ../../cmd/steer-ui-spec --root ../../..

const SchemaVersion = 1

type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// DNSProtocol describes the protocol-dependent DNS Profile fields shared by
// every frontend. Fields contains only conditional options; server and port
// remain common to every protocol.
type DNSProtocol struct {
	Value          string   `json:"value"`
	Label          string   `json:"label"`
	Fields         []string `json:"fields"`
	RequiredFields []string `json:"required_fields"`
	DefaultPort    int      `json:"default_port"`
}

type Condition struct {
	Field  string   `json:"field"`
	Values []string `json:"values"`
}

// InputFormat carries only lightweight, high-frequency form constraints.
// Canonical validation remains authoritative for every frontend.
type InputFormat struct {
	Kind              string   `json:"kind"`
	Schemes           []string `json:"schemes,omitempty"`
	Absolute          bool     `json:"absolute,omitempty"`
	ForbidCredentials bool     `json:"forbid_credentials,omitempty"`
	ForbidFragment    bool     `json:"forbid_fragment,omitempty"`
	Positive          bool     `json:"positive,omitempty"`
	Prefix            string   `json:"prefix,omitempty"`
	Pattern           string   `json:"pattern,omitempty"`
}

type Field struct {
	Key           string      `json:"key"`
	Label         string      `json:"label"`
	Control       string      `json:"control"`
	Section       string      `json:"section"`
	Types         []string    `json:"types,omitempty"`
	RequiredTypes []string    `json:"required_types,omitempty"`
	Options       []Choice    `json:"options,omitempty"`
	When          *Condition  `json:"when,omitempty"`
	Sensitive     bool        `json:"sensitive,omitempty"`
	Multiline     bool        `json:"multiline,omitempty"`
	Placeholder   string      `json:"placeholder,omitempty"`
	Default       interface{} `json:"default,omitempty"`
}

type PlatformCapabilities struct {
	RawEditor        bool   `json:"raw_editor"`
	SourceMAC        bool   `json:"source_mac"`
	SourceMACReason  string `json:"source_mac_reason,omitempty"`
	SystemComponents bool   `json:"system_components"`
}

// CollectionReference describes one inbound reference that must block
// deleting the target object in every frontend.
type CollectionReference struct {
	TargetCollection string `json:"target_collection"`
	SourceCollection string `json:"source_collection"`
	SourceObjectType string `json:"source_object_type"`
	Field            string `json:"field"`
	Multiple         bool   `json:"multiple,omitempty"`
}

// CollectionOrderingPolicy defines the shared user-visible ordering boundary.
// Frontends may use native controls, but every move is draft-only, identifies
// objects by stable ID and must stay within the declared group/filter.
type CollectionOrderingPolicy struct {
	StableIDField          string   `json:"stable_id_field"`
	MoveActions            []string `json:"move_actions"`
	GroupField             string   `json:"group_field,omitempty"`
	MovableKinds           []string `json:"movable_kinds,omitempty"`
	PinnedLastBooleanField string   `json:"pinned_last_boolean_field,omitempty"`
	SourceOwnedRefresh     string   `json:"source_owned_refresh,omitempty"`
}

type NavigationItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type NavigationGroup struct {
	Key   string           `json:"key"`
	Label string           `json:"label"`
	Items []NavigationItem `json:"items"`
}

type IDPolicy struct {
	Pattern            string            `json:"pattern"`
	MaxLength          int               `json:"max_length"`
	AutoGenerate       bool              `json:"auto_generate"`
	CollectionPrefixes map[string]string `json:"collection_prefixes"`
}

type DNSBoundary struct {
	CaptureMode          string   `json:"capture_mode"`
	CaptureScope         string   `json:"capture_scope"`
	Exclusions           []string `json:"exclusions"`
	BootstrapBoundary    string   `json:"bootstrap_boundary"`
	EncryptedDNSBoundary string   `json:"encrypted_dns_boundary"`
	DiagnosticBoundary   string   `json:"diagnostic_boundary"`
}

type SubscriptionInventoryContract struct {
	ChangesActiveGeneration bool   `json:"changes_active_generation"`
	UnreferencedNodes       string `json:"unreferenced_nodes"`
	StaleReferencedNodes    string `json:"stale_referenced_nodes"`
	Notice                  string `json:"notice"`
}

type NodeDisplaySortingContract struct {
	Modes             []string `json:"modes"`
	DirectionModes    []string `json:"direction_modes"`
	DefaultDirection  string   `json:"default_direction"`
	ConnectDirection  string   `json:"connect_direction"`
	DownloadDirection string   `json:"download_direction"`
}

type Contract struct {
	SchemaVersion                     int                                 `json:"schema_version"`
	CanonicalSchema                   int                                 `json:"canonical_schema"`
	SubscriptionUpdateIntervalDefault string                              `json:"subscription_update_interval_default"`
	IDPolicy                          IDPolicy                            `json:"id_policy"`
	CreationDefaults                  map[string]map[string]interface{}   `json:"creation_defaults"`
	CreationRequiredFields            map[string][]string                 `json:"creation_required_fields"`
	InputFormats                      map[string]InputFormat              `json:"input_formats"`
	NodeTypes                         []Choice                            `json:"node_types"`
	NodeFields                        []Field                             `json:"node_fields"`
	LogLevels                         []Choice                            `json:"log_levels"`
	BootstrapProtocols                []Choice                            `json:"bootstrap_protocols"`
	BootstrapStrategies               []Choice                            `json:"bootstrap_strategies"`
	RouteKinds                        []Choice                            `json:"route_kinds"`
	DNSProtocols                      []DNSProtocol                       `json:"dns_protocols"`
	LocalProxyProtocols               []Choice                            `json:"local_proxy_protocols"`
	RuleNetworks                      []Choice                            `json:"rule_networks"`
	RuleProtocols                     []Choice                            `json:"rule_protocols"`
	RuleMatchFields                   []string                            `json:"rule_match_fields"`
	RuleConnectionOnlyFields          []string                            `json:"rule_connection_only_fields"`
	CollectionReferences              []CollectionReference               `json:"collection_references"`
	CollectionOrdering                map[string]CollectionOrderingPolicy `json:"collection_ordering"`
	DomainPrefixes                    []string                            `json:"domain_prefixes"`
	IPPrefixes                        []string                            `json:"ip_prefixes"`
	PlatformCapabilities              map[string]PlatformCapabilities     `json:"platform_capabilities"`
	Navigation                        []NavigationGroup                   `json:"navigation"`
	DNSBoundaries                     map[string]DNSBoundary              `json:"dns_boundaries"`
	SubscriptionInventory             SubscriptionInventoryContract       `json:"subscription_inventory"`
	NodeDisplaySorting                NodeDisplaySortingContract          `json:"node_display_sorting"`
}

func choices(values ...string) []Choice {
	result := make([]Choice, 0, len(values)/2)
	for index := 0; index+1 < len(values); index += 2 {
		result = append(result, Choice{Value: values[index], Label: values[index+1]})
	}
	return result
}

func allExcept(excluded ...string) []string {
	skip := map[string]bool{}
	for _, value := range excluded {
		skip[value] = true
	}
	result := []string{}
	for _, value := range nodeTypeValues() {
		if !skip[value] {
			result = append(result, value)
		}
	}
	return result
}

func nodeTypeValues() []string {
	return []string{"socks", "http", "shadowsocks", "vmess", "vless", "trojan", "hysteria", "hysteria2", "shadowtls", "tuic", "anytls", "naive", "ssh", "tor"}
}

func stringField(key, label, section string, types ...string) Field {
	return Field{Key: key, Label: label, Control: "text", Section: section, Types: types}
}

func ContractValue() Contract {
	allRemote := allExcept("tor")
	tlsTypes := []string{"http", "vmess", "hysteria", "vless", "hysteria2", "trojan", "shadowtls", "tuic", "anytls", "naive"}
	transportTypes := []string{"vmess", "vless", "trojan"}
	contract := Contract{
		SchemaVersion:                     SchemaVersion,
		CanonicalSchema:                   9,
		SubscriptionUpdateIntervalDefault: "6h",
		IDPolicy: IDPolicy{
			Pattern: `^[a-z][a-z0-9_-]{0,31}$`, MaxLength: 32, AutoGenerate: true,
			CollectionPrefixes: map[string]string{
				"nodes": "node", "routes": "route", "dns_profiles": "dns", "local_proxies": "proxy",
				"rules": "rule", "subscriptions": "subscription",
			},
		},
		CreationDefaults: map[string]map[string]interface{}{
			"main":          {"schema_version": 9, "enabled": false, "log_level": "warn"},
			"bootstrap":     {"protocol": "udp", "server": "1.1.1.1", "server_port": 53, "strategy": "prefer_ipv4"},
			"nodes":         {"enabled": true, "type": "socks", "server": "", "server_port": 1080},
			"routes":        {"enabled": true, "kind": "single", "node": ""},
			"dns_profiles":  {"enabled": true, "protocol": "udp", "server": "", "server_port": 53},
			"local_proxies": {"enabled": true, "protocol": "mixed", "listen": "127.0.0.1", "listen_port": 1090},
			"rules":         {"enabled": true, "default": false, "dns_profile": "", "route": "direct"},
			"subscriptions": {"enabled": true, "url": "", "update_interval": "6h"},
		},
		CreationRequiredFields: map[string][]string{
			"main":          {"schema_version", "enabled", "log_level"},
			"bootstrap":     {"protocol", "server", "server_port", "strategy"},
			"nodes":         {"id", "enabled", "type", "server", "server_port"},
			"routes":        {"id", "enabled", "kind", "node"},
			"dns_profiles":  {"id", "enabled", "protocol", "server", "server_port"},
			"local_proxies": {"id", "enabled", "protocol", "listen", "listen_port"},
			"rules":         {"id", "enabled", "default", "dns_profile", "route"},
			"subscriptions": {"id", "enabled", "url", "update_interval"},
		},
		CollectionOrdering: map[string]CollectionOrderingPolicy{
			"nodes": {
				StableIDField: "id", MoveActions: []string{"up", "down"}, GroupField: "source_subscription",
				SourceOwnedRefresh: "subscription_refresh_may_rebuild_source_order",
			},
			"routes":        {StableIDField: "id", MoveActions: []string{"up", "down"}, MovableKinds: []string{"single"}},
			"dns_profiles":  {StableIDField: "id", MoveActions: []string{"up", "down"}},
			"local_proxies": {StableIDField: "id", MoveActions: []string{"up", "down"}},
			"rules": {
				StableIDField: "id", MoveActions: []string{"up", "down"}, PinnedLastBooleanField: "default",
			},
			"subscriptions": {StableIDField: "id", MoveActions: []string{"up", "down"}},
		},
		InputFormats: map[string]InputFormat{
			"probe_url":         {Kind: "url", Schemes: []string{"https"}, Absolute: true, ForbidCredentials: true, ForbidFragment: true},
			"subscription_url":  {Kind: "url", Schemes: []string{"http", "https"}, Absolute: true},
			"positive_duration": {Kind: "duration", Positive: true, Pattern: `^[1-9][0-9]*(ms|s|m|h)$`},
			"dns_http_path":     {Kind: "string", Prefix: "/"},
		},
		NodeTypes: choices(
			"socks", "SOCKS", "http", "HTTP CONNECT", "shadowsocks", "Shadowsocks",
			"vmess", "VMess", "vless", "VLESS", "trojan", "Trojan",
			"hysteria", "Hysteria", "hysteria2", "Hysteria2", "shadowtls", "ShadowTLS",
			"tuic", "TUIC", "anytls", "AnyTLS", "naive", "NaiveProxy", "ssh", "SSH", "tor", "Tor",
		),
		LogLevels:           choices("error", "Error", "warn", "Warning", "info", "Info", "debug", "Debug"),
		BootstrapProtocols:  choices("udp", "UDP", "tcp", "TCP"),
		BootstrapStrategies: choices("prefer_ipv4", "Prefer IPv4", "prefer_ipv6", "Prefer IPv6", "ipv4_only", "IPv4 only", "ipv6_only", "IPv6 only"),
		RouteKinds:          choices("direct", "Direct", "block", "Reject", "single", "Single node"),
		DNSProtocols: []DNSProtocol{
			{Value: "udp", Label: "UDP", Fields: []string{}, RequiredFields: []string{}, DefaultPort: 53},
			{Value: "tcp", Label: "TCP", Fields: []string{}, RequiredFields: []string{}, DefaultPort: 53},
			{Value: "tls", Label: "DoT", Fields: []string{"tls_server_name", "insecure"}, RequiredFields: []string{"tls_server_name"}, DefaultPort: 853},
			{Value: "https", Label: "DoH", Fields: []string{"tls_server_name", "path", "insecure"}, RequiredFields: []string{"tls_server_name"}, DefaultPort: 443},
			{Value: "quic", Label: "DoQ", Fields: []string{"tls_server_name", "insecure"}, RequiredFields: []string{"tls_server_name"}, DefaultPort: 853},
			{Value: "h3", Label: "DoH3", Fields: []string{"tls_server_name", "path", "insecure"}, RequiredFields: []string{"tls_server_name"}, DefaultPort: 443},
		},
		LocalProxyProtocols: choices("mixed", "Mixed (SOCKS + HTTP)", "socks", "SOCKS", "http", "HTTP CONNECT"),
		RuleNetworks:        choices("tcp", "TCP", "udp", "UDP"),
		RuleProtocols: choices(
			"tls", "TLS", "http", "HTTP", "quic", "QUIC", "dns", "DNS", "stun", "STUN",
			"bittorrent", "BitTorrent", "dtls", "DTLS", "ssh", "SSH", "rdp", "RDP", "ntp", "NTP",
		),
		RuleMatchFields:          []string{"inbound", "domain_match", "ip_match", "source_ip_cidr", "source_mac_address", "network", "protocol", "port"},
		RuleConnectionOnlyFields: []string{"ip_match", "network", "protocol", "port"},
		CollectionReferences: []CollectionReference{
			{TargetCollection: "nodes", SourceCollection: "routes", SourceObjectType: "route", Field: "node"},
			{TargetCollection: "routes", SourceCollection: "rules", SourceObjectType: "rule", Field: "route"},
			{TargetCollection: "routes", SourceCollection: "routes", SourceObjectType: "route", Field: "detour"},
			{TargetCollection: "dns_profiles", SourceCollection: "rules", SourceObjectType: "rule", Field: "dns_profile"},
			{TargetCollection: "local_proxies", SourceCollection: "rules", SourceObjectType: "rule", Field: "inbound", Multiple: true},
		},
		DomainPrefixes: []string{"full:", "domain:", "regexp:", "geosite:"},
		IPPrefixes:     []string{"geoip:"},
		PlatformCapabilities: map[string]PlatformCapabilities{
			"openwrt": {RawEditor: false, SourceMAC: true, SystemComponents: false},
			"linux":   {RawEditor: true, SourceMAC: true, SystemComponents: false},
			"macos":   {RawEditor: true, SourceMAC: false, SourceMACReason: "macOS utun traffic does not expose the original LAN neighbor MAC address", SystemComponents: true},
		},
		Navigation: []NavigationGroup{
			{Key: "status", Label: "Status", Items: []NavigationItem{{Key: "overview", Label: "Overview"}}},
			{Key: "configuration", Label: "Configuration", Items: []NavigationItem{
				{Key: "general", Label: "General"}, {Key: "nodes", Label: "Nodes"}, {Key: "routes", Label: "Routes"},
				{Key: "dns", Label: "DNS Profiles"}, {Key: "proxies", Label: "Local Proxies"}, {Key: "rules", Label: "Rules"},
			}},
			{Key: "services", Label: "Services", Items: []NavigationItem{
				{Key: "subscriptions", Label: "Subscriptions"}, {Key: "diagnostics", Label: "Diagnostics"}, {Key: "system", Label: "System"},
			}},
			{Key: "advanced", Label: "Advanced", Items: []NavigationItem{{Key: "advanced", Label: "Advanced Configuration"}}},
		},
		DNSBoundaries: map[string]DNSBoundary{
			"linux": dnsBoundary(
				"native_hijack_with_local_shim",
				"sing-box handles host and forwarded port-53 DNS; a dedicated shim covers host and LAN queries addressed to this host.",
				[]string{"TUN routing excludes non-global destinations", "marked Steer DNS and TUN traffic is excluded from recapture"},
			),
			"openwrt": dnsBoundary(
				"native_hijack_with_local_shim",
				"sing-box handles router and LAN port-53 DNS; a dedicated shim covers queries addressed to the router itself.",
				[]string{"TUN routing excludes non-global destinations", "marked Steer DNS and TUN traffic is excluded from recapture"},
			),
			"macos": dnsBoundary(
				"tun_port53_hijack",
				"Steer manages physical network services’ system DNS and restores original settings on stop. Additional port-53 interception applies only to traffic entering TUN.",
				[]string{"loopback", "link-local", "multicast", "document and reserved special-use ranges"},
			),
		},
		SubscriptionInventory: SubscriptionInventoryContract{
			ChangesActiveGeneration: false,
			UnreferencedNodes:       "removed",
			StaleReferencedNodes:    "preserved",
			Notice:                  "Subscription inventory updated; current Active configuration was not changed. Removed unreferenced nodes are deleted automatically, while nodes still referenced by Routes are preserved as stale.",
		},
		NodeDisplaySorting: NodeDisplaySortingContract{
			Modes:             []string{"default", "connect", "download"},
			DirectionModes:    []string{"best_first", "worst_first"},
			DefaultDirection:  "best_first",
			ConnectDirection:  "ascending",
			DownloadDirection: "descending",
		},
	}

	contract.NodeFields = []Field{
		{Key: "enabled", Label: "Enabled", Control: "boolean", Section: "general", Types: nodeTypeValues(), Default: true},
		{Key: "name", Label: "Name", Control: "text", Section: "general", Types: nodeTypeValues()},
		{Key: "server", Label: "Server", Control: "text", Section: "general", Types: allRemote, RequiredTypes: allRemote, Placeholder: "example.com or IP"},
		{Key: "server_port", Label: "Port", Control: "integer", Section: "general", Types: allRemote, RequiredTypes: allRemote, Default: 443},
		stringField("uuid", "UUID", "protocol", "vmess", "vless", "tuic"),
		stringField("username", "Username", "protocol", "socks", "http", "naive", "ssh"),
		{Key: "password", Label: "Password", Control: "password", Section: "protocol", Types: []string{"socks", "http", "shadowsocks", "trojan", "hysteria", "hysteria2", "shadowtls", "tuic", "anytls", "naive", "ssh"}, RequiredTypes: []string{"shadowsocks", "trojan", "hysteria", "hysteria2", "tuic", "anytls", "naive"}, Sensitive: true},
		{Key: "method", Label: "Method", Control: "text", Section: "protocol", Types: []string{"shadowsocks"}, RequiredTypes: []string{"shadowsocks"}, Placeholder: "2022-blake3-aes-128-gcm"},
		{Key: "plugin", Label: "Plugin", Control: "select", Section: "protocol", Types: []string{"shadowsocks"}, Options: choices("", "None", "obfs-local", "obfs-local", "v2ray-plugin", "v2ray-plugin")},
		stringField("plugin_options", "Plugin options", "protocol", "shadowsocks"),
		{Key: "security", Label: "Security", Control: "select", Section: "protocol", Types: []string{"vmess"}, Options: choices("", "Default", "auto", "auto", "none", "none", "zero", "zero", "aes-128-gcm", "aes-128-gcm", "chacha20-poly1305", "chacha20-poly1305", "aes-128-ctr", "aes-128-ctr")},
		{Key: "alter_id", Label: "Alter ID", Control: "integer", Section: "protocol", Types: []string{"vmess"}},
		{Key: "network", Label: "Network", Control: "select", Section: "protocol", Types: []string{"vmess"}, Options: choices("", "Default", "tcp", "TCP", "udp", "UDP")},
		{Key: "packet_encoding", Label: "UDP packet encoding", Control: "select", Section: "protocol", Types: []string{"vmess", "vless"}, Options: choices("", "Default", "xudp", "XUDP", "packetaddr", "PacketAddr")},
		{Key: "flow", Label: "Flow", Control: "select", Section: "protocol", Types: []string{"vless"}, Options: choices("", "None", "xtls-rprx-vision", "XTLS Vision")},
		{Key: "transport", Label: "Transport", Control: "select", Section: "transport", Types: transportTypes, Options: choices("tcp", "TCP / Raw", "ws", "WebSocket", "grpc", "gRPC", "http", "HTTP", "quic", "QUIC"), Default: "tcp"},
		{Key: "transport_path", Label: "Transport path", Control: "text", Section: "transport", Types: transportTypes, When: &Condition{Field: "transport", Values: []string{"ws", "http"}}, Placeholder: "/path"},
		{Key: "transport_host", Label: "Transport host", Control: "text", Section: "transport", Types: transportTypes, When: &Condition{Field: "transport", Values: []string{"ws", "http"}}, Placeholder: "example.com"},
		{Key: "service_name", Label: "gRPC service name", Control: "text", Section: "transport", Types: transportTypes, When: &Condition{Field: "transport", Values: []string{"grpc"}}},
		{Key: "server_ports", Label: "Port hopping ranges", Control: "string-list", Section: "protocol", Types: []string{"hysteria", "hysteria2"}, Placeholder: "20000:21000"},
		{Key: "hop_interval", Label: "Port hopping interval", Control: "text", Section: "protocol", Types: []string{"hysteria", "hysteria2"}, Placeholder: "30s"},
		{Key: "obfs_type", Label: "Obfuscation", Control: "select", Section: "protocol", Types: []string{"hysteria2"}, Options: choices("", "None", "salamander", "Salamander")},
		{Key: "obfs_password", Label: "Obfuscation password", Control: "password", Section: "protocol", Types: []string{"hysteria", "hysteria2"}, Sensitive: true},
		{Key: "up_mbps", Label: "Upload Mbps", Control: "integer", Section: "protocol", Types: []string{"hysteria", "hysteria2"}, RequiredTypes: []string{"hysteria"}},
		{Key: "down_mbps", Label: "Download Mbps", Control: "integer", Section: "protocol", Types: []string{"hysteria", "hysteria2"}, RequiredTypes: []string{"hysteria"}},
		{Key: "version", Label: "Version", Control: "select-integer", Section: "protocol", Types: []string{"shadowtls"}, Options: choices("1", "1", "2", "2", "3", "3"), Default: 3},
		{Key: "congestion_control", Label: "Congestion control", Control: "select", Section: "protocol", Types: []string{"tuic"}, Options: choices("", "Default", "cubic", "cubic", "new_reno", "new_reno", "bbr", "bbr")},
		{Key: "udp_relay_mode", Label: "UDP relay mode", Control: "select", Section: "protocol", Types: []string{"tuic"}, Options: choices("", "Default", "native", "native", "quic", "quic")},
		{Key: "udp_over_stream", Label: "UDP over stream", Control: "boolean", Section: "protocol", Types: []string{"tuic"}},
		{Key: "zero_rtt_handshake", Label: "0-RTT handshake", Control: "boolean", Section: "advanced", Types: []string{"tuic"}},
		{Key: "heartbeat", Label: "Heartbeat", Control: "text", Section: "advanced", Types: []string{"tuic"}, Placeholder: "10s"},
		{Key: "quic", Label: "QUIC", Control: "boolean", Section: "advanced", Types: []string{"naive"}},
		{Key: "quic_congestion_control", Label: "QUIC congestion control", Control: "select", Section: "advanced", Types: []string{"naive"}, Options: choices("", "Default", "bbr", "bbr", "bbr2", "bbr2", "cubic", "cubic", "reno", "reno")},
		{Key: "insecure_concurrency", Label: "Insecure concurrency", Control: "integer", Section: "advanced", Types: []string{"naive"}},
		{Key: "private_key", Label: "Private key", Control: "password", Section: "protocol", Types: []string{"ssh"}, Sensitive: true, Multiline: true},
		stringField("host_key", "Host key", "protocol", "ssh"),
		{Key: "host_key_algorithms", Label: "Host key algorithms", Control: "string-list", Section: "protocol", Types: []string{"ssh"}, Placeholder: "ssh-ed25519, rsa-sha2-512"},
		stringField("executable_path", "Executable path", "protocol", "tor"),
		{Key: "extra_args", Label: "Extra arguments", Control: "string-list", Section: "protocol", Types: []string{"tor"}},
		stringField("data_directory", "Data directory", "protocol", "tor"),
		{Key: "tls_server_name", Label: "TLS server name", Control: "text", Section: "tls", Types: tlsTypes, RequiredTypes: []string{"hysteria", "hysteria2", "trojan", "shadowtls", "tuic", "anytls", "naive"}, Placeholder: "server.example.com"},
		{Key: "alpn", Label: "ALPN", Control: "string-list", Section: "tls", Types: tlsTypes, Placeholder: "h3"},
		{Key: "utls_fingerprint", Label: "uTLS fingerprint", Control: "select", Section: "tls", Types: tlsTypes, Options: choices("", "System default", "chrome", "chrome", "firefox", "firefox", "safari", "safari", "edge", "edge", "random", "random")},
		{Key: "insecure", Label: "Skip certificate verification", Control: "boolean", Section: "tls", Types: tlsTypes},
		stringField("reality_public_key", "REALITY public key", "tls", "vless"),
		stringField("reality_short_id", "REALITY short ID", "tls", "vless"),
	}

	for index := range contract.NodeFields {
		if contract.NodeFields[index].Key == "uuid" {
			contract.NodeFields[index].RequiredTypes = []string{"vmess", "vless", "tuic"}
		}
		if contract.NodeFields[index].Key == "username" {
			contract.NodeFields[index].RequiredTypes = []string{"ssh"}
		}
	}
	return contract
}

func dnsBoundary(mode, scope string, exclusions []string) DNSBoundary {
	return DNSBoundary{
		CaptureMode: mode, CaptureScope: scope, Exclusions: exclusions,
		BootstrapBoundary:    "Bootstrap resolves infrastructure hostnames such as a DNS upstream hostname. Direct UDP/TCP bootstrap can use plaintext port 53, but it does not carry the original user query name.",
		EncryptedDNSBoundary: "Application-owned DoH, DoT and DoQ is ordinary business traffic. Port-53 capture alone neither identifies nor redirects it; control requires that traffic to enter the captured path and a separately verified block or redirect policy.",
		DiagnosticBoundary:   "The diagnostic verifies port-53 artifacts in the published Active generation; it is not packet observation and does not prove encrypted-DNS blocking or zero leakage.",
	}
}

func (contract Contract) FieldsForNodeType(nodeType string) []Field {
	result := []Field{}
	for _, field := range contract.NodeFields {
		for _, candidate := range field.Types {
			if candidate == nodeType {
				result = append(result, field)
				break
			}
		}
	}
	return result
}
