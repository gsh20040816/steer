// SPDX-License-Identifier: GPL-3.0-or-later

// Package model defines Steer's platform-neutral canonical intent.
package intent

const SchemaVersion = 9

type Intent struct {
	WireGuardTunnels []WireGuardTunnel `json:"wireguard_tunnels,omitempty"`
	Main             Main              `json:"main"`
	Bootstrap        Bootstrap         `json:"bootstrap"`
	Nodes            []Node            `json:"nodes"`
	Subscriptions    []Subscription    `json:"subscriptions"`
	Routes           []Route           `json:"routes"`
	DNSProfiles      []DNSProfile      `json:"dns_profiles"`
	LocalProxies     []LocalProxy      `json:"local_proxies"`
	Rules            []Rule            `json:"rules"`
}

type Main struct {
	ID                 string `json:"id"`
	SchemaVersion      int    `json:"schema_version"`
	Enabled            bool   `json:"enabled"`
	LogLevel           string `json:"log_level"`
	ProbeDirectURL     string `json:"probe_direct"`
	ProbeProxyURL      string `json:"probe_proxy"`
	SpeedtestProxyURL  string `json:"speedtest_proxy"`
	DNSCacheCapacity   int    `json:"dns_cache_capacity,omitempty"`
	DNSCachePersist    bool   `json:"dns_cache_persist,omitempty"`
	DirectBypass       string `json:"direct_bypass,omitempty"`
	DNSOptimisticCache bool   `json:"dns_optimistic_cache,omitempty"`
}

type Bootstrap struct {
	ID         string `json:"id"`
	Protocol   string `json:"protocol"`
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
	Strategy   string `json:"strategy"`
}

type Node struct {
	ID         string `json:"id"`
	Enabled    bool   `json:"enabled"`
	Name       string `json:"name,omitempty"`
	Type       string `json:"type"`
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
	NodeCredentials
	NodeTransport
	NodeProtocol
	NodeTLS
	NodeSource
}

// NodeCredentials contains authentication and host-key material shared by
// proxy protocols. It is embedded in Node so the canonical model stays easy
// to consume while the ownership of fields remains explicit.
type NodeCredentials struct {
	UUID              string   `json:"uuid,omitempty"`
	Username          string   `json:"username,omitempty"`
	Password          string   `json:"password,omitempty"`
	PrivateKey        string   `json:"private_key,omitempty"`
	HostKey           string   `json:"host_key,omitempty"`
	HostKeyAlgorithms []string `json:"host_key_algorithms,omitempty"`
}

type NodeTransport struct {
	Network        string `json:"network,omitempty"`
	Transport      string `json:"transport,omitempty"`
	TransportPath  string `json:"transport_path,omitempty"`
	TransportHost  string `json:"transport_host,omitempty"`
	ServiceName    string `json:"service_name,omitempty"`
	PacketEncoding string `json:"packet_encoding,omitempty"`
	Flow           string `json:"flow,omitempty"`
}

type NodeProtocol struct {
	Security              string   `json:"security,omitempty"`
	AlterID               int      `json:"alter_id,omitempty"`
	Version               int      `json:"version,omitempty"`
	Method                string   `json:"method,omitempty"`
	Plugin                string   `json:"plugin,omitempty"`
	PluginOptions         string   `json:"plugin_options,omitempty"`
	CongestionControl     string   `json:"congestion_control,omitempty"`
	UDPRelayMode          string   `json:"udp_relay_mode,omitempty"`
	UDPOverStream         bool     `json:"udp_over_stream,omitempty"`
	ZeroRTTHandshake      bool     `json:"zero_rtt_handshake,omitempty"`
	Heartbeat             string   `json:"heartbeat,omitempty"`
	QUIC                  bool     `json:"quic,omitempty"`
	QUICCongestionControl string   `json:"quic_congestion_control,omitempty"`
	InsecureConcurrency   int      `json:"insecure_concurrency,omitempty"`
	ServerPorts           []string `json:"server_ports,omitempty"`
	HopInterval           string   `json:"hop_interval,omitempty"`
	ObfsType              string   `json:"obfs_type,omitempty"`
	ObfsPassword          string   `json:"obfs_password,omitempty"`
	UpMbps                int      `json:"up_mbps,omitempty"`
	DownMbps              int      `json:"down_mbps,omitempty"`
	ExecutablePath        string   `json:"executable_path,omitempty"`
	ExtraArgs             []string `json:"extra_args,omitempty"`
	DataDirectory         string   `json:"data_directory,omitempty"`
}

