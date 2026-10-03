// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gsh20040816/steer/go/internal/compiler"
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
// process and before the new LaunchDaemon is bootstrapped.
func (paths Paths) Publish(prepared PreparedGeneration) error {
	if prepared.Directory == "" || prepared.Metadata.GenerationID == "" {
		return fmt.Errorf("cannot publish an incomplete macOS generation")
	}
	root, err := filepath.Abs(paths.GenerationsDirectory)
	if err != nil {
		return fmt.Errorf("resolve macOS generations directory: %w", err)
	}
	candidateDirectory, err := filepath.Abs(prepared.Directory)
	if err != nil {
		return fmt.Errorf("resolve macOS candidate directory: %w", err)
	}
	relative, err := filepath.Rel(root, candidateDirectory)
	if err != nil || relative == "." || relative == ".." || len(relative) < 3 || relative[:3] == ".."+string(filepath.Separator) {
		return fmt.Errorf("macOS candidate is outside the generations directory")
	}
	info, err := os.Stat(candidateDirectory)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("macOS candidate directory is unavailable: %w", err)
	}
	metadataContent, err := readJSON(filepath.Join(candidateDirectory, "generation.json"))
	if err != nil {
		return err
	}
	var metadata GenerationMetadata
	if err := unmarshalStrict(metadataContent, &metadata); err != nil {
		return fmt.Errorf("decode macOS candidate metadata: %w", err)
	}
	if metadata.SchemaVersion != RuntimeSchemaVersion || metadata.GenerationID != prepared.Metadata.GenerationID || metadata.IntentDigest != prepared.Metadata.IntentDigest {
		return fmt.Errorf("macOS candidate metadata does not match the prepared generation")
	}
	current := CurrentGeneration{
		SchemaVersion: RuntimeSchemaVersion,
		GenerationID:  prepared.Metadata.GenerationID,
		Directory:     filepath.Base(candidateDirectory),
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
	if current.SchemaVersion != RuntimeSchemaVersion || current.GenerationID == "" || current.IntentDigest == "" ||
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
	directory := filepath.Join(paths.GenerationsDirectory, current.Directory)
	metadata, err := readGenerationMetadata(directory)
	if err != nil {
		return CurrentGeneration{}, model.Intent{}, err
	}
	if metadata.GenerationID != current.GenerationID || metadata.IntentDigest != current.IntentDigest {
		return CurrentGeneration{}, model.Intent{}, fmt.Errorf("current macOS generation metadata does not match current.json")
	}
	value, err := generation.ReadIntent(directory)
	if err != nil {
		return CurrentGeneration{}, model.Intent{}, err
	}
	if !value.Main.Enabled {
		return CurrentGeneration{}, model.Intent{}, fmt.Errorf("current macOS generation is disabled")
	}
	if digest := compiler.IntentDigest(value); digest != current.IntentDigest {
		return CurrentGeneration{}, model.Intent{}, fmt.Errorf("current macOS intent digest does not match current.json")
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
