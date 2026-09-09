// SPDX-License-Identifier: GPL-3.0-or-later

// steer-macos is the supported macOS control binary. It deliberately uses an
// ordinary launchd LaunchDaemon and the external sing-box TUN runtime; it does
// not require Apple Developer entitlements. The native GUI is a frontend for
// this helper and never owns the packet-processing runtime.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	coreapply "github.com/gsh20040816/steer/go/internal/apply"
	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/geodata"
	model "github.com/gsh20040816/steer/go/internal/intent"
	macosplatform "github.com/gsh20040816/steer/go/internal/platform/macos"
	"github.com/gsh20040816/steer/go/internal/subscription"
)

var version = "development"

const (
	defaultRunDirectory   = "/Library/Application Support/Steer/run"
	defaultStateDirectory = "/Library/Application Support/Steer/state"
	defaultConfigPath     = "/Library/Application Support/Steer/config/config.json"
	defaultSingBoxBinary  = "/usr/local/libexec/steer/sing-box"
	defaultGeoDataDir     = "/Library/Application Support/Steer/geodata-seed"
	defaultLaunchctl      = "/bin/launchctl"
	defaultLaunchDaemon   = "/Library/LaunchDaemons/com.steer.steer.plist"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "version":
		_, err := fmt.Fprintln(stdout, version)
		return err
	case "validate":
		return runValidate(args[1:], stdout)
	case "compile":
		return runCompile(args[1:], stdout)
	case "parse-nodes":
		return runParseNodes(args[1:], stdout)
	case "export-node":
		return runExportNode(args[1:], stdout)
	case "probe":
		return runProbe(args[1:], stdout)
	case "_diagnostics":
		return runDiagnostics(args[1:], stdout)
	case "_probe-results":
		return runProbeResults(args[1:], stdout)
	case "_state":
		return runUIState(args[1:], stdout)
	case "geo-catalog":
		return runGeoCatalog(args[1:], stdout)
	case "subscription":
		return runSubscription(args[1:], stdout)
	case "verify-geodata":
		return runVerifyGeoData(args[1:])
	case "prepare":
		return runPrepare(args[1:], stdout)
	case "apply":
		return runApply(args[1:], stdout)
	case "health":
		return runHealth(args[1:])
	case "status":
		return runStatus(args[1:], stdout)
	case "cleanup":
		return runCleanup(args[1:])
	case "control":
		return runControlClient(args[1:], stdout)
	case "_control":
		return runControlService(args[1:])
	case "_run":
		return runService(args[1:])
	default:
		return usage()
	}
}

