// SPDX-License-Identifier: GPL-3.0-or-later

package linux

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapply "github.com/gsh20040816/steer/go/internal/apply"
	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/generation"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

type healthyStatusRunner struct{}

func (healthyStatusRunner) Output(_ context.Context, name string, _ ...string) ([]byte, error) {
	switch name {
	case "/usr/bin/systemctl":
		return []byte("ActiveState=active\nMainPID=4242\n"), nil
	case "ip", "/usr/sbin/nft":
		return []byte(`{}`), nil
	default:
		return nil, fmt.Errorf("unexpected command %s", name)
	}
}

func prepareCurrentStatusGeneration(t *testing.T, runDirectory string, value model.Intent) string {
	t.Helper()
	plan := NewPlan(value)
	compiled := compiler.Compile(value, compiler.Options{Target: plan.CompilerTarget()})
	candidate, err := generation.Create(filepath.Join(runDirectory, "generations"), value, compiled.SingBox)
	if err != nil {
		t.Fatal(err)
	}
	if err := generation.WriteJSON(filepath.Join(candidate.Directory, "platform.json"), plan); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(candidate.Directory, filepath.Join(runDirectory, "current")); err != nil {
		t.Fatal(err)
	}
	return candidate.Directory
}

func TestReadStatusReportsActualCurrentGeneration(t *testing.T) {
	runDirectory := t.TempDir()
	active := validIntent()
	active.Main.Enabled = true
	active.Main.LogLevel = "warn"
	activeDirectory := prepareCurrentStatusGeneration(t, runDirectory, active)

	saved := active
	saved.Main.LogLevel = "info"
	savedPlan := NewPlan(saved)
	savedCompiled := compiler.Compile(saved, compiler.Options{Target: savedPlan.CompilerTarget()})
	record := coreapply.Record{
		Sequence: "17", Timestamp: "2026-08-26T08:00:00Z",
		Result: coreapply.Result{
			OK: false, Error: "start candidate Linux generation: failed", CandidateGeneration: filepath.Join(runDirectory, "generations", "candidate.failed"),
			IntentDigest: savedCompiled.IntentDigest, RuntimeDigest: savedCompiled.RuntimeDigest,
		},
	}
	if err := generation.WriteJSON(filepath.Join(runDirectory, "last-apply.json"), record); err != nil {
		t.Fatal(err)
	}

	options := BackendOptions{RunDirectory: runDirectory, CheckListeners: func([]int) error { return nil }}
	status := ReadStatus(context.Background(), healthyStatusRunner{}, options)
	activeCompiled := compiler.Compile(active, compiler.Options{Target: NewPlan(active).CompilerTarget()})
	if !status.Healthy || status.Generation != filepath.Base(activeDirectory) || status.IntentDigest != activeCompiled.IntentDigest || status.RuntimeDigest != activeCompiled.RuntimeDigest {
		t.Fatalf("status did not report current generation: %#v", status)
	}
	if status.Generation == filepath.Base(record.Result.CandidateGeneration) || status.LastApply == nil || status.LastApply.Result.Activated {
		t.Fatalf("failed candidate leaked into active status: %#v", status)
	}
	if !HasPendingApply(saved, status, options) {
		t.Fatal("failed activation did not retain pending Apply")
	}
}

func TestPendingApplyUsesRuntimeProjection(t *testing.T) {
	runDirectory := t.TempDir()
	active := validIntent()
	active.Main.Enabled = true
	prepareCurrentStatusGeneration(t, runDirectory, active)
	options := BackendOptions{RunDirectory: runDirectory, CheckListeners: func([]int) error { return nil }}
	status := ReadStatus(context.Background(), healthyStatusRunner{}, options)

	inventoryOnly := active
	inventoryOnly.Subscriptions = []model.Subscription{{ID: "feed", Enabled: true, URL: "https://feed.example/sub"}}
	inventoryOnly.Nodes = []model.Node{{ID: "inventory", Enabled: true, Type: "socks", Server: "inventory.example", ServerPort: 1080}}
	if HasPendingApply(inventoryOnly, status, options) {
		t.Fatal("unreferenced subscription inventory manufactured pending Apply")
	}

	runtimeChange := active
	runtimeChange.Main.LogLevel = "debug"
	if !HasPendingApply(runtimeChange, status, options) {
		t.Fatal("runtime-affecting saved change was not pending")
	}

	failed := status
	failed.LastApply = &coreapply.Record{Result: coreapply.Result{RuntimeDigest: status.RuntimeDigest}}
	if !HasPendingApply(active, failed, options) {
		t.Fatal("failed Apply did not remain pending when the current projection matched")
	}
}

