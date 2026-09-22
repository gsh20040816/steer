// SPDX-License-Identifier: GPL-3.0-or-later
package intent

import (
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

func ValidateNode(value Node) Validation {
	validation := Validation{Errors: []Issue{}, Warnings: []Issue{}, WarningGroups: []WarningGroup{}}
	err := func(code, objectType, objectID, option, message string) {
		validation.Errors = append(validation.Errors, Issue{Code: code, ObjectType: objectType, ObjectID: objectID, Option: option, Message: message})
	}
	warn := func(code, objectType, objectID, option, message string) {
		validation.Warnings = append(validation.Warnings, Issue{Code: code, ObjectType: objectType, ObjectID: objectID, Option: option, Message: message})
	}
	validateNode(value, err, warn)
	validation.OK = len(validation.Errors) == 0
	validation.WarningGroups = GroupWarnings(validation.Warnings)
	return validation
}

var (
	validID       = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	validMAC      = regexp.MustCompile(`(?i)^([0-9a-f]{2}:){5}[0-9a-f]{2}$`)
	validGeo      = regexp.MustCompile(`^[a-z0-9_!.\-]+(@[a-z0-9_!.\-]+)?$`)
	validDuration = regexp.MustCompile(`^[1-9][0-9]*(ms|s|m|h)$`)
)

func Validate(intent Intent) Validation {
	return ValidateWithOptions(intent, ValidationOptions{IPv6WildcardDualStack: true})
}

// Listener describes an already allocated socket address. Owner is used only
// to make a collision diagnostic identify the listener that owns the address.
type Listener struct {
	Address string
	Port    int
	Owner   string
}

// ValidationOptions describes runtime listener semantics which cannot be
// inferred from platform-independent Canonical Intent.
type ValidationOptions struct {
	ReservedListeners     []Listener
	IPv6WildcardDualStack bool
}

// ValidateWithOptions combines the Canonical contract with the listener
// addresses reserved by one platform adapter.
func ValidateWithOptions(intent Intent, options ValidationOptions) Validation {
	validation := Validation{Errors: []Issue{}, Warnings: []Issue{}, WarningGroups: []WarningGroup{}}
	err := func(code, objectType, objectID, option, message string) {
		validation.Errors = append(validation.Errors, Issue{Code: code, ObjectType: objectType, ObjectID: objectID, Option: option, Message: message})
	}
	warn := func(code, objectType, objectID, option, message string) {
		validation.Warnings = append(validation.Warnings, Issue{Code: code, ObjectType: objectType, ObjectID: objectID, Option: option, Message: message})
	}

	if intent.Main.SchemaVersion != SchemaVersion {
		err("UNSUPPORTED_SCHEMA", "steer", intent.Main.ID, "schema_version", "only schema 9 is supported")
	}
	if !validID.MatchString(intent.Main.ID) {
		err("INVALID_ID", "steer", intent.Main.ID, "id", "invalid section ID")
	}
	if !oneOf(intent.Main.LogLevel, "error", "warn", "info", "debug") {
		err("INVALID_LOG_LEVEL", "steer", intent.Main.ID, "log_level", "log level must be error, warn, info or debug")
	}
	if intent.Main.DNSCacheCapacity != 0 && (intent.Main.DNSCacheCapacity < 1024 || intent.Main.DNSCacheCapacity > 10_000_000) {
		err("INVALID_CACHE_CAPACITY", "steer", intent.Main.ID, "dns_cache_capacity", "DNS cache capacity must be 1024..10000000")
	}
	if !oneOf(intent.Main.DirectBypass, "", "off", "static", "dns") {
		err("INVALID_DIRECT_BYPASS", "steer", intent.Main.ID, "direct_bypass", "direct bypass must be off, static or dns")
	}
	validateProbeURL(intent.Main.ProbeDirectURL, intent.Main.ID, "probe_direct", err)
	validateProbeURL(intent.Main.ProbeProxyURL, intent.Main.ID, "probe_proxy", err)
	validateProbeURL(intent.Main.SpeedtestProxyURL, intent.Main.ID, "speedtest_proxy", err)
	validateBootstrap(intent.Bootstrap, err)

	globalIDs := map[string]string{}
	register := func(objectType, id string) {
		if !validID.MatchString(id) {
			err("INVALID_ID", objectType, id, "id", "ID must start with lowercase ASCII and contain only lowercase ASCII, digits, _ or -")
		}
		if previous, exists := globalIDs[id]; exists {
			err("GLOBAL_DUPLICATE_ID", objectType, id, "id", "ID is already used by "+previous)
		} else {
			globalIDs[id] = objectType
		}
	}
	register("steer", intent.Main.ID)
	register("bootstrap", intent.Bootstrap.ID)
	for _, value := range intent.Nodes {
		register("node", value.ID)
	}
	for _, value := range intent.Subscriptions {
		register("subscription", value.ID)
	}
	for _, value := range intent.Routes {
		register("route", value.ID)
	}
	for _, value := range intent.DNSProfiles {
		register("dns_profile", value.ID)
	}
	for _, value := range intent.LocalProxies {
		register("local_proxy", value.ID)
	}
	for _, value := range intent.Rules {
		register("rule", value.ID)
	}

	nodes := make(map[string]Node, len(intent.Nodes))
	for _, node := range intent.Nodes {
		nodes[node.ID] = node
		if !node.Enabled {
			continue
		}
		validateNode(node, err, warn)
	}
	for _, subscription := range intent.Subscriptions {
		if !subscription.Enabled {
			continue
		}
		validateSubscription(subscription, err)
	}
	routes := make(map[string]Route, len(intent.Routes))
	for _, route := range intent.Routes {
		routes[route.ID] = route
	}
	directRouteCount := 0
	for _, route := range intent.Routes {
		if !route.Enabled {
			continue
		}
		if !oneOf(route.Kind, "direct", "block", "single") {
			err("UNSUPPORTED_ROUTE_KIND", "route", route.ID, "kind", "route kind must be direct, block or single")
			continue
		}
		if route.Kind == "direct" {
			directRouteCount++
		}
		if route.Kind == "single" {
			node, exists := nodes[route.Node]
			if !exists {
				err("DANGLING_NODE", "route", route.ID, "node", "referenced node does not exist")
			} else if !node.Enabled {
				err("DISABLED_NODE", "route", route.ID, "node", "referenced node is disabled")
			}
			if route.Detour != "" {
				detour, exists := routes[route.Detour]
				switch {
				case !exists:
					err("DANGLING_DETOUR", "route", route.ID, "detour", "referenced detour route does not exist")
				case !detour.Enabled:
					err("DISABLED_DETOUR", "route", route.ID, "detour", "referenced detour route is disabled")
				case detour.Kind != "single":
					err("INVALID_DETOUR_KIND", "route", route.ID, "detour", "detour route must be a single-node route")
				}
			}
		} else if route.Node != "" {
			err("UNEXPECTED_NODE", "route", route.ID, "node", "only single routes accept a node")
		} else if route.Detour != "" {
			err("UNEXPECTED_DETOUR", "route", route.ID, "detour", "only single routes accept a detour")
		}
	}
	validateRouteDetourCycles(routes, intent.Routes, err)
	if directRouteCount != 1 {
		err("DIRECT_ROUTE_COUNT", "route", "", "", "exactly one enabled direct Route is required for bootstrap and core loop prevention")
	}
	dnsProfiles := make(map[string]DNSProfile, len(intent.DNSProfiles))
	for _, profile := range intent.DNSProfiles {
		dnsProfiles[profile.ID] = profile
		if !profile.Enabled {
			continue
		}
		validateDNSProfile(profile, err, warn)
	}
	localProxies := make(map[string]LocalProxy, len(intent.LocalProxies))
	listeners := append([]Listener{}, options.ReservedListeners...)
	for _, proxy := range intent.LocalProxies {
		localProxies[proxy.ID] = proxy
		if !proxy.Enabled {
			continue
		}
		validateLocalProxy(proxy, err)
		listener := Listener{Address: proxy.Listen, Port: proxy.ListenPort, Owner: proxy.ID}
		for _, previous := range listeners {
			if ListenersOverlap(listener, previous, options.IPv6WildcardDualStack) {
				err("PORT_COLLISION", "local_proxy", proxy.ID, "listen_port", "listen address collides with "+previous.Owner)
				break
			}
		}
		listeners = append(listeners, listener)
	}

	defaultCount, defaultSeen := 0, false
	for _, rule := range intent.Rules {
		if !rule.Enabled {
			continue
		}
		if defaultSeen && !rule.Default {
			err("RULE_AFTER_DEFAULT", "rule", rule.ID, "", "enabled rule appears after Default")
		}
		if rule.Default {
			defaultCount++
			defaultSeen = true
		}
		validateRule(rule, routes, dnsProfiles, localProxies, err, warn)
	}
	if defaultCount != 1 {
		err("DEFAULT_COUNT", "rule", "", "", "exactly one enabled Default rule is required")
	}
	validation.Warnings = ReachableWarnings(intent, validation.Warnings)
	validation.WarningGroups = GroupWarnings(validation.Warnings)
	validation.OK = len(validation.Errors) == 0
	return validation
}

// ListenersOverlap reports whether two listener addresses would compete for
// the same port. An IPv4 wildcard owns only IPv4. An IPv6 wildcard additionally
// owns IPv4 when the target runtime opens it as a dual-stack socket.
func ListenersOverlap(first, second Listener, ipv6WildcardDualStack bool) bool {
	if first.Port < 1 || first.Port > 65535 || first.Port != second.Port {
		return false
	}
	firstAddress, firstErr := netip.ParseAddr(first.Address)
	secondAddress, secondErr := netip.ParseAddr(second.Address)
	if firstErr != nil || secondErr != nil {
		return false
	}
	firstAddress = firstAddress.Unmap()
	secondAddress = secondAddress.Unmap()
	if firstAddress == secondAddress {
		return true
	}
	if firstAddress.Is4() == secondAddress.Is4() {
		return firstAddress.IsUnspecified() || secondAddress.IsUnspecified()
	}
	if !ipv6WildcardDualStack {
		return false
	}
	return (firstAddress.Is6() && firstAddress.IsUnspecified()) || (secondAddress.Is6() && secondAddress.IsUnspecified())
}

// ValidGeoCategory exposes the Canonical selector grammar to the shared
// platform validator without duplicating it in each frontend or adapter.
func ValidGeoCategory(value string) bool { return validGeo.MatchString(value) }

func validateProbeURL(raw, objectID, option string, err issueFn) {
	if raw == "" {
		err("REQUIRED_PROBE_URL", "steer", objectID, option, "probe URL is required")
		return
	}
	parsed, parseErr := url.Parse(raw)
	if parseErr != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		err("INVALID_PROBE_URL", "steer", objectID, option, "probe must be an HTTPS URL without credentials or fragment: "+raw)
	}
}

