// SPDX-License-Identifier: GPL-3.0-or-later

package subscription

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

func storeNode(subscriptionID, id, server string) model.Node {
	return model.Node{
		ID: id, Enabled: true, Name: id, Type: "socks", Server: server, ServerPort: 1080,
		NodeCredentials: model.NodeCredentials{Username: "user", Password: "secret"},
		NodeSource:      model.NodeSource{SourceSubscription: subscriptionID, SourceFingerprint: "fp-" + id},
	}
}

func storeConfig(subscriptions ...model.Subscription) model.Intent {
	return model.Intent{
		Main:          model.Main{ID: "main", SchemaVersion: model.SchemaVersion, Enabled: true, LogLevel: "warn", ProbeDirectURL: "https://direct.example/", ProbeProxyURL: "https://proxy.example/", SpeedtestProxyURL: "https://speed.example/"},
		Bootstrap:     model.Bootstrap{ID: "bootstrap", Protocol: "udp", Server: "1.1.1.1", ServerPort: 53, Strategy: "prefer_ipv4"},
		Nodes:         []model.Node{{ID: "manual", Enabled: true, Type: "socks", Server: "192.0.2.1", ServerPort: 1080}},
		Subscriptions: subscriptions,
		Routes:        []model.Route{{ID: "direct", Enabled: true, Kind: "direct"}},
		DNSProfiles:   []model.DNSProfile{{ID: "dns", Enabled: true, Protocol: "udp", Server: "1.1.1.1", ServerPort: 53}},
		Rules:         []model.Rule{{ID: "default", Enabled: true, Default: true, DNSProfile: "dns", Route: "direct"}},
	}
}

