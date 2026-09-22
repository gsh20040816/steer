// SPDX-License-Identifier: GPL-3.0-or-later

// Package compiler lowers validated Canonical Intent into one deterministic
// sing-box configuration. Platform adapters provide native inbound fragments
// and generate their operating-system configuration separately.
package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"sort"
	"strings"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

type Output struct {
	IntentDigest         string         `json:"intent_digest"`
	RuntimeDigest        string         `json:"runtime_digest"`
	RequiredCapabilities []string       `json:"required_capabilities"`
	GeoRuleSets          []GeoRuleSet   `json:"geo_rule_sets"`
	SingBox              map[string]any `json:"sing_box"`
}

type Options struct {
	StateDirectory   string
	GeoDataDirectory string
	GeoDataBaseURL   string
	Target           Target
}

// Target contains sing-box-native fragments selected by one platform adapter.
// It deliberately contains no nftables, routing-table or service-manager plan.
type Target struct {
	Inbounds             []any      `json:"inbounds"`
	DNSInboundTags       []string   `json:"dns_inbound_tags"`
	DNSCapture           DNSCapture `json:"dns_capture"`
	BypassInboundTags    []string   `json:"bypass_inbound_tags,omitempty"`
	SniffInboundTags     []string   `json:"sniff_inbound_tags"`
	DirectRouteAddress   []string   `json:"direct_route_address"`
	RequiredCapabilities []string   `json:"required_capabilities"`
}

// DNSCaptureMode makes the ownership boundary for DNS traffic explicit. A
// dedicated DNS inbound has already classified its traffic; Darwin TUN capture
// must additionally restrict hijacking to TCP/UDP destination port 53.
type DNSCaptureMode string

const (
	DNSCaptureNone            DNSCaptureMode = "none"
	DNSCaptureInboundHijack   DNSCaptureMode = "inbound_hijack"
	DNSCaptureTUNPort53Hijack DNSCaptureMode = "tun_port53_hijack"
)

type DNSCapture struct {
	Mode        DNSCaptureMode `json:"mode"`
	InboundTags []string       `json:"inbound_tags"`
	// Port-53 traffic that reaches TUN without native destination rewriting.
	TUNInboundTags []string `json:"tun_inbound_tags,omitempty"`
}

type DNSPath struct {
	Profile string `json:"profile"`
	Route   string `json:"route"`
	Tag     string `json:"tag"`
}

type GeoRuleSet struct {
	Kind        string `json:"kind"`
	Category    string `json:"category"`
	Tag         string `json:"tag"`
	InitialPath string `json:"initial_path"`
	URL         string `json:"url"`
}

func Compile(intent model.Intent, options Options) Output {
	if options.StateDirectory == "" {
		options.StateDirectory = "/var/lib/steer"
	}
	if options.GeoDataDirectory == "" {
		options.GeoDataDirectory = "/usr/share/steer/geodata-seed"
	}
	if options.GeoDataBaseURL == "" {
		options.GeoDataBaseURL = "https://gsh20040816.github.io/steer/geodata/latest/rules"
	}
	geoSets := collectGeoRuleSets(intent, options.GeoDataDirectory, options.GeoDataBaseURL)
	dnsPaths := collectDNSPaths(intent)
	output := Output{
		IntentDigest:         IntentDigest(intent),
		RequiredCapabilities: requiredCapabilities(intent, options.Target),
		GeoRuleSets:          geoSets,
	}
	output.SingBox = compileSingBox(expandWireGuardRules(intent), options.Target, dnsPaths, geoSets, options.StateDirectory)
	output.RuntimeDigest = RuntimeDigest(intent, output.SingBox)
	return output
}

// IntentDigest identifies the complete canonical saved document.
func IntentDigest(intent model.Intent) string {
	return digestValue(intent)
}

