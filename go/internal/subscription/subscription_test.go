// SPDX-License-Identifier: GPL-3.0-or-later
package subscription

// These tests lock the platform-neutral subscription URI semantics.

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/gsh20040816/steer/go/internal/compiler"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

func TestParseTUICALPNAllowInsecureAndEmptyPassword(t *testing.T) {
	raw := "tuic://00000000-0000-4000-8000-000000000001@example.com:39823?congestion_control=bbr&alpn=h3%2Ch2&sni=example.com&udp_relay_mode=quic&allow_insecure=0#singbox_tuic"
	node, err := ParseURI(raw)
	if err != nil {
		t.Fatal(err)
	}
	if node.Type != "tuic" || node.UUID != "00000000-0000-4000-8000-000000000001" || node.Password != "" || node.ServerPort != 39823 {
		t.Fatalf("unexpected TUIC credentials/endpoint: %#v", node)
	}
	if !reflect.DeepEqual(node.ALPN, []string{"h3", "h2"}) || node.CongestionControl != "bbr" || node.UDPRelayMode != "quic" || node.TLSServerName != "example.com" || node.Insecure {
		t.Fatalf("TUIC compatibility fields were not lowered: %#v", node)
	}
	result := ParseList(raw)
	if result.Skipped != 0 || len(result.Nodes) != 1 {
		t.Fatalf("TUIC link was not accepted by ParseList: %#v", result)
	}
}

func TestParseTUICAllowInsecureAliases(t *testing.T) {
	for _, value := range []string{"0", "1", "true", "false"} {
		raw := "tuic://00000000-0000-4000-8000-000000000001@example.com:443?allow_insecure=" + value
		node, err := ParseURI(raw)
		if err != nil {
			t.Fatalf("allow_insecure=%s: %v", value, err)
		}
		want := value == "1" || value == "true"
		if node.Insecure != want {
			t.Fatalf("allow_insecure=%s normalized to %v, want %v", value, node.Insecure, want)
		}
	}
	for _, raw := range []string{
		"tuic://00000000-0000-4000-8000-000000000001@example.com:443?allow_insecure=1&insecure=0",
		"tuic://00000000-0000-4000-8000-000000000001@example.com:443?allow_insecure=maybe",
		"tuic://00000000-0000-4000-8000-000000000001@example.com:443?alpn=",
		"tuic://00000000-0000-4000-8000-000000000001@example.com:443?alpn=h3,,h2",
		"tuic://00000000-0000-4000-8000-000000000001@example.com:443?alpn=h3%0Ah2",
		"tuic://00000000-0000-4000-8000-000000000001@example.com:443?alpn=" + strings.Repeat("a", 256),
	} {
		if _, err := ParseURI(raw); err == nil {
			t.Fatalf("invalid TUIC URI was accepted: %s", raw)
		}
	}
	if _, err := ParseURI("tuic://00000000-0000-4000-8000-000000000001@example.com:443?allow_insecure=0&insecure=false"); err != nil {
		t.Fatalf("equivalent boolean aliases should be accepted: %v", err)
	}
}

func TestParseStandardURIs(t *testing.T) {
	result := ParseList("vless://00000000-0000-4000-8000-000000000001@example.com:443?security=tls&sni=edge.example.com&type=ws&path=%2Fproxy\n" +
		"ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@example.com:8388#ss")
	nodes := result.Nodes
	if len(nodes) != 2 || nodes[0].Type != "vless" || nodes[0].Transport != "ws" || nodes[1].Type != "shadowsocks" {
		t.Fatalf("unexpected parsed nodes: %#v", nodes)
	}
	if result.Skipped != 0 {
		t.Fatalf("valid nodes were skipped: %#v", result)
	}
	if Fingerprint(nodes[0]) != Fingerprint(nodes[0]) {
		t.Fatal("fingerprint is not stable")
	}
}

