// SPDX-License-Identifier: GPL-3.0-or-later
package openwrt

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

const minimalConfig = `
config steer 'main'
	option schema_version '9'
	option enabled '1'
	option log_level 'warn'
	option probe_direct 'https://www.baidu.com/'
	option probe_proxy 'https://www.google.com/generate_204'
	option speedtest_proxy 'https://speed.cloudflare.com/__down?bytes=1000000'

config bootstrap 'bootstrap'
	option protocol 'udp'
	option server '1.1.1.1'
	option server_port '53'
	option strategy 'prefer_ipv4'

config route 'direct'
	option kind 'direct'

config dns_profile 'direct_dns'
	option protocol 'udp'
	option server '1.1.1.1'
	option server_port '53'

config rule 'default'
	option default '1'
	option dns_profile 'direct_dns'
	option route 'direct'
`

func TestDecodeCanonicalIntent(t *testing.T) {
	intent, err := Decode(strings.NewReader(minimalConfig))
	if err != nil {
		t.Fatal(err)
	}
	if intent.Main.SchemaVersion != model.SchemaVersion || !intent.Main.Enabled || intent.Main.ProbeDirectURL != "https://www.baidu.com/" {
		t.Fatalf("unexpected main: %#v", intent.Main)
	}
	if validation := model.Validate(intent); !validation.OK {
		t.Fatalf("decoded intent is invalid: %#v", validation.Errors)
	}
}

func TestDecodeTUICALPNList(t *testing.T) {
	config := minimalConfig + `
config node 'tuic'
	option enabled '1'
	option type 'tuic'
	option server 'example.com'
	option server_port '443'
	option uuid '00000000-0000-4000-8000-000000000001'
	option tls_server_name 'example.com'
	list alpn 'h3'
	list alpn 'h2'
`
	intent, err := Decode(strings.NewReader(config))
	if err != nil {
		t.Fatal(err)
	}
	if len(intent.Nodes) != 1 || !reflect.DeepEqual(intent.Nodes[0].ALPN, []string{"h3", "h2"}) {
		t.Fatalf("TUIC ALPN list did not survive UCI decode: %#v", intent.Nodes)
	}
	if validation := model.Validate(intent); !validation.OK {
		t.Fatalf("decoded TUIC ALPN node is invalid: %#v", validation.Errors)
	}
}

func TestDecodeRealUCIExportWithMultilinePrivateKey(t *testing.T) {
	configPath := filepath.Join("..", "..", "..", "..", "tests", "fixtures", "multiline-private-key-valid", "steer")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := DecodeBytes(content)
	if err != nil {
		t.Fatal(err)
	}
	want := "-----BEGIN OPENSSH PRIVATE KEY-----\nowner's line\nline two\n-----END OPENSSH PRIVATE KEY-----\n"
	if len(intent.Nodes) != 1 || intent.Nodes[0].PrivateKey != want {
		t.Fatalf("multiline private key did not survive UCI export decoding: %#v", intent.Nodes)
	}
	if validation := model.Validate(intent); !validation.OK {
		t.Fatalf("multiline private-key candidate failed validation: %#v", validation.Errors)
	}
}

