// SPDX-License-Identifier: GPL-3.0-or-later
package openwrt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	model "github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/platform/openwrt/uci"
	"github.com/gsh20040816/steer/go/internal/subscription"
)

type SubscriptionSnapshot = subscription.Snapshot
type SubscriptionStatus = subscription.Status
type SubscriptionUpdateError = subscription.UpdateError

func ReadSubscriptionStatus(configPath, stateDirectory string) ([]SubscriptionStatus, error) {
	config, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read UCI for subscription status: %w", err)
	}
	intent, err := DecodeBytes(config)
	if err != nil {
		return nil, err
	}
	statuses := make([]SubscriptionStatus, 0, len(intent.Subscriptions))
	for _, configured := range intent.Subscriptions {
		snapshot, readErr := readSubscriptionSnapshot(SubscriptionSnapshotPath(stateDirectory, configured.ID))
		var saved *SubscriptionSnapshot
		if readErr == nil {
			saved = &snapshot
		}
		statuses = append(statuses, subscription.BuildStatus(configured, intent.Nodes, intent.Routes, saved, readErr))
	}
	return statuses, nil
}

func SubscriptionSnapshotPath(stateDirectory, id string) string {
	return filepath.Join(stateDirectory, "subscriptions", id+".json")
}

func UpdateConfiguredSubscriptions(ctx context.Context, client *http.Client, configPath, stateDirectory string, id string) ([]SubscriptionSnapshot, error) {
	return UpdateConfiguredSubscriptionsWithWriter(ctx, client, configPath, stateDirectory, id, SystemUCIWriter(configPath))
}

// UCIWriter applies one complete batch and must include the commit operation.
// Subscription refresh intentionally uses this narrow path: it changes the
// node list in UCI, but never invokes Apply or emits an ubus event.
type UCIWriter func(context.Context, string) error

type UCIStore struct {
	write UCIWriter
	batch strings.Builder
}

var _ subscription.Store = (*UCIStore)(nil)

func (store *UCIStore) ReplaceNodes(_ context.Context, subscriptionID string, existing, replacement []model.Node) error {
	appendSubscriptionBatch(&store.batch, subscriptionID, existing, replacement)
	return nil
}

func (store *UCIStore) RemoveNode(_ context.Context, subscriptionID, nodeID string) error {
	fmt.Fprintf(&store.batch, "delete steer.%s\n", nodeID)
	return nil
}

func (store *UCIStore) Commit(ctx context.Context) error {
	if store.batch.Len() == 0 {
		return nil
	}
	return store.write(ctx, store.batch.String()+"commit steer\n")
}

func SystemUCIWriter(configPath string) UCIWriter {
	configDirectory := filepath.Dir(configPath)
	return func(ctx context.Context, batch string) error {
		commandCtx, cancel := withCommandTimeout(ctx, defaultCommandTimeout)
		defer cancel()
		command := newCommandContext(commandCtx, "/sbin/uci", "-c", configDirectory, "batch")
		command.Stdin = strings.NewReader(batch)
		output, err := command.CombinedOutput()
		if err != nil {
			if commandCtx.Err() != nil {
				err = commandCtx.Err()
			}
			return fmt.Errorf("uci batch: %w: %s", err, strings.TrimSpace(string(output)))
		}
		if detail := strings.TrimSpace(string(output)); detail != "" {
			return fmt.Errorf("uci batch reported an error: %s", detail)
		}
		return nil
	}
}

func UpdateConfiguredSubscriptionsWithWriter(ctx context.Context, client *http.Client, configPath, stateDirectory string, id string, writeUCI UCIWriter) ([]SubscriptionSnapshot, error) {
	config, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read UCI for subscription update: %w", err)
	}
	intent, err := DecodeBytes(config)
	if err != nil {
		return nil, err
	}
	scheduleTime := time.Now()
	var result []SubscriptionSnapshot
	var failures []error
	store := &UCIStore{write: writeUCI}
	var changes []subscription.Change
	for _, configured := range intent.Subscriptions {
		if !configured.Enabled || (id != "" && configured.ID != id) {
			continue
		}
		var previous *SubscriptionSnapshot
		if saved, readErr := readSubscriptionSnapshot(SubscriptionSnapshotPath(stateDirectory, configured.ID)); readErr == nil {
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
		old := make([]model.Node, 0)
		for _, node := range intent.Nodes {
			if node.SourceSubscription == configured.ID {
				old = append(old, node)
			}
		}
		merged := subscription.Merge(configured.ID, old, fetched.Nodes, intent.Routes)
		existingNodes := intent.Nodes
		intent.Nodes = subscription.Replace(existingNodes, configured.ID, merged)
		changes = append(changes, subscription.Change{SubscriptionID: configured.ID, Existing: existingNodes, Replacement: merged})
		snapshot := subscription.SuccessfulSnapshot(configured, previous, old, merged, fetched, time.Now())
		result = append(result, snapshot)
	}
	if id != "" && len(result) == 0 && len(failures) == 0 {
		return nil, fmt.Errorf("enabled subscription %q was not found", id)
	}
	if len(changes) > 0 {
		validation := Validate(intent)
		if !validation.OK {
			issue := validation.Errors[0]
			return nil, fmt.Errorf("subscription update produced invalid candidate: %s %q option %q: %s", issue.ObjectType, issue.ObjectID, issue.Option, issue.Message)
		}
		if err := subscription.Persist(ctx, store, changes); err != nil {
			return nil, err
		}
		if err := store.Commit(ctx); err != nil {
			return nil, err
		}
		for _, snapshot := range result {
			if err := saveSubscriptionSnapshot(stateDirectory, snapshot); err != nil {
				return nil, err
			}
		}
	}
	return result, errors.Join(failures...)
}

