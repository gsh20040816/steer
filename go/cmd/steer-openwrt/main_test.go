// SPDX-License-Identifier: GPL-3.0-or-later
package main

// CLI tests lock the OpenWrt command surface and Apply serialization.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreapply "github.com/gsh20040816/steer/go/internal/apply"
	model "github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/subscription"
)

func TestExportPackagedFreshDefaultsAsCanonicalIntent(t *testing.T) {
	configPath := filepath.Join("..", "..", "..", "steer", "files", "etc", "config", "steer")
	output := captureStdout(t, func() error {
		return runExportIntent([]string{"--config", configPath})
	})
	var value model.Intent
	if err := json.Unmarshal(output, &value); err != nil {
		t.Fatalf("decode Canonical export: %v\n%s", err, output)
	}
	if validation := model.Validate(value); !validation.OK {
		t.Fatalf("fresh Canonical export is invalid: %#v", validation.Errors)
	}
	if len(value.Routes) != 2 || value.Routes[0] != (model.Route{ID: "direct", Enabled: true, Kind: "direct"}) ||
		value.Routes[1] != (model.Route{ID: "block", Enabled: false, Kind: "block"}) {
		t.Fatalf("unexpected fresh Canonical routes: %#v", value.Routes)
	}
	var document map[string]any
	if err := json.Unmarshal(output, &document); err != nil {
		t.Fatal(err)
	}
	for _, raw := range document["routes"].([]any) {
		if _, exists := raw.(map[string]any)["name"]; exists {
			t.Fatalf("optional empty route name leaked into Canonical export: %s", output)
		}
	}
}

