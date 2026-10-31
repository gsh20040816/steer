// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"bytes"
	"context"
	"encoding/json"
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

			root := t.TempDir()
			configPath := filepath.Join(root, "config.json")
			stateDirectory := filepath.Join(root, "state")
			value := validIntent()
			value.Subscriptions = []model.Subscription{{ID: "public", Enabled: true, URL: server.URL, UpdateInterval: test.interval}}
			if _, err := SavedStore(configPath, stateDirectory).Save(value, ""); err != nil {
				t.Fatal(err)
			}
			if test.snapshotAge != 0 {
				if err := subscription.SaveSnapshot(stateDirectory, SubscriptionSnapshot{SubscriptionID: "public", URL: server.URL, FetchedAt: time.Now().Add(-test.snapshotAge)}); err != nil {
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

func TestConfiguredSubscriptionUpdatePersistsStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("socks://user:pass@127.0.0.1:1080#Imported\n"))
	}))
	defer server.Close()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDirectory := filepath.Join(root, "state")
	value := validIntent()
	value.Subscriptions = []model.Subscription{{ID: "public", Enabled: true, Name: "Public", URL: server.URL, UpdateInterval: "1h"}}
	if _, err := SavedStore(configPath, stateDirectory).Save(value, ""); err != nil {
		t.Fatal(err)
	}
	snapshots, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), configPath, stateDirectory, "public")
	if err != nil || len(snapshots) != 1 || snapshots[0].Added != 1 {
		t.Fatalf("unexpected subscription update: %#v %v", snapshots, err)
	}
	statuses, err := ReadSubscriptionStatus(configPath, stateDirectory)
	if err != nil || len(statuses) != 1 || statuses[0].NodeCount != 1 || !strings.Contains(statuses[0].URL, "http") {
		t.Fatalf("unexpected subscription status: %#v %v", statuses, err)
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
	referenced := model.Node{ID: "public_referenced", Enabled: true, Type: "socks", Server: "referenced.example", ServerPort: 1080, NodeSource: model.NodeSource{SourceSubscription: "public"}}
	unreferenced := model.Node{ID: "public_unreferenced", Enabled: true, Type: "socks", Server: "unreferenced.example", ServerPort: 1080, NodeSource: model.NodeSource{SourceSubscription: "public"}}
	if err := (subscription.NodeStore{Directory: subscription.NodeDirectory(stateDirectory)}).Save("public", []model.Node{referenced, unreferenced}); err != nil {
		t.Fatal(err)
	}
	value := validIntent()
	value.Subscriptions = []model.Subscription{{ID: "public", Enabled: true, URL: server.URL}}
	value.Routes = append(value.Routes, model.Route{ID: "subscription_route", Enabled: true, Kind: "single", Node: referenced.ID})
	revision, err := store.Save(value, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), configPath, stateDirectory, "public"); err != nil {
		t.Fatal(err)
	}
	if _, current, err := store.Load(); err != nil || current != revision {
		t.Fatalf("subscription update changed config.json: %v", err)
	}
	loaded, _, err := store.LoadSaved()
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]model.Node, len(loaded.Nodes))
	for _, node := range loaded.Nodes {
		byID[node.ID] = node
	}
	if !byID[referenced.ID].PinnedStale || byID[unreferenced.ID].ID != "" {
		t.Fatalf("subscription disappearance lifecycle drifted: %#v", loaded.Nodes)
	}
	statuses, err := ReadSubscriptionStatus(configPath, stateDirectory)
	if err != nil || len(statuses) != 1 || len(statuses[0].Stale) != 1 || len(statuses[0].Stale[0].ReferencedBy) != 1 {
		t.Fatalf("referenced stale status drifted: %#v %v", statuses, err)
	}
	if err := SetSubscriptionNodeEnabled(configPath, stateDirectory, "public", referenced.ID, false); subscription.ErrorPayloadOf(err).ErrorCode != subscription.ErrorNodeStillReferenced {
		t.Fatalf("disabling a referenced node was not rejected: %v", err)
	}
	list, err := ListSubscriptionNodes(configPath, stateDirectory, "")
	if err != nil || len(list.Subscriptions) != 1 || len(list.Subscriptions[0].Nodes) != 2 {
		t.Fatalf("unexpected node list: %#v %v", list, err)
	}
}

func TestMigrateMovesSubscriptionNodesWithoutChangingTheRuntime(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDirectory := filepath.Join(root, "state")
	legacy := validIntent()
	legacy.Main.SchemaVersion = subscription.LegacySchemaVersion
	legacy.Nodes = []model.Node{
		{ID: "public_b", Enabled: true, Type: "socks", Server: "b.example", ServerPort: 1080, NodeSource: model.NodeSource{SourceSubscription: "public", SourceFingerprint: "fp-b"}},
		{ID: "manual", Enabled: true, Type: "socks", Server: "192.0.2.1", ServerPort: 1080},
	}
	legacy.Subscriptions = []model.Subscription{{ID: "public", Enabled: true, URL: "https://feed.example/"}}
	legacy.Routes = append(legacy.Routes, model.Route{ID: "via_b", Enabled: true, Kind: "single", Node: "public_b"})
	legacy.Rules = append([]model.Rule{{ID: "proxied", Enabled: true, DomainMatch: []string{"domain:example.com"}, DNSProfile: "dns", Route: "via_b"}}, legacy.Rules...)
	var encoded bytes.Buffer
	if err := model.EncodeJSON(&encoded, legacy); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, encoded.Bytes(), 0o640); err != nil {
		t.Fatal(err)
	}
	if changed, err := Migrate(configPath, stateDirectory); err != nil || !changed || NeedsMigration(configPath) {
		t.Fatalf("migration did not run: %v", err)
	}
	saved, _, err := SavedStore(configPath, stateDirectory).LoadSaved()
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
	if changed, err := Migrate(configPath, stateDirectory); err != nil || changed {
		t.Fatalf("migration is not idempotent: %v", err)
	}
}

func TestFailedRefreshKeepsLastSuccessfulSubscriptionStatus(t *testing.T) {
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			http.Error(writer, "temporary", http.StatusServiceUnavailable)
			return
		}
		_, _ = writer.Write([]byte("socks://user:pass@127.0.0.1:1080#Imported\n"))
	}))
	defer server.Close()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDirectory := filepath.Join(root, "state")
	value := validIntent()
	value.Subscriptions = []model.Subscription{{ID: "public", Enabled: true, URL: server.URL}}
	if _, err := SavedStore(configPath, stateDirectory).Save(value, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), configPath, stateDirectory, "public"); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	if _, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), configPath, stateDirectory, "public"); err == nil || err.Error() != "update subscription public: subscription server returned HTTP 503" {
		t.Fatalf("unexpected safe update failure: %v", err)
	}
	statuses, err := ReadSubscriptionStatus(configPath, stateDirectory)
	if err != nil || len(statuses) != 1 || statuses[0].LastSuccess == nil || statuses[0].LastFailure == nil || statuses[0].NodeCount != 1 {
		t.Fatalf("failed refresh destroyed successful facts: %#v %v", statuses, err)
	}
}