func runVerifyGeoData(args []string) error {
	flags := flag.NewFlagSet("verify-geodata", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	directory := flags.String("directory", "", "verified geodata seed directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *directory == "" {
		return errors.New("verify-geodata requires --directory")
	}
	if err := geodata.VerifyDirectory(*directory); err != nil {
		return fmt.Errorf("verify geodata seed: %w", err)
	}
	return nil
}

func runParseNodes(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("parse-nodes", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	inputPath := flags.String("input", "", "file containing one proxy URI per line")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *inputPath == "" {
		return errors.New("parse-nodes requires --input")
	}
	content, err := os.ReadFile(*inputPath)
	if err != nil {
		return fmt.Errorf("read node import: %w", err)
	}
	parsed, err := subscription.ParseList(string(content))
	if err != nil {
		return err
	}
	if len(parsed.Nodes) == 0 {
		return fmt.Errorf("node import contained no valid nodes (%d skipped)", parsed.Skipped)
	}
	return writeJSON(stdout, struct {
		Nodes          []model.Node                 `json:"nodes"`
		Skipped        int                          `json:"skipped"`
		SkippedReasons []subscription.SkippedReason `json:"skipped_reasons,omitempty"`
	}{Nodes: parsed.Nodes, Skipped: parsed.Skipped, SkippedReasons: parsed.SkippedReasons})
}

func runExportNode(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("export-node", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	inputPath := flags.String("input", "", "file containing one Canonical node")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *inputPath == "" {
		return errors.New("export-node requires --input")
	}
	file, err := os.Open(*inputPath)
	if err != nil {
		return fmt.Errorf("read node export: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, (2<<20)+1))
	decoder.DisallowUnknownFields()
	var node model.Node
	if err := decoder.Decode(&node); err != nil {
		return fmt.Errorf("decode node export: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("decode node export: trailing value")
		}
		return fmt.Errorf("decode node export: %w", err)
	}
	uri, err := subscription.EncodeURI(node)
	if err != nil {
		return fmt.Errorf("export node: %w", err)
	}
	return writeJSON(stdout, map[string]string{"uri": uri})
}

func runValidate(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultConfigPath, "canonical JSON configuration")
	geoDataDirectory := flags.String("geodata", defaultGeoDataDir, "package-owned Geo SRS seed directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("validate accepts flags only")
	}
	value, err := loadIntent(*configPath)
	if err != nil {
		return err
	}
	validation := macosplatform.ValidateWithGeoDataDirectory(value, *geoDataDirectory)
	if err := writeJSON(stdout, validation); err != nil {
		return err
	}
	if !validation.OK {
		return errors.New("configuration validation failed")
	}
	return nil
}

func runCompile(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("compile", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultConfigPath, "canonical JSON configuration")
	stateDirectory := flags.String("state-dir", defaultStateDirectory, "macOS derived state directory")
	geoDataDirectory := flags.String("geodata", defaultGeoDataDir, "package-owned Geo SRS seed directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("compile accepts flags only")
	}
	value, err := loadIntent(*configPath)
	if err != nil {
		return err
	}
	validation := macosplatform.ValidateWithGeoDataDirectory(value, *geoDataDirectory)
	if !validation.OK {
		return macosplatform.ValidationError{Validation: validation}
	}
	backend := macosplatform.NewBackend(macosplatform.ExecRunner{}, value, macosplatform.BackendOptions{
		StateDirectory: *stateDirectory, GeoDataDirectory: *geoDataDirectory,
	})
	bundle := compiler.Compile(value, backend.CompilerOptions())
	return writeJSON(stdout, bundle)
}

func runPrepare(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("prepare", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultConfigPath, "canonical JSON configuration")
	options := bindBackendFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("prepare accepts flags only")
	}
	value, err := loadIntent(*configPath)
	if err != nil {
		return err
	}
	backend := macosplatform.NewBackend(macosplatform.ExecRunner{}, value, options.value())
	compiled := compiler.Compile(value, backend.CompilerOptions())
	candidate, err := backend.Prepare(context.Background(), value, compiled)
	if err != nil {
		return err
	}
	return writeJSON(stdout, candidate)
}

func runApply(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultConfigPath, "canonical JSON configuration")
	options := bindBackendFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("apply accepts flags only")
	}
	return runLockedApply(options.runDirectory, func() (coreapply.Result, error) {
		value, err := loadIntent(*configPath)
		if err != nil {
			return coreapply.Result{}, err
		}
		backend := macosplatform.NewBackend(macosplatform.ExecRunner{}, value, options.value())
		return coreapply.Run(context.Background(), value, backend.CompilerOptions(), backend)
	}, stdout)
}

func runHealth(args []string) error {
	flags := flag.NewFlagSet("health", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	options := bindBackendFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("health accepts flags only")
	}
	backend := macosplatform.NewBackend(macosplatform.ExecRunner{}, model.Intent{}, options.value())
	return backend.WaitCurrentHealthy(context.Background(), options.healthTimeout)
}

func runStatus(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	options := bindBackendFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("status accepts flags only")
	}
	backend := macosplatform.NewBackend(macosplatform.ExecRunner{}, model.Intent{}, options.value())
	return writeJSON(stdout, backend.ReadStatus(context.Background()))
}

func runCleanup(args []string) error {
	flags := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	options := bindBackendFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("cleanup accepts flags only")
	}
	backend := macosplatform.NewBackend(macosplatform.ExecRunner{}, model.Intent{}, options.value())
	return backend.Disable(context.Background())
}

