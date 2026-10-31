// SPDX-License-Identifier: GPL-3.0-or-later

package linux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gsh20040816/steer/go/internal/compiler"
	model "github.com/gsh20040816/steer/go/internal/intent"
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
		{name: "manual empty interval", manualID: "feed", wantFetch: true},
		{name: "first scheduled fetch", interval: "1h", wantFetch: true},
		{name: "automatic before due", interval: "1h", snapshotAge: 30 * time.Minute},
		{name: "automatic just due", interval: "1h", snapshotAge: time.Hour, wantFetch: true},
		{name: "manual before due", interval: "1h", manualID: "feed", snapshotAge: 30 * time.Minute, wantFetch: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				_, _ = writer.Write([]byte("socks://user:pass@127.0.0.1:1080#Imported\n"))
			}))
			defer server.Close()

			root := t.TempDir()
			configPath := filepath.Join(root, "config.json")
			stateDirectory := filepath.Join(root, "state")
			value := validIntent()
			value.Subscriptions = []model.Subscription{{ID: "feed", Enabled: true, URL: server.URL, UpdateInterval: test.interval}}
			if _, err := SavedStore(configPath, stateDirectory).Save(value, ""); err != nil {
				t.Fatal(err)
			}
			if test.snapshotAge != 0 {
				if err := subscription.SaveSnapshot(stateDirectory, SubscriptionSnapshot{SubscriptionID: "feed", URL: server.URL, FetchedAt: time.Now().Add(-test.snapshotAge)}); err != nil {
					t.Fatal(err)
				}
			}

			snapshots, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), configPath, stateDirectory, test.manualID)
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
		})
	}
}

func TestSubscriptionUpdateWritesOnlyTheNodeStore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("socks://user:pass@new.example:1080#New\n"))
	}))
	defer server.Close()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDirectory := filepath.Join(root, "state")
	store := SavedStore(configPath, stateDirectory)
	referenced := model.Node{ID: "feed_referenced", Enabled: true, Type: "socks", Server: "referenced.example", ServerPort: 1080, NodeSource: model.NodeSource{SourceSubscription: "feed"}}
	if err := (subscription.NodeStore{Directory: filepath.Join(stateDirectory, "subscription-nodes")}).Save("feed", []model.Node{referenced}); err != nil {
		t.Fatal(err)
	}
	value := validIntent()
	value.Subscriptions = []model.Subscription{{ID: "feed", Enabled: true, URL: server.URL}}
	value.Routes = append(value.Routes, model.Route{ID: "subscription_route", Enabled: true, Kind: "single", Node: referenced.ID})
	revision, err := store.Save(value, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), configPath, stateDirectory, "feed"); err != nil {
		t.Fatal(err)
	}
	config, after, err := store.Load()
	if err != nil || after != revision || len(config.Nodes) != 0 {
		t.Fatalf("subscription update changed config.json: %#v %v", config.Nodes, err)
	}
	saved, _, err := store.LoadSaved()
	if err != nil || len(saved.Nodes) != 2 {
		t.Fatalf("stored nodes were not attached: %#v %v", saved.Nodes, err)
	}
	statuses, err := ReadSubscriptionStatus(configPath, stateDirectory)
	if err != nil || len(statuses) != 1 || statuses[0].NodeCount != 2 || len(statuses[0].Stale) != 1 || len(statuses[0].Stale[0].ReferencedBy) != 1 {
		t.Fatalf("referenced stale status drifted: %#v %v", statuses, err)
	}

	list, err := ListSubscriptionNodes(configPath, stateDirectory, "feed")
	if err != nil || len(list.Subscriptions) != 1 || len(list.Subscriptions[0].Nodes) != 2 {
		t.Fatalf("unexpected node list: %#v %v", list, err)
	}
	if err := SetSubscriptionNodeEnabled(configPath, stateDirectory, "feed", referenced.ID, false); subscription.ErrorPayloadOf(err).ErrorCode != subscription.ErrorNodeStillReferenced {
		t.Fatalf("disabling a referenced node was not rejected: %v", err)
	}
	if err := CleanSubscriptionNode(configPath, stateDirectory, "feed", referenced.ID); subscription.ErrorPayloadOf(err).ErrorCode != subscription.ErrorNodeStillReferenced {
		t.Fatalf("cleaning a referenced stale node was not rejected: %v", err)
	}
	if _, current, _ := store.Load(); current != revision {
		t.Fatal("node operations changed config.json")
	}
}