func validateRouteDetourCycles(routes map[string]Route, ordered []Route, err issueFn) {
	state := map[string]uint8{}
	stack := []string{}
	positions := map[string]int{}
	var visit func(string)
	visit = func(id string) {
		state[id] = 1
		positions[id] = len(stack)
		stack = append(stack, id)
		route := routes[id]
		if route.Enabled && route.Kind == "single" && route.Detour != "" {
			detour, exists := routes[route.Detour]
			if exists && detour.Enabled && detour.Kind == "single" {
				switch state[detour.ID] {
				case 0:
					visit(detour.ID)
				case 1:
					start := positions[detour.ID]
					cycle := append(append([]string{}, stack[start:]...), detour.ID)
					err("ROUTE_DETOUR_CYCLE", "route", id, "detour", "route detour cycle: "+strings.Join(cycle, " -> "))
				}
			}
		}
		stack = stack[:len(stack)-1]
		delete(positions, id)
		state[id] = 2
	}
	for _, route := range ordered {
		if route.Enabled && route.Kind == "single" && state[route.ID] == 0 {
			visit(route.ID)
		}
	}
}

func validateSubscription(value Subscription, err issueFn) {
	parsed, parseErr := url.Parse(value.URL)
	if parseErr != nil || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) || parsed.Host == "" {
		err("INVALID_SUBSCRIPTION_URL", "subscription", value.ID, "url", "subscription URL must be an absolute HTTP or HTTPS URL")
	}
	if value.UpdateInterval != "" && !validDuration.MatchString(value.UpdateInterval) {
		err("INVALID_UPDATE_INTERVAL", "subscription", value.ID, "update_interval", "update interval must be a positive duration such as 6h")
	}
}

