// SPDX-License-Identifier: GPL-3.0-or-later
package openwrt

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	model "github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/platform/openwrt/uci"
	"github.com/gsh20040816/steer/go/internal/subscription"
)

type SubscriptionSnapshot = subscription.Snapshot
type SubscriptionStatus = subscription.Status
type SubscriptionUpdateError = subscription.UpdateError

// DefaultNodeDirectory holds the subscription node store. It must survive a
// reboot, and /var is in RAM on OpenWrt, so it lives next to the UCI files.
const DefaultNodeDirectory = "/etc/steer/subscription-nodes"

// LoadConfig reads only the UCI configuration, without subscription nodes.
func LoadConfig(configPath string) (model.Intent, error) {
	config, err := os.ReadFile(configPath)
	if err != nil {
		return model.Intent{}, fmt.Errorf("read UCI configuration: %w", err)
	}
	return DecodeBytes(config)
}

// LoadSaved returns the complete Saved Intent: the UCI configuration plus the
// stored nodes of every configured subscription.
func LoadSaved(configPath, nodeDirectory string) (model.Intent, error) {
	config, err := LoadConfig(configPath)
	if err != nil {
		return model.Intent{}, err
	}
	return subscription.Attach(config, subscription.NodeStore{Directory: nodeDirectory})
}

func ReadSubscriptionStatus(configPath, stateDirectory, nodeDirectory string) ([]SubscriptionStatus, error) {
	saved, err := LoadSaved(configPath, nodeDirectory)
	if err != nil {
		return nil, err
	}
	return subscription.Statuses(saved, stateDirectory), nil
}

// UpdateConfiguredSubscriptions writes only the node store and the status
// snapshots. UCI is never changed, so a refresh triggers no procd reload.
func UpdateConfiguredSubscriptions(ctx context.Context, client *http.Client, configPath, stateDirectory, nodeDirectory, id string) ([]SubscriptionSnapshot, error) {
	saved, err := LoadSaved(configPath, nodeDirectory)
	if err != nil {
		return nil, err
	}
	return subscription.Update(ctx, client, saved, subscription.NodeStore{Directory: nodeDirectory}, stateDirectory, id, Validate)
}

func CleanSubscriptionNode(configPath, nodeDirectory, id, nodeID string) error {
	saved, err := LoadSaved(configPath, nodeDirectory)
	if err != nil {
		return err
	}
	return subscription.CleanStale(saved, subscription.NodeStore{Directory: nodeDirectory}, id, nodeID, Validate)
}

func SetSubscriptionNodeEnabled(configPath, nodeDirectory, id, nodeID string, enabled bool) error {
	saved, err := LoadSaved(configPath, nodeDirectory)
	if err != nil {
		return err
	}
	return subscription.SetNodeEnabled(saved, subscription.NodeStore{Directory: nodeDirectory}, id, nodeID, enabled, Validate)
}

func ListSubscriptionNodes(configPath, nodeDirectory, id string) (subscription.NodeList, error) {
	saved, err := LoadSaved(configPath, nodeDirectory)
	if err != nil {
		return subscription.NodeList{}, err
	}
	return subscription.ListNodes(saved, id)
}

// RemoveOrphanNodes drops store files of subscriptions deleted from UCI. LuCI
// writes UCI directly, so a successful Apply is where OpenWrt notices.
func RemoveOrphanNodes(configPath, nodeDirectory string) error {
	config, err := LoadConfig(configPath)
	if err != nil {
		return err
	}
	return subscription.NodeStore{Directory: nodeDirectory}.RemoveOrphans(config.Subscriptions)
}

// UCIWriter applies one complete batch and must include the commit operation.
type UCIWriter func(context.Context, string) error

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
