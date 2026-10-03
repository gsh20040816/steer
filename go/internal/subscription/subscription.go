// SPDX-License-Identifier: GPL-3.0-or-later

// Package subscription parses the deliberately small, interoperable node
// subscription format accepted by Steer: one standard proxy URI per line or
// one Base64-wrapped block containing those lines.  It never preserves an
// opaque JSON blob; every field is lowered into model.Node and validated.
package subscription

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

type ParseResult struct {
	Nodes          []model.Node
	Skipped        int
	SkippedReasons []SkippedReason
}

// SkippedReason is a safe, bounded explanation for one rejected subscription
// entry. It intentionally contains no URI, credentials, or raw parser error.
type SkippedReason struct {
	Scheme    string `json:"scheme,omitempty"`
	Code      string `json:"code"`
	Parameter string `json:"parameter,omitempty"`
	Detail    string `json:"detail"`
}

func ParseList(raw string) ParseResult {
	result := ParseResult{Nodes: []model.Node{}}
	text := strings.TrimSpace(raw)
	if text == "" {
		return result
	}
	if !strings.Contains(text, "://") {
		decoded, err := decodeBase64(text)
		if err == nil {
			candidate := strings.TrimSpace(decoded)
			if strings.Contains(candidate, "://") {
				text = candidate
			} else if strings.HasPrefix(candidate, "{") {
				node, parseErr := parseVMessPayload(candidate)
				appendParsedNode(&result, node, parseErr, "vmess")
				return result
			}
		}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		node, err := ParseURI(line)
		appendParsedNode(&result, node, err, line)
	}
	return result
}

func appendParsedNode(result *ParseResult, node model.Node, err error, raw string) {
	if err == nil {
		validation := model.ValidateNode(node)
		if !validation.OK {
			err = fmt.Errorf("invalid node")
		}
	}
	if err != nil {
		result.Skipped++
		reason := skippedReason(err)
		reason.Scheme = uriScheme(raw)
		result.SkippedReasons = append(result.SkippedReasons, reason)
		return
	}
	result.Nodes = append(result.Nodes, node)
}

func uriScheme(raw string) string {
	if separator := strings.IndexByte(raw, ':'); separator > 0 {
		return strings.ToLower(raw[:separator])
	}
	if raw == "vmess" || raw == "ss" {
		return raw
	}
	return "unknown"
}

func skippedReason(err error) SkippedReason {
	message := err.Error()
	reason := SkippedReason{Code: "INVALID_NODE", Detail: "节点格式或参数无效"}
	reason.Parameter = quotedParameter(message)
	if strings.Contains(message, "unsupported node URI scheme") {
		reason.Code, reason.Detail = "UNSUPPORTED_SCHEME", "不支持的节点协议"
	} else if strings.Contains(message, "unsupported URI parameter") {
		reason.Code, reason.Detail = "UNSUPPORTED_PARAMETER", "包含不支持的分享链接参数"
	} else if strings.Contains(message, "must be boolean") {
		reason.Code, reason.Detail = "INVALID_BOOLEAN", "布尔参数值无效"
	} else if strings.Contains(message, "conflicting URI parameter") {
		reason.Code, reason.Detail = "CONFLICTING_PARAMETER", "分享链接参数互相冲突"
	} else if strings.Contains(message, "invalid VMess") || strings.Contains(message, "VMess ") {
		reason.Code, reason.Detail = "INVALID_VMESS", "VMess 分享链接格式无效"
	} else if strings.Contains(message, "invalid Base64") || strings.Contains(message, "Base64") {
		reason.Code, reason.Detail = "INVALID_BASE64", "Base64 内容无效"
	} else if strings.Contains(message, "ALPN") || strings.Contains(message, "alpn") {
		reason.Code, reason.Detail = "INVALID_ALPN", "ALPN 参数无效"
	}
	return reason
}

func quotedParameter(message string) string {
	start := strings.IndexByte(message, '"')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(message[start+1:], '"')
	if end < 0 {
		return ""
	}
	return message[start+1 : start+1+end]
}

