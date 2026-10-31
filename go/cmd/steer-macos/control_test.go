// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreapply "github.com/gsh20040816/steer/go/internal/apply"
	"github.com/gsh20040816/steer/go/internal/geodata"
	model "github.com/gsh20040816/steer/go/internal/intent"
	macosplatform "github.com/gsh20040816/steer/go/internal/platform/macos"
	"github.com/gsh20040816/steer/go/internal/probe"
)

func TestAuthorizedControlPeer(t *testing.T) {
	if !authorizedControlPeer(0, nil, 80) {
		t.Fatal("root must be authorized")
	}
	if !authorizedControlPeer(501, []uint32{20, 80}, 80) {
		t.Fatal("admin group member must be authorized")
	}
	if authorizedControlPeer(501, []uint32{20}, 80) {
		t.Fatal("non-admin user must not be authorized")
	}
}

func TestControlServiceApplyUsesOnlyStructuredHooks(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("..", "..", "..", "linux", "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	written := false
	applied := false
	service := &controlService{
		configPath: "/fixed/config.json",
		options:    macosplatform.BackendOptions{StateDirectory: t.TempDir()},
		adminGID:   80,
		revision: func(string) (string, error) {
			return controlRevision(document), nil
		},
		write: func(path string, content []byte, gid int) error {
			if path != "/fixed/config.json" || gid != 80 || string(content) != string(document) {
				t.Fatalf("unexpected structured save input: path=%q gid=%d", path, gid)
			}
			written = true
			return nil
		},
		apply: func(value model.Intent, _ macosplatform.BackendOptions) (coreapply.Result, error) {
			if value.Main.SchemaVersion != model.SchemaVersion {
				t.Fatalf("unexpected canonical schema %d", value.Main.SchemaVersion)
			}
			applied = true
			return coreapply.Result{OK: true, Activated: true, Restarted: false}, nil
		},
		status: func(macosplatform.BackendOptions) macosplatform.Status {
			return macosplatform.DefaultStatus()
		},
	}
	response := service.handle(controlRequest{
		SchemaVersion:    controlSchemaVersion,
		Operation:        "apply",
		Document:         string(document),
		ExpectedRevision: controlRevision(document),
	})
	if !response.OK || response.Status == nil || response.Validation == nil || !response.Validation.OK || !written || !applied {
		t.Fatalf("unexpected response or hook state: response=%+v written=%v applied=%v", response, written, applied)
	}
	if response.ApplyResult == nil || !response.ApplyResult.Activated || response.ApplyResult.Restarted {
		t.Fatalf("control apply must return the Apply result with restarted: %+v", response.ApplyResult)
	}
	encoded, _ := json.Marshal(response)
	if !strings.Contains(string(encoded), `"restarted":false`) {
		t.Fatalf("control response omitted restarted: %s", encoded)
	}
}

func TestControlServiceRejectsUnrestrictedOperations(t *testing.T) {
	service := &controlService{}
	response := service.handle(controlRequest{
		SchemaVersion: controlSchemaVersion,
		Operation:     "shell",
		Document:      `{}`,
	})
	if response.OK || response.Error != "unsupported control operation" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestControlServiceProbeForwardsOnlyStructuredSelection(t *testing.T) {
	called := false
	service := &controlService{
		configPath: "/fixed/config.json",
		options:    macosplatform.BackendOptions{StateDirectory: t.TempDir()},
		probe: func(_ context.Context, configPath string, options macosplatform.BackendOptions, selection probeSelection) (probeResponse, error) {
			called = true
			if configPath != "/fixed/config.json" || options.RunDirectory != "" {
				t.Fatalf("probe received unexpected fixed paths: %q %#v", configPath, options)
			}
			if selection.Kind != "speedtest" || selection.NodeID != "node-a" || selection.RouteID != "" || !selection.Download {
				t.Fatalf("probe received unexpected selection: %#v", selection)
			}
			return probeResponse{
				TestReport: macosplatform.TestReport{Scope: "nodes", ObjectID: "node-a", Kind: "download", OK: false, TestedAt: time.Now()},
			}, nil
		},
	}
	response := service.handle(controlRequest{
		SchemaVersion: controlSchemaVersion,
		Operation:     "probe",
		Kind:          "speedtest",
		NodeID:        "node-a",
		Download:      true,
	})
	if !called || !response.OK || len(response.Payload) == 0 {
		t.Fatalf("structured probe was not returned: called=%v response=%+v", called, response)
	}
	var report probe.LatestProbeResult
	if err := decodeStrictJSON(response.Payload, &report); err != nil {
		t.Fatal(err)
	}
	if report.OK || report.Scope != "nodes" || report.ObjectID != "node-a" {
		t.Fatalf("HTTP probe failure was confused with control failure: %#v", report)
	}
}

func TestControlServiceExposesSafeLatestProbeResultsSeparatelyFromDiagnostics(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("..", "..", "..", "linux", "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, document, 0o600); err != nil {
		t.Fatal(err)
	}
	options := macosplatform.BackendOptions{StateDirectory: filepath.Join(root, "state")}
	if err := macosplatform.SaveTestReport(options, probe.Report{
		Scope: "nodes", ObjectID: "node-a", Kind: "connect", TestedAt: time.Now(),
		Error: "temporary sing-box private_key=secret", Results: []probe.Result{{URL: "https://user:token@example.test/probe?token=secret"}},
	}); err != nil {
		t.Fatal(err)
	}
	service := &controlService{configPath: configPath, options: options}
	response := service.handle(controlRequest{SchemaVersion: controlSchemaVersion, Operation: "diagnostics"})
	if !response.OK {
		t.Fatalf("diagnostics request failed: %+v", response)
	}
	var diagnostics probe.Diagnostics
	if err := decodeStrictJSON(response.Payload, &diagnostics); err != nil {
		t.Fatalf("diagnostics payload drifted: %v %#v", err, diagnostics)
	}
	latestResponse := service.handle(controlRequest{SchemaVersion: controlSchemaVersion, Operation: "probe-results"})
	var latest probe.LatestProbeResults
	if !latestResponse.OK || decodeStrictJSON(latestResponse.Payload, &latest) != nil || len(latest.Results) != 1 || latest.Results[0].ErrorSummary != "请查看诊断日志" {
		t.Fatalf("latest probe results payload drifted: %+v %#v", latestResponse, latest)
	}
	encoded := string(response.Payload) + string(latestResponse.Payload)
	for _, secret := range []string{"user:token", "secret", "private_key", "temporary sing-box"} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("diagnostics payload leaked %q: %s", secret, encoded)
		}
	}
	for _, forbidden := range []string{"\"reports\"", "saved_digest", "active_digest", "\"results\":"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("ordinary probe contract exposed %q: %s", forbidden, encoded)
		}
	}
}

