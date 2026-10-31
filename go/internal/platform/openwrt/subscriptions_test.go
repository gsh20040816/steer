// SPDX-License-Identifier: GPL-3.0-or-later
package openwrt

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	model "github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/platform/openwrt/uci"
	"github.com/gsh20040816/steer/go/internal/subscription"
)

func TestConfiguredSubscriptionSchedule(t *testing.T) {
	tests := []struct {
		name        string
		interval    string
		manualID    string
		snapshotAge time.Duration
		wantFetch   bool
	}{
		{name: "automatic empty interval"},
		{name: "manual empty interval", manualID: "public", wantFetch: true},
		{name: "first scheduled fetch", interval: "1h", wantFetch: true},
		{name: "automatic before due", interval: "1h", snapshotAge: 30 * time.Minute},
		{name: "automatic just due", interval: "1h", snapshotAge: time.Hour, wantFetch: true},
		{name: "manual before due", interval: "1h", manualID: "public", snapshotAge: 30 * time.Minute, wantFetch: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				_, _ = writer.Write([]byte("socks://user:pass@127.0.0.1:1080#Imported\n"))
			}))
			defer server.Close()

			config := validSubscriptionConfig(server.URL)
			if test.interval != "" {
				config = strings.Replace(config, "\toption url '"+server.URL+"'\n", "\toption url '"+server.URL+"'\n\toption update_interval '"+test.interval+"'\n", 1)
			}
			paths := writeSubscriptionFixture(t, config)
			if test.snapshotAge != 0 {
				if err := subscription.SaveSnapshot(paths.state, SubscriptionSnapshot{SubscriptionID: "public", URL: server.URL, FetchedAt: time.Now().Add(-test.snapshotAge)}); err != nil {
					t.Fatal(err)
				}
			}
			snapshots, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), paths.config, paths.state, paths.nodes, test.manualID)
			if err != nil {
				t.Fatal(err)
			}
			wantCount := int32(0)
			if test.wantFetch {
				wantCount = 1
			}
			if got := requests.Load(); got != wantCount || len(snapshots) != int(wantCount) {
				t.Fatalf("requests=%d snapshots=%d, want %d", got, len(snapshots), wantCount)
			}
			paths.assertUCIUnchanged(t)
		})
	}
}

type subscriptionFixture struct {
	config, state, nodes, original string
}

