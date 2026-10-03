// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	model "github.com/gsh20040816/steer/go/internal/intent"
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
			if _, err := (IntentStore{Paths: Paths{ConfigPath: configPath}}).Save(value, ""); err != nil {
				t.Fatal(err)
			}
			if test.snapshotAge != 0 {
				if err := saveSubscriptionSnapshot(stateDirectory, SubscriptionSnapshot{SubscriptionID: "public", URL: server.URL, FetchedAt: time.Now().Add(-test.snapshotAge)}); err != nil {
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
	if _, err := (IntentStore{Paths: Paths{ConfigPath: configPath}}).Save(value, ""); err != nil {
		t.Fatal(err)
	}
	snapshots, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), configPath, stateDirectory, "public")
	if err != nil || len(snapshots) != 1 || len(snapshots[0].Nodes) != 1 {
		t.Fatalf("unexpected subscription update: %#v %v", snapshots, err)
	}
	statuses, err := ReadSubscriptionStatus(configPath, stateDirectory)
	if err != nil || len(statuses) != 1 || statuses[0].NodeCount != 1 || !strings.Contains(statuses[0].URL, "http") {
		t.Fatalf("unexpected subscription status: %#v %v", statuses, err)
	}
}

func TestConfiguredSubscriptionUpdateDropsOnlyUnreferencedDisappearedNodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("socks://user:pass@new.example:1080#New\n"))
	}))
	defer server.Close()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDirectory := filepath.Join(root, "state")
	value := validIntent()
	value.Subscriptions = []model.Subscription{{ID: "public", Enabled: true, URL: server.URL}}
	referenced := model.Node{ID: "public_referenced", Enabled: true, Type: "socks", Server: "referenced.example", ServerPort: 1080, NodeSource: model.NodeSource{SourceSubscription: "public"}}
	unreferenced := model.Node{ID: "public_unreferenced", Enabled: true, Type: "socks", Server: "unreferenced.example", ServerPort: 1080, NodeSource: model.NodeSource{SourceSubscription: "public"}}
	value.Nodes = append(value.Nodes, referenced, unreferenced)
	value.Routes = append(value.Routes, model.Route{ID: "subscription_route", Enabled: true, Kind: "single", Node: referenced.ID})
	if _, err := (IntentStore{Paths: Paths{ConfigPath: configPath}}).Save(value, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), configPath, stateDirectory, "public"); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := (IntentStore{Paths: Paths{ConfigPath: configPath}}).Load()
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
	if _, err := (IntentStore{Paths: Paths{ConfigPath: configPath}}).Save(value, ""); err != nil {
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