func TestNodeStoreRoundTripWritesPrivateFilesOnlyWhenChanged(t *testing.T) {
	store := NodeStore{Directory: filepath.Join(t.TempDir(), "subscription-nodes")}
	if nodes, err := store.Load("feed"); err != nil || nodes == nil || len(nodes) != 0 {
		t.Fatalf("a never fetched subscription must have no nodes: %#v %v", nodes, err)
	}
	nodes := []model.Node{storeNode("feed", "feed_b", "b.example"), storeNode("feed", "feed_a", "a.example")}
	if err := store.Save("feed", nodes); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.Path("feed"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("node store file must be private: %v %v", info.Mode(), err)
	}
	var document map[string]any
	content, _ := os.ReadFile(store.Path("feed"))
	if err := json.Unmarshal(content, &document); err != nil || document["schema_version"] != float64(1) || document["subscription_id"] != "feed" {
		t.Fatalf("unexpected node store document: %s %v", content, err)
	}
	loaded, err := store.Load("feed")
	if err != nil || !reflect.DeepEqual(loaded, nodes) {
		t.Fatalf("node store did not keep order and fields: %#v %v", loaded, err)
	}
	// An identical save must not rewrite the file: the mode change survives.
	if err := os.Chmod(store.Path("feed"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("feed", nodes); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(store.Path("feed")); info.Mode().Perm() != 0o640 {
		t.Fatal("unchanged nodes were rewritten")
	}
	nodes[0].Enabled = false
	if err := store.Save("feed", nodes); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(store.Path("feed")); info.Mode().Perm() != 0o600 {
		t.Fatal("changed nodes were not rewritten atomically")
	}
	if err := store.Save("feed", []model.Node{storeNode("other", "other_a", "a.example")}); err == nil {
		t.Fatal("a node of another subscription was stored")
	}
	for _, document := range []string{
		`{"schema_version":2,"subscription_id":"feed","nodes":[]}`,
		`{"schema_version":1,"subscription_id":"other","nodes":[]}`,
		`{"schema_version":1,"subscription_id":"feed","nodes":[{"id":"x","enabled":true,"type":"socks","server":"a","server_port":1,"source_subscription":"other"}]}`,
		`{"schema_version":1,"subscription_id":"feed","nodes":[],"extra":true}`,
	} {
		if err := os.WriteFile(store.Path("feed"), []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load("feed"); err == nil {
			t.Fatalf("invalid node store document was accepted: %s", document)
		}
	}
	if _, err := store.Load("../escape"); err == nil {
		t.Fatal("a path-like subscription ID was accepted")
	}
}

func TestAttachJoinsStoredNodesOfConfiguredSubscriptions(t *testing.T) {
	store := NodeStore{Directory: t.TempDir()}
	for _, id := range []string{"enabled", "disabled", "orphan"} {
		if err := store.Save(id, []model.Node{storeNode(id, id+"_node", id+".example")}); err != nil {
			t.Fatal(err)
		}
	}
	config := storeConfig(model.Subscription{ID: "enabled", Enabled: true, URL: "https://feed.example/"}, model.Subscription{ID: "disabled", URL: "https://feed.example/"})
	saved, err := Attach(config, store)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, node := range saved.Nodes {
		ids = append(ids, node.ID)
	}
	if !reflect.DeepEqual(ids, []string{"manual", "enabled_node", "disabled_node"}) {
		t.Fatalf("complete Saved Intent has nodes %v", ids)
	}
	if len(config.Nodes) != 1 {
		t.Fatal("Attach changed the configuration it was given")
	}

	config.Nodes = append(config.Nodes, storeNode("enabled", "copied", "c.example"))
	config.Nodes = append(config.Nodes, model.Node{ID: "stale_mark", Enabled: true, Type: "socks", Server: "s", ServerPort: 1, NodeSource: model.NodeSource{PinnedStale: true}})
	_, err = Attach(config, store)
	var configErr ConfigError
	if !errors.As(err, &configErr) || len(configErr.Validation.Errors) != 2 ||
		configErr.Validation.Errors[0].Code != "SUBSCRIPTION_NODE_IN_CONFIG" || configErr.Validation.Errors[0].ObjectID != "copied" ||
		configErr.Validation.Errors[1].Option != "pinned_stale" {
		t.Fatalf("subscription nodes in the configuration were not rejected: %v", err)
	}

	if err := store.RemoveOrphans(storeConfig(model.Subscription{ID: "enabled"}).Subscriptions); err != nil {
		t.Fatal(err)
	}
	for id, exists := range map[string]bool{"enabled": true, "disabled": false, "orphan": false} {
		if _, err := os.Stat(store.Path(id)); (err == nil) != exists {
			t.Fatalf("orphan cleanup kept=%v for %s", err == nil, id)
		}
	}
}

func subscriptionServer(t *testing.T, body *atomic.Value, requests *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		text := body.Load().(string)
		if text == "" {
			http.Error(writer, "temporary", http.StatusServiceUnavailable)
			return
		}
		_, _ = writer.Write([]byte(text))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestUpdateWritesOnlyTheNodeStoreAndStatus(t *testing.T) {
	var body atomic.Value
	var requests atomic.Int32
	body.Store("socks://user:pass@new.example:1080#New\n")
	server := subscriptionServer(t, &body, &requests)
	root := t.TempDir()
	store := NodeStore{Directory: filepath.Join(root, "nodes")}
	stateDirectory := filepath.Join(root, "state")
	referenced := storeNode("feed", "feed_referenced", "referenced.example")
	unreferenced := storeNode("feed", "feed_unreferenced", "unreferenced.example")
	if err := store.Save("feed", []model.Node{referenced, unreferenced}); err != nil {
		t.Fatal(err)
	}
	config := storeConfig(model.Subscription{ID: "feed", Enabled: true, URL: server.URL, UpdateInterval: "1h"})
	config.Routes = append(config.Routes, model.Route{ID: "via_feed", Enabled: true, Kind: "single", Node: referenced.ID})
	saved, err := Attach(config, store)
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := Update(context.Background(), server.Client(), saved, store, stateDirectory, "", model.Validate)
	if err != nil || len(snapshots) != 1 || snapshots[0].Added != 1 {
		t.Fatalf("update failed: %#v %v", snapshots, err)
	}
	nodes, err := store.Load("feed")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]model.Node{}
	for _, node := range nodes {
		byID[node.ID] = node
	}
	if len(nodes) != 2 || !byID[referenced.ID].PinnedStale || byID[unreferenced.ID].ID != "" {
		t.Fatalf("store did not keep referenced and drop unreferenced nodes: %#v", nodes)
	}
	snapshot, err := os.ReadFile(SnapshotPath(stateDirectory, "feed"))
	if err != nil || strings.Contains(string(snapshot), `"nodes"`) || strings.Contains(string(snapshot), "pass") {
		t.Fatalf("status snapshot must not copy nodes or credentials: %s %v", snapshot, err)
	}

	// Not due: no request. A refetch keeps IDs and the enabled state.
	saved, _ = Attach(config, store)
	if snapshots, err := Update(context.Background(), server.Client(), saved, store, stateDirectory, "", model.Validate); err != nil || len(snapshots) != 0 || requests.Load() != 1 {
		t.Fatalf("an update that is not due fetched: %d %v", requests.Load(), err)
	}
	statuses := Statuses(saved, stateDirectory)
	if len(statuses) != 1 || statuses[0].NodeCount != 2 || statuses[0].Current != 1 || statuses[0].Added != 1 ||
		len(statuses[0].Stale) != 1 || statuses[0].Stale[0].ReferencedBy[0].ID != "via_feed" {
		t.Fatalf("status was not built from stored nodes: %#v", statuses)
	}

	// A failure keeps the last successful facts and the stored nodes.
	body.Store("")
	if _, err := Update(context.Background(), server.Client(), saved, store, stateDirectory, "feed", model.Validate); err == nil ||
		err.Error() != "update subscription feed: subscription server returned HTTP 503" {
		t.Fatalf("unexpected safe update failure: %v", err)
	}
	if after, _ := store.Load("feed"); !reflect.DeepEqual(after, nodes) {
		t.Fatal("a failed fetch changed stored nodes")
	}
	if status := Statuses(saved, stateDirectory)[0]; status.LastSuccess == nil || status.LastFailure == nil || status.NodeCount != 2 {
		t.Fatalf("failed refresh destroyed successful facts: %#v", status)
	}

	// An invalid candidate writes nothing.
	body.Store("socks://user:pass@other.example:1080#Other\n")
	reject := func(model.Intent) model.Validation {
		return model.Validation{Errors: []model.Issue{{Code: "TEST", ObjectType: "node", ObjectID: "x", Message: "rejected"}}}
	}
	if _, err := Update(context.Background(), server.Client(), saved, store, stateDirectory, "feed", reject); err == nil || !strings.Contains(err.Error(), "invalid candidate") {
		t.Fatalf("invalid candidate was accepted: %v", err)
	}
	if after, _ := store.Load("feed"); !reflect.DeepEqual(after, nodes) {
		t.Fatal("an invalid candidate reached the store")
	}
	if _, err := Update(context.Background(), server.Client(), saved, store, stateDirectory, "missing", model.Validate); err == nil {
		t.Fatal("updating an unknown subscription succeeded")
	}
}

func TestUpdateKeepsOtherSubscriptionsWhenOneFails(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("socks://user:pass@good.example:1080#Good\n"))
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "down", http.StatusBadGateway)
	}))
	defer bad.Close()
	root := t.TempDir()
	store := NodeStore{Directory: filepath.Join(root, "nodes")}
	config := storeConfig(
		model.Subscription{ID: "bad", Enabled: true, URL: bad.URL, UpdateInterval: "1h"},
		model.Subscription{ID: "good", Enabled: true, URL: good.URL, UpdateInterval: "1h"},
	)
	saved, _ := Attach(config, store)
	snapshots, err := Update(context.Background(), good.Client(), saved, store, filepath.Join(root, "state"), "", model.Validate)
	var updateErr UpdateError
	if len(snapshots) != 1 || snapshots[0].SubscriptionID != "good" || !errors.As(err, &updateErr) || updateErr.SubscriptionID != "bad" {
		t.Fatalf("one failing subscription held back the others: %#v %v", snapshots, err)
	}
	if nodes, _ := store.Load("good"); len(nodes) != 1 {
		t.Fatalf("the healthy subscription was not stored: %#v", nodes)
	}
}