func writeSubscriptionFixture(t *testing.T, config string) subscriptionFixture {
	t.Helper()
	root := t.TempDir()
	paths := subscriptionFixture{
		config: filepath.Join(root, "steer"), state: filepath.Join(root, "state"),
		nodes: filepath.Join(root, "subscription-nodes"), original: config,
	}
	if err := os.WriteFile(paths.config, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return paths
}

// assertUCIUnchanged checks the schema 10 promise that subscription work
// never writes UCI, so it can never trigger a procd reload.
func (paths subscriptionFixture) assertUCIUnchanged(t *testing.T) {
	t.Helper()
	content, err := os.ReadFile(paths.config)
	if err != nil || string(content) != paths.original {
		t.Fatalf("subscription operation changed UCI: %v\n%s", err, content)
	}
}

func (paths subscriptionFixture) storedNodes(t *testing.T) []model.Node {
	t.Helper()
	nodes, err := subscription.NodeStore{Directory: paths.nodes}.Load("public")
	if err != nil {
		t.Fatal(err)
	}
	return nodes
}

func TestSubscriptionDisappearanceIsPinnedStale(t *testing.T) {
	old := model.Node{ID: "public_old", Enabled: false, Type: "vless", Server: "old.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{UUID: "00000000-0000-4000-8000-000000000001"}}
	fresh := model.Node{Type: "vless", Server: "new.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{UUID: "00000000-0000-4000-8000-000000000002"}}
	merged := subscription.Merge("public", []model.Node{old}, []model.Node{fresh}, []model.Route{{ID: "proxy", Kind: "single", Node: old.ID}})
	stale := false
	for _, node := range merged {
		if node.PinnedStale {
			stale = true
			if node.Enabled {
				t.Fatalf("disabled stale node was re-enabled: %#v", merged)
			}
		}
	}
	if len(merged) != 2 || !stale {
		t.Fatalf("disappeared node was not pinned stale: %#v", merged)
	}
}

func TestSubscriptionDisappearanceDropsUnreferencedNode(t *testing.T) {
	old := model.Node{ID: "public_old", Enabled: true, Type: "vless", Server: "old.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{UUID: "00000000-0000-4000-8000-000000000001"}}
	fresh := model.Node{Type: "vless", Server: "new.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{UUID: "00000000-0000-4000-8000-000000000002"}}
	merged := subscription.Merge("public", []model.Node{old}, []model.Node{fresh}, nil)
	if len(merged) != 1 || merged[0].Server != fresh.Server || merged[0].PinnedStale {
		t.Fatalf("unreferenced disappeared node was retained: %#v", merged)
	}
}

func TestSubscriptionRefreshPreservesDisabledNode(t *testing.T) {
	old := model.Node{ID: "public_node", Enabled: false, Type: "trojan", Server: "same.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{Password: "secret"}}
	fresh := old
	fresh.ID = ""
	fresh.Enabled = true
	merged := subscription.Merge("public", []model.Node{old}, []model.Node{fresh}, nil)
	if len(merged) != 1 || merged[0].ID != old.ID || merged[0].Enabled {
		t.Fatalf("subscription refresh did not preserve the local enabled state: %#v", merged)
	}
}

func TestSubscriptionDuplicateFingerprintsAreCollapsed(t *testing.T) {
	first := model.Node{Name: "First", Type: "trojan", Server: "same.example", ServerPort: 443, NodeCredentials: model.NodeCredentials{Password: "secret"}}
	second := first
	second.Name = "Duplicate label"
	merged := subscription.Merge("public", nil, []model.Node{first, second}, nil)
	if len(merged) != 1 || merged[0].Name != "First" {
		t.Fatalf("duplicate subscription identities were not collapsed deterministically: %#v", merged)
	}
}

func TestSubscriptionNodeIDIsAddressableByUCI(t *testing.T) {
	node := model.Node{Type: "socks", Server: "proxy.example", ServerPort: 1080}
	id := subscription.Merge("public", nil, []model.Node{node}, nil)[0].ID
	if !strings.HasPrefix(id, "public_") || !uci.IsIdentifier(id) || strings.Contains(id, "-") {
		t.Fatalf("subscription node ID is not a strict UCI identifier: %q", id)
	}
}

func TestCleanSubscriptionNodeWritesOnlyTheNodeStore(t *testing.T) {
	for _, test := range []struct {
		name              string
		stale, referenced bool
		code              string
	}{
		{name: "current", code: subscription.ErrorNodeCurrent},
		{name: "referenced stale", stale: true, referenced: true, code: subscription.ErrorNodeStillReferenced},
		{name: "unreferenced stale", stale: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths := writeCleanupFixture(t, test.stale, test.referenced)
			err := CleanSubscriptionNode(paths.config, paths.nodes, "public", "public_stale")
			paths.assertUCIUnchanged(t)
			if test.code != "" {
				if subscription.ErrorPayloadOf(err).ErrorCode != test.code || len(paths.storedNodes(t)) != 1 {
					t.Fatalf("cleanup was not refused with %s: %v", test.code, err)
				}
				return
			}
			if err != nil || len(paths.storedNodes(t)) != 0 {
				t.Fatalf("unreferenced stale node was not removed: %v", err)
			}
		})
	}
}

func TestSetSubscriptionNodeEnabledWritesOnlyTheNodeStore(t *testing.T) {
	paths := writeCleanupFixture(t, false, true)
	if err := SetSubscriptionNodeEnabled(paths.config, paths.nodes, "public", "public_stale", false); subscription.ErrorPayloadOf(err).ErrorCode != subscription.ErrorNodeStillReferenced {
		t.Fatalf("disabling a referenced node was not rejected: %v", err)
	}
	paths = writeCleanupFixture(t, false, false)
	if err := SetSubscriptionNodeEnabled(paths.config, paths.nodes, "public", "public_stale", false); err != nil {
		t.Fatal(err)
	}
	paths.assertUCIUnchanged(t)
	if nodes := paths.storedNodes(t); len(nodes) != 1 || nodes[0].Enabled {
		t.Fatalf("disabled state was not stored: %#v", nodes)
	}
	list, err := ListSubscriptionNodes(paths.config, paths.nodes, "")
	if err != nil || len(list.Subscriptions) != 1 || list.Subscriptions[0].Nodes[0].ManualCopy.SourceSubscription != "" {
		t.Fatalf("unexpected subscription node list: %#v %v", list, err)
	}
	statuses, err := ReadSubscriptionStatus(paths.config, paths.state, paths.nodes)
	if err != nil || len(statuses) != 1 || statuses[0].NodeCount != 1 {
		t.Fatalf("status did not use the node store: %#v %v", statuses, err)
	}
}

func TestLoadSavedRejectsSubscriptionNodesInUCI(t *testing.T) {
	config := strings.Replace(validSubscriptionConfig("https://example.com/subscription"), "config route 'block'", `config node 'public_old'
	option type 'socks'
	option server '192.0.2.10'
	option server_port '1080'
	option source_subscription 'public'

config route 'block'`, 1)
	paths := writeSubscriptionFixture(t, config)
	_, err := LoadSaved(paths.config, paths.nodes)
	var configErr subscription.ConfigError
	if !errors.As(err, &configErr) || configErr.Validation.Errors[0].Code != "SUBSCRIPTION_NODE_IN_CONFIG" || configErr.Validation.Errors[0].ObjectID != "public_old" {
		t.Fatalf("a subscription node in UCI was accepted: %v", err)
	}
}

func writeCleanupFixture(t *testing.T, stale, referenced bool) subscriptionFixture {
	t.Helper()
	config := validSubscriptionConfig("https://example.com/subscription")
	if referenced {
		config = strings.Replace(config, "config route 'block'", `config route 'proxy'
	option kind 'single'
	option node 'public_stale'

config route 'block'`, 1)
	}
	paths := writeSubscriptionFixture(t, config)
	node := model.Node{
		ID: "public_stale", Enabled: true, Type: "socks", Server: "192.0.2.10", ServerPort: 1080,
		NodeSource: model.NodeSource{SourceSubscription: "public", SourceFingerprint: "fixture", PinnedStale: stale},
	}
	if err := (subscription.NodeStore{Directory: paths.nodes}).Save("public", []model.Node{node}); err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestUpdateSubscriptionWritesNodeStoreAndDoesNotApply(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("vless://00000000-0000-4000-8000-000000000001@example.com:443?security=tls&sni=edge.example.com\n"))
	}))
	defer server.Close()
	paths := writeSubscriptionFixture(t, validSubscriptionConfig(server.URL))
	result, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), paths.config, paths.state, paths.nodes, "public")
	if err != nil {
		t.Fatal(err)
	}
	paths.assertUCIUnchanged(t)
	nodes := paths.storedNodes(t)
	if len(result) != 1 || len(nodes) != 1 || nodes[0].SourceSubscription != "public" || !nodes[0].Enabled {
		t.Fatalf("unexpected subscription update: result=%#v nodes=%#v", result, nodes)
	}
	saved, err := LoadSaved(paths.config, paths.nodes)
	if err != nil || len(saved.Nodes) != 1 || saved.Nodes[0].ID != nodes[0].ID {
		t.Fatalf("stored nodes were not attached to the Saved Intent: %#v %v", saved.Nodes, err)
	}
}

