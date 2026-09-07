// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	coreapply "github.com/gsh20040816/steer/go/internal/apply"
	"github.com/gsh20040816/steer/go/internal/capability"
	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/generation"
	"github.com/gsh20040816/steer/go/internal/geodata"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

const DefaultLaunchDaemonLabel = "com.steer.steer"

type BackendOptions struct {
	RunDirectory      string
	StateDirectory    string
	GeoDataDirectory  string
	SingBoxBinary     string
	LaunchctlBinary   string
	LaunchDaemonLabel string
	LaunchDaemonPlist string
	IfconfigBinary    string
	HealthTimeout     time.Duration
	CheckTUN          func([]string) error
	CheckDNS          func() error
}

type Backend struct {
	runner  Runner
	options BackendOptions
	plan    Plan
}

func NewBackend(runner Runner, value model.Intent, options BackendOptions) *Backend {
	if runner == nil {
		runner = ExecRunner{}
	}
	options = normalizeBackendOptions(options)
	return &Backend{runner: runner, options: options, plan: NewPlan(value)}
}

func (backend *Backend) CompilerOptions() compiler.Options {
	return compiler.Options{
		StateDirectory: backend.options.StateDirectory, GeoDataDirectory: backend.options.GeoDataDirectory,
		Target: backend.plan.CompilerTarget(),
	}
}

func (backend *Backend) Prepare(ctx context.Context, value model.Intent, compiled compiler.Output) (candidate generation.Candidate, returnErr error) {
	validation := ValidateWithGeoDataDirectory(value, backend.options.GeoDataDirectory)
	if !validation.OK {
		return generation.Candidate{}, ValidationError{Validation: validation}
	}
	versionOutput, err := backend.runner.Output(ctx, backend.options.SingBoxBinary, "version")
	if err != nil {
		return generation.Candidate{}, fmt.Errorf("inspect macOS sing-box: %w", err)
	}
	capabilityReport := capability.Parse(string(versionOutput), compiled.RequiredCapabilities)
	if !capabilityReport.OK {
		return generation.Candidate{}, fmt.Errorf("sing-box capability check failed: %v", capabilityReport.Errors)
	}
	if err := geodata.ValidateRequiredRules(compiled.GeoRuleSets, backend.options.GeoDataDirectory); err != nil {
		return generation.Candidate{}, err
	}
	paths := backend.paths()
	if err := paths.Ensure(); err != nil {
		return generation.Candidate{}, err
	}
	candidate, err = generation.Create(paths.GenerationsDirectory, value, compiled.SingBox)
	if err != nil {
		return generation.Candidate{}, err
	}
	defer func() {
		if returnErr != nil {
			_ = os.RemoveAll(candidate.Directory)
		}
	}()
	if err := generation.WriteJSON(filepath.Join(candidate.Directory, "platform.json"), backend.plan); err != nil {
		return generation.Candidate{}, err
	}
	metadata := GenerationMetadata{
		SchemaVersion: RuntimeSchemaVersion,
		GenerationID:  compiled.IntentDigest,
		IntentDigest:  compiled.IntentDigest,
	}
	if err := generation.WriteJSON(filepath.Join(candidate.Directory, "generation.json"), metadata); err != nil {
		return generation.Candidate{}, err
	}
	if _, err := backend.runner.Output(ctx, backend.options.SingBoxBinary, "check", "-c", filepath.Join(candidate.Directory, "sing-box.json")); err != nil {
		return generation.Candidate{}, fmt.Errorf("macOS sing-box native check failed: %w", err)
	}
	return candidate, nil
}

func (backend *Backend) Activate(ctx context.Context, candidate generation.Candidate) error {
	if err := backend.stopLaunchDaemon(ctx); err != nil {
		return err
	}
	if err := backend.publishCandidate(candidate); err != nil {
		return err
	}
	return backend.startLaunchDaemon(ctx)
}