func TestRuntimeReportsStructuredSingBoxVersionAndBuildTags(t *testing.T) {
	root := t.TempDir()
	singBox := filepath.Join(root, "sing-box")
	if err := os.WriteFile(singBox, []byte("#!/bin/sh\nprintf 'sing-box version 1.14.0\\nTags: with_quic,with_utls\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() error {
		return runRuntime([]string{"--sing-box", singBox, "--geodata", filepath.Join(root, "missing-geodata")})
	})
	var result struct {
		SingBox struct {
			Version string   `json:"version"`
			Tags    []string `json:"tags"`
		} `json:"sing_box"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	if result.SingBox.Version != "1.14.0" || strings.Join(result.SingBox.Tags, ",") != "with_quic,with_utls" {
		t.Fatalf("unexpected structured sing-box runtime facts: %s", output)
	}
}

func TestParseNodesWritesPrivateExclusiveResult(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "result.json")
	document := "vless://00000000-0000-4000-8000-000000000001@example.com:443?security=tls&sni=edge.example.com&type=ws&path=%2Fproxy\n"
	runWithDocument := func() error {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.WriteString(document); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		original := os.Stdin
		os.Stdin = reader
		defer func() {
			os.Stdin = original
			reader.Close()
		}()
		return runParseNodes([]string{"--output", outputPath})
	}
	if err := runWithDocument(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("node import result permissions = %04o, want 0600", info.Mode().Perm())
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Nodes []model.Node `json:"nodes"`
	}
	if err := json.Unmarshal(content, &result); err != nil || len(result.Nodes) != 1 || result.Nodes[0].Type != "vless" {
		t.Fatalf("unexpected private node import result: %v %#v\n%s", err, result, content)
	}
	if err := runWithDocument(); err == nil || !strings.Contains(err.Error(), "create private node import result") {
		t.Fatalf("existing result was not protected by exclusive creation: %v", err)
	}
}

func TestRunExportNodeSerializesConfiguredNode(t *testing.T) {
	defaults, err := os.ReadFile(filepath.Join("..", "..", "..", "steer", "files", "etc", "config", "steer"))
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "steer")
	configured := string(defaults) + `

config node 'node_main'
	option enabled '1'
	option name 'Edge'
	option type 'vless'
	option server 'proxy.example'
	option server_port '443'
	option uuid '00000000-0000-4000-8000-000000000001'
	option tls_server_name 'edge.example'
`
	if err := os.WriteFile(configPath, []byte(configured), 0o600); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() error {
		return runExportNode([]string{"--config", configPath, "--id", "node_main"})
	})
	var result struct {
		OK  bool   `json:"ok"`
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	parsed, err := subscription.ParseURI(result.URI)
	if err != nil || !result.OK || parsed.Type != "vless" {
		t.Fatalf("unexpected node export: result=%#v parsed=%#v err=%v output=%s", result, parsed, err, output)
	}
}

func captureStdout(t *testing.T, run func() error) []byte {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	runErr := run()
	os.Stdout = original
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	output, readErr := io.ReadAll(reader)
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
	return output
}

func TestApplyRecordFailurePreservesOperationError(t *testing.T) {
	runDirectory := t.TempDir()
	if err := os.Mkdir(filepath.Join(runDirectory, "last-apply.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	original := errors.New("original apply failure")
	err := runLockedApplyOutput(runDirectory, func() (coreapply.Result, error) {
		return coreapply.Result{}, original
	}, func(coreapply.Result) {})
	if !errors.Is(err, original) || !strings.Contains(err.Error(), "publish Apply result") {
		t.Fatalf("Apply errors were not preserved together: %v", err)
	}
}

func TestApplyLockSerializesTransactions(t *testing.T) {
	runDirectory := t.TempDir()
	first, err := acquireApplyLock(runDirectory)
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan error, 1)
	release := make(chan struct{})
	go func() {
		second, err := acquireApplyLock(runDirectory)
		acquired <- err
		if err == nil {
			<-release
			second.Close()
		}
	}()
	select {
	case err := <-acquired:
		t.Fatalf("second Apply did not wait for the first lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatal(err)
		}
		close(release)
	case <-time.After(time.Second):
		t.Fatal("second Apply did not acquire the released lock")
	}
}

func TestApplyLockWaitHonorsContextDeadline(t *testing.T) {
	runDirectory := t.TempDir()
	first, err := acquireApplyLock(runDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = acquireApplyLockContext(ctx, runDirectory)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Apply lock wait returned %v", err)
	}
}

func TestSubscriptionSubcommandParsesTrailingFlags(t *testing.T) {
	err := runSubscription([]string{"status", "--config", "/definitely/missing/steer"})
	if err == nil || !strings.Contains(err.Error(), "read UCI for subscription status") {
		t.Fatalf("trailing flags were not parsed by the status subcommand: %v", err)
	}
}

func TestProbeParsesRouteAndRejectsAmbiguousTargets(t *testing.T) {
	err := runProbe([]string{"--kind", "speedtest", "--route", "route_a", "--config", "/definitely/missing/steer"})
	if err == nil || !strings.Contains(err.Error(), "read UCI for route test") {
		t.Fatalf("route test flags were not parsed: %v", err)
	}
	err = runProbe([]string{"--kind", "speedtest", "--node", "node_a", "--route", "route_a"})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("ambiguous node and route test was accepted: %v", err)
	}
}

func TestProbeBusinessFailureReturnsPersistedLatestDTO(t *testing.T) {
	configPath := filepath.Join("..", "..", "..", "steer", "files", "etc", "config", "steer")
	stateDirectory := t.TempDir()
	output := captureStdout(t, func() error {
		return runProbe([]string{
			"--kind", "speedtest", "--node", "missing-node", "--config", configPath,
			"--state-dir", stateDirectory, "--run-dir", t.TempDir(),
		})
	})
	var result struct {
		Scope        string `json:"scope"`
		ObjectID     string `json:"object_id"`
		OK           bool   `json:"ok"`
		Stale        bool   `json:"stale"`
		ErrorSummary string `json:"error_summary"`
	}
	if err := json.Unmarshal(output, &result); err != nil || result.OK || result.Stale || result.Scope != "nodes" || result.ObjectID != "missing-node" || result.ErrorSummary == "" {
		t.Fatalf("probe failure did not immediately return the backend DTO: %v %#v\n%s", err, result, output)
	}
}

func TestUsageExposesOnlyFrozenPublicCommands(t *testing.T) {
	message := usage().Error()
	for _, command := range []string{"version", "validate", "apply", "health", "status", "probe", "subscription", "geo-catalog", "cleanup"} {
		if !strings.Contains(message, command) {
			t.Fatalf("public command %q is missing from usage: %s", command, message)
		}
	}
	for _, removed := range []string{"compile", "plan", "prepare", "capabilities", "rollback", "migrate", "_start"} {
		if strings.Contains(message, removed) {
			t.Fatalf("non-public command %q leaked into usage: %s", removed, message)
		}
	}
}
