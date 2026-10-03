// SPDX-License-Identifier: GPL-3.0-or-later

package linux

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
			if _, err := (IntentStore{Path: configPath}).Save(value, ""); err != nil {
				t.Fatal(err)
			}
			if test.snapshotAge != 0 {
				if err := saveSubscriptionSnapshot(stateDirectory, SubscriptionSnapshot{SubscriptionID: "feed", URL: server.URL, FetchedAt: time.Now().Add(-test.snapshotAge)}); err != nil {
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

func TestConfiguredSubscriptionUpdateDropsOnlyUnreferencedDisappearedNodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("socks://user:pass@new.example:1080#New\n"))
	}))
	defer server.Close()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDirectory := filepath.Join(root, "state")
	value := validIntent()
	value.Subscriptions = []model.Subscription{{ID: "feed", Enabled: true, URL: server.URL}}
	referenced := model.Node{ID: "feed_referenced", Enabled: true, Type: "socks", Server: "referenced.example", ServerPort: 1080, NodeSource: model.NodeSource{SourceSubscription: "feed"}}
	unreferenced := model.Node{ID: "feed_unreferenced", Enabled: true, Type: "socks", Server: "unreferenced.example", ServerPort: 1080, NodeSource: model.NodeSource{SourceSubscription: "feed"}}
	value.Nodes = append(value.Nodes, referenced, unreferenced)
	value.Routes = append(value.Routes, model.Route{ID: "subscription_route", Enabled: true, Kind: "single", Node: referenced.ID})
	if _, err := (IntentStore{Path: configPath}).Save(value, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), configPath, stateDirectory, "feed"); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := (IntentStore{Path: configPath}).Load()
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
	value.Subscriptions = []model.Subscription{{ID: "feed", Enabled: true, URL: server.URL}}
	if _, err := (IntentStore{Path: configPath}).Save(value, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), configPath, stateDirectory, "feed"); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	if _, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), configPath, stateDirectory, "feed"); err == nil || err.Error() != "update subscription feed: subscription server returned HTTP 503" {
		t.Fatalf("unexpected safe update failure: %v", err)
	}
	statuses, err := ReadSubscriptionStatus(configPath, stateDirectory)
	if err != nil || len(statuses) != 1 {
		t.Fatalf("read status: %#v %v", statuses, err)
	}
	status := statuses[0]
	if status.NeverFetched || status.LastSuccess == nil || status.LastFailure == nil || status.LastFailure.At == nil ||
		status.LastFailure.Summary != "subscription server returned HTTP 503" || status.NodeCount != 1 {
		t.Fatalf("failed refresh destroyed successful facts: %#v", status)
	}
}

func TestOneFailingSubscriptionDoesNotBlockOthers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/broken" {
			http.Error(writer, "gone", http.StatusServiceUnavailable)
			return
		}
		_, _ = writer.Write([]byte("socks://user:pass@127.0.0.1:1080#Imported\n"))
	}))
	defer server.Close()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDirectory := filepath.Join(root, "state")
	value := validIntent()
	value.Subscriptions = []model.Subscription{
		{ID: "broken", Enabled: true, URL: server.URL + "/broken", UpdateInterval: "1h"},
		{ID: "working", Enabled: true, URL: server.URL + "/working", UpdateInterval: "1h"},
	}
	if _, err := (IntentStore{Path: configPath}).Save(value, ""); err != nil {
		t.Fatal(err)
	}
	snapshots, err := UpdateConfiguredSubscriptions(context.Background(), server.Client(), configPath, stateDirectory, "")
	if err == nil || len(snapshots) != 1 || snapshots[0].SubscriptionID != "working" {
		t.Fatalf("working subscription was held back: %#v %v", snapshots, err)
	}
	loaded, _, err := (IntentStore{Path: configPath}).Load()
	if err != nil || len(loaded.Nodes) != len(value.Nodes)+1 {
		t.Fatalf("working subscription nodes were not saved: %#v %v", loaded.Nodes, err)
	}
}
