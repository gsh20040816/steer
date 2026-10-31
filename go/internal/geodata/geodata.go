// SPDX-License-Identifier: GPL-3.0-or-later

// Package geodata validates the package-owned seed manifest used by remote
// sing-box rule-sets. Conversion belongs to the release workflow, never to a
// user device.
package geodata

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gsh20040816/steer/go/internal/compiler"
)

const (
	ManifestSchemaVersion = 1
	DefaultSeedDirectory  = "/usr/share/steer/geodata-seed"
	UpstreamRepository    = "Loyalsoldier/v2ray-rules-dat"
	GeoViewCommit         = "3c91926d360b8f49d47520639e574608318baf12"

	ErrorManifestInvalid  = "GEO_MANIFEST_INVALID"
	ErrorCategoryNotFound = "GEO_CATEGORY_NOT_FOUND"
	ErrorSeedInvalid      = "GEO_SEED_INVALID"
)

var validCategory = regexp.MustCompile(`^[a-z0-9_!.\-]+(@[a-z0-9_!.\-]+)?$`)

type Error struct {
	Code     string `json:"code"`
	Kind     string `json:"kind,omitempty"`
	Category string `json:"category,omitempty"`
	Path     string `json:"path,omitempty"`
	Err      error  `json:"-"`
}

func (value *Error) Error() string {
	switch value.Code {
	case ErrorManifestInvalid:
		return fmt.Sprintf("Geo seed manifest %q is invalid: %v", value.Path, value.Err)
	case ErrorCategoryNotFound:
		return fmt.Sprintf("%s category %q is not available in the package seed manifest", value.Kind, value.Category)
	case ErrorSeedInvalid:
		return fmt.Sprintf("Geo seed %q for %s:%s is invalid: %v", value.Path, value.Kind, value.Category, value.Err)
	default:
		return fmt.Sprintf("Geo seed validation failed: %v", value.Err)
	}
}

func (value *Error) Unwrap() error { return value.Err }

type Manifest struct {
	SchemaVersion int              `json:"schema_version"`
	Upstream      UpstreamIdentity `json:"upstream"`
	Tools         ToolIdentity     `json:"tools"`
	Rules         []Rule           `json:"rules"`
}

type UpstreamIdentity struct {
	Repository    string `json:"repository"`
	Version       string `json:"version"`
	GeoSiteSHA256 string `json:"geosite_sha256"`
	GeoIPSHA256   string `json:"geoip_sha256"`
}

type ToolIdentity struct {
	GeoViewRef     string `json:"geoview_ref"`
	SingBoxVersion string `json:"sing_box_version"`
}

// Rule describes one SRS file. Tag, Path, SHA256 and Size are recorded by the
// Geo workflow for reference; Steer only checks that a required file exists.
type Rule struct {
	Kind     string `json:"kind"`
	Category string `json:"category"`
	Tag      string `json:"tag"`
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

func ReadManifest(seedDirectory string) (Manifest, error) {
	seedDirectory = normalizedSeedDirectory(seedDirectory)
	path := filepath.Join(seedDirectory, "manifest.json")
	content, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, manifestError(path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, manifestError(path, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return Manifest{}, manifestError(path, err)
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, manifestError(path, err)
	}
	return manifest, nil
}

// ValidateManifest checks what Steer reads from a manifest: its format version
// and the categories, which come from upstream data. The identity fields,
// paths, sizes and digests are only recorded.
func ValidateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("schema_version must be %d", ManifestSchemaVersion)
	}
	seen := make(map[string]struct{}, len(manifest.Rules))
	for index, rule := range manifest.Rules {
		if !validCategory.MatchString(rule.Category) {
			return fmt.Errorf("rules[%d] has invalid category %q", index, rule.Category)
		}
		key := rule.Kind + "\x00" + rule.Category
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate Geo rule %s:%s", rule.Kind, rule.Category)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func Catalog(seedDirectory, kind string) ([]string, error) {
	if kind != "geosite" && kind != "geoip" {
		return nil, fmt.Errorf("Geo catalog kind must be geosite or geoip")
	}
	manifest, err := ReadManifest(seedDirectory)
	if err != nil {
		return nil, err
	}
	values := make([]string, 0)
	for _, rule := range manifest.Rules {
		if rule.Kind == kind {
			values = append(values, rule.Category)
		}
	}
	sort.Strings(values)
	return values, nil
}

// ValidateRequiredRules reports a category that the manifest does not list
// and a required SRS file that is missing, before Apply touches the runtime.
func ValidateRequiredRules(ruleSets []compiler.GeoRuleSet, seedDirectory string) error {
	if len(ruleSets) == 0 {
		return nil
	}
	manifest, err := ReadManifest(seedDirectory)
	if err != nil {
		return err
	}
	available := make(map[string]bool, len(manifest.Rules))
	for _, rule := range manifest.Rules {
		available[rule.Kind+"\x00"+rule.Category] = true
	}
	for _, required := range ruleSets {
		if !available[required.Kind+"\x00"+required.Category] {
			return &Error{Code: ErrorCategoryNotFound, Kind: required.Kind, Category: required.Category}
		}
		if _, err := os.Stat(required.InitialPath); err != nil {
			return &Error{Code: ErrorSeedInvalid, Kind: required.Kind, Category: required.Category, Path: required.InitialPath, Err: err}
		}
	}
	return nil
}

func normalizedSeedDirectory(value string) string {
	if strings.TrimSpace(value) == "" {
		return DefaultSeedDirectory
	}
	return filepath.Clean(value)
}

func manifestError(path string, err error) error {
	return &Error{Code: ErrorManifestInvalid, Path: path, Err: err}
}