func CleanSubscriptionNode(configPath, stateDirectory, id, nodeID string) (SubscriptionSnapshot, error) {
	return CleanSubscriptionNodeWithWriter(configPath, stateDirectory, id, nodeID, SystemUCIWriter(configPath))
}

func CleanSubscriptionNodeWithWriter(configPath, stateDirectory, id, nodeID string, writeUCI UCIWriter) (SubscriptionSnapshot, error) {
	if !uci.IsIdentifier(id) || !uci.IsIdentifier(nodeID) {
		return SubscriptionSnapshot{}, fmt.Errorf("subscription and node IDs must be safe UCI identifiers")
	}
	path := SubscriptionSnapshotPath(stateDirectory, id)
	snapshot, err := readSubscriptionSnapshot(path)
	if err != nil {
		return SubscriptionSnapshot{}, fmt.Errorf("read subscription snapshot: %w", err)
	}
	filtered := make([]model.Node, 0, len(snapshot.Nodes))
	var snapshotNode *model.Node
	for _, node := range snapshot.Nodes {
		if node.ID == nodeID && node.SourceSubscription == id {
			candidate := node
			snapshotNode = &candidate
			continue
		}
		filtered = append(filtered, node)
	}
	if snapshotNode == nil {
		return SubscriptionSnapshot{}, fmt.Errorf("subscription node %q was not found", nodeID)
	}
	if !snapshotNode.PinnedStale {
		return SubscriptionSnapshot{}, fmt.Errorf("subscription node %q is current and cannot be removed by cleanup", nodeID)
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		return SubscriptionSnapshot{}, fmt.Errorf("read UCI for subscription cleanup: %w", err)
	}
	intent, err := DecodeBytes(config)
	if err != nil {
		return SubscriptionSnapshot{}, err
	}
	candidate := intent
	candidate.Nodes = make([]model.Node, 0, len(intent.Nodes))
	found := false
	for _, node := range intent.Nodes {
		if node.ID == nodeID && node.SourceSubscription == id {
			if !node.PinnedStale {
				return SubscriptionSnapshot{}, fmt.Errorf("subscription node %q is current in UCI and cannot be removed by cleanup", nodeID)
			}
			found = true
			continue
		}
		candidate.Nodes = append(candidate.Nodes, node)
	}
	if !found {
		return SubscriptionSnapshot{}, fmt.Errorf("subscription node %q is not present in UCI", nodeID)
	}
	validation := Validate(candidate)
	if !validation.OK {
		for _, issue := range validation.Errors {
			if issue.Code == "DANGLING_NODE" && issue.ObjectType == "route" && issue.Option == "node" {
				return SubscriptionSnapshot{}, fmt.Errorf("NODE_STILL_REFERENCED: route %q still references subscription node %q", issue.ObjectID, nodeID)
			}
		}
		issue := validation.Errors[0]
		return SubscriptionSnapshot{}, fmt.Errorf("subscription cleanup produced invalid candidate: %s %q option %q: %s", issue.ObjectType, issue.ObjectID, issue.Option, issue.Message)
	}
	store := &UCIStore{write: writeUCI}
	if err := store.RemoveNode(context.Background(), id, nodeID); err != nil {
		return SubscriptionSnapshot{}, err
	}
	if err := store.Commit(context.Background()); err != nil {
		return SubscriptionSnapshot{}, err
	}
	snapshot.Nodes = filtered
	if err := saveSubscriptionSnapshot(stateDirectory, snapshot); err != nil {
		return SubscriptionSnapshot{}, err
	}
	return snapshot, nil
}

func saveSubscriptionSnapshot(stateDirectory string, snapshot SubscriptionSnapshot) error {
	path := SubscriptionSnapshotPath(stateDirectory, snapshot.SubscriptionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create subscription state: %w", err)
	}
	encoded, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode subscription snapshot: %w", err)
	}
	if err := atomicWrite(path, append(encoded, '\n')); err != nil {
		return fmt.Errorf("save subscription snapshot: %w", err)
	}
	return nil
}

