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
	if _, err := backend.Activate(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	if runner.bootoutCalls != 1 || runner.bootstrapCalls != 1 || !runner.loaded || !runner.running {
		t.Fatalf("unexpected launchd lifecycle: %#v", runner)
	}
}

func TestBackendSwitchesUnchangedRuntimeWithoutRestart(t *testing.T) {
	root := t.TempDir()
	runner := &launchdFakeRunner{}
	options := BackendOptions{
		RunDirectory: root + "/run", StateDirectory: root + "/state", SingBoxBinary: "/test/sing-box",
		LaunchctlBinary: "/test/launchctl", LaunchDaemonLabel: DefaultLaunchDaemonLabel,
		LaunchDaemonPlist: root + "/steer.plist", CheckTUN: func([]string) error { return nil }, CheckDNS: func() error { return nil },
	}
	activate := func(value model.Intent) (directory string, compiled compiler.Output, restarted bool) {
		t.Helper()
		backend := NewBackend(runner, value, options)
		compiled = compiler.Compile(value, backend.CompilerOptions())
		candidate, err := backend.Prepare(context.Background(), value, compiled)
		if err != nil {
			t.Fatal(err)
		}
		restarted, err = backend.Activate(context.Background(), candidate)
		if err != nil {
			t.Fatal(err)
		}
		if err := backend.Healthy(context.Background(), candidate); err != nil {
			t.Fatal(err)
		}
		if err := backend.Finalize(context.Background(), candidate); err != nil {
			t.Fatal(err)
		}
		return candidate.Directory, compiled, restarted
	}
	value := validIntent()
	first, _, restarted := activate(value)
	if !restarted || runner.bootstrapCalls != 1 {
		t.Fatalf("first Apply must start the LaunchDaemon: %#v", runner)
	}
	value.Nodes = []model.Node{{ID: "unused", Enabled: true, Type: "socks", Server: "192.0.2.1", ServerPort: 1080}}
	second, compiled, restarted := activate(value)
	if restarted || runner.bootoutCalls != 0 || runner.bootstrapCalls != 1 {
		t.Fatalf("an inventory-only change restarted the LaunchDaemon: %#v", runner)
	}
	current, err := NewBackend(runner, value, options).paths().LoadCurrent()
	if err != nil || current.Directory != filepath.Base(second) || current.GenerationID != compiled.IntentDigest {
		t.Fatalf("current.json was not switched to the candidate: %#v %v", current, err)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("previous generation was not pruned: %v", err)
	}
	status := NewBackend(runner, model.Intent{}, options).ReadStatus(context.Background())
	if !status.Healthy || status.GenerationID != compiled.IntentDigest || status.RuntimeDigest != compiled.RuntimeDigest {
		t.Fatalf("status does not describe the switched generation: %#v", status)
	}
	value.Main.LogLevel = "debug"
	if _, _, restarted := activate(value); !restarted || runner.bootoutCalls != 1 || runner.bootstrapCalls != 2 {
		t.Fatalf("a runtime change must restart the LaunchDaemon: %#v", runner)
	}
}

func TestDNSMarkerFollowsRuntimeContent(t *testing.T) {
	root := t.TempDir()
	backend := NewBackend(ExecRunner{}, model.Intent{}, BackendOptions{RunDirectory: root})
	for _, name := range []string{"old", "new", "other"} {
		content := `{"log":{}}`
		if name == "other" {
			content = `{"log":{"level":"debug"}}`
		}
		if err := os.MkdirAll(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name, "sing-box.json"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runtime, err := runtimeConfigDigest(filepath.Join(root, "old", "sing-box.json"))
	if err != nil {
		t.Fatal(err)
	}
	marker := fmt.Sprintf(`{"config":%q,"runtime":%q,"error":"probe failed"}`, filepath.Join(root, "old", "sing-box.json"), runtime)
	if err := os.WriteFile(filepath.Join(root, "dns-ready.json"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := backend.checkDNSReady(context.Background(), filepath.Join(root, "new")); err == nil || !strings.Contains(err.Error(), "probe failed") {
		t.Fatalf("marker for identical runtime content was not accepted: %v", err)
	}
	if err := backend.checkDNSReady(context.Background(), filepath.Join(root, "other")); err == nil || !strings.Contains(err.Error(), "different runtime") {
		t.Fatalf("marker for different runtime content was accepted: %v", err)
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
	if _, err := backend.Activate(context.Background(), candidate); err != nil {
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
	if _, err := backend.Activate(context.Background(), candidate); err != nil {
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
	if _, err := os.Stat(filepath.Join(root, "run", "generations", current.Directory, "platform.json")); err != nil {
		t.Fatal(err)
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
