// SPDX-License-Identifier: GPL-3.0-or-later

package linux

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	model "github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/subscription"
)

var ErrRevisionConflict = errors.New("configuration revision conflict")

// IntentStore is the Linux Saved state: config.json holds the configuration
// and NodeDirectory the subscription node store.
type IntentStore struct {
	Path             string
	GeoDataDirectory string
	NodeDirectory    string
}

func (store IntentStore) normalizedPath() string {
	if store.Path == "" {
		return "/etc/steer/config.json"
	}
	return store.Path
}

func (store IntentStore) nodes() subscription.NodeStore {
	return subscription.NodeStore{Directory: store.NodeDirectory}
}

// Load reads only the configuration file, which has no subscription nodes.
func (store IntentStore) Load() (model.Intent, string, error) {
	content, err := os.ReadFile(store.normalizedPath())
	if err != nil {
		return model.Intent{}, "", fmt.Errorf("read canonical intent: %w", err)
	}
	value, err := model.DecodeJSON(bytes.NewReader(content))
	if err != nil {
		return model.Intent{}, "", err
	}
	return value, revision(content), nil
}

// LoadSaved returns the complete Saved Intent: the configuration plus the
// stored nodes of every configured subscription.
func (store IntentStore) LoadSaved() (model.Intent, string, error) {
	config, configRevision, err := store.Load()
	if err != nil {
		return model.Intent{}, "", err
	}
	value, err := store.Complete(config)
	if err != nil {
		return model.Intent{}, "", err
	}
	return value, configRevision, nil
}

// Complete attaches the stored subscription nodes to a configuration, such as
// a draft that is about to be validated or saved.
func (store IntentStore) Complete(config model.Intent) (model.Intent, error) {
	value, err := subscription.Attach(config, store.nodes())
	var configErr subscription.ConfigError
	if errors.As(err, &configErr) {
		return model.Intent{}, ValidationError{Validation: configErr.Validation}
	}
	return value, err
}

// Save writes only the configuration after validating it with the stored
// subscription nodes attached. Store files of subscriptions this save removed
// are deleted afterwards.
func (store IntentStore) Save(config model.Intent, expectedRevision string) (string, error) {
	value, err := store.Complete(config)
	if err != nil {
		return "", err
	}
	validation := ValidateWithGeoDataDirectory(value, store.GeoDataDirectory)
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
	content := encoded.Bytes()
	if err := atomicWrite(store.normalizedPath(), content); err != nil {
		return "", fmt.Errorf("write canonical intent: %w", err)
	}
	// Orphans are never attached; a failed removal is retried later.
	_ = store.nodes().RemoveOrphans(config.Subscriptions)
	return revision(content), nil
}

func revision(content []byte) string {
	sum := sha256.Sum256(content)
	return `"sha256-` + hex.EncodeToString(sum[:]) + `"`
}

type ValidationError struct {
	Validation model.Validation
}

func (value ValidationError) Error() string {
	return fmt.Sprintf("candidate validation failed with %d error(s)", len(value.Validation.Errors))
}
