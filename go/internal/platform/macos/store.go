// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	coreapply "github.com/gsh20040816/steer/go/internal/apply"
	model "github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/subscription"
)

const RuntimeSchemaVersion = 1

var ErrRevisionConflict = errors.New("configuration revision conflict")

// Paths is the only filesystem layout known by the macOS adapter. The caller
// passes an explicit runtime root; this package never guesses a user or home
// directory.
type Paths struct {
	Root                 string `json:"root"`
	ConfigDirectory      string `json:"config_directory"`
	ConfigPath           string `json:"config_path"`
	GenerationsDirectory string `json:"generations_directory"`
	StateDirectory       string `json:"state_directory"`
	LogsDirectory        string `json:"logs_directory"`
}

func (paths Paths) Ensure() error {
	for _, directory := range []string{
		paths.ConfigDirectory, paths.GenerationsDirectory, paths.StateDirectory, paths.LogsDirectory,
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create macOS runtime directory %s: %w", directory, err)
		}
	}
	return nil
}

// IntentStore is the macOS Saved state: Paths.ConfigPath holds the
// configuration and Paths.StateDirectory the subscription node store.
type IntentStore struct {
	Paths            Paths
	GeoDataDirectory string
}

// SavedStore is the macOS Saved state for one config file and state directory.
func SavedStore(configPath, stateDirectory string) IntentStore {
	return IntentStore{Paths: Paths{ConfigPath: configPath, StateDirectory: stateDirectory}}
}

func (store IntentStore) nodes() subscription.NodeStore {
	if store.Paths.StateDirectory == "" {
		return subscription.NodeStore{}
	}
	return subscription.NodeStore{Directory: subscription.NodeDirectory(store.Paths.StateDirectory)}
}

// LoadSaved returns the complete Saved Intent: the configuration plus the
// stored nodes of every configured subscription.
func (store IntentStore) LoadSaved() (model.Intent, string, error) {
	config, revision, err := store.Load()
	if err != nil {
		return model.Intent{}, "", err
	}
	value, err := store.Complete(config)
	if err != nil {
		return model.Intent{}, "", err
	}
	return value, revision, nil
}

// Complete attaches the stored subscription nodes to a configuration, such as
// a document the GUI is about to save.
func (store IntentStore) Complete(config model.Intent) (model.Intent, error) {
	value, err := subscription.Attach(config, store.nodes())
	var configErr subscription.ConfigError
	if errors.As(err, &configErr) {
		return model.Intent{}, ValidationError{Validation: configErr.Validation}
	}
	return value, err
}

// RemoveOrphanNodes drops store files of subscriptions that the configuration
// no longer has. It runs after a successful save, Apply or update.
func (store IntentStore) RemoveOrphanNodes(config model.Intent) error {
	return store.nodes().RemoveOrphans(config.Subscriptions)
}

// Load reads only the configuration file, which has no subscription nodes.
func (store IntentStore) Load() (model.Intent, string, error) {
	content, err := os.ReadFile(store.Paths.ConfigPath)
	if err != nil {
		return model.Intent{}, "", fmt.Errorf("read macOS canonical intent: %w", err)
	}
	value, err := model.DecodeJSON(bytes.NewReader(content))
	if err != nil {
		return model.Intent{}, "", err
	}
	return value, contentRevision(content), nil
}

// Save writes only the configuration after validating it with the stored
// subscription nodes attached.
func (store IntentStore) Save(config model.Intent, expectedRevision string) (string, error) {
	value, err := store.Complete(config)
	if err != nil {
		return "", err
	}
	validation := ValidateWithGeoDataDirectory(value, store.geoDataDirectory())
	if !validation.OK {
		return "", ValidationError{Validation: validation}
	}
	if expectedRevision != "" {
		_, current, err := store.Load()
		if err != nil {
			return "", err
		}
		if current != expectedRevision {
			return "", ErrRevisionConflict
		}
	}
	var encoded bytes.Buffer
	if err := model.EncodeJSON(&encoded, config); err != nil {
		return "", err
	}
	if err := atomicWrite(store.Paths.ConfigPath, encoded.Bytes()); err != nil {
		return "", fmt.Errorf("write macOS canonical intent: %w", err)
	}
	// Orphans are never attached; a failed removal is retried later.
	_ = store.RemoveOrphanNodes(config)
	return contentRevision(encoded.Bytes()), nil
}

func (store IntentStore) geoDataDirectory() string {
	return normalizeGeoDataDirectory(store.GeoDataDirectory)
}

type Status struct {
	WireGuard     []WireGuardPeerStatus `json:"wireguard,omitempty"`
	SchemaVersion int                   `json:"schema_version"`
	Healthy       bool                  `json:"healthy"`
	GenerationID  string                `json:"generation_id,omitempty"`
	IntentDigest  string                `json:"intent_digest,omitempty"`
	RuntimeDigest string                `json:"runtime_digest,omitempty"`
	LastApply     *coreapply.Record     `json:"last_apply,omitempty"`
	Error         string                `json:"error,omitempty"`
}

func DefaultStatus() Status {
	return Status{SchemaVersion: RuntimeSchemaVersion}
}

func contentRevision(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256-" + hex.EncodeToString(sum[:])
}

func atomicWrite(path string, content []byte) error {
	return atomicWriteMode(path, content, 0o600)
}

func atomicWriteMode(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".steer-write-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func readJSON(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	return content, nil
}

func unmarshalStrict(content []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func jsonMarshal(value any) ([]byte, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode JSON: %w", err)
	}
	return encoded, nil
}

type ValidationError struct{ Validation model.Validation }

func (value ValidationError) Error() string {
	return fmt.Sprintf("candidate validation failed with %d error(s)", len(value.Validation.Errors))
}
