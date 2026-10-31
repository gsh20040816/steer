// SPDX-License-Identifier: GPL-3.0-or-later

package apply

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/generation"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

type recordingBackend struct {
	calls       []string
	prepareErr  error
	activateErr error
	healthyErr  error
	unchanged   bool
}

func (backend *recordingBackend) Prepare(context.Context, model.Intent, compiler.Output) (generation.Candidate, error) {
	backend.calls = append(backend.calls, "prepare")
	if backend.prepareErr != nil {
		return generation.Candidate{}, backend.prepareErr
	}
	return generation.Candidate{Directory: "/candidate"}, nil
}
func (backend *recordingBackend) Activate(context.Context, generation.Candidate) (bool, error) {
	backend.calls = append(backend.calls, "activate")
	return !backend.unchanged, backend.activateErr
}
func (backend *recordingBackend) Healthy(context.Context, generation.Candidate) error {
	backend.calls = append(backend.calls, "healthy")
	return backend.healthyErr
}
func (backend *recordingBackend) Finalize(context.Context, generation.Candidate) error {
	backend.calls = append(backend.calls, "finalize")
	return nil
}
func (backend *recordingBackend) Disable(context.Context) error {
	backend.calls = append(backend.calls, "disable")
	return nil
}

func TestRunUsesFixedLifecycle(t *testing.T) {
	backend := &recordingBackend{}
	value := validIntent(true)
	result, err := Run(context.Background(), value, compiler.Options{}, backend)
	if err != nil || !result.OK || result.Generation != "/candidate" || result.CandidateGeneration != "/candidate" || !result.Activated || result.RuntimeDigest == "" {
		t.Fatalf("unexpected Apply result: %#v %v", result, err)
	}
	if !reflect.DeepEqual(backend.calls, []string{"prepare", "activate", "healthy", "finalize"}) {
		t.Fatalf("unexpected lifecycle: %#v", backend.calls)
	}
}

func TestRunReportsWhetherActivateRestarted(t *testing.T) {
	result, err := Run(context.Background(), validIntent(true), compiler.Options{}, &recordingBackend{})
	if err != nil || !result.OK || !result.Restarted {
		t.Fatalf("changed runtime was not reported as restarted: %#v %v", result, err)
	}
	backend := &recordingBackend{unchanged: true}
	result, err = Run(context.Background(), validIntent(true), compiler.Options{}, backend)
	if err != nil || !result.OK || !result.Activated || result.Restarted {
		t.Fatalf("unchanged runtime was reported as restarted: %#v %v", result, err)
	}
	if !reflect.DeepEqual(backend.calls, []string{"prepare", "activate", "healthy", "finalize"}) {
		t.Fatalf("unchanged runtime skipped the lifecycle: %#v", backend.calls)
	}
	encoded, _ := json.Marshal(result)
	if !strings.Contains(string(encoded), `"restarted":false`) {
		t.Fatalf("restarted must always be present in the Apply result: %s", encoded)
	}
}

func TestRunDoesNotReportPreparedCandidateAsActive(t *testing.T) {
	prepareErr := errors.New("prepare rejected")
	backend := &recordingBackend{prepareErr: prepareErr}
	result, err := Run(context.Background(), validIntent(true), compiler.Options{}, backend)
	if !errors.Is(err, prepareErr) || result.CandidateGeneration != "" || result.Generation != "" || result.Activated {
		t.Fatalf("unexpected Prepare failure result: %#v %v", result, err)
	}

	activateErr := errors.New("activate rejected")
	backend = &recordingBackend{activateErr: activateErr}
	result, err = Run(context.Background(), validIntent(true), compiler.Options{}, backend)
	if !errors.Is(err, activateErr) || result.CandidateGeneration != "/candidate" || result.Generation != "" || result.Activated {
		t.Fatalf("candidate was reported active after Activate failure: %#v %v", result, err)
	}
	if !reflect.DeepEqual(backend.calls, []string{"prepare", "activate"}) {
		t.Fatalf("unexpected failed lifecycle: %#v", backend.calls)
	}
}

func TestRunKeepsActivationFactWhenHealthFails(t *testing.T) {
	healthyErr := errors.New("not healthy")
	backend := &recordingBackend{healthyErr: healthyErr}
	result, err := Run(context.Background(), validIntent(true), compiler.Options{}, backend)
	if !errors.Is(err, healthyErr) || result.Generation != "/candidate" || !result.Activated || result.OK {
		t.Fatalf("unexpected health failure result: %#v %v", result, err)
	}
}

func TestRunDisablesWithoutPreparingGeneration(t *testing.T) {
	backend := &recordingBackend{}
	result, err := Run(context.Background(), validIntent(false), compiler.Options{}, backend)
	if err != nil || !result.OK || !reflect.DeepEqual(backend.calls, []string{"disable"}) {
		t.Fatalf("unexpected disabled Apply: %#v %v %#v", result, err, backend.calls)
	}
}

func validIntent(enabled bool) model.Intent {
	return model.Intent{
		Main:        model.Main{ID: "main", SchemaVersion: model.SchemaVersion, Enabled: enabled, LogLevel: "warn", ProbeDirectURL: "https://direct.example/", ProbeProxyURL: "https://proxy.example/", SpeedtestProxyURL: "https://speed.example/"},
		Bootstrap:   model.Bootstrap{ID: "bootstrap", Protocol: "udp", Server: "1.1.1.1", ServerPort: 53, Strategy: "prefer_ipv4"},
		Routes:      []model.Route{{ID: "direct", Enabled: true, Kind: "direct"}},
		DNSProfiles: []model.DNSProfile{{ID: "dns", Enabled: true, Protocol: "udp", Server: "1.1.1.1", ServerPort: 53}},
		Rules:       []model.Rule{{ID: "default", Enabled: true, Default: true, DNSProfile: "dns", Route: "direct"}},
	}
}