func TestParseBase64VMess(t *testing.T) {
	raw := `{"v":"2","ps":"fixture","add":"vmess.example.com","port":"443","id":"00000000-0000-4000-8000-000000000001","aid":"0","scy":"auto","net":"ws","tls":"tls","sni":"edge.example.com","host":"edge.example.com","path":"/ws","type":"none"}`
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))
	result := ParseList(encoded)
	nodes := result.Nodes
	if len(nodes) != 1 || nodes[0].Type != "vmess" || nodes[0].Transport != "ws" || nodes[0].Network != "" {
		t.Fatalf("unexpected VMess node: %#v", nodes)
	}
}

func TestCredentialProxyDefaultPortsFollowScheme(t *testing.T) {
	tests := []struct {
		raw      string
		nodeType string
		port     int
		tlsName  string
	}{
		{raw: "socks://proxy.example", nodeType: "socks", port: 1080},
		{raw: "socks5://proxy.example", nodeType: "socks", port: 1080},
		{raw: "http://proxy.example", nodeType: "http", port: 80},
		{raw: "https://proxy.example", nodeType: "http", port: 443, tlsName: "proxy.example"},
		{raw: "https://proxy.example:8443", nodeType: "http", port: 8443, tlsName: "proxy.example"},
	}
	for _, testCase := range tests {
		node, err := ParseURI(testCase.raw)
		if err != nil {
			t.Fatalf("ParseURI(%q): %v", testCase.raw, err)
		}
		if node.Type != testCase.nodeType || node.ServerPort != testCase.port || node.TLSServerName != testCase.tlsName {
			t.Errorf("ParseURI(%q) = %#v; want type=%q port=%d tls=%q", testCase.raw, node, testCase.nodeType, testCase.port, testCase.tlsName)
		}
	}

	result := ParseList("socks://socks.example\nhttp://http.example\nhttps://https.example")
	if len(result.Nodes) != 3 || result.Nodes[0].ServerPort != 1080 || result.Nodes[1].ServerPort != 80 || result.Nodes[2].ServerPort != 443 {
		t.Fatalf("subscription list did not reuse scheme defaults: %#v", result.Nodes)
	}
}

func TestRejectUnknownParameter(t *testing.T) {
	if _, err := ParseURI("tuic://00000000-0000-4000-8000-000000000001:password@example.com:443?unsupported=x"); err == nil {
		t.Fatal("unknown URI parameter was silently accepted")
	}
}

func TestParseEmptyPCSCertificatePinCompatibilityMarker(t *testing.T) {
	raw := strings.Join([]string{
		"vless://00000000-0000-4000-8000-000000000001@example.com:443?security=tls&sni=edge.example&pcs=",
		"trojan://secret@example.com:443?sni=edge.example&pcs",
		"anytls://secret@example.com:443?sni=edge.example&type=tcp&pcs=",
	}, "\n")
	result := ParseList(raw)
	if result.Skipped != 0 || len(result.Nodes) != 3 {
		t.Fatalf("empty pcs compatibility markers were not ignored: %#v", result)
	}
}

func TestRejectNonEmptyPCSCertificatePin(t *testing.T) {
	for _, raw := range []string{
		"vless://00000000-0000-4000-8000-000000000001@example.com:443?security=tls&sni=edge.example&pcs=abcdef",
		"trojan://secret@example.com:443?sni=edge.example&pcs=abcdef",
		"anytls://secret@example.com:443?sni=edge.example&type=tcp&pcs=abcdef",
	} {
		if _, err := ParseURI(raw); err == nil || !strings.Contains(err.Error(), `unsupported URI parameter "pcs"`) {
			t.Fatalf("non-empty pcs certificate pin was not rejected safely: %v", err)
		}
	}
}