func ParseURI(raw string) (model.Node, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return model.Node{}, fmt.Errorf("invalid node URI")
	}
	if strings.IndexFunc(u.Fragment, unicode.IsControl) >= 0 {
		return model.Node{}, fmt.Errorf("node URI fragment contains control characters")
	}
	if _, err := url.ParseQuery(u.RawQuery); err != nil {
		return model.Node{}, fmt.Errorf("invalid node URI query: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "vmess" {
		return parseVMess(raw)
	}
	if scheme == "ss" {
		return parseShadowsocks(raw, u)
	}
	if scheme == "socks5" {
		return parseCredentialProxy(u, "socks")
	}
	if scheme == "socks" || scheme == "http" || scheme == "https" {
		return parseCredentialProxy(u, scheme)
	}
	if scheme == "vless" || scheme == "trojan" || scheme == "hysteria" || scheme == "hysteria2" || scheme == "hy2" || scheme == "shadowtls" || scheme == "tuic" || scheme == "anytls" || scheme == "naive+https" || scheme == "ssh" {
		return parseCredentialNode(u, scheme)
	}
	return model.Node{}, fmt.Errorf("unsupported node URI scheme %q", u.Scheme)
}

func parseCredentialProxy(u *url.URL, scheme string) (model.Node, error) {
	if u.Path != "" && u.Path != "/" {
		return model.Node{}, fmt.Errorf("%s URI path is unsupported", scheme)
	}
	if u.Hostname() == "" {
		return model.Node{}, fmt.Errorf("%s URI has no server", scheme)
	}
	defaultPort := map[string]int{"socks": 1080, "http": 80, "https": 443}[scheme]
	node := model.Node{Enabled: true, Type: scheme, Name: u.Fragment, Server: u.Hostname(), ServerPort: port(u, defaultPort)}
	if scheme == "https" {
		node.Type = "http"
		node.TLSServerName = u.Hostname()
	}
	if u.User != nil {
		node.Username = u.User.Username()
		node.Password, _ = u.User.Password()
	}
	if node.Name == "" {
		node.Name = node.Type + " " + u.Host
	}
	if err := rejectUnknown(u.Query(), map[string]bool{"sni": true, "insecure": true, "alpn": true, "fp": true, "fingerprint": true}); err != nil {
		return model.Node{}, err
	}
	if err := validateQueryValues(u.Query()); err != nil {
		return model.Node{}, err
	}
	if value := u.Query().Get("sni"); value != "" {
		node.TLSServerName = value
	}
	if value := u.Query().Get("insecure"); value != "" {
		node.Insecure, _ = strconv.ParseBool(value)
	}
	alpn, err := parseALPN(u.Query())
	if err != nil {
		return model.Node{}, err
	}
	node.ALPN = alpn
	node.UTLSFingerprint = first(u.Query().Get("fp"), u.Query().Get("fingerprint"))
	return node, nil
}