func TestControlServiceOverviewProbeRunsWithoutActiveGeneration(t *testing.T) {
	called := false
	service := &controlService{
		options: macosplatform.BackendOptions{StateDirectory: t.TempDir()},
		probe: func(context.Context, string, macosplatform.BackendOptions, probeSelection) (probeResponse, error) {
			called = true
			return probeResponse{TestReport: macosplatform.TestReport{
				Scope: "overview", Kind: "proxy", OK: true, SavedDigest: "saved-a", TestedAt: time.Now(),
			}}, nil
		},
	}
	response := service.handle(controlRequest{
		SchemaVersion: controlSchemaVersion,
		Operation:     "probe",
		Kind:          "proxy",
	})
	if !response.OK || !called {
		t.Fatalf("overview probe without Active was rejected: called=%v response=%+v", called, response)
	}
}

func TestControlServiceProbeFailureReturnsPersistedLatestDTO(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("..", "..", "..", "linux", "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, document, 0o600); err != nil {
		t.Fatal(err)
	}
	service := &controlService{
		configPath: configPath,
		options:    macosplatform.BackendOptions{StateDirectory: filepath.Join(root, "state")},
		probe: func(context.Context, string, macosplatform.BackendOptions, probeSelection) (probeResponse, error) {
			return probeResponse{}, errors.New("dial tcp: connection refused; password=secret")
		},
	}
	response := service.handle(controlRequest{SchemaVersion: controlSchemaVersion, Operation: "probe", Kind: "proxy"})
	var result probe.LatestProbeResult
	if !response.OK || decodeStrictJSON(response.Payload, &result) != nil || result.OK || result.Stale || result.ErrorSummary != "连接被拒绝" {
		t.Fatalf("probe failure did not immediately return the backend DTO: %+v %#v", response, result)
	}
}