func TestSetNodeEnabledAndCleanStaleWriteOnlyTheStore(t *testing.T) {
	store := NodeStore{Directory: t.TempDir()}
	used := storeNode("feed", "feed_used", "used.example")
	free := storeNode("feed", "feed_free", "free.example")
	stale := storeNode("feed", "feed_stale", "stale.example")
	stale.PinnedStale = true
	staleUsed := storeNode("feed", "feed_stale_used", "stale-used.example")
	staleUsed.PinnedStale = true
	if err := store.Save("feed", []model.Node{used, free, stale, staleUsed}); err != nil {
		t.Fatal(err)
	}
	config := storeConfig(model.Subscription{ID: "feed", URL: "https://feed.example/"})
	config.Routes = append(config.Routes,
		model.Route{ID: "via_used", Enabled: true, Kind: "single", Node: used.ID},
		model.Route{ID: "via_stale", Enabled: true, Kind: "single", Node: staleUsed.ID},
	)
	load := func() model.Intent {
		t.Helper()
		saved, err := Attach(config, store)
		if err != nil {
			t.Fatal(err)
		}
		return saved
	}
	code := func(err error) string {
		var operation OperationError
		if errors.As(err, &operation) {
			return operation.Code
		}
		return ""
	}
	if err := SetNodeEnabled(load(), store, "feed", used.ID, false, model.Validate); code(err) != ErrorNodeStillReferenced {
		t.Fatalf("disabling a referenced node was not rejected: %v", err)
	}
	if err := SetNodeEnabled(load(), store, "feed", free.ID, false, model.Validate); err != nil {
		t.Fatal(err)
	}
	if nodes, _ := store.Load("feed"); nodes[1].Enabled || !nodes[0].Enabled || len(nodes) != 4 {
		t.Fatalf("enabled state was not stored in place: %#v", nodes)
	}
	if err := SetNodeEnabled(load(), store, "feed", free.ID, true, model.Validate); err != nil {
		t.Fatal(err)
	}
	if err := SetNodeEnabled(load(), store, "other", free.ID, true, model.Validate); code(err) != ErrorSubscriptionNotFound {
		t.Fatalf("unknown subscription: %v", err)
	}
	if err := SetNodeEnabled(load(), store, "feed", "missing", true, model.Validate); code(err) != ErrorNodeNotFound {
		t.Fatalf("unknown node: %v", err)
	}

	if err := CleanStale(load(), store, "feed", used.ID, model.Validate); code(err) != ErrorNodeCurrent {
		t.Fatalf("cleanup removed a current node: %v", err)
	}
	err := CleanStale(load(), store, "feed", staleUsed.ID, model.Validate)
	payload := ErrorPayloadOf(err)
	if payload.ErrorCode != ErrorNodeStillReferenced || payload.Validation == nil || !strings.HasPrefix(payload.Error, "NODE_STILL_REFERENCED: route \"via_stale\"") {
		t.Fatalf("cleanup removed a referenced stale node: %#v", payload)
	}
	if err := CleanStale(load(), store, "feed", stale.ID, model.Validate); err != nil {
		t.Fatal(err)
	}
	if nodes, _ := store.Load("feed"); len(nodes) != 3 || nodes[2].ID != staleUsed.ID {
		t.Fatalf("cleanup did not remove only the stale node: %#v", nodes)
	}
}

