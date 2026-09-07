// SPDX-License-Identifier: GPL-3.0-or-later

package linux

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/geodata"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

type prepareRunner struct{ calls []string }

func (runner *prepareRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	runner.calls = append(runner.calls, call)
	if name == "/test/sing-box" && len(args) == 1 && args[0] == "version" {
		return []byte("sing-box version 1.14.0-rc.1\nTags: with_quic,with_utls\n"), nil
	}
	return nil, nil
}

func TestPrepareWritesLinuxGenerationFiles(t *testing.T) {
	root := t.TempDir()
	value := validIntent()
	backend := NewBackend(&prepareRunner{}, value, BackendOptions{RunDirectory: filepath.Join(root, "run"), StateDirectory: filepath.Join(root, "state"), SingBoxBinary: "/test/sing-box", NFTBinary: "/test/nft"})
	compiled := compiler.Compile(value, backend.CompilerOptions())
	candidate, err := backend.Prepare(context.Background(), value, compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(candidate.Directory)
	for _, name := range []string{"intent.json", "sing-box.json", "platform.json", "firewall.nft"} {
		if _, err := os.Stat(filepath.Join(candidate.Directory, name)); err != nil {
			t.Fatalf("generation file %s is missing: %v", name, err)
		}
	}
	firewall, err := os.ReadFile(filepath.Join(candidate.Directory, "firewall.nft"))
	if err != nil || !strings.Contains(string(firewall), "fib daddr type != local return") {
		t.Fatalf("unexpected Linux firewall: %s (%v)", firewall, err)
	}
}

func TestPrepareValidatesPackageOwnedGeoSeed(t *testing.T) {
	root := t.TempDir()
	seedDirectory := writeTestSeed(t, root, "geosite", "cn")
	value := validIntent()
	value.Rules = append([]model.Rule{{ID: "geo", Enabled: true, DomainMatch: []string{"geosite:cn"}, DNSProfile: "public", Route: "direct"}}, value.Rules...)
	runner := &prepareRunner{}
	backend := NewBackend(runner, value, BackendOptions{
		RunDirectory: filepath.Join(root, "run"), StateDirectory: filepath.Join(root, "state"),
		SingBoxBinary: "/test/sing-box", NFTBinary: "/test/nft", GeoDataDirectory: seedDirectory,
	})
	compiled := compiler.Compile(value, backend.CompilerOptions())
	candidate, err := backend.Prepare(context.Background(), value, compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(candidate.Directory)
	for _, call := range runner.calls {
		if strings.Contains(call, "geoview") || strings.Contains(call, "rule-set compile") {
			t.Fatalf("device-side Geo conversion was invoked: %#v", runner.calls)
		}
	}
}

func writeTestSeed(t *testing.T, root, kind, category string) string {
	t.Helper()
	seedDirectory := filepath.Join(root, "seed")
	rulesDirectory := filepath.Join(seedDirectory, "rules")
	if err := os.MkdirAll(rulesDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	tag := "steer-" + kind + "-" + category
	content := []byte("compiled\n")
	rulePath := filepath.Join(rulesDirectory, tag+".srs")
	if err := os.WriteFile(rulePath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	inputSum := sha256.Sum256([]byte("input"))
	manifest := geodata.Manifest{
		SchemaVersion: geodata.ManifestSchemaVersion,
		Upstream: geodata.UpstreamIdentity{
			Repository: geodata.UpstreamRepository, Version: "test",
			GeoSiteSHA256: hex.EncodeToString(inputSum[:]), GeoIPSHA256: hex.EncodeToString(inputSum[:]),
		},
		Tools: geodata.ToolIdentity{GeoViewRef: geodata.GeoViewCommit, SingBoxVersion: geodata.SingBoxCompiler},
		Rules: []geodata.Rule{{
			Kind: kind, Category: category, Tag: tag, Path: "rules/" + tag + ".srs",
			SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content)),
		}},
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDirectory, "manifest.json"), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	return seedDirectory
}