func TestControlServiceOverviewProbeDoesNotRequireStableActiveIdentity(t *testing.T) {
	service := &controlService{
		options: macosplatform.BackendOptions{StateDirectory: t.TempDir()},
		status: func(macosplatform.BackendOptions) macosplatform.Status {
			return macosplatform.Status{
				SchemaVersion: macosplatform.RuntimeSchemaVersion,
				GenerationID:  "generation-changing",
			}
		},
		probe: func(context.Context, string, macosplatform.BackendOptions, probeSelection) (probeResponse, error) {
			return probeResponse{
				TestReport: macosplatform.TestReport{Scope: "overview", Kind: "proxy", OK: true, SavedDigest: "saved-a", TestedAt: time.Now()},
			}, nil
		},
	}
	request := controlRequest{SchemaVersion: controlSchemaVersion, Operation: "probe", Kind: "proxy"}
	if response := service.handle(request); !response.OK {
		t.Fatalf("current-network probe was coupled to Active identity: %+v", response)
	}
}

func TestControlServiceProbeRejectsConfigurationAndPathFields(t *testing.T) {
	service := &controlService{
		probe: func(context.Context, string, macosplatform.BackendOptions, probeSelection) (probeResponse, error) {
			t.Fatal("invalid probe reached backend")
			return probeResponse{}, nil
		},
	}
	response := service.handle(controlRequest{
		SchemaVersion: controlSchemaVersion,
		Operation:     "probe",
		Kind:          "proxy",
		Document:      `{"url":"https://attacker.example/"}`,
	})
	if response.OK || !strings.Contains(response.Error, "only kind") {
		t.Fatalf("unexpected invalid probe response: %+v", response)
	}
	var request controlRequest
	if err := decodeStrictJSON([]byte(`{"schema_version":1,"operation":"probe","kind":"proxy","url":"https://attacker.example/"}`), &request); err == nil {
		t.Fatal("control probe accepted an arbitrary URL field")
	}
}