func TestSaveRejectsSubscriptionNodesAndDropsDeletedSubscriptionStore(t *testing.T) {
	root := t.TempDir()
	stateDirectory := filepath.Join(root, "state")
	store := SavedStore(filepath.Join(root, "config.json"), stateDirectory)
	nodes := subscription.NodeStore{Directory: filepath.Join(stateDirectory, "subscription-nodes")}
	if err := nodes.Save("feed", []model.Node{{ID: "feed_a", Enabled: true, Type: "socks", Server: "a.example", ServerPort: 1080, NodeSource: model.NodeSource{SourceSubscription: "feed"}}}); err != nil {
		t.Fatal(err)
	}
	value := validIntent()
	value.Subscriptions = []model.Subscription{{ID: "feed", URL: "https://feed.example/"}}
	if _, err := store.Save(value, ""); err != nil {
		t.Fatal(err)
	}
	copied := value
	copied.Nodes = []model.Node{{ID: "feed_a", Enabled: true, Type: "socks", Server: "a.example", ServerPort: 1080, NodeSource: model.NodeSource{SourceSubscription: "feed"}}}
	_, err := store.Save(copied, "")
	var validationErr ValidationError
	if !errors.As(err, &validationErr) || validationErr.Validation.Errors[0].Code != "SUBSCRIPTION_NODE_IN_CONFIG" {
		t.Fatalf("a draft with subscription nodes was saved: %v", err)
	}
	value.Routes = append(value.Routes, model.Route{ID: "via_feed", Enabled: true, Kind: "single", Node: "feed_a"})
	if _, err := store.Save(value, ""); err != nil {
		t.Fatal(err)
	}
	withoutSubscription := value
	withoutSubscription.Subscriptions = nil
	if _, err := store.Save(withoutSubscription, ""); !errors.As(err, &validationErr) {
		t.Fatalf("deleting a subscription whose node is still routed was accepted: %v", err)
	}
	value.Routes, value.Subscriptions = validIntent().Routes, nil
	if _, err := store.Save(value, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(nodes.Path("feed")); !os.IsNotExist(err) {
		t.Fatalf("a deleted subscription kept its node store file: %v", err)
	}
}

func TestMigrateMovesSubscriptionNodesWithoutChangingTheRuntime(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDirectory := filepath.Join(root, "state")
	legacy := validIntent()
	legacy.Main.SchemaVersion = subscription.LegacySchemaVersion
	legacy.Main.Enabled = true
	legacy.Nodes = []model.Node{
		{ID: "manual", Enabled: true, Type: "socks", Server: "192.0.2.1", ServerPort: 1080},
		{ID: "feed_b", Enabled: true, Type: "socks", Server: "b.example", ServerPort: 1080, NodeSource: model.NodeSource{SourceSubscription: "feed", SourceFingerprint: "fp-b"}},
		{ID: "feed_a", Enabled: false, Type: "socks", Server: "a.example", ServerPort: 1080, NodeSource: model.NodeSource{SourceSubscription: "feed", SourceFingerprint: "fp-a", PinnedStale: true}},
	}
	legacy.Subscriptions = []model.Subscription{{ID: "feed", Enabled: true, URL: "https://feed.example/"}}
	legacy.Routes = append(legacy.Routes, model.Route{ID: "via_b", Enabled: true, Kind: "single", Node: "feed_b"})
	legacy.Rules = append([]model.Rule{{ID: "proxied", Enabled: true, DomainMatch: []string{"domain:example.com"}, DNSProfile: "public", Route: "via_b"}}, legacy.Rules...)
	var encoded bytes.Buffer
	if err := model.EncodeJSON(&encoded, legacy); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if !NeedsMigration(configPath) {
		t.Fatal("schema 9 was not detected")
	}
	changed, err := Migrate(configPath, stateDirectory)
	if err != nil || !changed || NeedsMigration(configPath) {
		t.Fatalf("migration did not run: %v", err)
	}
	store := SavedStore(configPath, stateDirectory)
	config, _, err := store.Load()
	if err != nil || config.Main.SchemaVersion != model.SchemaVersion || len(config.Nodes) != 1 || config.Nodes[0].ID != "manual" {
		t.Fatalf("migrated config.json kept subscription nodes: %#v %v", config, err)
	}
	saved, _, err := store.LoadSaved()
	if err != nil {
		t.Fatal(err)
	}
	if validation := Validate(saved); !validation.OK {
		t.Fatalf("migrated Saved Intent is invalid: %#v", validation.Errors)
	}
	compile := func(value model.Intent) string {
		encoded, _ := json.Marshal(compiler.Compile(value, compiler.Options{Target: NewPlan(value).CompilerTarget()}).SingBox)
		return string(encoded)
	}
	if runtime := compile(saved); runtime != compile(legacy) || !strings.Contains(runtime, "b.example") {
		t.Fatal("migration changed the compiled sing-box configuration")
	}
	if nodes := saved.Nodes[1:]; nodes[0].ID != "feed_b" || nodes[1].ID != "feed_a" || nodes[1].Enabled || !nodes[1].PinnedStale {
		t.Fatalf("stored nodes lost order or state: %#v", nodes)
	}
	if changed, err := Migrate(configPath, stateDirectory); err != nil || changed {
		t.Fatalf("migration is not idempotent: %v", err)
	}
}
