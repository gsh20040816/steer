// SPDX-License-Identifier: GPL-3.0-or-later

package subscription

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

// Snapshot is the persistent fetch status of one subscription. It never holds
// nodes: they live only in the node store, so credentials are not copied.
type Snapshot struct {
	SubscriptionID string          `json:"subscription_id"`
	URL            string          `json:"url"`
	FetchedAt      time.Time       `json:"fetched_at"`
	Skipped        int             `json:"skipped"`
	SkippedReasons []SkippedReason `json:"skipped_reasons,omitempty"`
	Added          int             `json:"added,omitempty"`
	LastFailure    *Failure        `json:"last_failure,omitempty"`
}

// AutomaticUpdateDue applies the shared scheduling semantics used by every
// platform dispatcher. An empty interval is manual-only, while a configured
// interval is due on the first fetch and at the exact interval boundary.
func AutomaticUpdateDue(updateInterval string, fetchedAt, now time.Time) (bool, error) {
	if updateInterval == "" {
		return false, nil
	}
	interval, err := time.ParseDuration(updateInterval)
	if err != nil {
		return false, err
	}
	if interval <= 0 {
		return false, fmt.Errorf("duration must be positive")
	}
	return fetchedAt.IsZero() || !now.Before(fetchedAt.Add(interval)), nil
}

const maxSubscriptionBytes = 16 << 20

func Fetch(ctx context.Context, client *http.Client, configured model.Subscription) (ParseResult, error) {
	parsedURL, parseErr := url.Parse(configured.URL)
	if parseErr != nil || (!strings.EqualFold(parsedURL.Scheme, "http") && !strings.EqualFold(parsedURL.Scheme, "https")) || parsedURL.Host == "" {
		return ParseResult{}, fmt.Errorf("subscription URL must be an absolute HTTP or HTTPS URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, configured.URL, nil)
	if err != nil {
		return ParseResult{}, fmt.Errorf("create subscription request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return ParseResult{}, fmt.Errorf("download subscription: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ParseResult{}, fmt.Errorf("subscription returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSubscriptionBytes+1))
	if err != nil {
		return ParseResult{}, fmt.Errorf("read subscription: %w", err)
	}
	if len(body) > maxSubscriptionBytes {
		return ParseResult{}, fmt.Errorf("subscription exceeds the 16 MiB size limit")
	}
	parsed := ParseList(string(body))
	if len(parsed.Nodes) == 0 {
		return ParseResult{}, fmt.Errorf("subscription contains no valid nodes")
	}
	return parsed, nil
}

// Update refreshes the enabled subscriptions of the complete Saved Intent that
// are due, or only the named one. Nodes are merged with the stored nodes (route
// references come from the Saved configuration) and the complete Intent with
// every merged subscription must pass the platform validation. Only the node
// store and status snapshots are written: the Saved configuration is never
// written and nothing is applied. One failing subscription does not hold back
// the others; their failures are joined into the returned error.
func Update(ctx context.Context, client *http.Client, saved model.Intent, store NodeStore, stateDirectory, id string, validate func(model.Intent) model.Validation) ([]Snapshot, error) {
	scheduleTime := time.Now()
	var result []Snapshot
	var failures []error
	merged := map[string][]model.Node{}
	candidate := saved
	for _, configured := range saved.Subscriptions {
		if !configured.Enabled || (id != "" && configured.ID != id) {
			continue
		}
		var previous *Snapshot
		if snapshot, err := ReadSnapshot(stateDirectory, configured.ID); err == nil {
			previous = &snapshot
		}
		if id == "" {
			var fetchedAt time.Time
			if previous != nil {
				fetchedAt = previous.FetchedAt
			}
			due, err := AutomaticUpdateDue(configured.UpdateInterval, fetchedAt, scheduleTime)
			if err != nil {
				return nil, fmt.Errorf("subscription %s has invalid update interval: %w", configured.ID, err)
			}
			if !due {
				continue
			}
		}
		fetched, err := Fetch(ctx, client, configured)
		if err != nil {
			if saveErr := SaveSnapshot(stateDirectory, FailedSnapshot(configured, previous, err, time.Now())); saveErr != nil {
				err = saveErr
			}
			failures = append(failures, NewUpdateError(configured.ID, err))
			continue
		}
		old := SubscriptionNodes(saved, configured.ID)
		merged[configured.ID] = Merge(configured.ID, old, fetched.Nodes, saved.Routes)
		candidate.Nodes = Replace(candidate.Nodes, configured.ID, merged[configured.ID])
		result = append(result, SuccessfulSnapshot(configured, previous, old, merged[configured.ID], fetched, time.Now()))
	}
	if id != "" && len(result) == 0 && len(failures) == 0 {
		return nil, fmt.Errorf("enabled subscription %q was not found", id)
	}
	if len(result) > 0 {
		if validation := validate(candidate); !validation.OK {
			issue := validation.Errors[0]
			return nil, fmt.Errorf("subscription update produced invalid candidate: %s %q option %q: %s", issue.ObjectType, issue.ObjectID, issue.Option, issue.Message)
		}
		for _, snapshot := range result {
			if err := store.Save(snapshot.SubscriptionID, merged[snapshot.SubscriptionID]); err != nil {
				return nil, err
			}
			if err := SaveSnapshot(stateDirectory, snapshot); err != nil {
				return nil, err
			}
		}
	}
	// Orphans are harmless (never attached); a failed removal is retried by
	// the next update, save or Apply.
	_ = store.RemoveOrphans(saved.Subscriptions)
	return result, errors.Join(failures...)
}

func Merge(subscriptionID string, old, fresh []model.Node, routes []model.Route) []model.Node {
	oldByFingerprint := make(map[string]model.Node, len(old))
	for _, node := range old {
		oldByFingerprint[Fingerprint(node)] = node
	}
	referencedNodeIDs := make(map[string]bool, len(routes))
	for _, route := range routes {
		if route.Node != "" {
			referencedNodeIDs[route.Node] = true
		}
	}
	seen := make(map[string]bool, len(fresh))
	merged := make([]model.Node, 0, len(old)+len(fresh))
	for _, node := range fresh {
		fingerprint := Fingerprint(node)
		if seen[fingerprint] {
			continue
		}
		previous, exists := oldByFingerprint[fingerprint]
		enabled := true
		if exists {
			node.ID = previous.ID
			enabled = previous.Enabled
		}
		if node.ID == "" {
			node.ID = stableNodeID(subscriptionID, fingerprint)
		}
		node.Enabled, node.SourceSubscription, node.SourceFingerprint, node.PinnedStale = enabled, subscriptionID, fingerprint, false
		merged = append(merged, node)
		seen[fingerprint] = true
	}
	for _, node := range old {
		fingerprint := Fingerprint(node)
		if seen[fingerprint] || !referencedNodeIDs[node.ID] {
			continue
		}
		node.SourceSubscription, node.SourceFingerprint, node.PinnedStale = subscriptionID, fingerprint, true
		merged = append(merged, node)
	}
	sort.SliceStable(merged, func(left, right int) bool { return merged[left].ID < merged[right].ID })
	return merged
}

func Replace(existing []model.Node, subscriptionID string, replacement []model.Node) []model.Node {
	result := make([]model.Node, 0, len(existing)+len(replacement))
	for _, node := range existing {
		if node.SourceSubscription != subscriptionID {
			result = append(result, node)
		}
	}
	return append(result, replacement...)
}

func stableNodeID(subscriptionID, fingerprint string) string {
	prefix := subscriptionID
	if len(prefix) > 19 {
		prefix = prefix[:19]
	}
	return prefix + "_" + fingerprint[:12]
}
