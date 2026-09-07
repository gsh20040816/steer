// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gsh20040816/steer/go/internal/compiler"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

type launchdFakeRunner struct {
	loaded         bool
	running        bool
	bootoutCalls   int
	bootstrapCalls int
}

func (runner *launchdFakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	command := name + " " + strings.Join(args, " ")
	switch {
	case name == "/test/sing-box" && len(args) == 1 && args[0] == "version":
		return []byte("sing-box version 1.14.0-rc.1\nTags: with_quic,with_utls\n"), nil
	case name == "/test/sing-box" && len(args) == 3 && args[0] == "check":
		return nil, nil
	case name == "/test/launchctl" && len(args) >= 2 && args[0] == "print":
		if !runner.loaded {
			return nil, fmt.Errorf("label is unloaded")
		}
		if !runner.running {
			return []byte("state = not running\nlast exit code = 1\n"), nil
		}
		return []byte("state = running\npid = 42\n"), nil
	case name == "/test/launchctl" && len(args) >= 2 && args[0] == "bootout":
		runner.bootoutCalls++
		runner.loaded = false
		runner.running = false
		return nil, nil
	case name == "/test/launchctl" && len(args) >= 3 && args[0] == "kill":
		runner.running = false
		return nil, nil
	case name == "/test/launchctl" && len(args) >= 3 && args[0] == "bootstrap":
		runner.bootstrapCalls++
		if runner.loaded {
			return nil, fmt.Errorf("label is already loaded")
		}
		runner.loaded = true
		runner.running = true
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected command: %s", command)
	}
}

func TestBackendBootsOutRegisteredInactiveLaunchDaemon(t *testing.T) {
	root := t.TempDir()
	value := validIntent()
	runner := &launchdFakeRunner{loaded: true, running: false}
	backend := NewBackend(runner, value, BackendOptions{
		RunDirectory: root + "/run", StateDirectory: root + "/state", SingBoxBinary: "/test/sing-box",
		LaunchctlBinary: "/test/launchctl", LaunchDaemonLabel: DefaultLaunchDaemonLabel,
		LaunchDaemonPlist: root + "/steer.plist", CheckTUN: func([]string) error { return nil }, CheckDNS: func() error { return nil },
	})
	candidate, err := backend.Prepare(context.Background(), value, compiler.Compile(value, backend.CompilerOptions()))
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Activate(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	if runner.bootoutCalls != 1 || runner.bootstrapCalls != 1 || !runner.loaded || !runner.running {
		t.Fatalf("unexpected launchd lifecycle: %#v", runner)
	}
}

func TestReadStatusReportsActualRuntimeProjectionDigest(t *testing.T) {
	root := t.TempDir()
	value := validIntent()
	runner := &launchdFakeRunner{}
	backend := NewBackend(runner, value, BackendOptions{
		RunDirectory: root + "/run", StateDirectory: root + "/state", SingBoxBinary: "/test/sing-box",
		LaunchctlBinary: "/test/launchctl", LaunchDaemonLabel: DefaultLaunchDaemonLabel,
		LaunchDaemonPlist: root + "/steer.plist", CheckTUN: func([]string) error { return nil }, CheckDNS: func() error { return nil },
	})
	compiled := compiler.Compile(value, backend.CompilerOptions())
	candidate, err := backend.Prepare(context.Background(), value, compiled)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Activate(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	status := backend.ReadStatus(context.Background())
	if status.GenerationID == "" || status.IntentDigest != compiled.IntentDigest ||
		status.RuntimeDigest != compiled.RuntimeDigest || !status.Healthy {
		t.Fatalf("macOS Active status omitted the runtime projection: %#v", status)
	}
}

func TestBackendUsesLaunchdGenerationLifecycle(t *testing.T) {
	root := t.TempDir()
	value := validIntent()
	runner := &launchdFakeRunner{}
	backend := NewBackend(runner, value, BackendOptions{
		RunDirectory: root + "/run", StateDirectory: root + "/state", SingBoxBinary: "/test/sing-box",
		LaunchctlBinary: "/test/launchctl", LaunchDaemonLabel: DefaultLaunchDaemonLabel,
		LaunchDaemonPlist: root + "/steer.plist", CheckTUN: func([]string) error { return nil }, CheckDNS: func() error { return nil },
	})
	compiled := compiler.Compile(value, backend.CompilerOptions())
	candidate, err := backend.Prepare(context.Background(), value, compiled)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Activate(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	if err := backend.Healthy(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	if err := backend.Finalize(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	current, err := backend.paths().LoadCurrent()
	if err != nil || current.GenerationID == "" || current.Directory == "" {
		t.Fatalf("unexpected current generation: %#v %v", current, err)
	}
	platformPlan, err := os.ReadFile(filepath.Join(root, "run", "generations", current.Directory, "platform.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(platformPlan), "active_lan_prefixes") {
		t.Fatalf("generation persisted retired active-LAN state: %s", platformPlan)
	}
}

func TestReadStatusDoesNotReportBrokenGenerationAsStopped(t *testing.T) {
	root := t.TempDir()
	backend := NewBackend(ExecRunner{}, model.Intent{}, BackendOptions{RunDirectory: root})
	if status := backend.ReadStatus(context.Background()); status.Error != "" || status.Healthy {
		t.Fatalf("missing pointer: %+v", status)
	}
	if err := os.WriteFile(filepath.Join(root, "current.json"), []byte(`{broken`), 0600); err != nil {
		t.Fatal(err)
	}
	if status := backend.ReadStatus(context.Background()); status.Error == "" || status.Healthy {
		t.Fatalf("broken pointer must surface error: %+v", status)
	}
}