func TestRejectInvalidAndConflictingParameters(t *testing.T) {
	for _, raw := range []string{
		"tuic://00000000-0000-4000-8000-000000000001:secret@example.com:443?insecure=maybe",
		"vless://00000000-0000-4000-8000-000000000001@example.com:443?sni=a.example&serverName=b.example",
		"trojan://secret@example.com:443?sni=a.example&peer=b.example",
		"anytls://secret@example.com:443?type=ws",
		"tuic://00000000-0000-4000-8000-000000000001@example.com:443?sni=%ZZ",
		"tuic://00000000-0000-4000-8000-000000000001@example.com:443#bad%0Aname",
	} {
		if _, err := ParseURI(raw); err == nil {
			t.Fatalf("invalid URI was accepted: %s", raw)
		}
	}
}

func TestParsePassWallCompatibleAliases(t *testing.T) {
	result := ParseList(
		"trojan://secret@example.com:443?type=tcp&sni=edge.example&peer=edge.example&allowInsecure=1#trojan\n" +
			"anytls://secret@example.com:443?type=tcp&sni=edge.example&insecure=1#anytls\n" +
			"hysteria://secret@example.com:443?peer=edge.example&allowInsecure=1&upmbps=100&downmbps=100#hysteria\n")
	nodes := result.Nodes
	if len(nodes) != 3 {
		t.Fatalf("unexpected compatible node count: %d", len(nodes))
	}
	for _, node := range nodes {
		if node.TLSServerName != "edge.example" || !node.Insecure {
			t.Fatalf("compatibility aliases were not lowered explicitly: %#v", node)
		}
	}
}

func TestParseListSkipsOnlyInvalidNodes(t *testing.T) {
	result := ParseList("not-a-node\n" +
		"socks5://user:password@example.com:1080#SOCKS\n" +
		"trojan://secret@example.com:443?sni=edge.example#bad%0Aname\n")
	if result.Skipped != 2 || len(result.Nodes) != 1 || len(result.SkippedReasons) != 2 || result.Nodes[0].Type != "socks" {
		t.Fatalf("unexpected lenient parse result: %#v", result)
	}
	if result.SkippedReasons[0].Scheme != "unknown" || result.SkippedReasons[0].Code == "" || result.SkippedReasons[0].Detail == "" {
		t.Fatalf("skipped node lacks safe stable reason: %#v", result.SkippedReasons)
	}
	if strings.Contains(result.SkippedReasons[1].Detail, "bad") {
		t.Fatalf("skipped reason leaked raw fragment: %#v", result.SkippedReasons[1])
	}
}

func TestParseListAllowsAllNodesToBeSkipped(t *testing.T) {
	result := ParseList("not-a-node\nstill-not-a-node\n")
	if result.Skipped != 2 || len(result.Nodes) != 0 {
		t.Fatalf("all-invalid subscription did not become an empty result: %#v", result)
	}
}

func TestParseOpaqueBase64PayloadsContainingSlash(t *testing.T) {
	vmessJSON := `{"v":"2","ps":"࠿","add":"vmess.example.com","port":"443","id":"00000000-0000-4000-8000-000000000001","aid":"0","scy":"auto","net":"tcp","tls":"none","type":"none"}`
	vmessPayload := base64.StdEncoding.EncodeToString([]byte(vmessJSON))
	if !strings.Contains(vmessPayload, "/") {
		t.Fatal("VMess fixture does not exercise a slash in standard Base64")
	}
	vmess, err := ParseURI("vmess://" + vmessPayload)
	if err != nil || vmess.Name != "࠿" {
		t.Fatalf("VMess slash payload: node=%#v err=%v", vmess, err)
	}

	ssPayload := base64.StdEncoding.EncodeToString([]byte("2022-blake3-aes-128-gcm:࠿@example.com:8388"))
	if !strings.Contains(ssPayload, "/") {
		t.Fatal("Shadowsocks fixture does not exercise a slash in standard Base64")
	}
	shadowsocks, err := ParseURI("ss://" + ssPayload + "#SS2022")
	if err != nil || shadowsocks.Method != "2022-blake3-aes-128-gcm" || shadowsocks.Password != "࠿" {
		t.Fatalf("Shadowsocks slash payload: node=%#v err=%v", shadowsocks, err)
	}
}