func TestListNodesCarriesTheSharedManualCopy(t *testing.T) {
	store := NodeStore{Directory: t.TempDir()}
	node := storeNode("feed", "feed_node", "node.example")
	node.Enabled, node.PinnedStale, node.ALPN = false, true, []string{"h2"}
	if err := store.Save("feed", []model.Node{node}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("orphan", []model.Node{storeNode("orphan", "orphan_node", "orphan.example")}); err != nil {
		t.Fatal(err)
	}
	saved, err := Attach(storeConfig(model.Subscription{ID: "empty"}, model.Subscription{ID: "feed"}), store)
	if err != nil {
		t.Fatal(err)
	}
	list, err := ListNodes(saved, "")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Subscriptions []struct {
			ID    string           `json:"id"`
			Nodes []map[string]any `json:"nodes"`
		} `json:"subscriptions"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Subscriptions) != 2 || document.Subscriptions[0].ID != "empty" || len(document.Subscriptions[0].Nodes) != 0 || document.Subscriptions[1].ID != "feed" {
		t.Fatalf("read interface must list exactly the configured subscriptions: %s", encoded)
	}
	listed := document.Subscriptions[1].Nodes[0]
	if listed["id"] != "feed_node" || listed["source_subscription"] != "feed" || listed["pinned_stale"] != true || listed["password"] != "secret" {
		t.Fatalf("listed node lost its fields: %v", listed)
	}
	manual, _ := listed["manual_copy"].(map[string]any)
	if _, hasID := manual["id"]; hasID || manual["enabled"] != true || manual["name"] != "feed_node" || manual["password"] != "secret" ||
		manual["source_subscription"] != nil || manual["source_fingerprint"] != nil || manual["pinned_stale"] != nil || manual["alpn"] == nil {
		t.Fatalf("manual copy does not follow the shared rule: %v", manual)
	}
	copy := CopyAsManual(node)
	copy.ALPN[0] = "h3"
	if node.ALPN[0] != "h2" || copy.ID != "" || copy.SourceSubscription != "" {
		t.Fatalf("CopyAsManual shares state with the original: %#v", copy)
	}
	if _, err := ListNodes(saved, "orphan"); ErrorPayloadOf(err).ErrorCode != ErrorSubscriptionNotFound {
		t.Fatalf("an orphan subscription was listed: %v", err)
	}
}

func TestPlanMigrationMovesSubscriptionNodesInOrder(t *testing.T) {
	config := storeConfig(model.Subscription{ID: "feed"}, model.Subscription{ID: "spare", Enabled: false})
	config.Main.SchemaVersion = LegacySchemaVersion
	b := storeNode("feed", "feed_b", "b.example")
	a := storeNode("feed", "feed_a", "a.example")
	a.Enabled, a.PinnedStale = false, true
	spare := storeNode("spare", "spare_x", "x.example")
	orphan := storeNode("gone", "gone_x", "gone.example")
	stray := model.Node{ID: "stray", Enabled: true, Type: "socks", Server: "s.example", ServerPort: 1, NodeSource: model.NodeSource{SourceFingerprint: "fp"}}
	config.Nodes = append(config.Nodes, b, orphan, a, spare, stray)
	migration := PlanMigration(config)
	if migration.Config.Main.SchemaVersion != model.SchemaVersion || config.Main.SchemaVersion != LegacySchemaVersion {
		t.Fatal("migration must set schema 10 on a copy")
	}
	if !reflect.DeepEqual(migration.Nodes["feed"], []model.Node{b, a}) || !reflect.DeepEqual(migration.Nodes["spare"], []model.Node{spare}) {
		t.Fatalf("subscription nodes were not moved with their state and order: %#v", migration.Nodes)
	}
	var kept []string
	for _, node := range migration.Config.Nodes {
		kept = append(kept, node.ID)
		if node.SourceSubscription != "" || node.SourceFingerprint != "" || node.PinnedStale {
			t.Fatalf("node %s kept a source field", node.ID)
		}
	}
	if !reflect.DeepEqual(kept, []string{"manual", "gone_x", "stray"}) || !reflect.DeepEqual(migration.Moved, []string{"feed_b", "feed_a", "spare_x"}) ||
		!reflect.DeepEqual(migration.Cleared, []string{"gone_x", "stray"}) {
		t.Fatalf("unexpected migration plan: kept=%v moved=%v cleared=%v", kept, migration.Moved, migration.Cleared)
	}
	if len(model.ConfigNodeIssues(migration.Config)) != 0 {
		t.Fatal("migrated configuration still contains subscription nodes")
	}

	root := t.TempDir()
	stateDirectory := filepath.Join(root, "state")
	legacy := `{"subscription_id":"feed","url":"https://feed.example/","fetched_at":"2026-08-26T08:00:00Z","nodes":[{"id":"feed_b","password":"secret"}],"skipped":1}`
	if err := os.MkdirAll(filepath.Join(stateDirectory, "subscriptions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SnapshotPath(stateDirectory, "feed"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NodeStore{Directory: filepath.Join(root, "nodes")}
	if err := WriteMigratedNodes(migration, store, stateDirectory); err != nil {
		t.Fatal(err)
	}
	if nodes, err := store.Load("feed"); err != nil || !reflect.DeepEqual(nodes, []model.Node{b, a}) {
		t.Fatalf("moved nodes were not stored: %#v %v", nodes, err)
	}
	snapshot, _ := os.ReadFile(SnapshotPath(stateDirectory, "feed"))
	if strings.Contains(string(snapshot), "secret") || !strings.Contains(string(snapshot), `"skipped": 1`) {
		t.Fatalf("schema 9 snapshot kept its node copy: %s", snapshot)
	}
	saved, err := Attach(migration.Config, store)
	if err != nil || len(saved.Nodes) != len(config.Nodes) {
		t.Fatalf("migrated Saved Intent lost nodes: %#v %v", saved.Nodes, err)
	}
	if !LegacyJSON([]byte(`{"main":{"schema_version":9}}`)) || LegacyJSON([]byte(`{"main":{"schema_version":10}}`)) || LegacyJSON([]byte(`{`)) {
		t.Fatal("LegacyJSON misread the schema header")
	}
}
