// SPDX-License-Identifier: GPL-3.0-or-later

package subscription

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

const NodeStoreSchemaVersion = 1

// NodeStore keeps subscription nodes outside the Saved configuration, one
// private file per subscription in a persistent platform directory. The Saved
// configuration holds only manual nodes, subscription settings and the rest of
// the user's choices; Attach joins the two into the complete Saved Intent.
type NodeStore struct {
	Directory string
}

// NodeDirectory is the node store inside a persistent platform state
// directory (Linux and macOS). OpenWrt keeps it under /etc instead because its
// state directory is in RAM.
func NodeDirectory(stateDirectory string) string {
	return filepath.Join(stateDirectory, "subscription-nodes")
}

// errNoNodeDirectory keeps a store without a directory from reading or
// deleting files relative to the working directory.
var errNoNodeDirectory = errors.New("subscription node store directory is not configured")

type nodeFile struct {
	SchemaVersion  int          `json:"schema_version"`
	SubscriptionID string       `json:"subscription_id"`
	Nodes          []model.Node `json:"nodes"`
}

func (store NodeStore) Path(id string) string {
	return filepath.Join(store.Directory, id+".json")
}

// Load returns the stored nodes of one subscription. A subscription that was
// never fetched has no file and no nodes.
func (store NodeStore) Load(id string) ([]model.Node, error) {
	if store.Directory == "" {
		return nil, errNoNodeDirectory
	}
	if !storeID(id) {
		return nil, fmt.Errorf("subscription ID %q cannot name a node store file", id)
	}
	content, err := os.ReadFile(store.Path(id))
	if errors.Is(err, fs.ErrNotExist) {
		return []model.Node{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read subscription %s nodes: %w", id, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var file nodeFile
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("decode subscription %s nodes: %w", id, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("decode subscription %s nodes: trailing value", id)
	}
	if file.SchemaVersion != NodeStoreSchemaVersion || file.SubscriptionID != id {
		return nil, fmt.Errorf("subscription %s node store has schema %d for %q", id, file.SchemaVersion, file.SubscriptionID)
	}
	for _, node := range file.Nodes {
		if node.SourceSubscription != id {
			return nil, fmt.Errorf("subscription %s node store contains node %q of %q", id, node.ID, node.SourceSubscription)
		}
	}
	if file.Nodes == nil {
		file.Nodes = []model.Node{}
	}
	return file.Nodes, nil
}

// Save replaces the stored nodes of one subscription with an atomic 0600
// write. Unchanged content is not rewritten, so periodic refreshes do not
// wear router flash.
func (store NodeStore) Save(id string, nodes []model.Node) error {
	if store.Directory == "" {
		return errNoNodeDirectory
	}
	if !storeID(id) {
		return fmt.Errorf("subscription ID %q cannot name a node store file", id)
	}
	for _, node := range nodes {
		if node.SourceSubscription != id {
			return fmt.Errorf("node %q does not belong to subscription %s", node.ID, id)
		}
	}
	if nodes == nil {
		nodes = []model.Node{}
	}
	encoded, err := json.MarshalIndent(nodeFile{SchemaVersion: NodeStoreSchemaVersion, SubscriptionID: id, Nodes: nodes}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode subscription %s nodes: %w", id, err)
	}
	encoded = append(encoded, '\n')
	if current, err := os.ReadFile(store.Path(id)); err == nil && bytes.Equal(current, encoded) {
		return nil
	}
	if err := writeFileAtomic(store.Path(id), encoded); err != nil {
		return fmt.Errorf("save subscription %s nodes: %w", id, err)
	}
	return nil
}

// RemoveOrphans deletes the node files of subscriptions that are no longer in
// the Saved configuration. Orphans are never attached, so this only reclaims
// space; it runs after a successful save, Apply or subscription update.
func (store NodeStore) RemoveOrphans(configured []model.Subscription) error {
	if store.Directory == "" {
		return errNoNodeDirectory
	}
	entries, err := os.ReadDir(store.Directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list subscription node store: %w", err)
	}
	keep := make(map[string]bool, len(configured))
	for _, subscription := range configured {
		keep[subscription.ID] = true
	}
	var failures []error
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || entry.IsDir() || !storeID(id) || keep[id] {
			continue
		}
		if err := os.Remove(filepath.Join(store.Directory, entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// ConfigError reports Saved configuration nodes that carry subscription
// source fields. Since schema 10 those nodes belong only to the node store.
type ConfigError struct{ Validation model.Validation }

func (err ConfigError) Error() string {
	return fmt.Sprintf("configuration contains %d subscription node field(s); subscription nodes belong to the subscription node store", len(err.Validation.Errors))
}

// Attach returns the complete Saved Intent: the Saved configuration plus the
// stored nodes of every configured subscription, enabled or not, after the
// manual nodes and in subscription order. Store files of subscriptions that are
// no longer configured are ignored. The configuration itself must not contain
// subscription nodes (SUBSCRIPTION_NODE_IN_CONFIG); that check runs before the
// store nodes are attached, so attached nodes keep their source fields.
// Validation, compile, probes, status and pending Apply all use the result.
func Attach(config model.Intent, store NodeStore) (model.Intent, error) {
	if issues := model.ConfigNodeIssues(config); len(issues) > 0 {
		return model.Intent{}, ConfigError{Validation: model.Validation{
			Errors: issues, Warnings: []model.Issue{}, WarningGroups: []model.WarningGroup{},
		}}
	}
	complete := config
	seen := make(map[string]bool, len(config.Subscriptions))
	for _, configured := range config.Subscriptions {
		// Invalid or repeated IDs are reported by validation, not here.
		if seen[configured.ID] || !storeID(configured.ID) {
			continue
		}
		seen[configured.ID] = true
		nodes, err := store.Load(configured.ID)
		if err != nil {
			return model.Intent{}, err
		}
		if len(nodes) == 0 {
			continue
		}
		if len(complete.Nodes) == len(config.Nodes) {
			complete.Nodes = append(make([]model.Node, 0, len(config.Nodes)+len(nodes)), config.Nodes...)
		}
		complete.Nodes = append(complete.Nodes, nodes...)
	}
	return complete, nil
}

// SubscriptionNodes returns the nodes of one subscription in a complete Saved
// Intent, in stored order.
func SubscriptionNodes(saved model.Intent, id string) []model.Node {
	nodes := make([]model.Node, 0)
	for _, node := range saved.Nodes {
		if node.SourceSubscription == id {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func SnapshotPath(stateDirectory, id string) string {
	return filepath.Join(stateDirectory, "subscriptions", id+".json")
}

// ReadSnapshot reads the status snapshot of one subscription. Snapshots
// written before schema 10 may still contain a nodes list; it is ignored and
// dropped by the next write.
func ReadSnapshot(stateDirectory, id string) (Snapshot, error) {
	content, err := os.ReadFile(SnapshotPath(stateDirectory, id))
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(content, &snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func SaveSnapshot(stateDirectory string, snapshot Snapshot) error {
	encoded, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode subscription snapshot: %w", err)
	}
	if err := writeFileAtomic(SnapshotPath(stateDirectory, snapshot.SubscriptionID), append(encoded, '\n')); err != nil {
		return fmt.Errorf("save subscription snapshot: %w", err)
	}
	return nil
}

func storeID(id string) bool {
	return id != "" && !strings.HasPrefix(id, ".") && filepath.Base(id) == id && !strings.ContainsAny(id, `/\`)
}

func writeFileAtomic(path string, content []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".steer-write-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	parent, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}
