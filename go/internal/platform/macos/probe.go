// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gsh20040816/steer/go/internal/compiler"
	model "github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/probe"
)

type TestReport = probe.Report

func ProbeOverview(ctx context.Context, configPath, runDirectory, kind string, client *http.Client) (TestReport, error) {
	value, err := readProbeIntent(configPath)
	if err != nil {
		return TestReport{}, fmt.Errorf("load saved macOS configuration for probe: %w", err)
	}
	target, download := "", false
	switch kind {
	case "direct":
		target = value.Main.ProbeDirectURL
	case "proxy":
		target = value.Main.ProbeProxyURL
	case "speedtest":
		target, download = value.Main.SpeedtestProxyURL, true
	default:
		return TestReport{}, fmt.Errorf("unsupported probe kind %q", kind)
	}
	if target == "" {
		return TestReport{}, fmt.Errorf("saved macOS intent has no %s HTTPS probe", kind)
	}
	if client == nil {
		client = probe.HTTPClient(nil, download)
	}
	report := probe.Run(ctx, client, "overview", "", kind, target, download)
	report.SavedDigest = compiler.IntentDigest(value)
	if current, _, err := runtimePaths(runDirectory, "").LoadCurrentIntent(); err == nil {
		report.ActiveGeneration = current.GenerationID
		report.ActiveDigest = current.IntentDigest
	}
	report = probe.SanitizeReport(report)
	return report, nil
}

func SpeedTestNode(ctx context.Context, configPath, singBoxPath, nodeID string, download bool) (TestReport, error) {
	value, err := readProbeIntent(configPath)
	if err != nil {
		return TestReport{}, err
	}
	var node model.Node
	found := false
	for _, candidate := range value.Nodes {
		if candidate.ID == nodeID {
			node, found = candidate, true
			break
		}
	}
	if !found || !node.Enabled {
		return TestReport{}, fmt.Errorf("enabled node %q was not found", nodeID)
	}
	target, kind := value.Main.ProbeProxyURL, "connect"
	if download {
		target, kind = value.Main.SpeedtestProxyURL, "download"
	}
	report, err := runTemporaryProbe(ctx, singBoxPath, value.Bootstrap, []any{compiler.CompileNodeOutbound(node)}, compiler.NodeOutboundTag(node.ID), "nodes", node.ID, kind, target, download)
	if err != nil {
		return TestReport{}, err
	}
	report.SavedDigest = compiler.IntentDigest(value)
	return probe.SanitizeReport(report), nil
}

func SpeedTestRoute(ctx context.Context, configPath, singBoxPath, routeID string, download bool) (TestReport, error) {
	value, err := readProbeIntent(configPath)
	if err != nil {
		return TestReport{}, err
	}
	var route model.Route
	found := false
	for _, candidate := range value.Routes {
		if candidate.ID == routeID {
			route, found = candidate, true
			break
		}
	}
	if !found || !route.Enabled || route.Kind != "single" {
		return TestReport{}, fmt.Errorf("enabled single-node route %q was not found", routeID)
	}
	outbounds := compiler.CompileRouteChainOutbounds(value, route.ID)
	if len(outbounds) == 0 {
		return TestReport{}, fmt.Errorf("compiled route test has no route-chain outbounds")
	}
	target, kind := value.Main.ProbeProxyURL, "connect"
	if download {
		target, kind = value.Main.SpeedtestProxyURL, "download"
	}
	report, err := runTemporaryProbe(ctx, singBoxPath, value.Bootstrap, outbounds, compiler.RouteOutboundTag(route.ID), "routes", route.ID, kind, target, download)
	if err != nil {
		return TestReport{}, err
	}
	report.SavedDigest = compiler.IntentDigest(value)
	return probe.SanitizeReport(report), nil
}

func runTemporaryProbe(ctx context.Context, singBoxPath string, bootstrap model.Bootstrap, outbounds []any, finalTag, scope, objectID, kind, target string, download bool) (TestReport, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return TestReport{}, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	temporary, err := os.MkdirTemp("", "steer-macos-test.")
	if err != nil {
		return TestReport{}, err
	}
	defer os.RemoveAll(temporary)
	config := temporaryProbeConfig(bootstrap, outbounds, finalTag, port)
	configPath := filepath.Join(temporary, "sing-box.json")
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return TestReport{}, err
	}
	if err := os.WriteFile(configPath, append(encoded, '\n'), 0o600); err != nil {
		return TestReport{}, err
	}
	if output, err := exec.CommandContext(ctx, singBoxPath, "check", "-c", configPath).CombinedOutput(); err != nil {
		return TestReport{}, fmt.Errorf("sing-box test config check failed: %w: %s", err, output)
	}
	process := exec.CommandContext(ctx, singBoxPath, "run", "-c", configPath)
	var diagnostics bytes.Buffer
	process.Stdout, process.Stderr = io.Discard, &diagnostics
	if err := process.Start(); err != nil {
		return TestReport{}, err
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = process.Process.Kill()
		_ = process.Wait()
	}
	defer stop()
	if err := waitTemporaryProbeReady(ctx, port); err != nil {
		return TestReport{}, err
	}
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	report := probe.Run(ctx, probe.HTTPClient(proxyURL, download), scope, objectID, kind, target, download)
	stop()
	if diagnostic := strings.TrimSpace(diagnostics.String()); !report.OK && diagnostic != "" {
		report.Error = "temporary sing-box: " + diagnostic
	}
	return report, nil
}

func temporaryProbeConfig(bootstrap model.Bootstrap, outbounds []any, finalTag string, port int) map[string]any {
	return map[string]any{
		"log": map[string]any{"level": "error"},
		"dns": map[string]any{"servers": []any{map[string]any{
			"type": bootstrap.Protocol, "tag": "steer-dns-bootstrap", "server": bootstrap.Server, "server_port": bootstrap.ServerPort,
		}}},
		"inbounds":  []any{map[string]any{"type": "mixed", "tag": "test-in", "listen": "127.0.0.1", "listen_port": port}},
		"outbounds": outbounds,
		"route": map[string]any{"final": finalTag, "auto_detect_interface": true,
			"default_domain_resolver": map[string]any{"server": "steer-dns-bootstrap", "strategy": bootstrap.Strategy}},
	}
}

func waitTemporaryProbeReady(ctx context.Context, port int) error {
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("temporary sing-box did not become ready: %w", err)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func readProbeIntent(configPath string) (model.Intent, error) {
	value, _, err := (IntentStore{Paths: Paths{ConfigPath: configPath}}).Load()
	if err != nil {
		return model.Intent{}, err
	}
	validation := Validate(value)
	if !validation.OK {
		return model.Intent{}, ValidationError{Validation: validation}
	}
	return value, nil
}