func TestReadStatusHashesTheCurrentRuntimeArtifact(t *testing.T) {
	runDirectory := t.TempDir()
	active := validIntent()
	active.Main.Enabled = true
	activeDirectory := prepareCurrentStatusGeneration(t, runDirectory, active)

	// Model a generation produced by a different compiler revision. Status must
	// describe the artifact on disk, not silently recompile its Intent.
	path := filepath.Join(activeDirectory, "sing-box.json")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var actual map[string]any
	if err := json.Unmarshal(content, &actual); err != nil {
		t.Fatal(err)
	}
	actual["runtime_marker"] = "from-generation"
	encoded, err := json.MarshalIndent(actual, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	options := BackendOptions{RunDirectory: runDirectory, CheckListeners: func([]int) error { return nil }}
	status := ReadStatus(context.Background(), healthyStatusRunner{}, options)
	expected := compiler.RuntimeDigest(active, actual)
	recompiled := compiler.Compile(active, compiler.Options{Target: NewPlan(active).CompilerTarget()}).RuntimeDigest
	if status.RuntimeDigest != expected || status.RuntimeDigest == recompiled {
		t.Fatalf("status did not hash actual runtime: status=%s actual=%s recompiled=%s", status.RuntimeDigest, expected, recompiled)
	}
}

func TestReadStatusDoesNotExposeDisabledGeneration(t *testing.T) {
	runDirectory := t.TempDir()
	disabled := validIntent()
	disabled.Main.Enabled = false
	prepareCurrentStatusGeneration(t, runDirectory, disabled)
	options := BackendOptions{RunDirectory: runDirectory, CheckListeners: func([]int) error { return nil }}
	status := ReadStatus(context.Background(), healthyStatusRunner{}, options)
	if status.Generation != "" || status.IntentDigest != "" || status.RuntimeDigest != "" || status.Healthy {
		t.Fatalf("disabled generation was reported active: %#v", status)
	}
	if HasPendingApply(disabled, status, options) {
		t.Fatal("disabled configuration without an active generation is pending")
	}
}

type activateRunner struct {
	calls  []string
	active bool
}

func (runner *activateRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	runner.calls = append(runner.calls, call)
	switch {
	case name == "/test/sing-box" && len(args) == 1 && args[0] == "version":
		return []byte("sing-box version 1.14.0-rc.1\nTags: with_quic,with_utls\n"), nil
	case name == "/test/systemctl" && args[0] == "show":
		if !runner.active {
			return []byte("ActiveState=inactive\nMainPID=0\n"), nil
		}
		return []byte("ActiveState=active\nMainPID=4242\n"), nil
	case name == "/test/systemctl" && args[0] == "start":
		runner.active = true
	case name == "/test/systemctl" && args[0] == "stop":
		runner.active = false
	case call == "/test/nft -j list tables":
		return []byte(`{"nftables":[]}`), nil
	}
	return []byte(`{}`), nil
}

func TestActivateKeepsServiceRunningWhenRuntimeIsUnchanged(t *testing.T) {
	root := t.TempDir()
	runner := &activateRunner{}
	apply := func(value model.Intent) coreapply.Result {
		t.Helper()
		backend := NewBackend(runner, value, BackendOptions{
			RunDirectory: filepath.Join(root, "run"), StateDirectory: filepath.Join(root, "state"),
			SingBoxBinary: "/test/sing-box", NFTBinary: "/test/nft", SystemctlBinary: "/test/systemctl",
			CheckListeners: func([]int) error { return nil },
		})
		result, err := coreapply.Run(context.Background(), value, backend.CompilerOptions(), backend)
		if err != nil || !result.OK {
			t.Fatalf("Apply failed: %#v %v", result, err)
		}
		return result
	}
	value := validIntent()
	value.Main.Enabled = true
	if result := apply(value); !result.Restarted {
		t.Fatalf("first Apply must start the service: %#v", result)
	}
	runner.calls = nil
	value.Nodes = append(value.Nodes, model.Node{ID: "unused", Enabled: true, Type: "socks", Server: "192.0.2.1", ServerPort: 1080})
	result := apply(value)
	if result.Restarted {
		t.Fatalf("inventory-only Apply restarted the service: %#v", result)
	}
	for _, call := range runner.calls {
		if strings.HasPrefix(call, "/test/systemctl stop") || strings.HasPrefix(call, "/test/systemctl start") || strings.HasPrefix(call, "/test/nft -f ") {
			t.Fatalf("unchanged runtime touched the service or nftables: %v", runner.calls)
		}
	}
	if target, err := os.Readlink(filepath.Join(root, "run", "current")); err != nil || target != result.Generation {
		t.Fatalf("current was not switched: %q %v", target, err)
	}
	runner.active = false
	if result := apply(value); !result.Restarted {
		t.Fatalf("an inactive service must be restarted: %#v", result)
	}
}