func TestDecodedUCIDNSProfileOptionsRemainSubjectToSemanticValidation(t *testing.T) {
	config := strings.Replace(minimalConfig, "option server_port '53'\n\nconfig rule", "option server_port '53'\n\toption tls_server_name 'stale.example'\n\toption path '/dns-query'\n\toption insecure '1'\n\nconfig rule", 1)
	intent, err := Decode(strings.NewReader(config))
	if err != nil {
		t.Fatal(err)
	}
	validation := model.Validate(intent)
	for _, option := range []string{"tls_server_name", "path", "insecure"} {
		found := false
		for _, issue := range validation.Errors {
			if issue.Code == "UNSUPPORTED_DNS_OPTION" && issue.Option == option {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("raw UCI retained unsupported %q without a stable error: %#v", option, validation.Errors)
		}
	}
}

func TestDecodePackagedFreshDefaults(t *testing.T) {
	configPath := filepath.Join("..", "..", "..", "..", "steer", "files", "etc", "config", "steer")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	value, err := DecodeBytes(config)
	if err != nil {
		t.Fatal(err)
	}
	wantRoutes := []model.Route{
		{ID: "direct", Enabled: true, Kind: "direct"},
		{ID: "block", Enabled: false, Kind: "block"},
	}
	if !reflect.DeepEqual(value.Routes, wantRoutes) {
		t.Fatalf("packaged fresh routes changed: %#v", value.Routes)
	}
	if validation := model.Validate(value); !validation.OK {
		t.Fatalf("packaged fresh defaults are invalid: %#v", validation.Errors)
	}
}

func TestDecodeSubscriptionConfiguration(t *testing.T) {
	config := minimalConfig + `
config subscription 'example'
	option enabled '1'
	option name 'Example'
	option url 'https://example.com/nodes'
	option update_interval '6h'
`
	intent, err := Decode(strings.NewReader(config))
	if err != nil {
		t.Fatal(err)
	}
	if len(intent.Subscriptions) != 1 || intent.Subscriptions[0].URL != "https://example.com/nodes" {
		t.Fatalf("unexpected subscriptions: %#v", intent.Subscriptions)
	}
	if validation := model.Validate(intent); !validation.OK {
		t.Fatalf("decoded subscription is invalid: %#v", validation.Errors)
	}
}

func TestDecodeRejectsImplementationAndLegacyFields(t *testing.T) {
	for _, field := range []string{"router_proxy", "tproxy_port", "response_mode", "outbound"} {
		config := minimalConfig
		switch field {
		case "response_mode":
			config = strings.Replace(config, "option protocol 'udp'\n\toption server '1.1.1.1'", "option protocol 'udp'\n\toption response_mode 'fastest-ip'\n\toption server '1.1.1.1'", 1)
		case "outbound":
			config = strings.Replace(config, "option route 'direct'", "option outbound 'direct'", 1)
		default:
			config = strings.Replace(config, "option log_level 'warn'", "option log_level 'warn'\n\toption "+field+" '1'", 1)
		}
		if _, err := Decode(strings.NewReader(config)); err == nil {
			t.Fatalf("legacy field %s was accepted", field)
		}
	}
}

func TestDecodeRejectsWrongScalarShape(t *testing.T) {
	config := strings.Replace(minimalConfig, "option log_level 'warn'", "option managed_zone 'lan'\n\toption log_level 'warn'", 1)
	if _, err := Decode(strings.NewReader(config)); err == nil {
		t.Fatal("removed managed_zone option must be rejected")
	}
}

func TestDecodeRouteDetour(t *testing.T) {
	config := strings.Replace(minimalConfig, "config route 'direct'", `config node 'front_node'
	option type 'socks'
	option server '192.0.2.10'
	option server_port '1080'

config node 'exit_node'
	option type 'socks'
	option server '192.0.2.11'
	option server_port '1080'

config route 'front'
	option kind 'single'
	option node 'front_node'

config route 'exit'
	option kind 'single'
	option node 'exit_node'
	option detour 'front'

config route 'direct'`, 1)
	intent, err := Decode(strings.NewReader(config))
	if err != nil {
		t.Fatal(err)
	}
	if len(intent.Routes) != 3 || intent.Routes[1].ID != "exit" || intent.Routes[1].Detour != "front" {
		t.Fatalf("route detour was not decoded: %#v", intent.Routes)
	}
}

func TestDecodeRejectsProbeListsInSchemaSeven(t *testing.T) {
	config := strings.Replace(minimalConfig, "option probe_direct 'https://www.baidu.com/'", "list probe_direct 'https://www.baidu.com/'", 1)
	if _, err := Decode(strings.NewReader(config)); err == nil || !strings.Contains(err.Error(), "must use option") {
		t.Fatalf("schema 7 probe list was accepted: %v", err)
	}
}

func TestDecodeRejectsCanonicalIDThatOpenWrtUCICannotAddress(t *testing.T) {
	config := strings.Replace(minimalConfig, "config route 'direct'", "config route 'direct-route'", 1)
	if _, err := Decode(strings.NewReader(config)); err == nil || !strings.Contains(err.Error(), "invalid Steer section ID") {
		t.Fatalf("hyphenated OpenWrt UCI ID was accepted: %v", err)
	}
}

func TestDecodeDirectBypass(t *testing.T) {
	for _, mode := range []string{"off", "static", "dns"} {
		config := strings.Replace(minimalConfig, "option log_level 'warn'", "option log_level 'warn'\n option direct_bypass '"+mode+"'", 1)
		value, err := Decode(strings.NewReader(config))
		if err != nil || value.Main.DirectBypass != mode {
			t.Fatalf("mode %q: value=%#v err=%v", mode, value.Main, err)
		}
	}
}
