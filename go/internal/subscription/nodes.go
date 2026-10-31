// SPDX-License-Identifier: GPL-3.0-or-later

package subscription

import (
	"encoding/json"
	"errors"
	"fmt"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

// Error codes shared by every platform interface for subscription node
// operations. Validation failures also carry the complete validation.
const (
	ErrorSubscriptionNotFound = "SUBSCRIPTION_NOT_FOUND"
	ErrorNodeNotFound         = "SUBSCRIPTION_NODE_NOT_FOUND"
	ErrorNodeStillReferenced  = "NODE_STILL_REFERENCED"
	ErrorNodeCurrent          = "SUBSCRIPTION_NODE_CURRENT"
	ErrorValidationFailed     = "VALIDATION_FAILED"
	ErrorNodeStoreFailed      = "SUBSCRIPTION_NODE_STORE_FAILED"
)

// OperationError is a rejected subscription node operation. Code is stable
// and locale-neutral; Error keeps the "CODE: detail" text older clients show.
type OperationError struct {
	Code       string
	Message    string
	Validation *model.Validation
}

func (err OperationError) Error() string { return err.Code + ": " + err.Message }

// ErrorPayload is the error body of subscription interfaces on every platform.
type ErrorPayload struct {
	OK         bool              `json:"ok"`
	ErrorCode  string            `json:"error_code,omitempty"`
	Error      string            `json:"error"`
	Validation *model.Validation `json:"validation,omitempty"`
}

// ErrorPayloadOf maps an operation error to the shared payload. A
// configuration that still contains subscription nodes is a validation failure.
func ErrorPayloadOf(err error) ErrorPayload {
	failure := ErrorPayload{Error: err.Error()}
	var operation OperationError
	var config ConfigError
	switch {
	case errors.As(err, &operation):
		failure.ErrorCode, failure.Validation = operation.Code, operation.Validation
	case errors.As(err, &config):
		failure.ErrorCode, failure.Validation = ErrorValidationFailed, &config.Validation
	}
	return failure
}

// ManualNode is the manual node produced by "Copy to my nodes". It has no ID:
// each frontend assigns one with its own ID rule when adding it to the draft.
type ManualNode struct{ model.Node }

func (node ManualNode) MarshalJSON() ([]byte, error) {
	encoded, err := json.Marshal(node.Node)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	delete(fields, "id")
	return json.Marshal(fields)
}

// ListedNode is one stored subscription node as returned by the read
// interface, together with its shared manual copy.
type ListedNode struct {
	model.Node
	ManualCopy ManualNode `json:"manual_copy"`
}

type SubscriptionNodeList struct {
	ID    string       `json:"id"`
	Nodes []ListedNode `json:"nodes"`
}

// NodeList is the read interface payload on every platform:
// {"subscriptions":[{"id":"…","nodes":[{…node…,"manual_copy":{…}}]}]}.
type NodeList struct {
	Subscriptions []SubscriptionNodeList `json:"subscriptions"`
}

// CopyAsManual is the only "Copy to my nodes" rule: every protocol field and
// credential is kept, the name defaults to the original name, subscription
// source fields are removed and the copy is enabled. The ID is left empty for
// the frontend. The original subscription node and routes are not changed.
func CopyAsManual(node model.Node) model.Node {
	copy := node
	copy.ID = ""
	copy.Enabled = true
	copy.SourceSubscription, copy.SourceFingerprint, copy.PinnedStale = "", "", false
	copy.HostKeyAlgorithms = append([]string(nil), node.HostKeyAlgorithms...)
	copy.ServerPorts = append([]string(nil), node.ServerPorts...)
	copy.ExtraArgs = append([]string(nil), node.ExtraArgs...)
	copy.ALPN = append([]string(nil), node.ALPN...)
	return copy
}

// ListNodes returns the stored nodes of the subscriptions in the Saved
// configuration, in configuration order. A non-empty id selects one.
func ListNodes(saved model.Intent, id string) (NodeList, error) {
	list := NodeList{Subscriptions: []SubscriptionNodeList{}}
	for _, configured := range saved.Subscriptions {
		if id != "" && configured.ID != id {
			continue
		}
		entry := SubscriptionNodeList{ID: configured.ID, Nodes: []ListedNode{}}
		for _, node := range SubscriptionNodes(saved, configured.ID) {
			entry.Nodes = append(entry.Nodes, ListedNode{Node: node, ManualCopy: ManualNode{CopyAsManual(node)}})
		}
		list.Subscriptions = append(list.Subscriptions, entry)
	}
	if id != "" && len(list.Subscriptions) == 0 {
		return NodeList{}, OperationError{Code: ErrorSubscriptionNotFound, Message: fmt.Sprintf("subscription %q is not in the Saved configuration", id)}
	}
	return list, nil
}

// SetNodeEnabled enables or disables one stored subscription node. The
// complete Saved Intent with the change must validate first, so disabling a
// node that an enabled route still uses is rejected. Only the node store is
// written.
func SetNodeEnabled(saved model.Intent, store NodeStore, id, nodeID string, enabled bool, validate func(model.Intent) model.Validation) error {
	nodes, index, err := findStoredNode(saved, id, nodeID)
	if err != nil {
		return err
	}
	if nodes[index].Enabled == enabled {
		return nil
	}
	nodes[index].Enabled = enabled
	candidate := saved
	candidate.Nodes = Replace(saved.Nodes, id, nodes)
	if err := checkCandidate(candidate, nodeID, "DISABLED_NODE", validate); err != nil {
		return err
	}
	return saveNodes(store, id, nodes)
}

// CleanStale removes one pinned stale node that no route references. Current
// nodes are never removed by cleanup, and routes are never changed.
func CleanStale(saved model.Intent, store NodeStore, id, nodeID string, validate func(model.Intent) model.Validation) error {
	nodes, index, err := findStoredNode(saved, id, nodeID)
	if err != nil {
		return err
	}
	if !nodes[index].PinnedStale {
		return OperationError{Code: ErrorNodeCurrent, Message: fmt.Sprintf("subscription node %q is current and cannot be removed by cleanup", nodeID)}
	}
	remaining := append(nodes[:index:index], nodes[index+1:]...)
	candidate := saved
	candidate.Nodes = Replace(saved.Nodes, id, remaining)
	if err := checkCandidate(candidate, nodeID, "DANGLING_NODE", validate); err != nil {
		return err
	}
	return saveNodes(store, id, remaining)
}

func findStoredNode(saved model.Intent, id, nodeID string) ([]model.Node, int, error) {
	configured := false
	for _, subscription := range saved.Subscriptions {
		configured = configured || subscription.ID == id
	}
	if !configured {
		return nil, 0, OperationError{Code: ErrorSubscriptionNotFound, Message: fmt.Sprintf("subscription %q is not in the Saved configuration", id)}
	}
	nodes := SubscriptionNodes(saved, id)
	for index := range nodes {
		if nodes[index].ID == nodeID {
			return nodes, index, nil
		}
	}
	return nil, 0, OperationError{Code: ErrorNodeNotFound, Message: fmt.Sprintf("subscription %q has no node %q", id, nodeID)}
}

// checkCandidate maps a route that still needs the node to the shared
// NODE_STILL_REFERENCED code and every other failure to VALIDATION_FAILED.
func checkCandidate(candidate model.Intent, nodeID, referenceCode string, validate func(model.Intent) model.Validation) error {
	validation := validate(candidate)
	if validation.OK {
		return nil
	}
	for _, issue := range validation.Errors {
		if issue.Code == referenceCode && issue.ObjectType == "route" && issue.Option == "node" {
			return OperationError{
				Code:       ErrorNodeStillReferenced,
				Message:    fmt.Sprintf("route %q still references subscription node %q", issue.ObjectID, nodeID),
				Validation: &validation,
			}
		}
	}
	issue := validation.Errors[0]
	return OperationError{
		Code:       ErrorValidationFailed,
		Message:    fmt.Sprintf("%s %q option %q: %s", issue.ObjectType, issue.ObjectID, issue.Option, issue.Message),
		Validation: &validation,
	}
}

func saveNodes(store NodeStore, id string, nodes []model.Node) error {
	if err := store.Save(id, nodes); err != nil {
		return OperationError{Code: ErrorNodeStoreFailed, Message: err.Error()}
	}
	return nil
}
