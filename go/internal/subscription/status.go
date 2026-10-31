// SPDX-License-Identifier: GPL-3.0-or-later

package subscription

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

// Failure is deliberately safe for persistent status and user interfaces.
// Summary never contains a configured subscription URL or downloaded content.
type Failure struct {
	At      *time.Time `json:"at"`
	Summary string     `json:"summary"`
}

type Reference struct {
	ObjectType string `json:"object_type"`
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
}

type StaleNode struct {
	ID           string      `json:"id"`
	Name         string      `json:"name,omitempty"`
	ReferencedBy []Reference `json:"referenced_by"`
}

// Status is the single subscription UI contract shared by every platform.
// LastSuccess and LastFailure are independent so a failed refresh never
// destroys the last known-good inventory facts.
type Status struct {
	ID             string          `json:"id"`
	Name           string          `json:"name,omitempty"`
	URL            string          `json:"url"`
	Enabled        bool            `json:"enabled"`
	UpdateInterval string          `json:"update_interval,omitempty"`
	NeverFetched   bool            `json:"never_fetched"`
	LastSuccess    *time.Time      `json:"last_success"`
	LastFailure    *Failure        `json:"last_failure"`
	NodeCount      int             `json:"node_count"`
	Current        int             `json:"current"`
	Added          int             `json:"added"`
	Skipped        int             `json:"skipped"`
	SkippedReasons []SkippedReason `json:"skipped_reasons,omitempty"`
	Stale          []StaleNode     `json:"stale"`
}

type UpdateError struct {
	SubscriptionID string
	Summary        string
}

func (err UpdateError) Error() string {
	return fmt.Sprintf("update subscription %s: %s", err.SubscriptionID, err.Summary)
}

func NewUpdateError(subscriptionID string, err error) error {
	return UpdateError{SubscriptionID: subscriptionID, Summary: SafeFailureSummary(err)}
}

// Statuses builds the status of every configured subscription from the
// complete Saved Intent and the status snapshots.
func Statuses(saved model.Intent, stateDirectory string) []Status {
	statuses := make([]Status, 0, len(saved.Subscriptions))
	for _, configured := range saved.Subscriptions {
		snapshot, err := ReadSnapshot(stateDirectory, configured.ID)
		var previous *Snapshot
		if err == nil {
			previous = &snapshot
		}
		statuses = append(statuses, BuildStatus(configured, saved.Nodes, saved.Routes, previous, err))
	}
	return statuses
}

func BuildStatus(configured model.Subscription, nodes []model.Node, routes []model.Route, snapshot *Snapshot, stateErr error) Status {
	status := Status{
		ID: configured.ID, Name: configured.Name, URL: configured.URL,
		Enabled: configured.Enabled, UpdateInterval: configured.UpdateInterval,
		Stale: []StaleNode{},
	}
	if snapshot != nil {
		if !snapshot.FetchedAt.IsZero() {
			lastSuccess := snapshot.FetchedAt.UTC()
			status.LastSuccess = &lastSuccess
		}
		status.LastFailure = snapshot.LastFailure
		status.Added = snapshot.Added
		status.Skipped = snapshot.Skipped
		status.SkippedReasons = append([]SkippedReason(nil), snapshot.SkippedReasons...)
	}
	if stateErr != nil && !errors.Is(stateErr, fs.ErrNotExist) && status.LastFailure == nil {
		status.LastFailure = &Failure{Summary: "saved subscription state is unavailable"}
	}

	for _, node := range nodes {
		if node.SourceSubscription != configured.ID {
			continue
		}
		status.NodeCount++
		if !node.PinnedStale {
			continue
		}
		stale := StaleNode{ID: node.ID, Name: node.Name, ReferencedBy: []Reference{}}
		for _, route := range routes {
			if route.Node == node.ID {
				stale.ReferencedBy = append(stale.ReferencedBy, Reference{ObjectType: "route", ID: route.ID, Name: route.Name})
			}
		}
		sort.Slice(stale.ReferencedBy, func(left, right int) bool {
			return stale.ReferencedBy[left].ID < stale.ReferencedBy[right].ID
		})
		status.Stale = append(status.Stale, stale)
	}
	sort.Slice(status.Stale, func(left, right int) bool { return status.Stale[left].ID < status.Stale[right].ID })
	status.Current = status.NodeCount - len(status.Stale)
	status.NeverFetched = status.LastSuccess == nil
	return status
}

func SuccessfulSnapshot(configured model.Subscription, previous *Snapshot, old, merged []model.Node, parsed ParseResult, now time.Time) Snapshot {
	snapshot := Snapshot{
		SubscriptionID: configured.ID,
		URL:            configured.URL,
		FetchedAt:      now.UTC(),
		Skipped:        parsed.Skipped,
		SkippedReasons: append([]SkippedReason(nil), parsed.SkippedReasons...),
		Added:          addedNodeCount(old, merged),
	}
	if previous != nil {
		snapshot.LastFailure = previous.LastFailure
	}
	return snapshot
}

func FailedSnapshot(configured model.Subscription, previous *Snapshot, err error, now time.Time) Snapshot {
	snapshot := Snapshot{SubscriptionID: configured.ID, URL: configured.URL}
	if previous != nil {
		snapshot = *previous
		snapshot.SubscriptionID = configured.ID
		snapshot.URL = configured.URL
	}
	at := now.UTC()
	snapshot.LastFailure = &Failure{At: &at, Summary: SafeFailureSummary(err)}
	return snapshot
}

func SafeFailureSummary(err error) string {
	if err == nil {
		return "subscription update failed"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "subscription request timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "subscription request was cancelled"
	}
	message := err.Error()
	if marker := "subscription returned HTTP "; strings.Contains(message, marker) {
		remainder := message[strings.Index(message, marker)+len(marker):]
		fields := strings.Fields(remainder)
		if len(fields) > 0 {
			if code, parseErr := strconv.Atoi(fields[0]); parseErr == nil && code >= 100 && code <= 599 {
				return fmt.Sprintf("subscription server returned HTTP %d", code)
			}
		}
	}
	for fragment, summary := range map[string]string{
		"absolute HTTP or HTTPS URL":      "subscription URL is invalid",
		"exceeds the 16 MiB size limit":   "subscription exceeds the 16 MiB size limit",
		"contains no valid nodes":         "subscription contains no valid nodes",
		"invalid update interval":         "subscription update interval is invalid",
		"invalid candidate":               "downloaded nodes conflict with the saved configuration",
		"configuration revision conflict": "saved configuration changed during the update",
	} {
		if strings.Contains(message, fragment) {
			return summary
		}
	}
	if strings.Contains(message, "download subscription") || strings.Contains(message, "read subscription") {
		return "subscription download failed"
	}
	return "subscription update failed"
}

func addedNodeCount(old, merged []model.Node) int {
	oldIDs := make(map[string]bool, len(old))
	for _, node := range old {
		oldIDs[node.ID] = true
	}
	added := 0
	for _, node := range merged {
		if !node.PinnedStale && !oldIDs[node.ID] {
			added++
		}
	}
	return added
}