type issueFn func(string, string, string, string, string)

func validateBootstrap(value Bootstrap, err issueFn) {
	if !oneOf(value.Protocol, "udp", "tcp") {
		err("UNSUPPORTED_BOOTSTRAP_PROTOCOL", "bootstrap", value.ID, "protocol", "bootstrap protocol must be udp or tcp")
	}
	if _, parseErr := netip.ParseAddr(value.Server); parseErr != nil {
		err("BOOTSTRAP_NOT_IP", "bootstrap", value.ID, "server", "bootstrap server must be an IP literal")
	}
	validPort(value.ServerPort, "bootstrap", value.ID, "server_port", err)
	if !validStrategy(value.Strategy) {
		err("INVALID_BOOTSTRAP_STRATEGY", "bootstrap", value.ID, "strategy", "invalid bootstrap strategy")
	}
}

func validateNode(value Node, err, warn issueFn) {
	validateNodeText(value, err)
	if value.PinnedStale {
		warn("SUBSCRIPTION_NODE_STALE", "node", value.ID, "pinned_stale", "subscription no longer advertises this node; remove Route references as soon as possible")
	}
	if !oneOf(value.Type, "socks", "http", "shadowsocks", "vmess", "trojan", "hysteria", "vless", "shadowtls", "tuic", "hysteria2", "anytls", "ssh", "naive", "tor") {
		err("UNSUPPORTED_NODE_TYPE", "node", value.ID, "type", "node type is not supported by the current sing-box baseline")
		return
	}
	if value.Type != "tor" && !validHost(value.Server) {
		err("INVALID_SERVER", "node", value.ID, "server", "invalid node server")
	}
	if value.Type != "tor" {
		validPort(value.ServerPort, "node", value.ID, "server_port", err)
	}
	if value.Network != "" {
		if value.Type != "vmess" {
			err("UNSUPPORTED_NODE_OPTION", "node", value.ID, "network", "network is only supported by VMess outbounds")
		} else if !oneOf(value.Network, "tcp", "udp") {
			err("INVALID_NODE_NETWORK", "node", value.ID, "network", "VMess network must be tcp or udp")
		}
	}
	if value.Security != "" && value.Type != "vmess" {
		err("UNSUPPORTED_NODE_OPTION", "node", value.ID, "security", "security is only supported by VMess outbounds")
	}
	switch value.Type {
	case "socks":
		if value.TLSServerName != "" || value.Insecure {
			err("UNSUPPORTED_NODE_OPTION", "node", value.ID, "tls_server_name", "SOCKS outbound has no TLS options")
		}
	case "http":
		if value.TransportPath != "" {
			err("UNSUPPORTED_NODE_OPTION", "node", value.ID, "transport_path", "HTTP outbound path is not represented")
		}
	case "shadowsocks":
		if value.Method == "" {
			err("REQUIRED", "node", value.ID, "method", "Shadowsocks method is required")
		}
		if value.Password == "" {
			err("REQUIRED", "node", value.ID, "password", "Shadowsocks password is required")
		}
		if value.Plugin != "" && !oneOf(value.Plugin, "obfs-local", "v2ray-plugin") {
			err("UNSUPPORTED_SHADOWSOCKS_PLUGIN", "node", value.ID, "plugin", "only obfs-local and v2ray-plugin are supported")
		}
	case "vmess":
		validateUUID(value.UUID, "VMess", value.ID, err)
		if value.Security != "" && !oneOf(value.Security, "auto", "none", "zero", "aes-128-gcm", "chacha20-poly1305", "aes-128-ctr") {
			err("UNSUPPORTED_VMESS_SECURITY", "node", value.ID, "security", "unsupported VMess security")
		}
		if value.AlterID < 0 {
			err("INVALID_INTEGER", "node", value.ID, "alter_id", "alter_id cannot be negative")
		}
		validateTransport(value, err)
	case "hysteria":
		if value.Password == "" {
			err("REQUIRED", "node", value.ID, "password", "Hysteria authentication is required")
		}
		validateHysteria(value, err)
	case "vless":
		validateUUID(value.UUID, "VLESS", value.ID, err)
		if value.Flow != "" && value.Flow != "xtls-rprx-vision" {
			err("UNSUPPORTED_VLESS_FLOW", "node", value.ID, "flow", "unsupported VLESS flow")
		}
		if value.PacketEncoding != "" && !oneOf(value.PacketEncoding, "xudp", "packetaddr") {
			err("INVALID_PACKET_ENCODING", "node", value.ID, "packet_encoding", "packet encoding must be xudp or packetaddr")
		}
		if value.RealityPublicKey != "" || value.RealityShortID != "" {
			if value.TLSServerName == "" {
				err("INCOMPLETE_REALITY", "node", value.ID, "tls_server_name", "Reality TLS server name is required")
			}
			if value.RealityPublicKey == "" {
				err("INCOMPLETE_REALITY", "node", value.ID, "reality_public_key", "Reality public key is required")
			}
			if value.RealityShortID == "" {
				err("INCOMPLETE_REALITY", "node", value.ID, "reality_short_id", "Reality short ID is required")
			}
		}
		validateTransport(value, err)
	case "hysteria2":
		if value.Password == "" {
			err("REQUIRED", "node", value.ID, "password", "Hysteria2 password is required")
		}
		if value.TLSServerName == "" {
			err("REQUIRED", "node", value.ID, "tls_server_name", "Hysteria2 TLS server name is required")
		}
		for _, portRange := range value.ServerPorts {
			if !validPortRange(portRange) {
				err("INVALID_PORT_RANGE", "node", value.ID, "server_ports", "invalid Hysteria2 port range: "+portRange)
			}
		}
		if value.HopInterval != "" && !validDuration.MatchString(value.HopInterval) {
			err("INVALID_DURATION", "node", value.ID, "hop_interval", "invalid Hysteria2 hop interval")
		}
		if value.ObfsType != "" && value.ObfsType != "salamander" {
			err("UNSUPPORTED_HYSTERIA2_OBFS", "node", value.ID, "obfs_type", "only salamander obfuscation is supported")
		}
		if value.ObfsType != "" && value.ObfsPassword == "" {
			err("REQUIRED", "node", value.ID, "obfs_password", "obfuscation password is required")
		}
		if value.UpMbps < 0 || value.DownMbps < 0 {
			err("INVALID_BANDWIDTH", "node", value.ID, "up_mbps", "bandwidth cannot be negative")
		}
	case "trojan":
		if value.Password == "" {
			err("REQUIRED", "node", value.ID, "password", "Trojan password is required")
		}
		if value.TLSServerName == "" {
			err("REQUIRED", "node", value.ID, "tls_server_name", "Trojan TLS server name is required")
		}
		validateTransport(value, err)
	case "shadowtls":
		if value.Version < 1 || value.Version > 3 {
			err("INVALID_VERSION", "node", value.ID, "version", "ShadowTLS version must be 1, 2 or 3")
		}
		if value.Version >= 2 && value.Password == "" {
			err("REQUIRED", "node", value.ID, "password", "ShadowTLS v2/v3 password is required")
		}
		if value.TLSServerName == "" {
			err("REQUIRED", "node", value.ID, "tls_server_name", "ShadowTLS TLS server name is required")
		}
	case "tuic":
		validateUUID(value.UUID, "TUIC", value.ID, err)
		validateALPN(value.ALPN, value.ID, err)
		if !oneOf(value.CongestionControl, "", "cubic", "new_reno", "bbr") {
			err("INVALID_CONGESTION_CONTROL", "node", value.ID, "congestion_control", "TUIC congestion control must be cubic, new_reno or bbr")
		}
		if !oneOf(value.UDPRelayMode, "", "native", "quic") {
			err("INVALID_UDP_RELAY_MODE", "node", value.ID, "udp_relay_mode", "TUIC UDP relay mode must be native or quic")
		}
		if value.UDPRelayMode != "" && value.UDPOverStream {
			err("CONFLICTING_OPTIONS", "node", value.ID, "udp_relay_mode", "TUIC udp_relay_mode conflicts with udp_over_stream")
		}
		if value.TLSServerName == "" {
			err("REQUIRED", "node", value.ID, "tls_server_name", "TUIC TLS server name is required")
		}
	case "anytls":
		if value.Password == "" {
			err("REQUIRED", "node", value.ID, "password", "AnyTLS password is required")
		}
		if value.TLSServerName == "" {
			err("REQUIRED", "node", value.ID, "tls_server_name", "AnyTLS TLS server name is required")
		}
	case "naive":
		if value.Password == "" {
			err("REQUIRED", "node", value.ID, "password", "NaiveProxy password is required")
		}
		if value.TLSServerName == "" {
			err("REQUIRED", "node", value.ID, "tls_server_name", "NaiveProxy TLS server name is required")
		}
	case "ssh":
		if value.Username == "" {
			err("REQUIRED", "node", value.ID, "username", "SSH user is required")
		}
		if value.Password == "" && value.PrivateKey == "" {
			err("REQUIRED", "node", value.ID, "password", "SSH requires password or private key")
		}
	case "tor":
		if value.Server != "" || value.ServerPort != 0 {
			err("UNEXPECTED_NODE_OPTION", "node", value.ID, "server", "Tor uses a local executable and does not accept a remote server")
		}
	}
	if len(value.ALPN) > 0 && !oneOf(value.Type, "http", "vmess", "hysteria", "vless", "hysteria2", "trojan", "shadowtls", "tuic", "anytls", "naive") {
		err("UNSUPPORTED_NODE_OPTION", "node", value.ID, "alpn", "ALPN is only supported by TLS outbounds")
	}
	if value.Insecure {
		warn("INSECURE_TLS", "node", value.ID, "insecure", "TLS certificate verification is disabled")
	}
}

