// SPDX-License-Identifier: GPL-3.0-or-later

package openwrt

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	coreapply "github.com/gsh20040816/steer/go/internal/apply"
	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/generation"
)

// Status exposes only Active runtime identity/health and the independent last
// Apply record. Draft and Saved facts belong to the UI lifecycle contract.
type Status struct {
	Healthy       bool              `json:"healthy"`
	Generation    string            `json:"generation,omitempty"`
	IntentDigest  string            `json:"intent_digest,omitempty"`
	RuntimeDigest string            `json:"runtime_digest,omitempty"`
	LastApply     *coreapply.Record `json:"last_apply,omitempty"`
}

func ReadStatus(ctx context.Context, runner Runner, runDirectory, nftBinary string) Status {
	status := Status{}
	if file, err := os.Open(filepath.Join(runDirectory, "last-apply.json")); err == nil {
		var record coreapply.Record
		if json.NewDecoder(file).Decode(&record) == nil && record.Sequence != "" {
			status.LastApply = &record
		}
		file.Close()
	}
	currentPath := filepath.Join(runDirectory, "current")
	if generationID := currentGenerationID(currentPath); generationID != "" {
		if value, err := generation.ReadIntent(currentPath); err == nil && value.Main.Enabled {
			status.Generation = generationID
			status.IntentDigest = compiler.IntentDigest(value)
			if file, openErr := os.Open(filepath.Join(currentPath, "sing-box.json")); openErr == nil {
				var singBox map[string]any
				if json.NewDecoder(file).Decode(&singBox) == nil {
					status.RuntimeDigest = compiler.RuntimeDigest(value, singBox)
				}
				file.Close()
			}
		}
	}
	plan, err := readCurrentPlan(runDirectory)
	if status.Generation != "" && err == nil && checkHealthOnce(ctx, runner, plan, checkListenerPorts, nftBinary) == nil {
		status.Healthy = true
	}
	return status
}
