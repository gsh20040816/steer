// SPDX-License-Identifier: GPL-3.0-or-later

package geodata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gsh20040816/steer/go/internal/compiler"
)

func writeSeed(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	rules := []Rule{
		seedRule(t, root, "geoip", "cn", []byte("geoip-cn\n")),
		seedRule(t, root, "geosite", "category-example", []byte("geosite-base\n")),
		seedRule(t, root, "geosite", "category-example@cn", []byte("geosite-cn\n")),
	}
	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Upstream: UpstreamIdentity{
			Repository: UpstreamRepository, Version: "test",
			GeoSiteSHA256: digest([]byte("geosite")), GeoIPSHA256: digest([]byte("geoip")),
		},
		Tools: ToolIdentity{GeoViewRef: GeoViewCommit, SingBoxVersion: SingBoxCompiler},
		Rules: rules,
	}
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), append(content, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func seedRule(t *testing.T, root, kind, category string, content []byte) Rule {
	t.Helper()
	tag := "steer-" + kind + "-" + category
	path := filepath.Join(root, "rules", tag+".srs")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return Rule{Kind: kind, Category: category, Tag: tag, Path: "rules/" + tag + ".srs", SHA256: digest(content), Size: int64(len(content))}
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func TestCatalogIncludesExactAttributeSelectors(t *testing.T) {
	root := writeSeed(t)
	values, err := Catalog(root, "geosite")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0] != "category-example" || values[1] != "category-example@cn" {
		t.Fatalf("unexpected catalog: %#v", values)
	}
}

func TestValidateRequiredRulesChecksManifestAndFiles(t *testing.T) {
	root := writeSeed(t)
	rule := compiler.GeoRuleSet{
		Kind: "geosite", Category: "category-example@cn", Tag: "steer-geosite-category-example@cn",
		InitialPath: filepath.Join(root, "rules", "steer-geosite-category-example@cn.srs"),
	}
	if err := ValidateRequiredRules([]compiler.GeoRuleSet{rule}, root); err != nil {
		t.Fatal(err)
	}
	missing := rule
	missing.Category = "missing"
	missing.Tag = "steer-geosite-missing"
	missing.InitialPath = filepath.Join(root, "rules", "steer-geosite-missing.srs")
	err := ValidateRequiredRules([]compiler.GeoRuleSet{missing}, root)
	var geoErr *Error
	if !errors.As(err, &geoErr) || geoErr.Code != ErrorCategoryNotFound {
		t.Fatalf("unexpected missing-category error: %#v", err)
	}
	// The manifest hash is not compared; a same-size file is accepted.
	if err := os.WriteFile(rule.InitialPath, []byte("geosite-xx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRequiredRules([]compiler.GeoRuleSet{rule}, root); err != nil {
		t.Fatalf("same-size seed file was rejected: %v", err)
	}
	if err := os.WriteFile(rule.InitialPath, []byte("truncated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = ValidateRequiredRules([]compiler.GeoRuleSet{rule}, root)
	if !errors.As(err, &geoErr) || geoErr.Code != ErrorSeedInvalid {
		t.Fatalf("unexpected size error: %#v", err)
	}
	if err := os.Remove(rule.InitialPath); err != nil {
		t.Fatal(err)
	}
	err = ValidateRequiredRules([]compiler.GeoRuleSet{rule}, root)
	if !errors.As(err, &geoErr) || geoErr.Code != ErrorSeedInvalid || geoErr.Path != rule.InitialPath {
		t.Fatalf("unexpected missing-file error: %#v", err)
	}
}

func TestVerifyDirectoryRejectsUndeclaredFiles(t *testing.T) {
	root := writeSeed(t)
	if err := VerifyDirectory(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rules", "extra.srs"), []byte("extra"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDirectory(root); err == nil {
		t.Fatal("undeclared seed file was accepted")
	}
}

func TestVerifyDirectoryRejectsUndeclaredRootEntry(t *testing.T) {
	root := writeSeed(t)
	if err := os.WriteFile(filepath.Join(root, "unexpected"), []byte("extra"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDirectory(root); err == nil {
		t.Fatal("undeclared root entry was accepted")
	}
}
