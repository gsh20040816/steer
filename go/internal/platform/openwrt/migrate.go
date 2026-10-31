// SPDX-License-Identifier: GPL-3.0-or-later

package openwrt

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	model "github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/platform/openwrt/uci"
	"github.com/gsh20040816/steer/go/internal/subscription"
)

// Migrate moves the subscription nodes of a schema 9 UCI configuration into
// the node store and sets schema 10. It is idempotent: every other schema is
// left untouched and reported by validation instead.
func Migrate(ctx context.Context, configPath, stateDirectory, nodeDirectory string) (bool, error) {
	return MigrateWithWriter(ctx, configPath, stateDirectory, nodeDirectory, SystemUCIWriter(configPath))
}

func MigrateWithWriter(ctx context.Context, configPath, stateDirectory, nodeDirectory string, writer UCIWriter) (bool, error) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return false, fmt.Errorf("read UCI for schema migration: %w", err)
	}
	document, err := uci.Parse(bytes.NewReader(content))
	if err != nil {
		return false, err
	}
	mainID := ""
	for _, section := range document.Sections {
		if section.Type == "steer" && section.Options["schema_version"] == fmt.Sprint(subscription.LegacySchemaVersion) {
			mainID = section.ID
		}
	}
	if mainID == "" {
		return false, nil
	}
	config, err := decodeDocument(document)
	if err != nil {
		return false, err
	}
	migration := subscription.PlanMigration(config)
	if err := subscription.WriteMigratedNodes(migration, subscription.NodeStore{Directory: nodeDirectory}, stateDirectory); err != nil {
		return false, err
	}
	cleared := make(map[string]bool, len(migration.Cleared))
	for _, id := range migration.Cleared {
		cleared[id] = true
	}
	var batch strings.Builder
	for _, id := range migration.Moved {
		fmt.Fprintf(&batch, "delete steer.%s\n", id)
	}
	for _, section := range document.Sections {
		if section.Type != "node" || !cleared[section.ID] {
			continue
		}
		for _, option := range []string{"source_subscription", "source_fingerprint", "pinned_stale"} {
			if _, exists := section.Options[option]; exists {
				fmt.Fprintf(&batch, "delete steer.%s.%s\n", section.ID, option)
			}
		}
	}
	fmt.Fprintf(&batch, "set steer.%s.schema_version='%d'\n", mainID, model.SchemaVersion)
	batch.WriteString("commit steer\n")
	if err := writer(ctx, batch.String()); err != nil {
		return false, err
	}
	return true, nil
}
