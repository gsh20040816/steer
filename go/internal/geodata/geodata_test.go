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
		Tools: ToolIdentity{GeoViewRef: GeoViewCommit, SingBoxVersion: "1.14.4"},
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

func TestValidateRequiredRulesChecksOnlyThatFilesExist(t *testing.T) {
	root := writeSeed(t)
	rule := compiler.GeoRuleSet{
		Kind: "geosite", Category: "category-example@cn", Tag: "steer-geosite-category-example@cn",
		InitialPath: filepath.Join(root, "rules", "steer-geosite-category-example@cn.srs"),
	}
	if err := ValidateRequiredRules([]compiler.GeoRuleSet{rule}); err != nil {
		t.Fatal(err)
	}
	// The recorded size and hash are not compared.
	if err := os.WriteFile(rule.InitialPath, []byte("replaced with other content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRequiredRules([]compiler.GeoRuleSet{rule}); err != nil {
		t.Fatalf("seed file with a different size was rejected: %v", err)
	}
	if err := os.Remove(rule.InitialPath); err != nil {
		t.Fatal(err)
	}
	err := ValidateRequiredRules([]compiler.GeoRuleSet{rule})
	var geoErr *Error
	if !errors.As(err, &geoErr) || geoErr.Code != ErrorSeedInvalid || geoErr.Path != rule.InitialPath {
		t.Fatalf("unexpected missing-file error: %#v", err)
	}
}