func TestParseVMessCommonTLSFields(t *testing.T) {
	raw := `{"v":"2","ps":"fixture","add":"vmess.example.com","port":"443","id":"00000000-0000-0000-0000-000000000001","aid":"0","scy":"auto","net":"ws","tls":"tls","sni":"edge.example.com","host":"edge.example.com","path":"/ws","type":"none","alpn":"h2,http/1.1","fp":"chrome","allowInsecure":"0"}`
	// Use a valid version-4 UUID as required by the shared validator.
	raw = strings.Replace(raw, "00000000-0000-0000-0000-000000000001", "00000000-0000-4000-8000-000000000001", 1)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(raw))
	node, err := ParseURI("vmess://" + encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(node.ALPN, []string{"h2", "http/1.1"}) || node.UTLSFingerprint != "chrome" || node.Insecure {
		t.Fatalf("VMess TLS fields were not lowered: %#v", node)
	}
}

func TestParseVMessV2rayNExport(t *testing.T) {
	// v2rayN exports a numeric "v", empty "alpn"/"fp" and client-specific keys.
	raw := `{"v":2,"ps":"fixture","add":"vmess.example.com","port":443,"id":"00000000-0000-4000-8000-000000000001","aid":0,"scy":"auto","net":"ws","tls":"tls","sni":"","host":"","path":"/ws","type":"none","alpn":"","fp":"","insecure":"0"}`
	node, err := ParseURI("vmess://" + base64.StdEncoding.EncodeToString([]byte(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if len(node.ALPN) != 0 || node.TLSServerName != "vmess.example.com" {
		t.Fatalf("v2rayN VMess export was not lowered: %#v", node)
	}
}

func TestParseCompleteSIP002Forms(t *testing.T) {
	for name, raw := range map[string]string{
		"plaintext 2022":     "ss://2022-blake3-aes-128-gcm:password@example.com:8388#SS2022",
		"plaintext escaped":  "ss://2022-blake3-aes-128-gcm:p%40ss%3Aword%2Fkey@example.com:8388",
		"encoded userinfo":   "ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@example.com:8388",
		"legacy password at": "ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:p@ssword@example.com:8388")),
		"standard plugin":    "ss://aes-256-gcm:password@example.com:8388?plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dexample.com",
	} {
		node, err := ParseURI(raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if node.Type != "shadowsocks" || node.Server != "example.com" || node.ServerPort != 8388 {
			t.Fatalf("%s: unexpected node %#v", name, node)
		}
		if name == "legacy password at" && node.Password != "p@ssword" {
			t.Fatalf("legacy @ password was truncated: %#v", node)
		}
		if name == "plaintext escaped" && node.Password != "p@ss:word/key" {
			t.Fatalf("escaped plaintext password was truncated: %#v", node)
		}
		if name == "standard plugin" && (node.Plugin != "obfs-local" || node.PluginOptions != "obfs=http;obfs-host=example.com") {
			t.Fatalf("standard plugin was not split: %#v", node)
		}
	}
}

func TestCompatibilityMatrixCoversEveryShareScheme(t *testing.T) {
	vmessJSON := `{"v":"2","ps":"vmess","add":"vmess.example","port":"443","id":"00000000-0000-4000-8000-000000000001","aid":"0","scy":"auto","net":"ws","tls":"tls","sni":"edge.example","host":"edge.example","path":"/ws","type":"none","alpn":"h2,http/1.1","fp":"chrome","allowInsecure":"false"}`
	vmess := "vmess://" + base64.RawURLEncoding.EncodeToString([]byte(vmessJSON))
	fixtures := []struct {
		name      string
		source    string
		raw       string
		want      string
		canonical []string
	}{
		{"socks", "SOCKS RFC 1928", "socks://user:pass@example.com:1080", "socks", []string{"server", "server_port", "username", "password"}},
		{"socks5", "SOCKS5 alias", "socks5://user:pass@example.com:1080", "socks", []string{"server", "server_port", "username", "password"}},
		{"http", "HTTP CONNECT", "http://user:pass@example.com:8080", "http", []string{"server", "server_port", "username", "password"}},
		{"https", "HTTPS CONNECT", "https://user:pass@example.com:443?sni=edge.example", "http", []string{"server", "server_port", "tls_server_name"}},
		{"shadowsocks SIP002", "SIP002", "ss://aes-256-gcm:password@example.com:8388?plugin=obfs-local%3Bobfs%3Dhttp", "shadowsocks", []string{"server", "server_port", "method", "password", "plugin_options"}},
		{"vmess Base64", "VMess v2 JSON", vmess, "vmess", []string{"server", "server_port", "uuid", "transport", "tls_server_name", "alpn"}},
		{"vless", "VLESS URI v1", "vless://00000000-0000-4000-8000-000000000001@example.com:443?security=tls&sni=edge.example&alpn=h2", "vless", []string{"server", "server_port", "uuid", "tls_server_name", "alpn"}},
		{"trojan", "Trojan URI", "trojan://secret@example.com:443?sni=edge.example&alpn=h2", "trojan", []string{"server", "server_port", "password", "tls_server_name", "alpn"}},
		{"hysteria", "Hysteria v1 URI", "hysteria://secret@example.com:443?sni=edge.example&upmbps=100&downmbps=100&obfs=obfs-secret", "hysteria", []string{"server", "server_port", "password", "up_mbps", "down_mbps", "obfs_password"}},
		{"hysteria2", "Hysteria2 URI", "hysteria2://secret@example.com:443?sni=edge.example&obfs=salamander&obfs-password=obfs-secret", "hysteria2", []string{"server", "server_port", "password", "tls_server_name", "obfs_type", "obfs_password"}},
		{"hy2 alias", "Hysteria2 hy2 alias", "hy2://secret@example.com:443?sni=edge.example", "hysteria2", []string{"server", "server_port", "password", "tls_server_name"}},
		{"shadowtls", "ShadowTLS URI", "shadowtls://secret@example.com:443?version=3&sni=edge.example", "shadowtls", []string{"server", "server_port", "password", "version", "tls_server_name"}},
		{"tuic", "TUIC v5 URI", "tuic://00000000-0000-4000-8000-000000000001@example.com:443?sni=edge.example&alpn=h3%2Ch2", "tuic", []string{"server", "server_port", "uuid", "tls_server_name", "alpn"}},
		{"anytls", "AnyTLS URI", "anytls://secret@example.com:443?sni=edge.example", "anytls", []string{"server", "server_port", "password", "tls_server_name"}},
		{"naive", "NaiveProxy HTTPS URI", "naive+https://user:secret@example.com:443?sni=edge.example", "naive", []string{"server", "server_port", "username", "password", "tls_server_name"}},
		{"ssh", "SSH URI", "ssh://user:secret@example.com:22", "ssh", []string{"server", "server_port", "username", "password"}},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			node, err := ParseURI(fixture.raw)
			if err != nil {
				t.Fatalf("ParseURI: %v", err)
			}
			if node.Type != fixture.want {
				t.Fatalf("type=%q, want %q", node.Type, fixture.want)
			}
			encoded, err := json.Marshal(node)
			if err != nil {
				t.Fatal(err)
			}
			var canonical map[string]any
			if err := json.Unmarshal(encoded, &canonical); err != nil {
				t.Fatal(err)
			}
			for _, field := range fixture.canonical {
				if _, ok := canonical[field]; !ok {
					t.Fatalf("%s (%s) lost canonical field %q: %s", fixture.source, fixture.name, field, encoded)
				}
			}
			if outbound := compiler.CompileNodeOutbound(node); outbound["type"] != fixture.want {
				t.Fatalf("%s did not lower to matching sing-box outbound: %#v", fixture.source, outbound)
			}
			if validation := model.ValidateNode(node); !validation.OK {
				t.Fatalf("canonical validation failed: %#v", validation.Errors)
			}
		})
	}
}