func TestUpdateSubscriptionSkipsInvalidCandidateNodes(t *testing.T) {
	vmess := `{"v":"2","ps":"invalid","add":"vmess.example.com","port":"443","id":"00000000-0000-4000-8000-000000000001","aid":"0","scy":"auto","net":"kcp","tls":"none"}`
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("vmess://" + base64.StdEncoding.EncodeToString([]byte(vmess)) + "\n" +
			"socks5://user:password@example.com:1080\n"))
	}))
	defer server.Close()
	paths := writeSubscriptionFixture(t, validSubscriptionConfig(server.URL))
	result, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), paths.config, paths.state, paths.nodes, "public")
	if err != nil {
		t.Fatal(err)
	}
	if nodes := paths.storedNodes(t); len(result) != 1 || result[0].Skipped != 1 || len(nodes) != 1 || nodes[0].Type != "socks" {
		t.Fatalf("invalid node was not skipped independently: %#v %#v", result, nodes)
	}
}

func TestUpdateSubscriptionAllInvalidPreservesExistingNodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("not-a-node\nstill-not-a-node\n"))
	}))
	defer server.Close()
	paths := writeSubscriptionFixture(t, validSubscriptionConfig(server.URL))
	old := model.Node{ID: "public_old", Enabled: true, Type: "socks", Server: "192.0.2.10", ServerPort: 1080, NodeSource: model.NodeSource{SourceSubscription: "public", SourceFingerprint: "fixture"}}
	if err := (subscription.NodeStore{Directory: paths.nodes}).Save("public", []model.Node{old}); err != nil {
		t.Fatal(err)
	}
	_, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), paths.config, paths.state, paths.nodes, "public")
	if err == nil || !strings.Contains(err.Error(), "no valid nodes") {
		t.Fatalf("all-invalid subscription was not rejected: %v", err)
	}
	if nodes := paths.storedNodes(t); len(nodes) != 1 || nodes[0].ID != old.ID {
		t.Fatalf("all-invalid subscription changed stored nodes: %#v", nodes)
	}
}