func parseCredentialNode(u *url.URL, scheme string) (model.Node, error) {
	if u.Path != "" && u.Path != "/" {
		return model.Node{}, fmt.Errorf("%s URI path is unsupported", scheme)
	}
	if u.Hostname() == "" {
		return model.Node{}, fmt.Errorf("%s URI has no server", scheme)
	}
	node := model.Node{Enabled: true, Server: u.Hostname(), ServerPort: port(u, 443), Name: u.Fragment}
	if node.Name == "" {
		node.Name = scheme + " " + u.Host
	}
	username := ""
	password := ""
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}
	query := u.Query()
	allowed := map[string][]string{
		"vless":       {"encryption", "flow", "security", "sni", "serverName", "fp", "fingerprint", "pcs", "pbk", "publicKey", "sid", "shortId", "type", "packetEncoding", "packet_encoding", "allowInsecure", "allow_insecure", "insecure", "alpn", "path", "host", "serviceName"},
		"trojan":      {"sni", "serverName", "peer", "fp", "fingerprint", "pcs", "type", "allowInsecure", "allow_insecure", "insecure", "alpn", "path", "host", "serviceName"},
		"hysteria":    {"sni", "peer", "insecure", "allowInsecure", "allow_insecure", "alpn", "fp", "fingerprint", "obfs", "obfs-password", "hop-interval", "hopInterval", "mport", "upmbps", "upMbps", "downmbps", "downMbps"},
		"hysteria2":   {"sni", "insecure", "allowInsecure", "allow_insecure", "alpn", "fp", "fingerprint", "obfs", "obfs-password", "hop-interval", "hopInterval", "mport", "upmbps", "upMbps", "downmbps", "downMbps"},
		"hy2":         {"sni", "insecure", "allowInsecure", "allow_insecure", "alpn", "fp", "fingerprint", "obfs", "obfs-password", "hop-interval", "hopInterval", "mport", "upmbps", "upMbps", "downmbps", "downMbps"},
		"shadowtls":   {"version", "sni", "insecure", "allowInsecure", "allow_insecure", "alpn", "fp", "fingerprint"},
		"tuic":        {"congestion_control", "udp_relay_mode", "udp_over_stream", "zero_rtt_handshake", "heartbeat", "sni", "insecure", "allowInsecure", "allow_insecure", "alpn", "fp", "fingerprint"},
		"anytls":      {"sni", "insecure", "allowInsecure", "allow_insecure", "alpn", "fp", "fingerprint", "pcs", "type"},
		"naive+https": {"sni", "insecure", "allowInsecure", "allow_insecure", "alpn", "quic", "quic_congestion_control", "insecure_concurrency", "fp", "fingerprint"},
		"ssh":         {},
	}
	if err := rejectUnknown(query, setAllowed(allowed[scheme])); err != nil {
		return model.Node{}, err
	}
	if err := validateQueryValues(query); err != nil {
		return model.Node{}, err
	}
	for _, value := range query["pcs"] {
		if value != "" {
			return model.Node{}, fmt.Errorf("unsupported URI parameter %q with a non-empty certificate pin", "pcs")
		}
	}
	if scheme == "anytls" && query.Get("type") != "" && !strings.EqualFold(query.Get("type"), "tcp") {
		return model.Node{}, fmt.Errorf("anytls URI parameter %q must be tcp", "type")
	}
	node.UTLSFingerprint = first(query.Get("fp"), query.Get("fingerprint"))
	alpn, parseErr := parseALPN(query)
	if parseErr != nil {
		return model.Node{}, parseErr
	}
	node.ALPN = alpn
	switch scheme {
	case "vless":
		node.Type, node.UUID = "vless", username
		if node.UUID == "" {
			return model.Node{}, fmt.Errorf("vless URI requires UUID")
		}
		if encryption := query.Get("encryption"); encryption != "" && encryption != "none" {
			return model.Node{}, fmt.Errorf("unsupported VLESS encryption %q", encryption)
		}
		node.Flow, node.PacketEncoding = query.Get("flow"), first(query.Get("packetEncoding"), query.Get("packet_encoding"))
		if node.Flow != "" && node.Flow != "xtls-rprx-vision" {
			return model.Node{}, fmt.Errorf("unsupported VLESS flow %q", node.Flow)
		}
		if node.PacketEncoding != "" && node.PacketEncoding != "xudp" && node.PacketEncoding != "packetaddr" {
			return model.Node{}, fmt.Errorf("unsupported VLESS packet encoding %q", node.PacketEncoding)
		}
		security := first(query.Get("security"), "none")
		if security != "none" && security != "tls" && security != "reality" {
			return model.Node{}, fmt.Errorf("unsupported VLESS security %q", security)
		}
		sni := first(query.Get("sni"), query.Get("serverName"))
		publicKey := first(query.Get("pbk"), query.Get("publicKey"))
		shortID := first(query.Get("sid"), query.Get("shortId"))
		fingerprint := first(query.Get("fp"), query.Get("fingerprint"))
		insecure := boolValue(first(query.Get("allow_insecure"), query.Get("allowInsecure"), query.Get("insecure")))
		if security != "reality" && (publicKey != "" || shortID != "") {
			return model.Node{}, fmt.Errorf("VLESS Reality parameters require security=reality")
		}
		if security == "none" && (sni != "" || fingerprint != "" || insecure || node.Flow != "") {
			return model.Node{}, fmt.Errorf("VLESS TLS parameters require security=tls or security=reality")
		}
		switch security {
		case "tls":
			if sni == "" {
				return model.Node{}, fmt.Errorf("VLESS TLS requires sni")
			}
			node.TLSServerName, node.UTLSFingerprint = sni, fingerprint
		case "reality":
			if sni == "" || publicKey == "" || shortID == "" || fingerprint == "" {
				return model.Node{}, fmt.Errorf("VLESS Reality requires sni, public key, short ID and fingerprint")
			}
			node.TLSServerName, node.RealityPublicKey, node.RealityShortID, node.UTLSFingerprint = sni, publicKey, shortID, fingerprint
		}
		node.Insecure = insecure
		setTransport(&node, query)
	case "trojan":
		node.Type, node.Password = "trojan", username
		node.TLSServerName = first(query.Get("sni"), query.Get("serverName"), query.Get("peer"), node.Server)
		// uTLS fingerprint is carried by the shared TLS group.
		node.Insecure = boolValue(first(query.Get("allow_insecure"), query.Get("allowInsecure"), query.Get("insecure")))
		setTransport(&node, query)
	case "hysteria", "hysteria2", "hy2":
		node.Type, node.Password = "hysteria2", username
		if scheme == "hysteria" {
			node.Type = "hysteria"
		}
		node.TLSServerName = first(query.Get("sni"), query.Get("peer"), node.Server)
		node.Insecure = boolValue(first(query.Get("insecure"), query.Get("allow_insecure"), query.Get("allowInsecure")))
		node.HopInterval = first(query.Get("hop-interval"), query.Get("hopInterval"))
		node.ObfsType, node.ObfsPassword = query.Get("obfs"), query.Get("obfs-password")
		if scheme == "hysteria" {
			node.ObfsPassword, node.ObfsType = first(query.Get("obfs-password"), query.Get("obfs")), ""
		}
		node.ServerPorts = splitPorts(query.Get("mport"))
		node.UpMbps = intValue(first(query.Get("upmbps"), query.Get("upMbps")))
		node.DownMbps = intValue(first(query.Get("downmbps"), query.Get("downMbps")))
	case "shadowtls":
		node.Type, node.Password = "shadowtls", username
		node.Version = intValue(first(query.Get("version"), "1"))
		node.TLSServerName = first(query.Get("sni"), node.Server)
		node.Insecure = boolValue(first(query.Get("insecure"), query.Get("allow_insecure"), query.Get("allowInsecure")))
	case "tuic":
		node.Type, node.UUID, node.Password = "tuic", username, password
		node.CongestionControl, node.UDPRelayMode = query.Get("congestion_control"), query.Get("udp_relay_mode")
		node.UDPOverStream, node.ZeroRTTHandshake = boolValue(query.Get("udp_over_stream")), boolValue(query.Get("zero_rtt_handshake"))
		node.Heartbeat, node.TLSServerName = query.Get("heartbeat"), first(query.Get("sni"), node.Server)
		node.Insecure = boolValue(first(query.Get("insecure"), query.Get("allow_insecure"), query.Get("allowInsecure")))
	case "anytls":
		node.Type, node.Password = "anytls", username
		node.TLSServerName, node.Insecure = first(query.Get("sni"), node.Server), boolValue(first(query.Get("insecure"), query.Get("allow_insecure"), query.Get("allowInsecure")))
	case "naive+https":
		node.Type, node.Username, node.Password = "naive", username, password
		node.TLSServerName, node.Insecure = first(query.Get("sni"), node.Server), boolValue(first(query.Get("insecure"), query.Get("allow_insecure"), query.Get("allowInsecure")))
		node.QUIC, node.QUICCongestionControl = boolValue(query.Get("quic")), query.Get("quic_congestion_control")
		node.InsecureConcurrency = intValue(query.Get("insecure_concurrency"))
	case "ssh":
		node.Type, node.Username, node.Password = "ssh", username, password
	}
	return node, nil
}