func validateNodeText(value Node, err issueFn) {
	type field struct {
		option string
		value  string
	}
	fields := []field{
		{"id", value.ID}, {"name", value.Name}, {"type", value.Type}, {"server", value.Server},
		{"uuid", value.UUID}, {"username", value.Username}, {"password", value.Password}, {"private_key", value.PrivateKey}, {"host_key", value.HostKey},
		{"network", value.Network}, {"transport", value.Transport}, {"transport_path", value.TransportPath}, {"transport_host", value.TransportHost},
		{"service_name", value.ServiceName}, {"packet_encoding", value.PacketEncoding}, {"flow", value.Flow},
		{"security", value.Security}, {"method", value.Method}, {"plugin", value.Plugin}, {"plugin_options", value.PluginOptions},
		{"congestion_control", value.CongestionControl}, {"udp_relay_mode", value.UDPRelayMode}, {"heartbeat", value.Heartbeat},
		{"quic_congestion_control", value.QUICCongestionControl}, {"hop_interval", value.HopInterval}, {"obfs_type", value.ObfsType},
		{"obfs_password", value.ObfsPassword}, {"executable_path", value.ExecutablePath}, {"data_directory", value.DataDirectory},
		{"tls_server_name", value.TLSServerName}, {"reality_public_key", value.RealityPublicKey}, {"reality_short_id", value.RealityShortID},
		{"utls_fingerprint", value.UTLSFingerprint}, {"source_subscription", value.SourceSubscription}, {"source_fingerprint", value.SourceFingerprint},
	}
	for _, item := range value.HostKeyAlgorithms {
		fields = append(fields, field{"host_key_algorithms", item})
	}
	for _, item := range value.ServerPorts {
		fields = append(fields, field{"server_ports", item})
	}
	for _, item := range value.ExtraArgs {
		fields = append(fields, field{"extra_args", item})
	}
	for _, item := range fields {
		invalidControl := func(character rune) bool {
			if item.option == "private_key" && (character == '\r' || character == '\n') {
				return false
			}
			return unicode.IsControl(character)
		}
		if strings.IndexFunc(item.value, invalidControl) >= 0 {
			err("CONTROL_CHARACTER", "node", value.ID, item.option, "node text fields cannot contain control characters")
		}
	}
}