// ActivateForServiceStart publishes a cold-start candidate without asking
// launchd to start itself recursively. The LaunchDaemon then supervises sing-box and system DNS.
func (backend *Backend) ActivateForServiceStart(_ context.Context, candidate generation.Candidate) error {
	return backend.publishCandidate(candidate)
}

func (backend *Backend) Healthy(ctx context.Context, candidate generation.Candidate) error {
	return backend.waitHealthy(ctx, candidate.Directory, backend.options.HealthTimeout)
}

func (backend *Backend) Finalize(_ context.Context, candidate generation.Candidate) error {
	entries, err := os.ReadDir(backend.options.RunDirectory + "/generations")
	if err != nil {
		return fmt.Errorf("list macOS generations: %w", err)
	}
	keep := filepath.Base(candidate.Directory)
	for _, entry := range entries {
		if entry.Name() == keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(backend.options.RunDirectory, "generations", entry.Name())); err != nil {
			return fmt.Errorf("prune macOS generation %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (backend *Backend) Disable(ctx context.Context) error {
	if err := backend.stopLaunchDaemon(ctx); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(backend.options.RunDirectory, "current.json")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove disabled macOS current generation: %w", err)
	}
	if err := os.RemoveAll(filepath.Join(backend.options.RunDirectory, "generations")); err != nil {
		return fmt.Errorf("remove disabled macOS generations: %w", err)
	}
	return nil
}

func (backend *Backend) paths() Paths {
	return runtimePaths(backend.options.RunDirectory, backend.options.StateDirectory)
}

func (backend *Backend) publishCandidate(candidate generation.Candidate) error {
	metadata, err := readGenerationMetadata(candidate.Directory)
	if err != nil {
		return err
	}
	return backend.paths().Publish(PreparedGeneration{
		Candidate: candidate,
		Metadata:  metadata,
	})
}

func readGenerationMetadata(directory string) (GenerationMetadata, error) {
	content, err := os.ReadFile(filepath.Join(directory, "generation.json"))
	if err != nil {
		return GenerationMetadata{}, fmt.Errorf("read macOS generation metadata: %w", err)
	}
	var metadata GenerationMetadata
	if err := unmarshalStrict(content, &metadata); err != nil {
		return GenerationMetadata{}, fmt.Errorf("decode macOS generation metadata: %w", err)
	}
	if metadata.SchemaVersion != RuntimeSchemaVersion || metadata.GenerationID == "" || metadata.IntentDigest == "" {
		return GenerationMetadata{}, fmt.Errorf("invalid macOS generation metadata")
	}
	return metadata, nil
}

func normalizeBackendOptions(options BackendOptions) BackendOptions {
	if options.RunDirectory == "" {
		options.RunDirectory = "/Library/Application Support/Steer/run"
	}
	if options.StateDirectory == "" {
		options.StateDirectory = "/Library/Application Support/Steer/state"
	}
	if options.GeoDataDirectory == "" {
		options.GeoDataDirectory = "/Library/Application Support/Steer/geodata-seed"
	}
	if options.SingBoxBinary == "" {
		options.SingBoxBinary = "/usr/local/libexec/steer/sing-box"
	}
	if options.LaunchctlBinary == "" {
		options.LaunchctlBinary = "/bin/launchctl"
	}
	if options.LaunchDaemonLabel == "" {
		options.LaunchDaemonLabel = DefaultLaunchDaemonLabel
	}
	if options.LaunchDaemonPlist == "" {
		options.LaunchDaemonPlist = "/Library/LaunchDaemons/" + options.LaunchDaemonLabel + ".plist"
	}
	if options.IfconfigBinary == "" {
		options.IfconfigBinary = "/sbin/ifconfig"
	}
	if options.HealthTimeout <= 0 {
		options.HealthTimeout = 30 * time.Second
	}
	return options
}

var _ coreapply.Backend = (*Backend)(nil)

// launchdOutputIsRunning is intentionally permissive for the normal launchd
// states used while bootstrap transitions into a running process.
func launchdOutputIsRunning(output string) bool {
	return strings.Contains(output, "state = running") || strings.Contains(output, "state = spawned")
}
