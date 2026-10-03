// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/generation"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

type recordingProbeTransport struct {
	urls []string
}

func (transport *recordingProbeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.urls = append(transport.urls, request.URL.String())
	return &http.Response{
		StatusCode: http.StatusNoContent,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    request,
	}, nil
}

func TestTemporaryProbeConfigUsesSharedBootstrapAndRoute(t *testing.T) {
	config := temporaryProbeConfig(
		model.Bootstrap{Protocol: "udp", Server: "1.1.1.1", ServerPort: 53, Strategy: "prefer_ipv4"},
		[]any{map[string]any{"type": "direct", "tag": "route"}},
		"route",
		12345,
	)
	route := config["route"].(map[string]any)
	if route["final"] != "route" || route["auto_detect_interface"] != true {
		t.Fatalf("unexpected temporary route: %#v", route)
	}
	inbound := config["inbounds"].([]any)[0].(map[string]any)
	if inbound["listen_port"] != 12345 || inbound["type"] != "mixed" {
		t.Fatalf("unexpected temporary inbound: %#v", inbound)
	}
}

func TestProbeOverviewUsesSavedTargetAndRecordsCurrentEnvironment(t *testing.T) {
	paths, err := NewPaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	active := loadProbeTestIntent(t)
	active.Main.Enabled = true
	active.Main.ProbeProxyURL = "https://active.example/proxy"
	active.Main.SpeedtestProxyURL = "https://active.example/download"
	publishProbeTestIntent(t, paths, active)

	saved := active
	saved.Main.ProbeProxyURL = "https://saved.example/proxy"
	saved.Main.SpeedtestProxyURL = "https://saved.example/download"
	writeProbeTestIntent(t, paths.ConfigPath, saved)

	transport := &recordingProbeTransport{}
	result, err := ProbeOverview(context.Background(), paths.ConfigPath, paths.Root, "proxy", &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	activeDigest := compiler.IntentDigest(active)
	if result.ActiveGeneration != activeDigest || result.ActiveDigest != activeDigest {
		t.Fatalf("probe identity = %q/%q, want %q", result.ActiveGeneration, result.ActiveDigest, activeDigest)
	}
	if result.TestedAt.IsZero() || result.Scope != "overview" || result.Kind != "proxy" || !result.OK {
		t.Fatalf("unexpected overview probe report: %#v", result)
	}
	if result.SavedDigest != compiler.IntentDigest(saved) {
		t.Fatalf("probe did not bind Saved identity: %#v", result)
	}
	if len(transport.urls) != 1 || transport.urls[0] != saved.Main.ProbeProxyURL {
		t.Fatalf("overview probe did not use the Saved target: %#v", transport.urls)
	}
}

func TestProbeOverviewRunsWithoutActiveGeneration(t *testing.T) {
	paths, err := NewPaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	saved := loadProbeTestIntent(t)
	saved.Main.Enabled = false
	writeProbeTestIntent(t, paths.ConfigPath, saved)

	for _, kind := range []string{"proxy", "speedtest"} {
		t.Run(kind, func(t *testing.T) {
			transport := &recordingProbeTransport{}
			report, err := ProbeOverview(context.Background(), paths.ConfigPath, paths.Root, kind, &http.Client{Transport: transport})
			if err != nil {
				t.Fatal(err)
			}
			if !report.OK || report.SavedDigest == "" || report.ActiveGeneration != "" || len(transport.urls) != 1 {
				t.Fatalf("probe without Active generation did not use the current network environment: report=%#v urls=%#v", report, transport.urls)
			}
		})
	}
}

func loadProbeTestIntent(t *testing.T) model.Intent {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "linux", "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	value, err := model.DecodeJSON(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func publishProbeTestIntent(t *testing.T, paths Paths, value model.Intent) {
	t.Helper()
	prepared, err := prepareTestGeneration(value, paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Publish(prepared); err != nil {
		t.Fatal(err)
	}
}

func writeProbeTestIntent(t *testing.T, path string, value model.Intent) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := model.EncodeJSON(&encoded, value); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func prepareTestGeneration(value model.Intent, paths Paths) (PreparedGeneration, error) {
	validation := Validate(value)
	if !validation.OK {
		return PreparedGeneration{}, ValidationError{Validation: validation}
	}
	if err := paths.Ensure(); err != nil {
		return PreparedGeneration{}, err
	}
	plan := NewPlan(value)
	compiled := compiler.Compile(value, plan.CompilerOptions(paths.StateDirectory))
	candidate, err := generation.Create(paths.GenerationsDirectory, value, compiled.SingBox)
	if err != nil {
		return PreparedGeneration{}, err
	}
	keepCandidate := false
	defer func() {
		if !keepCandidate {
			_ = os.RemoveAll(candidate.Directory)
		}
	}()
	metadata := GenerationMetadata{
		SchemaVersion: RuntimeSchemaVersion,
		GenerationID:  compiled.IntentDigest,
		IntentDigest:  compiled.IntentDigest,
	}
	if err := generation.WriteJSON(filepath.Join(candidate.Directory, "macos.json"), plan); err != nil {
		return PreparedGeneration{}, err
	}
	if err := generation.WriteJSON(filepath.Join(candidate.Directory, "generation.json"), metadata); err != nil {
		return PreparedGeneration{}, err
	}
	keepCandidate = true
	return PreparedGeneration{Candidate: candidate, Metadata: metadata}, nil
}