func validateUUID(value, protocol, objectID string, err issueFn) {
	if value == "" {
		err("REQUIRED", "node", objectID, "uuid", protocol+" UUID is required")
		return
	}
	if !regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(value) {
		err("INVALID_UUID", "node", objectID, "uuid", protocol+" UUID is invalid")
	}
}

func validateALPN(values []string, objectID string, err issueFn) {
	for _, value := range values {
		if value == "" {
			err("INVALID_ALPN", "node", objectID, "alpn", "ALPN entries must be non-empty")
			continue
		}
		if strings.Contains(value, ",") {
			err("INVALID_ALPN", "node", objectID, "alpn", "ALPN entries must not contain commas")
		}
		if len([]byte(value)) > 255 {
			err("INVALID_ALPN", "node", objectID, "alpn", "ALPN entries must be at most 255 bytes")
		}
		if strings.IndexFunc(value, unicode.IsControl) >= 0 {
			err("CONTROL_CHARACTER", "node", objectID, "alpn", "ALPN entries cannot contain control characters")
		}
	}
}

func validateTransport(value Node, err issueFn) {
	if value.Transport == "" || value.Transport == "tcp" || value.Transport == "raw" {
		return
	}
	if !oneOf(value.Transport, "ws", "grpc", "http", "quic") {
		err("UNSUPPORTED_TRANSPORT", "node", value.ID, "transport", "transport must be tcp, raw, ws, grpc, http or quic")
		return
	}
	if value.Transport == "ws" && value.TransportPath == "" {
		err("REQUIRED", "node", value.ID, "transport_path", "WebSocket transport path is required")
	}
	if value.Transport == "grpc" && value.ServiceName == "" {
		err("REQUIRED", "node", value.ID, "service_name", "gRPC service name is required")
	}
}