type NodeTLS struct {
	TLSServerName    string   `json:"tls_server_name,omitempty"`
	ALPN             []string `json:"alpn,omitempty"`
	Insecure         bool     `json:"insecure,omitempty"`
	RealityPublicKey string   `json:"reality_public_key,omitempty"`
	RealityShortID   string   `json:"reality_short_id,omitempty"`
	UTLSFingerprint  string   `json:"utls_fingerprint,omitempty"`
}

type NodeSource struct {
	SourceSubscription string `json:"source_subscription,omitempty"`
	SourceFingerprint  string `json:"source_fingerprint,omitempty"`
	PinnedStale        bool   `json:"pinned_stale,omitempty"`
}

type Subscription struct {
	ID             string `json:"id"`
	Enabled        bool   `json:"enabled"`
	Name           string `json:"name,omitempty"`
	URL            string `json:"url"`
	UpdateInterval string `json:"update_interval,omitempty"`
}

type Route struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	Name    string `json:"name,omitempty"`
	Kind    string `json:"kind"`
	Tunnel  string `json:"tunnel,omitempty"`
	Node    string `json:"node,omitempty"`
	Detour  string `json:"detour,omitempty"`
}

type DNSProfile struct {
	ID            string `json:"id"`
	Enabled       bool   `json:"enabled"`
	Name          string `json:"name,omitempty"`
	Protocol      string `json:"protocol"`
	Server        string `json:"server"`
	ServerPort    int    `json:"server_port"`
	TLSServerName string `json:"tls_server_name,omitempty"`
	Path          string `json:"path,omitempty"`
	Insecure      bool   `json:"insecure,omitempty"`
}

type LocalProxy struct {
	ID         string `json:"id"`
	Enabled    bool   `json:"enabled"`
	Name       string `json:"name,omitempty"`
	Protocol   string `json:"protocol"`
	Listen     string `json:"listen"`
	ListenPort int    `json:"listen_port"`
	Username   string `json:"username,omitempty"`
	Password   string `json:"password,omitempty"`
}

type Rule struct {
	ID               string   `json:"id"`
	Enabled          bool     `json:"enabled"`
	AllowedIPs       bool     `json:"allowed_ips,omitempty"`
	Default          bool     `json:"default"`
	Name             string   `json:"name,omitempty"`
	DNSProfile       string   `json:"dns_profile"`
	Route            string   `json:"route"`
	Inbound          []string `json:"inbound,omitempty"`
	DomainMatch      []string `json:"domain_match,omitempty"`
	IPMatch          []string `json:"ip_match,omitempty"`
	SourceIPCIDR     []string `json:"source_ip_cidr,omitempty"`
	SourceMACAddress []string `json:"source_mac_address,omitempty"`
	Network          []string `json:"network,omitempty"`
	Protocol         []string `json:"protocol,omitempty"`
	Port             []int    `json:"port,omitempty"`
}

type Issue struct {
	Code       string `json:"code"`
	ObjectType string `json:"object_type,omitempty"`
	ObjectID   string `json:"object_id,omitempty"`
	Option     string `json:"option,omitempty"`
	Message    string `json:"message"`
}

// WarningGroup is the bounded, user-facing representation of one class of
// runtime warning. Object IDs and raw validation messages deliberately stay
// in Warnings for field-level actions and never enter this overview contract.
type WarningGroup struct {
	Code        string `json:"code"`
	ObjectType  string `json:"object_type"`
	Option      string `json:"option"`
	Count       int    `json:"count"`
	Summary     string `json:"summary"`
	Destination string `json:"destination,omitempty"`
}

type Validation struct {
	OK            bool           `json:"ok"`
	Errors        []Issue        `json:"errors"`
	Warnings      []Issue        `json:"warnings"`
	WarningGroups []WarningGroup `json:"warning_groups"`
}