func parseShadowsocks(raw string, u *url.URL) (model.Node, error) {
	if err := rejectUnknown(u.Query(), map[string]bool{"plugin": true, "plugin-opts": true}); err != nil {
		return model.Node{}, err
	}
	if err := validateQueryValues(u.Query()); err != nil {
		return model.Node{}, err
	}
	plugin, pluginOptions, err := shadowsocksPlugin(u.Query())
	if err != nil {
		return model.Node{}, err
	}
	var credential, server string
	serverPort := 443
	if u.User != nil && u.Hostname() != "" {
		server, serverPort = u.Hostname(), port(u, 443)
		if password, plaintext := u.User.Password(); plaintext {
			credential = u.User.Username() + ":" + password
		} else {
			credential, err = decodeBase64(u.User.Username())
			if err != nil {
				return model.Node{}, fmt.Errorf("invalid Shadowsocks credential: %w", err)
			}
		}
	} else {
		payload, decodeErr := decodeBase64(opaquePayload(raw))
		if decodeErr != nil {
			return model.Node{}, fmt.Errorf("invalid legacy Shadowsocks payload: %w", decodeErr)
		}
		at := strings.LastIndex(payload, "@")
		if at < 0 {
			return model.Node{}, fmt.Errorf("ss URI must contain method/password and server")
		}
		credential = payload[:at]
		serverURL, parseErr := url.Parse("ss://" + payload[at+1:])
		if parseErr != nil || serverURL.Hostname() == "" {
			return model.Node{}, fmt.Errorf("invalid Shadowsocks server")
		}
		server, serverPort = serverURL.Hostname(), port(serverURL, 443)
	}
	methodPassword := strings.SplitN(credential, ":", 2)
	if len(methodPassword) != 2 {
		return model.Node{}, fmt.Errorf("ss URI credential must be method:password")
	}
	if methodPassword[0] == "" || methodPassword[1] == "" {
		return model.Node{}, fmt.Errorf("ss URI method and password must be non-empty")
	}
	return model.Node{Enabled: true, Type: "shadowsocks", Name: u.Fragment, Server: server, ServerPort: serverPort, NodeCredentials: model.NodeCredentials{Password: methodPassword[1]}, NodeProtocol: model.NodeProtocol{Method: methodPassword[0], Plugin: plugin, PluginOptions: pluginOptions}}, nil
}

