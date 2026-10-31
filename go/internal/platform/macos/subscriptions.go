// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"

	model "github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/subscription"
)

type SubscriptionSnapshot = subscription.Snapshot
type SubscriptionStatus = subscription.Status
type SubscriptionUpdateError = subscription.UpdateError

func ReadSubscriptionStatus(configPath, stateDirectory string) ([]SubscriptionStatus, error) {
	saved, _, err := SavedStore(configPath, stateDirectory).LoadSaved()
	if err != nil {
		return nil, err
	}
	return subscription.Statuses(saved, stateDirectory), nil
}

// UpdateConfiguredSubscriptions writes only the node store and the status
// snapshots; config.json and its revision are never changed.
func UpdateConfiguredSubscriptions(ctx context.Context, client *http.Client, configPath, stateDirectory, id string) ([]SubscriptionSnapshot, error) {
	store := SavedStore(configPath, stateDirectory)
	saved, _, err := store.LoadSaved()
	if err != nil {
		return nil, err
	}
	return subscription.Update(ctx, client, saved, store.nodes(), stateDirectory, id, Validate)
}

func CleanSubscriptionNode(configPath, stateDirectory, id, nodeID string) error {
	store := SavedStore(configPath, stateDirectory)
	saved, _, err := store.LoadSaved()
	if err != nil {
		return err
	}
	return subscription.CleanStale(saved, store.nodes(), id, nodeID, Validate)
}

func SetSubscriptionNodeEnabled(configPath, stateDirectory, id, nodeID string, enabled bool) error {
	store := SavedStore(configPath, stateDirectory)
	saved, _, err := store.LoadSaved()
	if err != nil {
		return err
	}
	return subscription.SetNodeEnabled(saved, store.nodes(), id, nodeID, enabled, Validate)
}

func ListSubscriptionNodes(configPath, stateDirectory, id string) (subscription.NodeList, error) {
	saved, _, err := SavedStore(configPath, stateDirectory).LoadSaved()
	if err != nil {
		return subscription.NodeList{}, err
	}
	return subscription.ListNodes(saved, id)
}

// RemoveOrphanNodes drops store files of subscriptions that are no longer in
// the configuration. It runs after a successful save or Apply.
func RemoveOrphanNodes(configPath, stateDirectory string) error {
	store := SavedStore(configPath, stateDirectory)
	config, _, err := store.Load()
	if err != nil {
		return err
	}
	return store.RemoveOrphanNodes(config)
}

// NeedsMigration reports whether the configuration is still schema 9.
func NeedsMigration(configPath string) bool {
	content, err := os.ReadFile(configPath)
	return err == nil && subscription.LegacyJSON(content)
}

// Migrate moves the subscription nodes of a schema 9 config.json into the
// node store and sets schema 10. It is idempotent: every other schema is left
// untouched and reported by validation instead. The caller restores the
// configuration's admin-group permissions.
func Migrate(configPath, stateDirectory string) (bool, error) {
	store := SavedStore(configPath, stateDirectory)
	content, err := os.ReadFile(configPath)
	if err != nil {
		return false, fmt.Errorf("read macOS canonical intent for migration: %w", err)
	}
	if !subscription.LegacyJSON(content) {
		return false, nil
	}
	config, err := model.DecodeJSON(bytes.NewReader(content))
	if err != nil {
		return false, err
	}
	migration := subscription.PlanMigration(config)
	if err := subscription.WriteMigratedNodes(migration, store.nodes(), stateDirectory); err != nil {
		return false, err
	}
	var encoded bytes.Buffer
	if err := model.EncodeJSON(&encoded, migration.Config); err != nil {
		return false, err
	}
	if err := atomicWrite(configPath, encoded.Bytes()); err != nil {
		return false, fmt.Errorf("write migrated macOS canonical intent: %w", err)
	}
	return true, nil
}
