// SPDX-License-Identifier: GPL-3.0-or-later

package uispec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	model "github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/subscription"
)

// Shared UI fixtures use the schema 10 shape: configuration nodes are manual
// nodes only, and subscription nodes appear in a separate subscription_nodes
// payload that is exactly what the subscription node read interface returns.
func TestSharedFixturesUseTheSubscriptionNodeReadShape(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "ui", "*-fixtures.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no shared UI fixtures: %v", err)
	}
	payloads := 0
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var document any
		if err := json.Unmarshal(content, &document); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		payloads += checkFixtureValue(t, filepath.Base(path), "", document)
	}
	if payloads == 0 {
		t.Fatal("no shared fixture carries a subscription_nodes payload")
	}
}

func checkFixtureValue(t *testing.T, file, path string, value any) int {
	t.Helper()
	payloads := 0
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			childPath := path + "." + key
			switch {
			case key == "subscription_nodes":
				checkSubscriptionNodesPayload(t, file+childPath, child)
				payloads++
			case key == "source_subscription" || key == "source_fingerprint" || key == "pinned_stale" || key == "manual_copy":
				t.Errorf("%s%s: configuration objects must not carry %s; list subscription nodes in a subscription_nodes payload", file, path, key)
			default:
				payloads += checkFixtureValue(t, file, childPath, child)
			}
		}
	case []any:
		for _, child := range typed {
			payloads += checkFixtureValue(t, file, path+"[]", child)
		}
	}
	return payloads
}

func checkSubscriptionNodesPayload(t *testing.T, path string, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Subscriptions []struct {
			ID    string            `json:"id"`
			Nodes []json.RawMessage `json:"nodes"`
		} `json:"subscriptions"`
	}
	if err := json.Unmarshal(encoded, &raw); err != nil || raw.Subscriptions == nil {
		t.Fatalf("%s: not a subscription node list: %v", path, err)
	}
	for _, entry := range raw.Subscriptions {
		if entry.Nodes == nil {
			t.Fatalf("%s: subscription %q has no nodes array", path, entry.ID)
		}
		for _, listed := range entry.Nodes {
			var node model.Node
			if err := json.Unmarshal(listed, &node); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			if node.SourceSubscription != entry.ID {
				t.Fatalf("%s: node %q belongs to %q, not %q", path, node.ID, node.SourceSubscription, entry.ID)
			}
			want, err := json.Marshal(subscription.ListedNode{Node: node, ManualCopy: subscription.ManualNode{Node: subscription.CopyAsManual(node)}})
			if err != nil {
				t.Fatal(err)
			}
			var got, expected any
			if err := json.Unmarshal(listed, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(want, &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, expected) {
				t.Errorf("%s: node %q differs from the read interface:\n got %s\nwant %s", path, node.ID, listed, want)
			}
		}
	}
}

// The subscription status fixture and its subscription_nodes payload describe
// the same stored nodes: counts and stale markers agree.
func TestSubscriptionStatusFixtureMatchesItsNodePayload(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "ui", "subscription-status-fixtures.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		SubscriptionNodes subscription.NodeList `json:"subscription_nodes"`
		Cases             []struct {
			Status subscription.Status `json:"status"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	lists := map[string][]subscription.ListedNode{}
	for _, entry := range document.SubscriptionNodes.Subscriptions {
		lists[entry.ID] = entry.Nodes
	}
	if len(lists) != len(document.Cases) {
		t.Fatalf("payload lists %d subscriptions for %d status cases", len(lists), len(document.Cases))
	}
	for _, fixture := range document.Cases {
		status := fixture.Status
		nodes, exists := lists[status.ID]
		if !exists || len(nodes) != status.NodeCount {
			t.Fatalf("subscription %q: payload has %d nodes, status counts %d", status.ID, len(nodes), status.NodeCount)
		}
		stale := map[string]bool{}
		for _, node := range nodes {
			if node.PinnedStale {
				stale[node.ID] = true
			}
		}
		if len(stale) != len(status.Stale) || len(nodes)-len(stale) != status.Current {
			t.Fatalf("subscription %q: stale and current counts disagree with the payload", status.ID)
		}
		for _, node := range status.Stale {
			if !stale[node.ID] {
				t.Fatalf("subscription %q: stale node %q is not pinned_stale in the payload", status.ID, node.ID)
			}
		}
	}
}