func shadowsocksPlugin(query url.Values) (string, string, error) {
	plugin := query.Get("plugin")
	options := ""
	if separator := strings.IndexByte(plugin, ';'); separator >= 0 {
		plugin, options = plugin[:separator], plugin[separator+1:]
	}
	legacyOptions := query.Get("plugin-opts")
	if options != "" && legacyOptions != "" && options != legacyOptions {
		return "", "", fmt.Errorf("conflicting Shadowsocks plugin options")
	}
	if options == "" {
		options = legacyOptions
	}
	if plugin == "" && options != "" {
		return "", "", fmt.Errorf("Shadowsocks plugin options require a plugin")
	}
	return plugin, options, nil
}

func opaquePayload(raw string) string {
	separator := strings.IndexByte(raw, ':')
	if separator < 0 {
		return ""
	}
	payload := strings.TrimPrefix(raw[separator+1:], "//")
	if end := strings.IndexAny(payload, "?#"); end >= 0 {
		payload = payload[:end]
	}
	return payload
}

func parseVMess(raw string) (model.Node, error) {
	decoded, err := decodeBase64(opaquePayload(raw))
	if err != nil {
		return model.Node{}, fmt.Errorf("invalid VMess Base64 payload: %w", err)
	}
	return parseVMessPayload(decoded)
}