func runService(args []string) error {
	flags := flag.NewFlagSet("_run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultConfigPath, "canonical JSON configuration")
	options := bindBackendFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("_run accepts flags only")
	}
	if _, err := os.Stat(filepath.Join(options.runDirectory, "current.json")); os.IsNotExist(err) {
		if err := prepareColdStart(*configPath, options.value()); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	current, err := macosplatform.NewBackend(macosplatform.ExecRunner{}, model.Intent{}, options.value()).CurrentConfigPath()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	return macosplatform.NewBackend(macosplatform.ExecRunner{}, model.Intent{}, options.value()).RunSupervised(ctx, current)
}

func prepareColdStart(configPath string, options macosplatform.BackendOptions) error {
	lock, err := acquireLock(options.RunDirectory)
	if err != nil {
		return err
	}
	defer lock.Close()
	if _, err := os.Stat(filepath.Join(options.RunDirectory, "current.json")); err == nil {
		return nil
	}
	value, err := loadIntent(configPath)
	if err != nil {
		return err
	}
	backend := macosplatform.NewBackend(macosplatform.ExecRunner{}, value, options)
	compiled := compiler.Compile(value, backend.CompilerOptions())
	if !value.Main.Enabled {
		return errors.New("macOS Steer is disabled")
	}
	candidate, err := backend.Prepare(context.Background(), value, compiled)
	if err != nil {
		return err
	}
	if err := backend.ActivateForServiceStart(context.Background(), candidate); err != nil {
		return err
	}
	return backend.Finalize(context.Background(), candidate)
}

type backendFlags struct {
	runDirectory, stateDirectory, geoDataDirectory, singBoxBinary string
	launchctlBinary, label, plist                                 string
	healthTimeout                                                 time.Duration
}

func bindBackendFlags(flags *flag.FlagSet) *backendFlags {
	value := &backendFlags{}
	flags.StringVar(&value.runDirectory, "run-dir", defaultRunDirectory, "macOS runtime directory")
	flags.StringVar(&value.stateDirectory, "state-dir", defaultStateDirectory, "macOS derived state directory")
	flags.StringVar(&value.geoDataDirectory, "geodata", defaultGeoDataDir, "package-owned Geo SRS seed directory")
	flags.StringVar(&value.singBoxBinary, "sing-box", defaultSingBoxBinary, "sing-box binary")
	flags.StringVar(&value.launchctlBinary, "launchctl", defaultLaunchctl, "launchctl binary")
	flags.StringVar(&value.label, "label", macosplatform.DefaultLaunchDaemonLabel, "LaunchDaemon label")
	flags.StringVar(&value.plist, "launchd-plist", defaultLaunchDaemon, "LaunchDaemon plist")
	flags.DurationVar(&value.healthTimeout, "timeout", macosplatform.DefaultHealthTimeout, "health deadline")
	return value
}

func (value *backendFlags) value() macosplatform.BackendOptions {
	return macosplatform.BackendOptions{
		RunDirectory: value.runDirectory, StateDirectory: value.stateDirectory,
		GeoDataDirectory: value.geoDataDirectory, SingBoxBinary: value.singBoxBinary, LaunchctlBinary: value.launchctlBinary,
		LaunchDaemonLabel: value.label, LaunchDaemonPlist: value.plist,
		HealthTimeout: value.healthTimeout,
	}
}

func loadIntent(path string) (model.Intent, error) {
	file, err := os.Open(path)
	if err != nil {
		return model.Intent{}, fmt.Errorf("open canonical config: %w", err)
	}
	defer file.Close()
	value, err := model.DecodeJSON(file)
	if err != nil {
		return model.Intent{}, err
	}
	return value, nil
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func usage() error {
	return errors.New("usage: steer-macos {version|validate|compile|parse-nodes|probe|geo-catalog|subscription|verify-geodata|prepare|apply|health|status|cleanup|control|_diagnostics|_probe-results|_state|_control|_run} [flags]")
}