func validateHysteria(value Node, err issueFn) {
	for _, portRange := range value.ServerPorts {
		if !validPortRange(portRange) {
			err("INVALID_PORT_RANGE", "node", value.ID, "server_ports", "invalid Hysteria port range: "+portRange)
		}
	}
	if value.TLSServerName == "" {
		err("REQUIRED", "node", value.ID, "tls_server_name", "Hysteria TLS server name is required")
	}
	if value.UpMbps == 0 || value.DownMbps == 0 {
		err("REQUIRED", "node", value.ID, "up_mbps", "Hysteria upload and download Mbps are required")
	}
	if value.UpMbps < 0 || value.DownMbps < 0 {
		err("INVALID_BANDWIDTH", "node", value.ID, "up_mbps", "bandwidth cannot be negative")
	}
}

func validateDNSProfile(value DNSProfile, err, warn issueFn) {
	supported := oneOf(value.Protocol, "udp", "tcp", "tls", "https", "quic", "h3")
	encrypted := oneOf(value.Protocol, "tls", "https", "quic", "h3")
	httpPath := oneOf(value.Protocol, "https", "h3")
	if !supported {
		err("UNSUPPORTED_DNS_PROTOCOL", "dns_profile", value.ID, "protocol", "DNS protocol must be udp, tcp, tls, https, quic or h3")
	}
	if !validHost(value.Server) {
		err("INVALID_DNS_SERVER", "dns_profile", value.ID, "server", "invalid DNS server")
	}
	validPort(value.ServerPort, "dns_profile", value.ID, "server_port", err)
	if supported && !encrypted && value.TLSServerName != "" {
		err("UNSUPPORTED_DNS_OPTION", "dns_profile", value.ID, "tls_server_name", "TLS server name is only supported by encrypted DNS protocols")
	}
	if supported && !encrypted && value.Insecure {
		err("UNSUPPORTED_DNS_OPTION", "dns_profile", value.ID, "insecure", "skip certificate verification is only supported by encrypted DNS protocols")
	}
	if supported && !httpPath && value.Path != "" {
		err("UNSUPPORTED_DNS_OPTION", "dns_profile", value.ID, "path", "HTTP path is only supported by DoH and DoH3")
	}
	if encrypted && value.TLSServerName == "" {
		err("REQUIRED", "dns_profile", value.ID, "tls_server_name", "encrypted DNS requires a TLS server name")
	}
	if httpPath && value.Path != "" && !strings.HasPrefix(value.Path, "/") {
		err("INVALID_DNS_PATH", "dns_profile", value.ID, "path", "DoH path must start with /")
	}
	if encrypted && value.Insecure {
		warn("INSECURE_TLS", "dns_profile", value.ID, "insecure", "DNS TLS certificate verification is disabled")
	}
}