func TestFailedRefreshKeepsLastSuccessfulSnapshot(t *testing.T) {
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			http.Error(writer, "temporary", http.StatusServiceUnavailable)
			return
		}
		_, _ = writer.Write([]byte("socks://user:pass@127.0.0.1:1080#Imported\n"))
	}))
	defer server.Close()
	paths := writeSubscriptionFixture(t, validSubscriptionConfig(server.URL))
	if _, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), paths.config, paths.state, paths.nodes, "public"); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	if _, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), paths.config, paths.state, paths.nodes, "public"); err == nil || err.Error() != "update subscription public: subscription server returned HTTP 503" {
		t.Fatalf("unexpected safe update failure: %v", err)
	}
	snapshot, err := subscription.ReadSnapshot(paths.state, "public")
	if err != nil || snapshot.FetchedAt.IsZero() || snapshot.LastFailure == nil || len(paths.storedNodes(t)) != 1 {
		t.Fatalf("failed refresh destroyed successful facts: %#v %v", snapshot, err)
	}
}

func TestUpdateSubscriptionSkipsControlCharacters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("trojan://secret@example.com:443?sni=example.com#bad%0Aname\n" +
			"socks://user:password@example.com:1080#safe\n"))
	}))
	defer server.Close()
	paths := writeSubscriptionFixture(t, validSubscriptionConfig(server.URL))
	result, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), paths.config, paths.state, paths.nodes, "public")
	if err != nil {
		t.Fatal(err)
	}
	if nodes := paths.storedNodes(t); len(result) != 1 || result[0].Skipped != 1 || len(nodes) != 1 || strings.Contains(nodes[0].Name, "\n") {
		t.Fatalf("control-character node was stored: result=%#v nodes=%#v", result, nodes)
	}
}

func TestUpdateSubscriptionStillRejectsGlobalCandidateConflicts(t *testing.T) {
	fresh := model.Node{Enabled: true, Type: "socks", Server: "example.com", ServerPort: 1080}
	collisionID := subscription.Merge("public", nil, []model.Node{fresh}, nil)[0].ID
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("socks://example.com:1080\n"))
	}))
	defer server.Close()
	config := strings.Replace(validSubscriptionConfig(server.URL), "config route 'block'", "config route '"+collisionID+"'\n\toption kind 'block'\n\nconfig route 'block'", 1)
	paths := writeSubscriptionFixture(t, config)
	_, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), paths.config, paths.state, paths.nodes, "public")
	if err == nil || !strings.Contains(err.Error(), "invalid candidate") || len(paths.storedNodes(t)) != 0 {
		t.Fatalf("global candidate conflict was not rejected atomically: %v", err)
	}
}

func validSubscriptionConfig(subscriptionURL string) string {
	return `config steer 'main'
	option schema_version '10'
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

config route 'block'
	option kind 'block'

config dns_profile 'resolver'
	option protocol 'udp'
	option server '1.1.1.1'
	option server_port '53'

config rule 'default'
	option default '1'
	option dns_profile 'resolver'
	option route 'direct'

config subscription 'public'
	option enabled '1'
	option url '` + subscriptionURL + `'
`
}
