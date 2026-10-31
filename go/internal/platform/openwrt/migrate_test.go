// SPDX-License-Identifier: GPL-3.0-or-later

package openwrt

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/gsh20040816/steer/go/internal/compiler"
	model "github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/subscription"
)

const schema9SubscriptionConfig = `config steer 'main'
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

config node 'manual'
	option type 'socks'
	option server '192.0.2.1'
	option server_port '1080'

config node 'public_b'
	option name 'B'
	option type 'trojan'
	option server 'b.example'
	option server_port '443'
	option password 'secret'
	option tls_server_name 'b.example'
	option source_subscription 'public'
	option source_fingerprint 'fp-b'

config node 'gone_x'
	option type 'socks'
	option server '192.0.2.9'
	option server_port '1080'
	option source_subscription 'gone'
	option source_fingerprint 'fp-x'

config node 'public_a'
	option enabled '0'
	option type 'socks'
	option server 'a.example'
	option server_port '1080'
	option source_subscription 'public'
	option source_fingerprint 'fp-a'
	option pinned_stale '1'

config subscription 'public'
	option enabled '1'
	option url 'https://example.com/subscription'

config subscription 'paused'
	option enabled '0'
	option url 'https://example.com/paused'

config route 'direct'
	option kind 'direct'

config route 'via_b'
	option kind 'single'
	option node 'public_b'

config route 'via_gone'
	option kind 'single'
	option node 'gone_x'
	option detour 'via_b'

config dns_profile 'resolver'
	option protocol 'udp'
	option server '1.1.1.1'
	option server_port '53'

config rule 'proxied'
	option dns_profile 'resolver'
	option route 'via_gone'
	list domain_match 'domain:example.com'

config rule 'default'
	option default '1'
	option dns_profile 'resolver'
	option route 'direct'
`

// applyUCIBatch is the subset of "uci batch" that the migration emits.
func applyUCIBatch(t *testing.T, config, batch string) string {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(batch), "\n") {
		fields := strings.Fields(line)
		switch {
		case line == "commit steer":
		case fields[0] == "delete" && strings.Count(fields[1], ".") == 1:
			id := strings.TrimPrefix(fields[1], "steer.")
			config = regexp.MustCompile(`(?s)config \w+ '`+id+`'\n(\t[^\n]*\n)*\n?`).ReplaceAllString(config, "")
		case fields[0] == "delete":
			parts := strings.Split(fields[1], ".")
			section := regexp.MustCompile(`(?s)config \w+ '` + parts[1] + `'\n(\t[^\n]*\n)*`)
			config = section.ReplaceAllStringFunc(config, func(block string) string {
				return regexp.MustCompile(`\toption `+parts[2]+` '[^']*'\n`).ReplaceAllString(block, "")
			})
		case fields[0] == "set" && strings.HasPrefix(fields[1], "steer.main.schema_version="):
			config = strings.Replace(config, "option schema_version '9'", "option schema_version '"+strings.Trim(strings.TrimPrefix(fields[1], "steer.main.schema_version="), "'")+"'", 1)
		default:
			t.Fatalf("unexpected migration batch line %q", line)
		}
	}
	return config
}

func compiledSingBox(t *testing.T, value model.Intent) string {
	t.Helper()
	backend := NewBackend(nil, value, BackendOptions{StateDirectory: "/var/lib/steer", GeoDataDirectory: "/usr/share/steer/geodata-seed"})
	encoded, err := json.Marshal(compiler.Compile(value, backend.CompilerOptions()).SingBox)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestMigrateMovesSubscriptionNodesWithoutChangingTheRuntime(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "steer")
	stateDirectory := filepath.Join(root, "state")
	nodeDirectory := filepath.Join(root, "subscription-nodes")
	if err := os.WriteFile(configPath, []byte(schema9SubscriptionConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := DecodeBytes([]byte(schema9SubscriptionConfig))
	if err != nil {
		t.Fatal(err)
	}
	var batch string
	changed, err := MigrateWithWriter(context.Background(), configPath, stateDirectory, nodeDirectory, func(_ context.Context, value string) error {
		batch = value
		return os.WriteFile(configPath, []byte(applyUCIBatch(t, schema9SubscriptionConfig, value)), 0o600)
	})
	if err != nil || !changed {
		t.Fatalf("migration did not run: %v", err)
	}
	want := "delete steer.public_b\ndelete steer.public_a\n" +
		"delete steer.gone_x.source_subscription\ndelete steer.gone_x.source_fingerprint\n" +
		"set steer.main.schema_version='10'\ncommit steer\n"
	if batch != want {
		t.Fatalf("unexpected migration batch:\n%s", batch)
	}
	nodes, err := subscription.NodeStore{Directory: nodeDirectory}.Load("public")
	if err != nil || len(nodes) != 2 || nodes[0].ID != "public_b" || nodes[1].ID != "public_a" ||
		nodes[1].Enabled || !nodes[1].PinnedStale || nodes[1].SourceFingerprint != "fp-a" || nodes[0].Password != "secret" {
		t.Fatalf("subscription nodes were not moved with their state: %#v %v", nodes, err)
	}
	if _, err := os.Stat(filepath.Join(nodeDirectory, "paused.json")); !os.IsNotExist(err) {
		t.Fatal("a subscription without nodes got a store file")
	}
	after, err := LoadSaved(configPath, nodeDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if validation := Validate(after); !validation.OK {
		t.Fatalf("migrated Saved Intent is invalid: %#v", validation.Errors)
	}
	if runtime := compiledSingBox(t, after); runtime != compiledSingBox(t, before) || !strings.Contains(runtime, "b.example") || !strings.Contains(runtime, "192.0.2.9") {
		t.Fatal("migration changed the compiled sing-box configuration")
	}
	if len(after.Nodes) != len(before.Nodes) {
		t.Fatalf("migration lost nodes: %d != %d", len(after.Nodes), len(before.Nodes))
	}

	migrated, _ := os.ReadFile(configPath)
	changed, err = MigrateWithWriter(context.Background(), configPath, stateDirectory, nodeDirectory, func(context.Context, string) error {
		t.Fatal("a schema 10 configuration was migrated again")
		return nil
	})
	if err != nil || changed {
		t.Fatalf("migration is not idempotent: %v", err)
	}
	if again, _ := os.ReadFile(configPath); !reflect.DeepEqual(again, migrated) {
		t.Fatal("idempotent migration changed UCI")
	}
}