func validateLocalProxy(value LocalProxy, err issueFn) {
	if !oneOf(value.Protocol, "mixed", "socks", "http") {
		err("UNSUPPORTED_LOCAL_PROXY_PROTOCOL", "local_proxy", value.ID, "protocol", "local proxy protocol must be mixed, socks or http")
	}
	address, parseErr := netip.ParseAddr(value.Listen)
	if parseErr != nil {
		err("INVALID_LISTEN_ADDRESS", "local_proxy", value.ID, "listen", "listen address must be an IP literal")
	}
	validPort(value.ListenPort, "local_proxy", value.ID, "listen_port", err)
	if (value.Username == "") != (value.Password == "") {
		err("INCOMPLETE_LOCAL_PROXY_AUTH", "local_proxy", value.ID, "username", "username and password must be set together")
	}
	if parseErr == nil && !address.IsLoopback() && value.Username == "" {
		err("LOCAL_PROXY_AUTH_REQUIRED", "local_proxy", value.ID, "username", "non-loopback local proxy requires authentication")
	}
}

// DNSProjectionUnsupportedConditions returns rule fields that can only be
// evaluated after DNS resolution. Dropping any of them from a DNS reject would
// widen the reject beyond the rule's canonical match.
func DNSProjectionUnsupportedConditions(value Rule) []string {
	conditions := []string{}
	if len(value.IPMatch) > 0 {
		conditions = append(conditions, "ip_match")
	}
	if len(value.Network) > 0 {
		conditions = append(conditions, "network")
	}
	if len(value.Protocol) > 0 {
		conditions = append(conditions, "protocol")
	}
	if len(value.Port) > 0 {
		conditions = append(conditions, "port")
	}
	return conditions
}

func validateRule(value Rule, routes map[string]Route, dnsProfiles map[string]DNSProfile, localProxies map[string]LocalProxy, err, warn issueFn) {
	hasMatch := len(value.Inbound)+len(value.DomainMatch)+len(value.IPMatch)+len(value.SourceIPCIDR)+len(value.SourceMACAddress)+len(value.Network)+len(value.Protocol)+len(value.Port) > 0
	if value.Default && hasMatch {
		err("DEFAULT_HAS_MATCH", "rule", value.ID, "", "Default cannot have match conditions")
	}
	if !value.Default && !hasMatch {
		err("EMPTY_MATCH", "rule", value.ID, "", "non-Default rule requires a match condition")
	}
	profile, exists := dnsProfiles[value.DNSProfile]
	if !exists {
		err("DANGLING_DNS_PROFILE", "rule", value.ID, "dns_profile", "referenced DNS Profile does not exist")
	} else if !profile.Enabled {
		err("DISABLED_DNS_PROFILE", "rule", value.ID, "dns_profile", "referenced DNS Profile is disabled")
	}
	route, exists := routes[value.Route]
	if !exists {
		err("DANGLING_ROUTE", "rule", value.ID, "route", "referenced Route does not exist")
	} else if !route.Enabled {
		err("DISABLED_ROUTE", "rule", value.ID, "route", "referenced Route is disabled")
	}
	for _, inbound := range value.Inbound {
		proxy, exists := localProxies[inbound]
		if !exists {
			err("DANGLING_LOCAL_PROXY", "rule", value.ID, "inbound", "local proxy does not exist: "+inbound)
		} else if !proxy.Enabled {
			err("DISABLED_LOCAL_PROXY", "rule", value.ID, "inbound", "local proxy is disabled: "+inbound)
		}
	}
	if len(value.Inbound) > 0 && len(value.SourceMACAddress) > 0 {
		err("INCOMPATIBLE_SOURCE_SELECTORS", "rule", value.ID, "source_mac_address", "local proxy and source MAC cannot be combined")
	}
	for _, expression := range value.DomainMatch {
		validateDomainExpression(expression, value.ID, err)
	}
	for _, expression := range value.IPMatch {
		validateIPExpression(expression, value.ID, err)
	}
	for _, prefix := range value.SourceIPCIDR {
		if _, parseErr := netip.ParsePrefix(prefix); parseErr != nil {
			err("INVALID_IP_CIDR", "rule", value.ID, "source_ip_cidr", "invalid source prefix: "+prefix)
		}
	}
	for _, address := range value.SourceMACAddress {
		if !validMAC.MatchString(address) {
			err("INVALID_SOURCE_MAC_ADDRESS", "rule", value.ID, "source_mac_address", "invalid MAC address: "+address)
		}
	}
	for _, network := range value.Network {
		if !oneOf(network, "tcp", "udp") {
			err("INVALID_RULE_NETWORK", "rule", value.ID, "network", "network must be tcp or udp")
		}
	}
	for _, protocol := range value.Protocol {
		if !oneOf(protocol, "tls", "http", "quic", "dns", "stun", "bittorrent", "dtls", "ssh", "rdp", "ntp") {
			err("INVALID_RULE_PROTOCOL", "rule", value.ID, "protocol", "unsupported sniffed protocol: "+protocol)
		}
	}
	for _, port := range value.Port {
		validPort(port, "rule", value.ID, "port", err)
	}
	unsupportedDNSConditions := DNSProjectionUnsupportedConditions(value)
	if !value.Default && exists && route.Enabled && route.Kind == "block" && len(unsupportedDNSConditions) > 0 {
		warn("DNS_REJECT_PROJECTION_SKIPPED", "rule", value.ID, "route", "DNS reject projection is skipped because DNS cannot evaluate connection-stage conditions: "+strings.Join(unsupportedDNSConditions, ", ")+"; DNS queries continue to subsequent rules")
	} else if !value.Default && len(value.Inbound)+len(value.DomainMatch)+len(value.SourceIPCIDR)+len(value.SourceMACAddress) == 0 {
		warn("DNS_PROJECTION_EMPTY", "rule", value.ID, "dns_profile", "rule has only connection-stage conditions and produces no DNS projection")
	}
}