func TestParseBase64WrappedCRLFDocument(t *testing.T) {
	document := "# generated subscription\r\n\r\nvless://00000000-0000-4000-8000-000000000001@example.com:443?security=tls&sni=edge.example\r\nnot-a-node\r\n"
	encoded := base64.RawURLEncoding.EncodeToString([]byte(document))
	var wrapped strings.Builder
	for index := 0; index < len(encoded); index += 17 {
		end := index + 17
		if end > len(encoded) {
			end = len(encoded)
		}
		wrapped.WriteString(encoded[index:end])
		wrapped.WriteString("\r\n")
	}
	result := ParseList(wrapped.String())
	if len(result.Nodes) != 1 || result.Skipped != 1 {
		t.Fatalf("Base64 wrapped CRLF document was not parsed independently: %#v", result)
	}
}

func TestParseVLESSLowersSecurityIntoCanonicalTLSFields(t *testing.T) {
	tlsNode, err := ParseURI("vless://00000000-0000-4000-8000-000000000001@example.com:443?encryption=none&security=tls&sni=edge.example.com&fp=chrome")
	if err != nil {
		t.Fatal(err)
	}
	if tlsNode.Security != "" || tlsNode.TLSServerName != "edge.example.com" || tlsNode.UTLSFingerprint != "chrome" {
		t.Fatalf("VLESS TLS was not lowered into canonical fields: %#v", tlsNode)
	}
	realityNode, err := ParseURI("vless://00000000-0000-4000-8000-000000000001@example.com:443?security=reality&sni=edge.example.com&fp=chrome&pbk=public-key&sid=0123456789abcdef&flow=xtls-rprx-vision")
	if err != nil {
		t.Fatal(err)
	}
	if realityNode.Security != "" || realityNode.RealityPublicKey != "public-key" || realityNode.RealityShortID != "0123456789abcdef" {
		t.Fatalf("VLESS Reality was not lowered into canonical fields: %#v", realityNode)
	}
}

