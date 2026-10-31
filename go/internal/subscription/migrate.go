// SPDX-License-Identifier: GPL-3.0-or-later

package subscription

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

// LegacySchemaVersion is the last schema that kept subscription nodes in the
// Saved configuration.
const LegacySchemaVersion = 9

// LegacyJSON reports whether a canonical JSON configuration is still schema 9.
// Startup paths check this before taking the operation lock, which an Apply
// may hold while it waits for the service to start.
func LegacyJSON(content []byte) bool {
	var header struct {
		Main struct {
			SchemaVersion int `json:"schema_version"`
		} `json:"main"`
	}
	return json.Unmarshal(content, &header) == nil && header.Main.SchemaVersion == LegacySchemaVersion
}

// Migration moves the subscription nodes of a schema 9 configuration into the
// node store. Platforms write Nodes with WriteMigratedNodes first and then
// replace the configuration with Config, so an interrupted migration simply
// runs again.
type Migration struct {
	// Config is the schema 10 configuration with only manual nodes.
	Config model.Intent
	// Nodes holds the moved nodes of each configured subscription with their
	// IDs, enabled state, fingerprints, stale marks and order.
	Nodes map[string][]model.Node
	// Moved lists every node removed from the configuration.
	Moved []string
	// Cleared lists nodes that stay in the configuration as manual nodes
	// because their subscription is no longer configured or they carried a
	// stray source field; their source fields are removed. Keeping them keeps
	// every route reference, so the compiled runtime is the same.
	Cleared []string
}

// PlanMigration extracts the subscription nodes of a schema 9 configuration.
// The compiled sing-box configuration of the complete Saved Intent is the
// same before and after, because outbounds are compiled only from routes and
// every referenced node keeps its ID and content.
func PlanMigration(config model.Intent) Migration {
	configured := make(map[string]bool, len(config.Subscriptions))
	for _, subscription := range config.Subscriptions {
		configured[subscription.ID] = true
	}
	migration := Migration{Config: config, Nodes: map[string][]model.Node{}}
	migration.Config.Main.SchemaVersion = model.SchemaVersion
	migration.Config.Nodes = make([]model.Node, 0, len(config.Nodes))
	for _, node := range config.Nodes {
		switch {
		case node.SourceSubscription != "" && configured[node.SourceSubscription] && storeID(node.SourceSubscription):
			migration.Nodes[node.SourceSubscription] = append(migration.Nodes[node.SourceSubscription], node)
			migration.Moved = append(migration.Moved, node.ID)
			continue
		case node.SourceSubscription != "" || node.SourceFingerprint != "" || node.PinnedStale:
			node.SourceSubscription, node.SourceFingerprint, node.PinnedStale = "", "", false
			migration.Cleared = append(migration.Cleared, node.ID)
		}
		migration.Config.Nodes = append(migration.Config.Nodes, node)
	}
	return migration
}

// WriteMigratedNodes writes the moved nodes to the store and drops the node
// lists that schema 9 status snapshots copied, so credentials are left only
// in the store.
func WriteMigratedNodes(migration Migration, store NodeStore, stateDirectory string) error {
	for _, subscription := range migration.Config.Subscriptions {
		nodes, moved := migration.Nodes[subscription.ID]
		if !moved {
			continue
		}
		if err := store.Save(subscription.ID, nodes); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(filepath.Join(stateDirectory, "subscriptions"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || entry.IsDir() || !storeID(id) {
			continue
		}
		content, err := os.ReadFile(SnapshotPath(stateDirectory, id))
		if err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(content, &fields) != nil || fields["nodes"] == nil {
			continue
		}
		var snapshot Snapshot
		if err := json.NewDecoder(bytes.NewReader(content)).Decode(&snapshot); err != nil {
			continue
		}
		snapshot.SubscriptionID = id
		if err := SaveSnapshot(stateDirectory, snapshot); err != nil {
			return err
		}
	}
	return nil
}