func validateDomainExpression(value, id string, err issueFn) {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\t ") {
		err("INVALID_DOMAIN_MATCH", "rule", id, "domain_match", "invalid domain expression: "+value)
		return
	}
	prefix, payload := "keyword", value
	for _, candidate := range []string{"full:", "domain:", "regexp:", "geosite:"} {
		if strings.HasPrefix(value, candidate) {
			prefix, payload = strings.TrimSuffix(candidate, ":"), strings.TrimPrefix(value, candidate)
			break
		}
	}
	if payload == "" {
		err("INVALID_DOMAIN_MATCH", "rule", id, "domain_match", "empty domain expression: "+value)
		return
	}
	switch prefix {
	case "full", "domain":
		if !validDomain(payload) {
			err("INVALID_DOMAIN_MATCH", "rule", id, "domain_match", "invalid domain: "+payload)
		}
	case "regexp":
		if _, compileErr := regexp.Compile(payload); compileErr != nil {
			err("INVALID_DOMAIN_MATCH", "rule", id, "domain_match", "invalid regular expression: "+payload)
		}
	case "geosite":
		if !validGeo.MatchString(payload) {
			err("INVALID_GEO_CATEGORY", "rule", id, "domain_match", "invalid GeoSite category: "+payload)
		}
	case "keyword":
		if strings.Contains(value, ":") {
			err("INVALID_DOMAIN_MATCH", "rule", id, "domain_match", "unknown domain expression prefix: "+value)
		}
	}
}

func validateIPExpression(value, id string, err issueFn) {
	if strings.HasPrefix(value, "geoip:") {
		if !validGeo.MatchString(strings.TrimPrefix(value, "geoip:")) {
			err("INVALID_GEO_CATEGORY", "rule", id, "ip_match", "invalid GeoIP category: "+value)
		}
		return
	}
	if _, parseErr := netip.ParsePrefix(value); parseErr == nil {
		return
	}
	if _, parseErr := netip.ParseAddr(value); parseErr != nil {
		err("INVALID_IP_CIDR", "rule", id, "ip_match", "invalid destination IP or prefix: "+value)
	}
}

func validPort(value int, objectType, objectID, option string, err issueFn) {
	if value < 1 || value > 65535 {
		err("INVALID_PORT", objectType, objectID, option, "port must be 1..65535")
	}
}
func validStrategy(value string) bool {
	return oneOf(value, "prefer_ipv4", "prefer_ipv6", "ipv4_only", "ipv6_only")
}
func oneOf(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}
func validHost(value string) bool {
	if _, err := netip.ParseAddr(value); err == nil {
		return true
	}
	return validDomain(value)
}
func validDomain(value string) bool {
	if value == "" || len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}
func validPortRange(value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) > 2 {
		return false
	}
	first, firstErr := strconv.Atoi(parts[0])
	last := first
	lastErr := firstErr
	if len(parts) == 2 {
		last, lastErr = strconv.Atoi(parts[1])
	}
	return firstErr == nil && lastErr == nil && first >= 1 && last <= 65535 && first <= last
}
