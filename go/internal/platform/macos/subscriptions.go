// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	model "github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/subscription"
)

type SubscriptionSnapshot = subscription.Snapshot
type SubscriptionStatus = subscription.Status
type SubscriptionUpdateError = subscription.UpdateError

func existingNodes(nodes []model.Node, subscriptionID string) []model.Node {
	result := make([]model.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.SourceSubscription == subscriptionID {
			result = append(result, node)
		}
	}
	return result
}

func subscriptionSnapshotPath(stateDirectory, id string) string {
	return filepath.Join(stateDirectory, "subscriptions", id+".json")
}

func ReadSubscriptionStatus(configPath, stateDirectory string) ([]SubscriptionStatus, error) {
	value, _, err := (IntentStore{Paths: Paths{ConfigPath: configPath}}).Load()
	if err != nil {
		return nil, err
	}
	statuses := make([]SubscriptionStatus, 0, len(value.Subscriptions))
	for _, configured := range value.Subscriptions {
		snapshot, readErr := readSubscriptionSnapshot(subscriptionSnapshotPath(stateDirectory, configured.ID))
		var saved *SubscriptionSnapshot
		if readErr == nil {
			saved = &snapshot
		}
		statuses = append(statuses, subscription.BuildStatus(configured, value.Nodes, value.Routes, saved, readErr))
	}
	return statuses, nil
}

func UpdateConfiguredSubscriptions(ctx context.Context, client *http.Client, configPath, stateDirectory, id string) ([]SubscriptionSnapshot, error) {
	store := IntentStore{Paths: Paths{ConfigPath: configPath}}
	value, revision, err := store.Load()
	if err != nil {
		return nil, err
	}
	scheduleTime := time.Now()
	var snapshots []SubscriptionSnapshot
	var failures []error
	for _, configured := range value.Subscriptions {
		if !configured.Enabled || (id != "" && configured.ID != id) {
			continue
		}
		var previous *SubscriptionSnapshot
		if saved, readErr := readSubscriptionSnapshot(subscriptionSnapshotPath(stateDirectory, configured.ID)); readErr == nil {
			previous = &saved
		}
		if id == "" {
			var fetchedAt time.Time
			if previous != nil {
				fetchedAt = previous.FetchedAt
			}
			due, scheduleErr := subscription.AutomaticUpdateDue(configured.UpdateInterval, fetchedAt, scheduleTime)
			if scheduleErr != nil {
				return nil, fmt.Errorf("subscription %s has invalid update interval: %w", configured.ID, scheduleErr)
			}
			if !due {
				continue
			}
		}
		fetched, err := subscription.Fetch(ctx, client, configured)
		if err != nil {
			failure := subscription.FailedSnapshot(configured, previous, err, time.Now())
			if saveErr := saveSubscriptionSnapshot(stateDirectory, failure); saveErr != nil {
				err = saveErr
			}
			// One broken subscription must not hold back the others.
			failures = append(failures, subscription.NewUpdateError(configured.ID, err))
			continue
		}
		old := existingNodes(value.Nodes, configured.ID)
		merged := subscription.Merge(configured.ID, old, fetched.Nodes, value.Routes)
		value.Nodes = subscription.Replace(value.Nodes, configured.ID, merged)
		snapshots = append(snapshots, subscription.SuccessfulSnapshot(configured, previous, old, merged, fetched, time.Now()))
	}
	if id != "" && len(snapshots) == 0 && len(failures) == 0 {
		return nil, fmt.Errorf("enabled subscription %q was not found", id)
	}
	if len(snapshots) == 0 {
		return nil, errors.Join(failures...)
	}
	if _, err := store.Save(value, revision); err != nil {
		return nil, err
	}
	for _, snapshot := range snapshots {
		if err := saveSubscriptionSnapshot(stateDirectory, snapshot); err != nil {
			return nil, err
		}
	}
	return snapshots, errors.Join(failures...)
}

func CleanSubscriptionNode(configPath, stateDirectory, id, nodeID string) (SubscriptionSnapshot, error) {
	path := subscriptionSnapshotPath(stateDirectory, id)
	snapshot, err := readSubscriptionSnapshot(path)
	if err != nil {
		return SubscriptionSnapshot{}, err
	}
	store := IntentStore{Paths: Paths{ConfigPath: configPath}}
	value, revision, err := store.Load()
	if err != nil {
		return SubscriptionSnapshot{}, err
	}
	filtered := make([]model.Node, 0, len(value.Nodes))
	found := false
	for _, node := range value.Nodes {
		if node.ID == nodeID && node.SourceSubscription == id {
			if !node.PinnedStale {
				return SubscriptionSnapshot{}, fmt.Errorf("subscription node %q is current and cannot be removed", nodeID)
			}
			found = true
			continue
		}
		filtered = append(filtered, node)
	}
	if !found {
		return SubscriptionSnapshot{}, fmt.Errorf("stale subscription node %q was not found", nodeID)
	}
	value.Nodes = filtered
	validation := Validate(value)
	if !validation.OK {
		for _, issue := range validation.Errors {
			if issue.Code == "DANGLING_NODE" {
				return SubscriptionSnapshot{}, fmt.Errorf("NODE_STILL_REFERENCED: route %q still references subscription node %q", issue.ObjectID, nodeID)
			}
		}
		return SubscriptionSnapshot{}, ValidationError{Validation: validation}
	}
	if _, err := store.Save(value, revision); err != nil {
		return SubscriptionSnapshot{}, err
	}
	snapshotNodes := make([]model.Node, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		if node.ID == nodeID && node.SourceSubscription == id {
			continue
		}
		snapshotNodes = append(snapshotNodes, node)
	}
	snapshot.Nodes = snapshotNodes
	if err := saveSubscriptionSnapshot(stateDirectory, snapshot); err != nil {
		return SubscriptionSnapshot{}, err
	}
	return snapshot, nil
}

func readSubscriptionSnapshot(path string) (SubscriptionSnapshot, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return SubscriptionSnapshot{}, err
	}
	var snapshot SubscriptionSnapshot
	if err := json.Unmarshal(content, &snapshot); err != nil {
		return SubscriptionSnapshot{}, err
	}
	return snapshot, nil
}

func saveSubscriptionSnapshot(stateDirectory string, snapshot SubscriptionSnapshot) error {
	encoded, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(subscriptionSnapshotPath(stateDirectory, snapshot.SubscriptionID), append(encoded, '\n'))
}