func TestControlServiceRejectsInvalidDocumentBeforeWrite(t *testing.T) {
	service := &controlService{
		configPath: t.TempDir() + "/config.json",
		revision:   func(string) (string, error) { return "current", nil },
	}
	response := service.handle(controlRequest{
		SchemaVersion:    controlSchemaVersion,
		Operation:        "save",
		Document:         `{"main":`,
		ExpectedRevision: "current",
	})
	if response.OK || !strings.Contains(response.Error, "decode canonical configuration") {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestControlServiceReturnsStructuredGeoValidationBeforeWrite(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("..", "..", "..", "linux", "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	value, err := model.DecodeJSON(bytes.NewReader(document))
	if err != nil {
		t.Fatal(err)
	}
	value.Rules = append([]model.Rule{{
		ID: "geo", Enabled: true, DNSProfile: value.Rules[0].DNSProfile, Route: value.Rules[0].Route,
		DomainMatch: []string{"geosite:cn"},
	}}, value.Rules...)
	var candidate bytes.Buffer
	if err := model.EncodeJSON(&candidate, value); err != nil {
		t.Fatal(err)
	}
	written := false
	service := &controlService{
		configPath: filepath.Join(t.TempDir(), "config.json"),
		revision:   func(string) (string, error) { return "current", nil },
		options: macosplatform.BackendOptions{
			GeoDataDirectory: filepath.Join(t.TempDir(), "missing"),
		},
		write: func(string, []byte, int) error { written = true; return nil },
	}
	response := service.handle(controlRequest{
		SchemaVersion: controlSchemaVersion, Operation: "save", Document: candidate.String(), ExpectedRevision: "current",
	})
	if response.OK || response.Saved || written || response.Validation == nil {
		t.Fatalf("invalid candidate reached persistence: response=%+v written=%v", response, written)
	}
	if response.ErrorCode == controlRevisionRequired || response.ErrorCode == controlRevisionConflict {
		t.Fatalf("Geo validation was masked by revision handling: %+v", response)
	}
	for _, issue := range response.Validation.Errors {
		if issue.Code == geodata.ErrorManifestInvalid && issue.ObjectType == "rule" && issue.ObjectID == "geo" && issue.Option == "domain_match" {
			return
		}
	}
	t.Fatalf("structured manifest issue is missing: %#v", response.Validation.Errors)
}

func TestControlServiceSavedOnlyConfigurationCannotReachApplyHook(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("..", "..", "..", "linux", "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	applyCalls := 0
	writeCalls := 0
	service := &controlService{
		configPath: "/fixed/config.json",
		revision: func(string) (string, error) {
			return controlRevision(document), nil
		},
		write: func(_ string, _ []byte, _ int) error {
			writeCalls++
			return nil
		},
		apply: func(_ model.Intent, _ macosplatform.BackendOptions) (coreapply.Result, error) {
			applyCalls++
			return coreapply.Result{OK: true}, nil
		},
	}
	response := service.handle(controlRequest{
		SchemaVersion:    controlSchemaVersion,
		Operation:        "save",
		Document:         string(document),
		ExpectedRevision: controlRevision(document),
	})
	if !response.OK || !response.Saved || response.Applied {
		t.Fatalf("unexpected save response: %+v", response)
	}
	if writeCalls != 1 || applyCalls != 0 {
		t.Fatalf("saved-only configuration wrote %d time(s) and applied %d time(s)", writeCalls, applyCalls)
	}
}

func TestControlServiceRequiresExpectedRevision(t *testing.T) {
	service := &controlService{}
	response := service.handle(controlRequest{
		SchemaVersion: controlSchemaVersion,
		Operation:     "save",
		Document:      `{}`,
	})
	if response.OK || response.Saved || response.Applied || response.ErrorCode != controlRevisionRequired {
		t.Fatalf("unexpected missing-revision response: %+v", response)
	}
}

func TestControlServiceRejectsStaleGUIRevisionAfterSubscriptionUpdate(t *testing.T) {
	original, err := os.ReadFile(filepath.Join("..", "..", "..", "linux", "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	updatedIntent, err := model.DecodeJSON(bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	updatedIntent.Subscriptions = []model.Subscription{{ID: "public", Enabled: true, URL: "https://subscription.example/nodes"}}
	updatedIntent.Nodes = []model.Node{{
		ID: "public-node", Enabled: true, Type: "socks", Server: "127.0.0.1", ServerPort: 1080,
		NodeSource: model.NodeSource{SourceSubscription: "public"},
	}}
	var updatedBuffer bytes.Buffer
	if err := model.EncodeJSON(&updatedBuffer, updatedIntent); err != nil {
		t.Fatal(err)
	}
	updated := updatedBuffer.Bytes()
	for _, operation := range []string{"save", "apply"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			configPath := filepath.Join(root, "config.json")
			if err := os.WriteFile(configPath, updated, 0o600); err != nil {
				t.Fatal(err)
			}
			writeCalls, applyCalls := 0, 0
			service := &controlService{
				configPath: configPath,
				options:    macosplatform.BackendOptions{RunDirectory: filepath.Join(root, "run")},
				write: func(string, []byte, int) error {
					writeCalls++
					return nil
				},
				apply: func(model.Intent, macosplatform.BackendOptions) (coreapply.Result, error) {
					applyCalls++
					return coreapply.Result{OK: true}, nil
				},
			}
			response := service.handle(controlRequest{
				SchemaVersion:    controlSchemaVersion,
				Operation:        operation,
				Document:         string(original),
				ExpectedRevision: controlRevision(original),
			})
			if response.OK || response.Saved || response.Applied || response.ErrorCode != controlRevisionConflict ||
				response.Revision != controlRevision(updated) {
				t.Fatalf("stale %s returned unexpected response: %+v", operation, response)
			}
			if writeCalls != 0 || applyCalls != 0 {
				t.Fatalf("stale %s wrote %d time(s) and applied %d time(s)", operation, writeCalls, applyCalls)
			}
			after, err := os.ReadFile(configPath)
			if err != nil || string(after) != string(updated) {
				t.Fatalf("stale %s changed subscription-updated Saved content: %v\n%s", operation, err, after)
			}
		})
	}
}

func TestControlServiceReturnsRevisionOfNormalizedSavedBytes(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("..", "..", "..", "linux", "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	document = bytes.TrimSuffix(document, []byte{'\n'})
	var written []byte
	service := &controlService{
		configPath: "/fixed/config.json",
		revision:   func(string) (string, error) { return "loaded", nil },
		write: func(_ string, content []byte, _ int) error {
			written = append([]byte{}, content...)
			return nil
		},
	}
	response := service.handle(controlRequest{
		SchemaVersion:    controlSchemaVersion,
		Operation:        "save",
		Document:         string(document),
		ExpectedRevision: "loaded",
	})
	if !response.OK || !response.Saved || len(written) == 0 || written[len(written)-1] != '\n' ||
		response.Revision != controlRevision(written) {
		t.Fatalf("response revision does not describe normalized Saved bytes: %+v %q", response, written)
	}
}

func TestControlRevisionUsesStableSHA256Format(t *testing.T) {
	const want = "sha256-ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := controlRevision([]byte("abc")); got != want {
		t.Fatalf("control revision = %q, want %q", got, want)
	}
}

func TestDecodeStrictControlJSONRejectsUnknownFields(t *testing.T) {
	var request controlRequest
	err := decodeStrictJSON([]byte(`{"schema_version":1,"operation":"save","document":"{}","command":"id"}`), &request)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field rejection, found %v", err)
	}
}

func TestControlClientClosesRequestBeforeReadingResponse(t *testing.T) {
	placeholder, err := os.CreateTemp("/tmp", "steer-control-test-*")
	if err != nil {
		t.Fatal(err)
	}
	socketPath := placeholder.Name()
	if err := placeholder.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(socketPath); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(socketPath)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptUnix()
		if err != nil {
			serverDone <- err
			return
		}
		defer connection.Close()
		requestData, err := io.ReadAll(connection)
		if err != nil {
			serverDone <- err
			return
		}
		var request controlRequest
		if err := decodeStrictJSON(requestData, &request); err != nil {
			serverDone <- err
			return
		}
		if request.Operation != "save" || request.ExpectedRevision != "sha256-loaded" {
			serverDone <- errors.New("unexpected operation")
			return
		}
		serverDone <- json.NewEncoder(connection).Encode(controlResponse{SchemaVersion: controlSchemaVersion, OK: true})
	}()
	inputPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(inputPath, []byte(`{"main":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runControlClient([]string{
		"--socket", socketPath, "--operation", "save", "--input", inputPath,
		"--expected-revision", "sha256-loaded",
	}, &output); err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"ok": true`) {
		t.Fatalf("unexpected client output: %s", output.String())
	}
}

func TestRunProbeReturnsFlatActiveDTO(t *testing.T) {
	placeholder, err := os.CreateTemp("/tmp", "steer-probe-control-test-*")
	if err != nil {
		t.Fatal(err)
	}
	socketPath := placeholder.Name()
	if err := placeholder.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(socketPath); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(socketPath)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptUnix()
		if err != nil {
			serverDone <- err
			return
		}
		defer connection.Close()
		requestData, err := io.ReadAll(connection)
		if err != nil {
			serverDone <- err
			return
		}
		var request controlRequest
		if err := decodeStrictJSON(requestData, &request); err != nil {
			serverDone <- err
			return
		}
		if request.Operation != "probe" || request.Kind != "proxy" || request.Document != "" || request.ID != "" || request.NodeID != "" || request.RouteID != "" {
			serverDone <- errors.New("unexpected structured probe request")
			return
		}
		payload := json.RawMessage(`{"scope":"overview","kind":"proxy","ok":true,"stale":false,"tested_at":"2026-08-26T01:02:03Z","summary":"21 ms","error_summary":""}`)
		serverDone <- json.NewEncoder(connection).Encode(controlResponse{
			SchemaVersion: controlSchemaVersion,
			OK:            true,
			Payload:       payload,
		})
	}()
	var output bytes.Buffer
	if err := runProbe([]string{"--socket", socketPath, "--kind", "proxy"}, &output); err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"summary": "21 ms"`, `"stale": false`, `"tested_at": "2026-08-26T01:02:03Z"`} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("flat probe output is missing %s:\n%s", expected, output.String())
		}
	}
	if strings.Contains(output.String(), `"payload"`) {
		t.Fatalf("probe leaked the control envelope: %s", output.String())
	}
}

func TestControlSwitchUsesLatestSavedAndReturnsFailedApplyStatus(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("..", "..", "..", "linux", "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, document, 0600); err != nil {
		t.Fatal(err)
	}
	value, err := loadIntent(path)
	if err != nil {
		t.Fatal(err)
	}
	value.Main.LogLevel = "debug" // represents a newer external configuration
	var latest bytes.Buffer
	if err := model.EncodeJSON(&latest, value); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, latest.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	service := &controlService{
		configPath: path, options: macosplatform.BackendOptions{RunDirectory: t.TempDir()},
		write: func(path string, content []byte, _ int) error { return os.WriteFile(path, content, 0600) },
		apply: func(value model.Intent, _ macosplatform.BackendOptions) (coreapply.Result, error) {
			if value.Main.Enabled || value.Main.LogLevel != "debug" {
				t.Fatalf("lost latest saved configuration: %+v", value.Main)
			}
			return coreapply.Result{}, errors.New("stop failed")
		},
		status: func(macosplatform.BackendOptions) macosplatform.Status {
			return macosplatform.Status{Healthy: true, GenerationID: "still-running"}
		},
	}
	enabled := false
	response := service.handle(controlRequest{SchemaVersion: controlSchemaVersion, Operation: "set-enabled", Enabled: &enabled})
	if !response.Saved || response.Applied || response.OK || response.Status == nil || response.Status.GenerationID != "still-running" || len(response.Payload) == 0 {
		t.Fatalf("partial success must retain actual status and saved snapshot: %+v", response)
	}
	saved, err := loadIntent(path)
	if err != nil || saved.Main.Enabled || saved.Main.LogLevel != "debug" {
		t.Fatalf("saved state: %+v %v", saved.Main, err)
	}
	// Full-document saves still enforce their original revision.
	stale := service.handle(controlRequest{SchemaVersion: controlSchemaVersion, Operation: "save", Document: string(document), ExpectedRevision: controlRevision(document)})
	if stale.ErrorCode != controlRevisionConflict {
		t.Fatalf("stale save accepted: %+v", stale)
	}
}

func TestControlStatusReadsAuthoritativeServiceStatus(t *testing.T) {
	service := &controlService{status: func(macosplatform.BackendOptions) macosplatform.Status {
		return macosplatform.Status{Healthy: true, GenerationID: "active"}
	}}
	result := service.handle(controlRequest{SchemaVersion: controlSchemaVersion, Operation: "status"})
	if !result.OK || result.Status == nil || !result.Status.Healthy || result.Status.GenerationID != "active" {
		t.Fatalf("status: %+v", result)
	}
	for _, operation := range []string{"status", "state", "set-enabled"} {
		result = service.handle(controlRequest{SchemaVersion: controlSchemaVersion, Operation: operation, Document: `{}`})
		if result.OK {
			t.Fatalf("%s accepted a document", operation)
		}
	}
}
