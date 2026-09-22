// SPDX-License-Identifier: GPL-3.0-or-later

// Package geodata validates the package-owned seed manifest used by remote
// sing-box rule-sets. Conversion belongs to the release workflow, never to a
// user device.
package geodata

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
	SingBoxCompiler       = "1.14.1"

	ErrorManifestInvalid  = "GEO_MANIFEST_INVALID"
	ErrorCategoryNotFound = "GEO_CATEGORY_NOT_FOUND"
	ErrorSeedInvalid      = "GEO_SEED_INVALID"
)

var (
	validCategory = regexp.MustCompile(`^[a-z0-9_!.\-]+(@[a-z0-9_!.\-]+)?$`)
	validDigest   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

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

func ValidateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("schema_version must be %d", ManifestSchemaVersion)
	}
	if manifest.Upstream.Repository != UpstreamRepository || manifest.Upstream.Version == "" {
		return fmt.Errorf("upstream repository must be %q and version is required", UpstreamRepository)
	}
	if !validDigest.MatchString(manifest.Upstream.GeoSiteSHA256) || !validDigest.MatchString(manifest.Upstream.GeoIPSHA256) {
		return fmt.Errorf("upstream GeoSite and GeoIP SHA-256 values are required")
	}
	if manifest.Tools.GeoViewRef != GeoViewCommit || manifest.Tools.SingBoxVersion != SingBoxCompiler {
		return fmt.Errorf("conversion tools must be geoview %s and sing-box %s", GeoViewCommit, SingBoxCompiler)
	}
	if len(manifest.Rules) == 0 {
		return fmt.Errorf("rules must not be empty")
	}
	seen := make(map[string]struct{}, len(manifest.Rules))
	previousTag := ""
	for index, rule := range manifest.Rules {
		if rule.Kind != "geosite" && rule.Kind != "geoip" {
			return fmt.Errorf("rules[%d] has unsupported kind %q", index, rule.Kind)
		}
		if !validCategory.MatchString(rule.Category) {
			return fmt.Errorf("rules[%d] has invalid category %q", index, rule.Category)
		}
		expectedTag := "steer-" + rule.Kind + "-" + rule.Category
		if rule.Tag != expectedTag {
			return fmt.Errorf("rules[%d] tag must be %q", index, expectedTag)
		}
		expectedPath := filepath.ToSlash(filepath.Join("rules", expectedTag+".srs"))
		if rule.Path != expectedPath {
			return fmt.Errorf("rules[%d] path must be %q", index, expectedPath)
		}
		if !validDigest.MatchString(rule.SHA256) || rule.Size <= 0 {
			return fmt.Errorf("rules[%d] requires a SHA-256 and positive size", index)
		}
		key := rule.Kind + "\x00" + rule.Category
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate Geo rule %s:%s", rule.Kind, rule.Category)
		}
		seen[key] = struct{}{}
		if previousTag != "" && previousTag >= rule.Tag {
			return fmt.Errorf("rules must be strictly sorted by tag")
		}
		previousTag = rule.Tag
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

func ValidateRequiredRules(ruleSets []compiler.GeoRuleSet, seedDirectory string) error {
	if len(ruleSets) == 0 {
		return nil
	}
	seedDirectory = normalizedSeedDirectory(seedDirectory)
	manifest, err := ReadManifest(seedDirectory)
	if err != nil {
		return err
	}
	index := make(map[string]Rule, len(manifest.Rules))
	for _, rule := range manifest.Rules {
		index[rule.Kind+"\x00"+rule.Category] = rule
	}
	for _, required := range ruleSets {
		rule, exists := index[required.Kind+"\x00"+required.Category]
		if !exists {
			return &Error{Code: ErrorCategoryNotFound, Kind: required.Kind, Category: required.Category}
		}
		expectedPath := filepath.Join(seedDirectory, filepath.FromSlash(rule.Path))
		if required.Tag != rule.Tag || filepath.Clean(required.InitialPath) != expectedPath {
			return seedError(required, required.InitialPath, fmt.Errorf("compiler path or tag does not match the package manifest"))
		}
		if err := verifyRuleFile(expectedPath, rule); err != nil {
			return seedError(required, expectedPath, err)
		}
	}
	return nil
}

func VerifyDirectory(seedDirectory string) error {
	seedDirectory = normalizedSeedDirectory(seedDirectory)
	rootEntries, err := os.ReadDir(seedDirectory)
	if err != nil {
		return manifestError(seedDirectory, err)
	}
	if len(rootEntries) != 2 || rootEntries[0].Name() != "manifest.json" || !rootEntries[0].Type().IsRegular() || rootEntries[1].Name() != "rules" || !rootEntries[1].IsDir() {
		return manifestError(seedDirectory, fmt.Errorf("seed root must contain exactly a regular manifest.json and rules directory"))
	}
	manifest, err := ReadManifest(seedDirectory)
	if err != nil {
		return err
	}
	expected := make(map[string]Rule, len(manifest.Rules))
	for _, rule := range manifest.Rules {
		expected[filepath.Base(rule.Path)] = rule
		path := filepath.Join(seedDirectory, filepath.FromSlash(rule.Path))
		if err := verifyRuleFile(path, rule); err != nil {
			return &Error{Code: ErrorSeedInvalid, Kind: rule.Kind, Category: rule.Category, Path: path, Err: err}
		}
	}
	rulesDirectory := filepath.Join(seedDirectory, "rules")
	entries, err := os.ReadDir(rulesDirectory)
	if err != nil {
		return manifestError(rulesDirectory, err)
	}
	if len(entries) != len(expected) {
		return manifestError(rulesDirectory, fmt.Errorf("found %d files, manifest requires %d", len(entries), len(expected)))
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return manifestError(filepath.Join(rulesDirectory, entry.Name()), fmt.Errorf("unexpected directory"))
		}
		if _, exists := expected[entry.Name()]; !exists {
			return manifestError(filepath.Join(rulesDirectory, entry.Name()), fmt.Errorf("file is not declared by manifest"))
		}
	}
	return nil
}

func verifyRuleFile(path string, rule Rule) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	if info.Size() != rule.Size {
		return fmt.Errorf("size is %d, want %d", info.Size(), rule.Size)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(digest.Sum(nil))
	if actual != rule.SHA256 {
		return fmt.Errorf("SHA-256 is %s, want %s", actual, rule.SHA256)
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

func seedError(rule compiler.GeoRuleSet, path string, err error) error {
	return &Error{Code: ErrorSeedInvalid, Kind: rule.Kind, Category: rule.Category, Path: path, Err: err}
}