func appendSubscriptionBatch(batch *strings.Builder, subscriptionID string, existing, merged []model.Node) {
	for _, node := range existing {
		if node.SourceSubscription == subscriptionID {
			fmt.Fprintf(batch, "delete steer.%s\n", node.ID)
		}
	}
	for _, node := range merged {
		appendNodeBatch(batch, node)
	}
}

func appendNodeBatch(batch *strings.Builder, node model.Node) {
	fmt.Fprintf(batch, "set steer.%s=node\n", node.ID)
	setString := func(name, value string) {
		if value != "" {
			fmt.Fprintf(batch, "set steer.%s.%s=%s\n", node.ID, name, uciQuote(value))
		}
	}
	setBool := func(name string, value bool) {
		if value {
			fmt.Fprintf(batch, "set steer.%s.%s=1\n", node.ID, name)
		}
	}
	setInt := func(name string, value int) {
		if value != 0 {
			fmt.Fprintf(batch, "set steer.%s.%s=%d\n", node.ID, name, value)
		}
	}
	setString("name", node.Name)
	if !node.Enabled {
		fmt.Fprintf(batch, "set steer.%s.enabled=0\n", node.ID)
	}
	setString("type", node.Type)
	setString("server", node.Server)
	setInt("server_port", node.ServerPort)
	setString("uuid", node.UUID)
	setString("username", node.Username)
	setString("password", node.Password)
	setString("private_key", node.PrivateKey)
	setString("host_key", node.HostKey)
	setString("flow", node.Flow)
	setString("packet_encoding", node.PacketEncoding)
	setString("method", node.Method)
	setString("plugin", node.Plugin)
	setString("plugin_options", node.PluginOptions)
	setString("security", node.Security)
	setInt("alter_id", node.AlterID)
	setInt("version", node.Version)
	setString("network", node.Network)
	setString("transport", node.Transport)
	setString("transport_path", node.TransportPath)
	setString("transport_host", node.TransportHost)
	setString("service_name", node.ServiceName)
	setString("congestion_control", node.CongestionControl)
	setString("udp_relay_mode", node.UDPRelayMode)
	setBool("udp_over_stream", node.UDPOverStream)
	setBool("zero_rtt_handshake", node.ZeroRTTHandshake)
	setString("heartbeat", node.Heartbeat)
	setBool("quic", node.QUIC)
	setString("quic_congestion_control", node.QUICCongestionControl)
	setInt("insecure_concurrency", node.InsecureConcurrency)
	setString("executable_path", node.ExecutablePath)
	setString("data_directory", node.DataDirectory)
	setString("hop_interval", node.HopInterval)
	setString("obfs_type", node.ObfsType)
	setString("obfs_password", node.ObfsPassword)
	setInt("up_mbps", node.UpMbps)
	setInt("down_mbps", node.DownMbps)
	for _, value := range node.ALPN {
		fmt.Fprintf(batch, "add_list steer.%s.alpn=%s\n", node.ID, uciQuote(value))
	}
	setString("tls_server_name", node.TLSServerName)
	setBool("insecure", node.Insecure)
	setString("reality_public_key", node.RealityPublicKey)
	setString("reality_short_id", node.RealityShortID)
	setString("utls_fingerprint", node.UTLSFingerprint)
	setString("source_subscription", node.SourceSubscription)
	setString("source_fingerprint", node.SourceFingerprint)
	setBool("pinned_stale", node.PinnedStale)
	for name, values := range map[string][]string{"server_ports": node.ServerPorts, "host_key_algorithms": node.HostKeyAlgorithms, "extra_args": node.ExtraArgs} {
		for _, value := range values {
			fmt.Fprintf(batch, "add_list steer.%s.%s=%s\n", node.ID, name, uciQuote(value))
		}
	}
}

func uciQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func readSubscriptionSnapshot(path string) (SubscriptionSnapshot, error) {
	file, err := os.Open(path)
	if err != nil {
		return SubscriptionSnapshot{}, err
	}
	defer file.Close()
	var snapshot SubscriptionSnapshot
	if err := json.NewDecoder(file).Decode(&snapshot); err != nil {
		return SubscriptionSnapshot{}, err
	}
	return snapshot, nil
}

// SetSavedEnabled modifies the committed file without reading or committing any
// CLI or LuCI pending delta. The caller holds the shared operation lock.
func SetSavedEnabled(ctx context.Context, configPath string, enabled bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	updated, err := uci.SetEnabled(string(content), enabled)
	if err != nil {
		return err
	}
	return atomicWrite(configPath, []byte(updated))
}