func TestParseVLESSRejectsSecurityContradictions(t *testing.T) {
	for _, raw := range []string{
		"vless://00000000-0000-4000-8000-000000000001@example.com:443?security=tls",
		"vless://00000000-0000-4000-8000-000000000001@example.com:443?security=none&sni=edge.example.com",
		"vless://00000000-0000-4000-8000-000000000001@example.com:443?security=none&flow=xtls-rprx-vision",
		"vless://00000000-0000-4000-8000-000000000001@example.com:443?security=tls&sni=edge.example.com&pbk=public-key",
		"vless://00000000-0000-4000-8000-000000000001@example.com:443?security=unsupported",
		"vless://00000000-0000-4000-8000-000000000001@example.com:443?encryption=aes-128-gcm",
	} {
		if _, err := ParseURI(raw); err == nil {
			t.Fatalf("contradictory VLESS URI was accepted: %s", raw)
		}
	}
}

func TestParseHysteriaMPortPreservesSingleAndMultipleRanges(t *testing.T) {
	for raw, expected := range map[string][]string{
		"hy2://secret@example.com:443?mport=20000:30000":       {"20000:30000"},
		"hy2://secret@example.com:443?mport=12000-12010,13000": {"12000:12010", "13000"},
	} {
		node, err := ParseURI(raw)
		if err != nil {
			t.Fatal(err)
		}
		if node.ServerPort != 443 || len(node.ServerPorts) != len(expected) {
			t.Fatalf("unexpected Hysteria2 ports for %s: %#v", raw, node)
		}
		for index := range expected {
			if node.ServerPorts[index] != expected[index] {
				t.Fatalf("Hysteria2 ports for %s = %#v, want %#v", raw, node.ServerPorts, expected)
			}
		}
	}
}
