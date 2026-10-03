// SPDX-License-Identifier: GPL-3.0-or-later

package linux

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
	"path/filepath"
	"strings"
	"time"

	"github.com/gsh20040816/steer/go/internal/compiler"
	model "github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/probe"
)

type TestReport = probe.Report
type TestResult = probe.Result

func ProbeOverviewWithState(ctx context.Context, configPath, runDirectory, stateDirectory, kind string, client *http.Client) (TestReport, error) {
	intent, err := readProbeIntent(configPath)
	if err != nil {
		return TestReport{}, recordProbeFailure(stateDirectory, "overview", "", kind, err, probe.Identity{})
	}
	identity := probe.Identity{SavedDigest: compiler.IntentDigest(intent)}
	if generationID, _, _, activeIdentity, err := readCurrentIdentity(BackendOptions{RunDirectory: runDirectory}); err == nil {
		identity.ActiveGeneration = generationID
		identity.ActiveDigest = activeIdentity.IntentDigest
	}
	target, download := "", false
	switch kind {
	case "direct":
		target = intent.Main.ProbeDirectURL
	case "proxy":
		target = intent.Main.ProbeProxyURL
	case "speedtest":
		target, download = intent.Main.SpeedtestProxyURL, true
	default:
		err := fmt.Errorf("unsupported probe kind %q", kind)
		return TestReport{}, recordProbeFailure(stateDirectory, "overview", "", kind, err, identity)
	}
	if target == "" {
		err := fmt.Errorf("saved intent has no %s HTTPS probe", kind)
		return TestReport{}, recordProbeFailure(stateDirectory, "overview", "", kind, err, identity)
	}
	if client == nil {
		client = probe.HTTPClient(nil, download)
	}
	report := probe.Run(ctx, client, "overview", "", kind, target, download)
	report = probe.BindReportIdentity(report, identity)
	report = probe.SanitizeReport(report)
	if stateDirectory != "" {
		if err := probe.SaveReport(stateDirectory, report); err != nil {
			return report, err
		}
	}
	return report, nil
}

func SpeedTestNode(ctx context.Context, configPath, stateDirectory, singBoxPath, nodeID string, download bool) (TestReport, error) {
	value, err := readProbeIntent(configPath)
	if err != nil {
		return TestReport{}, recordProbeFailure(stateDirectory, "nodes", nodeID, probeKind(download), err, probe.Identity{})
	}
	identity := probe.Identity{SavedDigest: compiler.IntentDigest(value)}
	var node model.Node
	found := false
	for _, candidate := range value.Nodes {
		if candidate.ID == nodeID {
			node, found = candidate, true
			break
		}
	}
	if !found || !node.Enabled {
		err := fmt.Errorf("enabled node %q was not found", nodeID)
		return TestReport{}, recordProbeFailure(stateDirectory, "nodes", nodeID, probeKind(download), err, identity)
	}
	target, kind := value.Main.ProbeProxyURL, "connect"
	if download {
		target, kind = value.Main.SpeedtestProxyURL, "download"
	}
	report, err := runTemporaryProbe(ctx, singBoxPath, value.Bootstrap, []any{compiler.CompileNodeOutbound(node)}, compiler.NodeOutboundTag(node.ID), "nodes", node.ID, kind, target, download)
	if err != nil {
		return TestReport{}, recordProbeFailure(stateDirectory, "nodes", nodeID, kind, err, identity)
	}
	report = probe.BindReportIdentity(report, identity)
	report = probe.SanitizeReport(report)
	if err := probe.SaveReport(stateDirectory, report); err != nil {
		return report, err
	}
	return report, nil
}

func SpeedTestRoute(ctx context.Context, configPath, stateDirectory, singBoxPath, routeID string, download bool) (TestReport, error) {
	value, err := readProbeIntent(configPath)
	if err != nil {
		return TestReport{}, recordProbeFailure(stateDirectory, "routes", routeID, probeKind(download), err, probe.Identity{})
	}
	identity := probe.Identity{SavedDigest: compiler.IntentDigest(value)}
	var route model.Route
	found := false
	for _, candidate := range value.Routes {
		if candidate.ID == routeID {
			route, found = candidate, true
			break
		}
	}
	if !found || !route.Enabled || route.Kind != "single" {
		err := fmt.Errorf("enabled single-node route %q was not found", routeID)
		return TestReport{}, recordProbeFailure(stateDirectory, "routes", routeID, probeKind(download), err, identity)
	}
	outbounds := compiler.CompileRouteChainOutbounds(value, route.ID)
	if len(outbounds) == 0 {
		err := fmt.Errorf("compiled route test has no route-chain outbounds")
		return TestReport{}, recordProbeFailure(stateDirectory, "routes", routeID, probeKind(download), err, identity)
	}
	target, kind := value.Main.ProbeProxyURL, "connect"
	if download {
		target, kind = value.Main.SpeedtestProxyURL, "download"
	}
	report, err := runTemporaryProbe(ctx, singBoxPath, value.Bootstrap, outbounds, compiler.RouteOutboundTag(route.ID), "routes", route.ID, kind, target, download)
	if err != nil {
		return TestReport{}, recordProbeFailure(stateDirectory, "routes", routeID, kind, err, identity)
	}
	report = probe.BindReportIdentity(report, identity)
	report = probe.SanitizeReport(report)
	if err := probe.SaveReport(stateDirectory, report); err != nil {
		return report, err
	}
	return report, nil
}

