// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"fmt"
	"path/filepath"

	"github.com/gsh20040816/steer/go/internal/generation"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

type GenerationMetadata struct {
	SchemaVersion int    `json:"schema_version"`
	GenerationID  string `json:"generation_id"`
	IntentDigest  string `json:"intent_digest"`
}

type PreparedGeneration struct {
	generation.Candidate
	Metadata GenerationMetadata
}

type CurrentGeneration struct {
	SchemaVersion int    `json:"schema_version"`
	GenerationID  string `json:"generation_id"`
	Directory     string `json:"directory"`
	IntentDigest  string `json:"intent_digest"`
}

// Publish records the candidate after launchd has stopped the old sing-box
// process and before the new LaunchDaemon is bootstrapped. The candidate is
// one that Prepare created in the generations directory.
func (paths Paths) Publish(prepared PreparedGeneration) error {
	current := CurrentGeneration{
		SchemaVersion: RuntimeSchemaVersion,
		GenerationID:  prepared.Metadata.GenerationID,
		Directory:     filepath.Base(prepared.Directory),
		IntentDigest:  prepared.Metadata.IntentDigest,
	}
	encoded, err := marshalJSON(current)
	if err != nil {
		return err
	}
	// current.json contains only generation identifiers and a directory name.
	// Keep it world-readable so the unprivileged GUI can report status without
	// requesting administrator authorization; generated sing-box configs remain
	// private under the root-owned runtime directory.
	return atomicWriteMode(filepath.Join(paths.Root, "current.json"), encoded, 0o644)
}

func (paths Paths) LoadCurrent() (CurrentGeneration, error) {
	content, err := readJSON(filepath.Join(paths.Root, "current.json"))
	if err != nil {
		return CurrentGeneration{}, err
	}
	var current CurrentGeneration
	if err := unmarshalStrict(content, &current); err != nil {
		return CurrentGeneration{}, fmt.Errorf("decode macOS current generation: %w", err)
	}
	if current.SchemaVersion != RuntimeSchemaVersion ||
		current.Directory == "" || current.Directory == "." || filepath.Base(current.Directory) != current.Directory {
		return CurrentGeneration{}, fmt.Errorf("invalid macOS current generation contract")
	}
	return current, nil
}

// LoadCurrentIntent resolves only the immutable Intent named by current.json.
// Saved config is deliberately not a fallback: a Save without Apply must not
// alter diagnostics for the running data plane.
func (paths Paths) LoadCurrentIntent() (CurrentGeneration, model.Intent, error) {
	current, err := paths.LoadCurrent()
	if err != nil {
		return CurrentGeneration{}, model.Intent{}, err
	}
	value, err := generation.ReadIntent(filepath.Join(paths.GenerationsDirectory, current.Directory))
	if err != nil {
		return CurrentGeneration{}, model.Intent{}, err
	}
	if !value.Main.Enabled {
		return CurrentGeneration{}, model.Intent{}, fmt.Errorf("current macOS generation is disabled")
	}
	return current, value, nil
}

func marshalJSON(value any) ([]byte, error) {
	encoded, err := jsonMarshal(value)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}