func parseVMessPayload(decoded string) (model.Node, error) {
	var value struct {
		Name           string          `json:"ps"`
		Add            string          `json:"add"`
		Port           json.Number     `json:"port"`
		UUID           string          `json:"id"`
		AlterID        json.RawMessage `json:"aid"`
		Security       string          `json:"scy"`
		Network        string          `json:"net"`
		TLS            string          `json:"tls"`
		SNI            string          `json:"sni"`
		Host           string          `json:"host"`
		Path           string          `json:"path"`
		Type           string          `json:"type"`
		ALPN           json.RawMessage `json:"alpn"`
		FP             string          `json:"fp"`
		PacketEncoding string          `json:"packetEncoding"`
		AllowInsecure  json.RawMessage `json:"allowInsecure"`
		SkipCertVerify json.RawMessage `json:"skip-cert-verify"`
	}
	// v2rayN-style payloads vary between clients ("v" may be a number, extra
	// keys appear), so unknown fields are ignored.
	decoder := json.NewDecoder(strings.NewReader(decoded))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return model.Node{}, fmt.Errorf("invalid VMess JSON payload: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return model.Node{}, fmt.Errorf("invalid VMess JSON payload: trailing value")
		}
		return model.Node{}, fmt.Errorf("invalid VMess JSON payload: %w", err)
	}
	serverPort, err := strconv.Atoi(value.Port.String())
	if err != nil {
		return model.Node{}, fmt.Errorf("invalid VMess port")
	}
	node := model.Node{Enabled: true, Type: "vmess", Name: value.Name, Server: value.Add, ServerPort: serverPort,
		NodeCredentials: model.NodeCredentials{UUID: value.UUID},
		NodeTransport:   model.NodeTransport{Transport: value.Network, TransportHost: value.Host, TransportPath: value.Path, PacketEncoding: value.PacketEncoding},
		NodeProtocol:    model.NodeProtocol{AlterID: rawInt(value.AlterID), Security: value.Security},
		NodeTLS:         model.NodeTLS{TLSServerName: value.SNI, UTLSFingerprint: value.FP}}
	if node.Transport == "grpc" {
		node.ServiceName, node.TransportPath = value.Path, ""
	}
	if len(value.ALPN) > 0 {
		var alpnValue string
		var alpnValues []string
		if err := json.Unmarshal(value.ALPN, &alpnValue); err == nil {
			// v2rayN writes "alpn":"" when no ALPN is configured.
			if alpnValue != "" {
				alpnValues = strings.Split(alpnValue, ",")
			}
		} else if err := json.Unmarshal(value.ALPN, &alpnValues); err != nil {
			return model.Node{}, fmt.Errorf("VMess alpn must be a string or string list")
		}
		if err := validateALPNValues(alpnValues); err != nil {
			return model.Node{}, err
		}
		node.ALPN = alpnValues
	}
	allowInsecure := value.AllowInsecure
	if len(allowInsecure) == 0 {
		allowInsecure = value.SkipCertVerify
	} else if len(value.SkipCertVerify) > 0 {
		firstValue, firstErr := rawBool(value.AllowInsecure, "allowInsecure")
		secondValue, secondErr := rawBool(value.SkipCertVerify, "skip-cert-verify")
		if firstErr != nil {
			return model.Node{}, firstErr
		}
		if secondErr != nil {
			return model.Node{}, secondErr
		}
		if firstValue != secondValue {
			return model.Node{}, fmt.Errorf("conflicting VMess certificate verification parameters")
		}
	}
	if len(allowInsecure) > 0 {
		insecure, err := rawBool(allowInsecure, "allowInsecure")
		if err != nil {
			return model.Node{}, err
		}
		node.Insecure = insecure
	}
	if value.TLS != "" && value.TLS != "none" {
		node.TLSServerName = first(node.TLSServerName, node.Server)
	}
	if node.Name == "" {
		node.Name = "VMess " + node.Server
	}
	return node, nil
}

func rawBool(value json.RawMessage, key string) (bool, error) {
	if strings.TrimSpace(string(value)) == "null" {
		return false, fmt.Errorf("VMess %q must be boolean", key)
	}
	var boolean bool
	if err := json.Unmarshal(value, &boolean); err == nil {
		return boolean, nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		return parseQueryBool(text, key)
	}
	var number json.Number
	if err := json.Unmarshal(value, &number); err == nil {
		return parseQueryBool(number.String(), key)
	}
	return false, fmt.Errorf("VMess %q must be boolean", key)
}

func rawInt(value json.RawMessage) int {
	var number int
	if json.Unmarshal(value, &number) == nil {
		return number
	}
	var text string
	if json.Unmarshal(value, &text) == nil {
		return intValue(text)
	}
	return 0
}

func setTransport(node *model.Node, query url.Values) {
	node.Transport, node.TransportPath, node.TransportHost, node.ServiceName = query.Get("type"), query.Get("path"), query.Get("host"), query.Get("serviceName")
	if node.Transport == "" {
		node.Transport = "tcp"
	}
}

func rejectUnknown(values url.Values, allowed map[string]bool) error {
	for key := range values {
		if !allowed[key] {
			return fmt.Errorf("unsupported URI parameter %q", key)
		}
	}
	return nil
}