func runTemporaryProbe(ctx context.Context, singBoxPath string, bootstrap model.Bootstrap, outbounds []any, finalTag, scope, objectID, kind, target string, download bool) (TestReport, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return TestReport{}, fmt.Errorf("allocate test listener: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	temporary, err := os.MkdirTemp("", "steer-test.")
	if err != nil {
		return TestReport{}, fmt.Errorf("create test directory: %w", err)
	}
	defer os.RemoveAll(temporary)
	config, err := temporaryProbeConfig(bootstrap, outbounds, finalTag, port)
	if err != nil {
		return TestReport{}, err
	}
	configPath := filepath.Join(temporary, "sing-box.json")
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return TestReport{}, fmt.Errorf("encode test config: %w", err)
	}
	if err := os.WriteFile(configPath, append(encoded, '\n'), 0o600); err != nil {
		return TestReport{}, fmt.Errorf("write test config: %w", err)
	}
	check := newManagedCommand(ctx, singBoxPath, "check", "-c", configPath)
	if output, err := check.CombinedOutput(); err != nil {
		return TestReport{}, fmt.Errorf("sing-box test config check failed: %w: %s", err, output)
	}
	process := newManagedCommand(ctx, singBoxPath, "run", "-c", configPath)
	var diagnostics bytes.Buffer
	process.Stdout, process.Stderr = io.Discard, &diagnostics
	if err := process.Start(); err != nil {
		return TestReport{}, fmt.Errorf("start temporary sing-box: %w", err)
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

func temporaryProbeConfig(bootstrap model.Bootstrap, outbounds []any, finalTag string, port int) (map[string]any, error) {
	marked := make([]any, 0, len(outbounds))
	for _, value := range outbounds {
		outbound, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("temporary probe outbound has unexpected type %T", value)
		}
		copy := make(map[string]any, len(outbound)+1)
		for key, field := range outbound {
			copy[key] = field
		}
		// The main Linux auto-redirect path must not capture a probe that is
		// intentionally testing a different node or route.
		copy["routing_mark"] = AutoRedirectOutputMark
		marked = append(marked, copy)
	}
	return map[string]any{
		"log": map[string]any{"level": "error"},
		"dns": map[string]any{
			"servers": []any{map[string]any{
				"type": bootstrap.Protocol, "tag": "steer-dns-bootstrap", "server": bootstrap.Server,
				"server_port": bootstrap.ServerPort, "routing_mark": AutoRedirectOutputMark,
			}},
		},
		"inbounds":  []any{map[string]any{"type": "mixed", "tag": "test-in", "listen": "127.0.0.1", "listen_port": port}},
		"outbounds": marked,
		"route": map[string]any{"final": finalTag, "auto_detect_interface": true,
			"default_domain_resolver": map[string]any{"server": "steer-dns-bootstrap", "strategy": bootstrap.Strategy}},
	}, nil
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
			return fmt.Errorf("temporary sing-box did not become ready: %w", ctx.Err())
		case <-deadline.C:
			return fmt.Errorf("temporary sing-box did not become ready: %w", err)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func readProbeIntent(path string) (model.Intent, error) {
	value, _, err := (IntentStore{Path: path}).Load()
	if err != nil {
		return model.Intent{}, err
	}
	validation := Validate(value)
	if !validation.OK {
		return model.Intent{}, ValidationError{Validation: validation}
	}
	return value, nil
}

func recordProbeFailure(stateDirectory, scope, objectID, kind string, probeErr error, identity probe.Identity) error {
	if kind != "direct" && kind != "proxy" && kind != "speedtest" && kind != "connect" && kind != "download" {
		kind = "connect"
	}
	failure := probe.BindReportIdentity(probe.FailureReport(scope, objectID, kind, probeErr), identity)
	if saveErr := probe.SaveReport(stateDirectory, failure); saveErr != nil {
		return fmt.Errorf("%w; save sanitized probe failure: %v", probeErr, saveErr)
	}
	return probeErr
}

func probeKind(download bool) string {
	if download {
		return "download"
	}
	return "connect"
}