// RuntimeDigest identifies the runtime projection that is actually activated.
// Probe scalars live in the active Intent even though they are not sing-box
// fields. Subscription metadata and unreferenced node inventory are
// deliberately absent because they do not change active traffic or probes.
func RuntimeDigest(intent model.Intent, singBox map[string]any) string {
	return digestValue(struct {
		Enabled        bool           `json:"enabled"`
		ProbeDirect    string         `json:"probe_direct"`
		ProbeProxy     string         `json:"probe_proxy"`
		ProbeSpeedtest string         `json:"probe_speedtest"`
		SingBox        map[string]any `json:"sing_box"`
	}{
		Enabled: intent.Main.Enabled, ProbeDirect: intent.Main.ProbeDirectURL,
		ProbeProxy: intent.Main.ProbeProxyURL, ProbeSpeedtest: intent.Main.SpeedtestProxyURL, SingBox: singBox,
	})
}

func digestValue(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func requiredCapabilities(intent model.Intent, target Target) []string {
	capabilities := append([]string{}, target.RequiredCapabilities...)
	for _, tunnel := range intent.WireGuardTunnels {
		if tunnel.Enabled {
			capabilities = append(capabilities, "with_wireguard", "with_gvisor")
		}
	}
	for _, node := range intent.Nodes {
		if !node.Enabled {
			continue
		}
		if node.Type == "hysteria" || node.Type == "hysteria2" || node.Type == "tuic" || node.Transport == "quic" || node.QUIC {
			capabilities = append(capabilities, "with_quic")
		}
		if node.UTLSFingerprint != "" {
			capabilities = append(capabilities, "with_utls")
		}
	}
	for _, profile := range intent.DNSProfiles {
		if !profile.Enabled {
			continue
		}
		if profile.Protocol == "quic" || profile.Protocol == "h3" {
			capabilities = append(capabilities, "dns_quic")
		}
	}
	sort.Strings(capabilities)
	return unique(capabilities)
}

func collectDNSPaths(intent model.Intent) []DNSPath {
	seen := map[string]bool{}
	routes := indexRoutes(intent.Routes)
	var paths []DNSPath
	for _, rule := range intent.Rules {
		if !rule.Enabled {
			continue
		}
		if routes[rule.Route].Kind == "block" {
			continue
		}
		key := rule.DNSProfile + "\x00" + rule.Route
		if seen[key] {
			continue
		}
		seen[key] = true
		paths = append(paths, DNSPath{Profile: rule.DNSProfile, Route: rule.Route, Tag: dnsPathTag(rule.DNSProfile, rule.Route)})
	}
	return paths
}

func collectGeoRuleSets(intent model.Intent, seedDirectory, baseURL string) []GeoRuleSet {
	seen := map[string]bool{}
	var result []GeoRuleSet
	for _, rule := range intent.Rules {
		if !rule.Enabled || rule.Default {
			continue
		}
		for _, expression := range rule.DomainMatch {
			if strings.HasPrefix(expression, "geosite:") {
				addGeoSet(&result, seen, seedDirectory, baseURL, "geosite", strings.TrimPrefix(expression, "geosite:"))
			}
		}
		for _, expression := range rule.IPMatch {
			if strings.HasPrefix(expression, "geoip:") {
				addGeoSet(&result, seen, seedDirectory, baseURL, "geoip", strings.TrimPrefix(expression, "geoip:"))
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Tag < result[j].Tag })
	return result
}

func addGeoSet(result *[]GeoRuleSet, seen map[string]bool, seedDirectory, baseURL, kind, category string) {
	tag := "steer-" + kind + "-" + category
	if seen[tag] {
		return
	}
	seen[tag] = true
	*result = append(*result, GeoRuleSet{
		Kind: kind, Category: category, Tag: tag,
		InitialPath: strings.TrimRight(seedDirectory, "/") + "/rules/" + tag + ".srs",
		URL:         strings.TrimRight(baseURL, "/") + "/" + tag + ".srs",
	})
}

func compileSingBox(intent model.Intent, target Target, dnsPaths []DNSPath, geoRuleSets []GeoRuleSet, stateDirectory string) map[string]any {
	profiles := indexDNSProfiles(intent.DNSProfiles)
	routes := indexRoutes(intent.Routes)

	inbounds := append([]any{}, target.Inbounds...)
	sniffInboundTags := append([]string{}, target.SniffInboundTags...)
	for _, proxy := range intent.LocalProxies {
		if !proxy.Enabled {
			continue
		}
		inbound := map[string]any{"type": proxy.Protocol, "tag": localProxyTag(proxy.ID), "listen": proxy.Listen, "listen_port": proxy.ListenPort}
		if proxy.Username != "" {
			inbound["users"] = []any{map[string]any{"username": proxy.Username, "password": proxy.Password}}
		}
		inbounds = append(inbounds, inbound)
		sniffInboundTags = append(sniffInboundTags, localProxyTag(proxy.ID))
	}

	nodes := indexNodes(intent.Nodes)
	outbounds := make([]any, 0, len(intent.Routes))
	for _, route := range intent.Routes {
		if !route.Enabled {
			continue
		}
		switch route.Kind {
		case "direct":
			outbounds = append(outbounds, map[string]any{"type": "direct", "tag": routeTag(route.ID)})
		case "block":
			// Block is a Canonical policy target, not a sing-box outbound.
			// sing-box deprecated the legacy block outbound in 1.11; rules
			// referencing this route are lowered to the reject action below.
			continue
		case "wireguard":
			outbounds = append(outbounds, map[string]any{"type": "selector", "tag": routeTag(route.ID), "outbounds": []string{wireGuardTag(route.Tunnel)}})
		case "single":
			outbounds = append(outbounds, compileRouteOutbound(route, nodes))
		}
	}

	dnsServers := []any{map[string]any{
		"type": intent.Bootstrap.Protocol, "tag": "steer-dns-bootstrap", "server": intent.Bootstrap.Server,
		"server_port": intent.Bootstrap.ServerPort,
	}}
	for _, path := range dnsPaths {
		dnsServers = append(dnsServers, compileDNSPath(profiles[path.Profile], routes[path.Route], path, intent.Bootstrap.Strategy))
	}

	endpoints, wireGuardAccess := compileWireGuard(intent)
	routeRules := append([]any{}, wireGuardAccess...)
	var privateFallback []any
	capture := target.DNSCapture
	if capture.Mode == "" && len(capture.InboundTags) == 0 && len(target.DNSInboundTags) > 0 {
		capture.Mode = DNSCaptureInboundHijack
		capture.InboundTags = append([]string{}, target.DNSInboundTags...)
	}
	switch capture.Mode {
	case DNSCaptureInboundHijack:
		if len(capture.InboundTags) > 0 {
			routeRules = append(routeRules, map[string]any{"inbound": capture.InboundTags, "action": "hijack-dns"})
		}
	case DNSCaptureTUNPort53Hijack:
		if len(capture.InboundTags) > 0 {
			routeRules = append(routeRules, map[string]any{
				"inbound": capture.InboundTags, "network": []string{"tcp", "udp"}, "port": []uint16{53}, "action": "hijack-dns",
			})
		}
	}
	if capture.Mode != DNSCaptureNone && len(capture.TUNInboundTags) > 0 {
		routeRules = append(routeRules, map[string]any{
			"inbound": capture.TUNInboundTags, "network": []string{"tcp", "udp"}, "port": []uint16{53}, "action": "hijack-dns",
		})
	}
	if directID := enabledDirectRouteID(intent); directID != "" && len(endpoints) == 0 {
		// Linux auto_redirect otherwise captures ICMP echo into sing-box.
		// Pre-match bypass keeps ping on the kernel path; ICMP echo
		// that still arrives on TUN falls back to the required Direct route.
		routeRules = append(routeRules, map[string]any{
			"network": []string{"icmp"}, "action": "bypass", "outbound": routeTag(directID),
		})
	}
	if len(target.DirectRouteAddress) > 0 {
		for _, route := range intent.Routes {
			if route.Enabled && route.Kind == "direct" {
				direct := map[string]any{
					"ip_cidr": target.DirectRouteAddress, "action": "route", "outbound": routeTag(route.ID),
				}
				if len(target.SniffInboundTags) > 0 {
					direct["inbound"] = target.SniffInboundTags
				}
				if len(endpoints) > 0 {
					privateFallback = append(privateFallback, direct)
				} else {
					routeRules = append(routeRules, direct)
				}
				break
			}
		}
	}
	routeRules = append(routeRules, compileDirectBypass(intent, target)...)
	routeRules = append(routeRules, map[string]any{"inbound": sniffInboundTags, "action": "sniff", "timeout": "300ms"})
	// Keep server unset so sing-box reuses the DNS Router rules below to select
	// the rule-specific (DNS Profile, Route) path. Resolve is a non-final action
	// and sing-box only performs its lookup for domain destinations, leaving the
	// already-addressed TUN path as a no-op before normal route evaluation.
	routeRules = append(routeRules, map[string]any{"inbound": sniffInboundTags, "action": "resolve"})
	dnsRules := []any{}
	for _, t := range intent.WireGuardTunnels {
		if t.Enabled {
			for _, p := range t.Peers {
				if p.Server != "" {
					dnsRules = append(dnsRules, map[string]any{"domain": []string{p.Server}, "action": "route", "server": "steer-dns-bootstrap", "disable_cache": true})
				}
			}
		}
	}
	var defaultRule model.Rule
	for _, rule := range intent.Rules {
		if !rule.Enabled {
			continue
		}
		if rule.Default {
			defaultRule = rule
			continue
		}
		routeMatch := compileRuleMatch(rule, false)
		if routes[rule.Route].Kind == "block" {
			routeMatch["action"] = "reject"
		} else {
			routeMatch["action"] = "route"
			routeMatch["outbound"] = routeTag(rule.Route)
		}
		routeRules = append(routeRules, routeMatch)
		if dnsMatch := compileDNSMatch(rule); len(dnsMatch) > 0 {
			if routes[rule.Route].Kind == "block" {
				if len(model.DNSProjectionUnsupportedConditions(rule)) > 0 {
					continue
				}
				dnsMatch["action"] = "reject"
			} else {
				dnsMatch["action"] = "route"
				dnsMatch["server"] = dnsPathTag(rule.DNSProfile, rule.Route)
			}
			dnsRules = append(dnsRules, dnsMatch)
		}
	}
	routeRules = append(routeRules, privateFallback...)
	finalDNS := "steer-dns-bootstrap"
	if routes[defaultRule.Route].Kind == "block" {
		dnsRules = append(dnsRules, map[string]any{"action": "reject"})
	} else {
		finalDNS = dnsPathTag(defaultRule.DNSProfile, defaultRule.Route)
	}
	ruleSets := []any{}
	if len(geoRuleSets) > 0 {
		tags := make([]string, 0, len(geoRuleSets))
		for _, ruleSet := range geoRuleSets {
			tags = append(tags, ruleSet.Tag)
		}
		first := geoRuleSets[0]
		ruleSets = append(ruleSets, map[string]any{
			"type": "remote", "tag": tags, "format": "binary",
			"url": geoTemplate(first.URL, first.Tag), "initial_path": geoTemplate(first.InitialPath, first.Tag),
			"update_interval": "1d",
		})
	}
	dnsOptions := map[string]any{
		"servers": dnsServers, "rules": dnsRules, "final": finalDNS,
		"cache_capacity": intent.Main.DNSCacheCapacity, "reverse_mapping": true,
	}
	if intent.Main.DNSOptimisticCache {
		dnsOptions["optimistic"] = true
	}
	finalRoute := routeTag(defaultRule.Route)
	if routes[defaultRule.Route].Kind == "block" {
		routeRules = append(routeRules, map[string]any{"action": "reject"})
		for _, route := range intent.Routes {
			if route.Enabled && route.Kind == "direct" {
				finalRoute = routeTag(route.ID)
				break
			}
		}
	}
	routeOptions := map[string]any{
		"rules": routeRules, "rule_set": ruleSets, "final": finalRoute, "auto_detect_interface": true,
		"default_domain_resolver": map[string]any{"server": "steer-dns-bootstrap", "strategy": intent.Bootstrap.Strategy},
	}
	result := map[string]any{
		"log":       map[string]any{"level": intent.Main.LogLevel, "timestamp": true},
		"dns":       dnsOptions,
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"route":     routeOptions,
	}
	if len(endpoints) > 0 {
		result["endpoints"] = endpoints
	}
	if len(geoRuleSets) > 0 {
		result["http_clients"] = []any{map[string]any{"tag": "steer-geodata"}}
		routeOptions["default_http_client"] = "steer-geodata"
	}
	if len(geoRuleSets) > 0 || intent.Main.DNSCachePersist {
		cacheFile := map[string]any{
			"enabled": true, "path": strings.TrimRight(stateDirectory, "/") + "/cache.db", "cache_id": "steer",
		}
		if intent.Main.DNSCachePersist {
			cacheFile["store_dns"] = true
		}
		result["experimental"] = map[string]any{"cache_file": cacheFile}
	}
	return result
}

func geoTemplate(value, tag string) string {
	return strings.TrimSuffix(value, tag+".srs") + "{tag}.srs"
}

func compileNode(node model.Node) map[string]any {
	result := map[string]any{"type": node.Type, "tag": nodeTag(node.ID)}
	if node.Type != "tor" {
		result["server"] = node.Server
		if len(node.ServerPorts) > 0 {
			result["server_ports"] = node.ServerPorts
		} else {
			result["server_port"] = node.ServerPort
		}
	}
	switch node.Type {
	case "socks":
		if node.Username != "" {
			result["username"] = node.Username
		}
		if node.Password != "" {
			result["password"] = node.Password
		}
	case "http":
		result["username"], result["password"] = node.Username, node.Password
		if tls := compileTLSIfConfigured(node); tls != nil {
			result["tls"] = tls
		}
	case "shadowsocks":
		result["method"], result["password"] = node.Method, node.Password
		if node.Plugin != "" {
			result["plugin"], result["plugin_opts"] = node.Plugin, node.PluginOptions
		}
	case "vmess":
		result["uuid"] = node.UUID
		if node.Security != "" {
			result["security"] = node.Security
		}
		if node.AlterID != 0 {
			result["alter_id"] = node.AlterID
		}
		if node.Network != "" {
			result["network"] = node.Network
		}
		if node.PacketEncoding != "" {
			result["packet_encoding"] = node.PacketEncoding
		}
		if tls := compileTLSIfConfigured(node); tls != nil {
			result["tls"] = tls
		}
		if transport := compileTransport(node); transport != nil {
			result["transport"] = transport
		}
	case "vless":
		result["uuid"], result["flow"], result["packet_encoding"] = node.UUID, node.Flow, node.PacketEncoding
		if tls := compileTLSIfConfigured(node); tls != nil {
			result["tls"] = tls
		}
		if transport := compileTransport(node); transport != nil {
			result["transport"] = transport
		}
	case "trojan":
		result["password"] = node.Password
		if tls := compileTLSIfConfigured(node); tls != nil {
			result["tls"] = tls
		}
		if transport := compileTransport(node); transport != nil {
			result["transport"] = transport
		}
	case "hysteria":
		result["auth_str"], result["hop_interval"], result["up_mbps"], result["down_mbps"] = node.Password, node.HopInterval, node.UpMbps, node.DownMbps
		result["tls"] = compileTLS(node.TLSServerName, node.Insecure, node.UTLSFingerprint, "", "", node.ALPN)
		if node.ObfsPassword != "" {
			result["obfs"] = node.ObfsPassword
		}
	case "shadowtls":
		result["version"], result["password"] = node.Version, node.Password
		result["tls"] = compileTLS(node.TLSServerName, node.Insecure, node.UTLSFingerprint, "", "", node.ALPN)
	case "tuic":
		result["uuid"] = node.UUID
		if node.Password != "" {
			result["password"] = node.Password
		}
		result["congestion_control"] = node.CongestionControl
		if node.UDPRelayMode != "" {
			result["udp_relay_mode"] = node.UDPRelayMode
		} else if node.UDPOverStream {
			result["udp_over_stream"] = true
		}
		result["zero_rtt_handshake"], result["heartbeat"] = node.ZeroRTTHandshake, node.Heartbeat
		result["tls"] = compileTLS(node.TLSServerName, node.Insecure, node.UTLSFingerprint, "", "", node.ALPN)
	case "hysteria2":
		result["password"], result["hop_interval"], result["up_mbps"], result["down_mbps"] = node.Password, node.HopInterval, node.UpMbps, node.DownMbps
		result["tls"] = compileTLS(node.TLSServerName, node.Insecure, node.UTLSFingerprint, "", "", node.ALPN)
		if node.ObfsType != "" {
			result["obfs"] = map[string]any{"type": node.ObfsType, "password": node.ObfsPassword}
		}
	case "anytls":
		result["password"] = node.Password
		result["tls"] = compileTLS(node.TLSServerName, node.Insecure, node.UTLSFingerprint, "", "", node.ALPN)
	case "naive":
		result["username"], result["password"] = node.Username, node.Password
		if node.InsecureConcurrency != 0 {
			result["insecure_concurrency"] = node.InsecureConcurrency
		}
		if node.QUIC {
			result["quic"] = true
		}
		if node.QUICCongestionControl != "" {
			result["quic_congestion_control"] = node.QUICCongestionControl
		}
		result["tls"] = compileTLS(node.TLSServerName, node.Insecure, node.UTLSFingerprint, "", "", node.ALPN)
	case "ssh":
		result["user"], result["password"] = node.Username, node.Password
		if node.PrivateKey != "" {
			result["private_key"] = node.PrivateKey
		}
		if len(node.HostKeyAlgorithms) > 0 {
			result["host_key_algorithms"] = node.HostKeyAlgorithms
		}
		if node.HostKey != "" {
			result["host_key"] = []string{node.HostKey}
		}
	case "tor":
		if node.ExecutablePath != "" {
			result["executable_path"] = node.ExecutablePath
		}
		if len(node.ExtraArgs) > 0 {
			result["extra_args"] = node.ExtraArgs
		}
		if node.DataDirectory != "" {
			result["data_directory"] = node.DataDirectory
		}
	}
	return clean(result)
}

// CompileNodeOutbound exposes the same typed lowering used by the final config
// for temporary node diagnostics such as speed tests.
func CompileNodeOutbound(node model.Node) map[string]any { return compileNode(node) }

func NodeOutboundTag(id string) string { return nodeTag(id) }

func RouteOutboundTag(id string) string { return routeTag(id) }

// CompileRouteChainOutbounds returns only the target single-node Route and its
// detour ancestry. Callers must validate the Intent before using the result.
func CompileRouteChainOutbounds(intent model.Intent, routeID string) []any {
	routes := indexRoutes(intent.Routes)
	included := map[string]bool{}
	for current := routeID; current != "" && !included[current]; {
		route, exists := routes[current]
		if !exists || !route.Enabled || route.Kind != "single" {
			return nil
		}
		included[current] = true
		current = route.Detour
	}
	nodes := indexNodes(intent.Nodes)
	outbounds := make([]any, 0, len(included))
	for _, route := range intent.Routes {
		if included[route.ID] {
			outbounds = append(outbounds, compileRouteOutbound(route, nodes))
		}
	}
	return outbounds
}

func compileRouteOutbound(route model.Route, nodes map[string]model.Node) map[string]any {
	outbound := compileNode(nodes[route.Node])
	outbound["tag"] = routeTag(route.ID)
	if route.Detour != "" {
		outbound["detour"] = routeTag(route.Detour)
	}
	return outbound
}

func compileTLSIfConfigured(node model.Node) map[string]any {
	if node.TLSServerName == "" && len(node.ALPN) == 0 && node.RealityPublicKey == "" && node.RealityShortID == "" && !node.Insecure && node.UTLSFingerprint == "" {
		return nil
	}
	return compileTLS(node.TLSServerName, node.Insecure, node.UTLSFingerprint, node.RealityPublicKey, node.RealityShortID, node.ALPN)
}

func compileTransport(node model.Node) map[string]any {
	switch node.Transport {
	case "", "tcp", "raw":
		return nil
	case "ws":
		result := map[string]any{"type": "ws", "path": node.TransportPath}
		if node.TransportHost != "" {
			result["headers"] = map[string]any{"Host": []string{node.TransportHost}}
		}
		return result
	case "grpc":
		return map[string]any{"type": "grpc", "service_name": node.ServiceName}
	case "http":
		result := map[string]any{"type": "http", "path": node.TransportPath}
		if node.TransportHost != "" {
			result["host"] = []string{node.TransportHost}
		}
		return result
	case "quic":
		return map[string]any{"type": "quic"}
	default:
		return nil
	}
}

func compileTLS(serverName string, insecure bool, fingerprint, publicKey, shortID string, alpn ...[]string) map[string]any {
	result := map[string]any{"enabled": true, "server_name": serverName, "insecure": insecure}
	if len(alpn) > 0 && len(alpn[0]) > 0 {
		result["alpn"] = append([]string(nil), alpn[0]...)
	}
	if fingerprint != "" {
		result["utls"] = map[string]any{"enabled": true, "fingerprint": fingerprint}
	}
	if publicKey != "" || shortID != "" {
		result["reality"] = map[string]any{"enabled": true, "public_key": publicKey, "short_id": shortID}
	}
	return clean(result)
}

func compileDNSPath(profile model.DNSProfile, route model.Route, path DNSPath, domainResolverStrategy string) map[string]any {
	result := map[string]any{"type": profile.Protocol, "tag": path.Tag, "server": profile.Server, "server_port": profile.ServerPort}
	if route.Kind == "single" || route.Kind == "wireguard" {
		result["detour"] = routeTag(path.Route)
	}
	if _, err := netip.ParseAddr(profile.Server); err != nil {
		result["domain_resolver"] = map[string]any{"server": "steer-dns-bootstrap", "strategy": domainResolverStrategy}
	}
	if profile.Protocol == "tls" || profile.Protocol == "https" || profile.Protocol == "quic" || profile.Protocol == "h3" {
		result["tls"] = map[string]any{"enabled": true, "server_name": profile.TLSServerName, "insecure": profile.Insecure}
	}
	if profile.Protocol == "https" || profile.Protocol == "h3" {
		result["path"] = profile.Path
	}
	return clean(result)
}

func compileRuleMatch(rule model.Rule, dns bool) map[string]any {
	groups := []map[string]any{}
	if len(rule.Inbound) > 0 {
		values := make([]string, len(rule.Inbound))
		for i, value := range rule.Inbound {
			values[i] = localProxyTag(value)
		}
		groups = append(groups, map[string]any{"inbound": values})
	}
	if len(rule.SourceMACAddress) > 0 {
		values := make([]string, len(rule.SourceMACAddress))
		for i, address := range rule.SourceMACAddress {
			values[i] = strings.ToLower(address)
		}
		groups = append(groups, map[string]any{"source_mac_address": values})
	}
	if len(rule.DomainMatch) > 0 {
		groups = append(groups, compileDomainGroup(rule.DomainMatch))
	}
	if !dns && len(rule.IPMatch) > 0 {
		groups = append(groups, compileIPGroup(rule.IPMatch))
	}
	if len(rule.SourceIPCIDR) > 0 {
		groups = append(groups, map[string]any{"source_ip_cidr": rule.SourceIPCIDR})
	}
	if !dns {
		if len(rule.Network) > 0 {
			groups = append(groups, map[string]any{"network": rule.Network})
		}
		if len(rule.Protocol) > 0 {
			groups = append(groups, map[string]any{"protocol": rule.Protocol})
		}
		if len(rule.Port) > 0 {
			groups = append(groups, map[string]any{"port": rule.Port})
		}
	}
	return combine(groups, "and")
}

func compileDNSMatch(rule model.Rule) map[string]any {
	return compileRuleMatch(rule, true)
}

func compileDomainGroup(expressions []string) map[string]any {
	groups := []map[string]any{}
	fields := map[string][]string{}
	for _, expression := range expressions {
		field, value := "domain_keyword", expression
		switch {
		case strings.HasPrefix(expression, "full:"):
			field, value = "domain", strings.TrimPrefix(expression, "full:")
		case strings.HasPrefix(expression, "domain:"):
			field, value = "domain_suffix", strings.TrimPrefix(expression, "domain:")
		case strings.HasPrefix(expression, "regexp:"):
			field, value = "domain_regex", strings.TrimPrefix(expression, "regexp:")
		case strings.HasPrefix(expression, "geosite:"):
			field, value = "rule_set", "steer-geosite-"+strings.TrimPrefix(expression, "geosite:")
		}
		fields[field] = append(fields[field], value)
	}
	keys := sortedKeys(fields)
	for _, field := range keys {
		groups = append(groups, map[string]any{field: fields[field]})
	}
	return combine(groups, "or")
}

func compileIPGroup(expressions []string) map[string]any {
	fields := map[string][]string{}
	for _, expression := range expressions {
		if strings.HasPrefix(expression, "geoip:") {
			fields["rule_set"] = append(fields["rule_set"], "steer-geoip-"+strings.TrimPrefix(expression, "geoip:"))
		} else {
			fields["ip_cidr"] = append(fields["ip_cidr"], expression)
		}
	}
	groups := []map[string]any{}
	for _, field := range sortedKeys(fields) {
		groups = append(groups, map[string]any{field: fields[field]})
	}
	return combine(groups, "or")
}

func combine(groups []map[string]any, mode string) map[string]any {
	if len(groups) == 0 {
		return map[string]any{}
	}
	if len(groups) == 1 {
		return groups[0]
	}
	rules := make([]any, len(groups))
	for index, group := range groups {
		rules[index] = group
	}
	return map[string]any{"type": "logical", "mode": mode, "rules": rules}
}

func clean(value map[string]any) map[string]any {
	for key, item := range value {
		switch typed := item.(type) {
		case string:
			if typed == "" {
				delete(value, key)
			}
		case bool:
			if !typed {
				delete(value, key)
			}
		case int:
			if typed == 0 {
				delete(value, key)
			}
		case []string:
			if len(typed) == 0 {
				delete(value, key)
			}
		case map[string]any:
			value[key] = clean(typed)
			if len(value[key].(map[string]any)) == 0 {
				delete(value, key)
			}
		}
	}
	return value
}

func indexDNSProfiles(values []model.DNSProfile) map[string]model.DNSProfile {
	result := map[string]model.DNSProfile{}
	for _, value := range values {
		result[value.ID] = value
	}
	return result
}
func indexRoutes(values []model.Route) map[string]model.Route {
	result := map[string]model.Route{}
	for _, value := range values {
		result[value.ID] = value
	}
	return result
}
func indexNodes(values []model.Node) map[string]model.Node {
	result := map[string]model.Node{}
	for _, value := range values {
		result[value.ID] = value
	}
	return result
}
func enabledDirectRouteID(intent model.Intent) string {
	for _, route := range intent.Routes {
		if route.Enabled && route.Kind == "direct" {
			return route.ID
		}
	}
	return ""
}

func routeTag(id string) string               { return "steer-route-" + id }
func nodeTag(id string) string                { return "steer-node-" + id }
func localProxyTag(id string) string          { return "steer-local-" + id }
func dnsPathTag(profile, route string) string { return "steer-dns-" + profile + "-via-" + route }
func sortedKeys[V any](values map[string]V) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
func unique(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