func setAllowed(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func validateQueryValues(values url.Values) error {
	booleanKeys := map[string]bool{"allow_insecure": true, "allowInsecure": true, "insecure": true, "udp_over_stream": true, "zero_rtt_handshake": true, "quic": true}
	for key, entries := range values {
		if len(entries) > 1 {
			if booleanKeys[key] {
				firstValue, err := parseQueryBool(entries[0], key)
				if err != nil {
					return err
				}
				for _, entry := range entries[1:] {
					value, err := parseQueryBool(entry, key)
					if err != nil {
						return err
					}
					if value != firstValue {
						return fmt.Errorf("conflicting URI parameter %q", key)
					}
				}
			} else {
				for _, entry := range entries[1:] {
					if entry != entries[0] {
						return fmt.Errorf("conflicting URI parameter %q", key)
					}
				}
			}
		}
	}
	for _, aliases := range [][]string{{"sni", "serverName", "peer"}, {"fp", "fingerprint"}, {"pbk", "publicKey"}, {"sid", "shortId"}, {"packetEncoding", "packet_encoding"}, {"hop-interval", "hopInterval"}, {"upmbps", "upMbps"}, {"downmbps", "downMbps"}} {
		var previous string
		for _, alias := range aliases {
			if value := values.Get(alias); value != "" {
				if previous != "" && previous != value {
					return fmt.Errorf("conflicting URI parameter aliases %q", strings.Join(aliases, "/"))
				}
				previous = value
			}
		}
	}
	var insecureValue *bool
	for _, key := range []string{"allow_insecure", "allowInsecure", "insecure"} {
		entries, present := values[key]
		if !present {
			continue
		}
		for _, entry := range entries {
			parsed, err := parseQueryBool(entry, key)
			if err != nil {
				return err
			}
			if insecureValue != nil && *insecureValue != parsed {
				return fmt.Errorf("conflicting URI parameter aliases %q", "allow_insecure/allowInsecure/insecure")
			}
			value := parsed
			insecureValue = &value
		}
	}
	for _, key := range []string{"udp_over_stream", "zero_rtt_handshake", "quic"} {
		if entries, present := values[key]; present {
			for _, entry := range entries {
				if _, err := parseQueryBool(entry, key); err != nil {
					return err
				}
			}
		}
	}
	if _, present := values["alpn"]; present {
		if _, err := parseALPN(values); err != nil {
			return err
		}
	}
	for _, key := range []string{"version", "upmbps", "upMbps", "downmbps", "downMbps", "insecure_concurrency"} {
		if value := values.Get(key); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 0 {
				return fmt.Errorf("URI parameter %q must be a non-negative integer", key)
			}
		}
	}
	return nil
}

func parseQueryBool(value, key string) (bool, error) {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("URI parameter %q must be boolean", key)
	}
	return parsed, nil
}

func parseALPN(query url.Values) ([]string, error) {
	entries, present := query["alpn"]
	if !present {
		return nil, nil
	}
	if len(entries) == 0 || entries[0] == "" {
		return nil, fmt.Errorf("URI parameter %q must contain at least one protocol", "alpn")
	}
	return parseALPNValue(entries[0])
}

func parseALPNValue(raw string) ([]string, error) {
	if raw == "" {
		return nil, fmt.Errorf("URI parameter %q must contain at least one protocol", "alpn")
	}
	values := strings.Split(raw, ",")
	if err := validateALPNValues(values); err != nil {
		return nil, err
	}
	return values, nil
}

func validateALPNValues(values []string) error {
	for _, value := range values {
		if value == "" {
			return fmt.Errorf("URI parameter %q contains an empty protocol", "alpn")
		}
		if len([]byte(value)) > 255 {
			return fmt.Errorf("URI parameter %q entries must be at most 255 bytes", "alpn")
		}
		if strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return fmt.Errorf("URI parameter %q cannot contain control characters", "alpn")
		}
	}
	return nil
}

func decodeBase64(value string) (string, error) {
	value = strings.Map(func(character rune) rune {
		if unicode.IsSpace(character) {
			return -1
		}
		return character
	}, value)
	value = strings.ReplaceAll(strings.ReplaceAll(value, "-", "+"), "_", "/")
	for len(value)%4 != 0 {
		value += "="
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("invalid Base64")
	}
	return string(decoded), nil
}

func Fingerprint(node model.Node) string {
	copy := node
	copy.ID, copy.Name, copy.Enabled = "", "", true
	copy.SourceSubscription, copy.SourceFingerprint, copy.PinnedStale = "", "", false
	encoded, _ := json.Marshal(copy)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func port(u *url.URL, fallback int) int {
	if value := u.Port(); value != "" {
		return intValue(value)
	}
	return fallback
}

func splitPorts(value string) []string {
	if value == "" {
		return nil
	}
	items := strings.Split(value, ",")
	for index, item := range items {
		if strings.Count(item, "-") == 1 {
			items[index] = strings.Replace(item, "-", ":", 1)
		}
	}
	return items
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func boolValue(value string) bool {
	parsed, _ := strconv.ParseBool(value)
	return parsed
}

func intValue(value string) int {
	parsed, _ := strconv.Atoi(value)
	return parsed
}
