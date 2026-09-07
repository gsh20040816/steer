// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	coreapply "github.com/gsh20040816/steer/go/internal/apply"
	"github.com/gsh20040816/steer/go/internal/capability"
	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/geodata"
	model "github.com/gsh20040816/steer/go/internal/intent"
	"github.com/gsh20040816/steer/go/internal/platform/openwrt"
	coreprobe "github.com/gsh20040816/steer/go/internal/probe"
	"github.com/gsh20040816/steer/go/internal/subscription"
	"github.com/gsh20040816/steer/go/internal/uistate"
)

var version = "development"

const applyLockTimeout = 2 * time.Minute

func main() {
	if err := run(os.Args[1:]); err != nil {
		if len(os.Args) < 2 || os.Args[1] != "apply" {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "_supervise":
		return runSupervise(args[1:])
	case "version":
		fmt.Println(version)
		return nil
	case "validate":
		return runValidate(args[1:])
	case "apply":
		return runApply(args[1:])
	case "health":
		return runHealth(args[1:])
	case "status":
		return runStatus(args[1:])
	case "probe":
		return runProbe(args[1:])
	case "subscription":
		return runSubscription(args[1:])
	case "geo-catalog":
		return runGeoCatalog(args[1:])
	case "cleanup":
		return runCleanup(args[1:])
	case "_parse-nodes":
		return runParseNodes(args[1:])
	case "_export-node":
		return runExportNode(args[1:])
	case "_export-intent":
		return runExportIntent(args[1:])
	case "_runtime":
		return runRuntime(args[1:])
	case "_diagnostics":
		return runDiagnostics(args[1:])
	case "_probe-results":
		return runProbeResults(args[1:])
	case "_state":
		return runUIState(args[1:])
	case "_start":
		return runServiceStart(args[1:])
	default:
		return usage()
	}
}

type uiLifecycleState struct {
	Saved        uistate.IntentState `json:"saved"`
	Active       openwrt.Status      `json:"active"`
	PendingApply bool                `json:"pending_apply"`
}

func runUIState(args []string) error {
	flags := flag.NewFlagSet("_state", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "/etc/config/steer", "UCI configuration file")
	runDirectory := flags.String("run-dir", "/run/steer", "runtime state directory")
	stateDirectory := flags.String("state-dir", "/var/lib/steer", "generated state directory")
	seedDirectory := flags.String("seed-dir", "/usr/share/steer/geodata-seed", "package-owned Geo seed directory")
	nftBinary := flags.String("nft", "/usr/sbin/nft", "nft binary")
	savedOnly := flags.Bool("saved-only", false, "omit Active runtime inspection")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("_state accepts flags only")
	}
	value, decodeValidation := loadIntent(*configPath)
	saved := uistate.IntentState{Counts: uistate.Counts{}, Validation: decodeValidation}
	if decodeValidation.OK {
		validation := openwrt.ValidateWithGeoDataDirectory(value, *seedDirectory)
		saved = uistate.FromIntent(value, validation, compiler.Options{
			StateDirectory: *stateDirectory, GeoDataDirectory: *seedDirectory,
			Target: openwrt.NewPlan(value).CompilerTarget(),
		})
	}
	active := openwrt.Status{}
	if !*savedOnly {
		active = openwrt.ReadStatus(context.Background(), openwrt.ExecRunner{}, *runDirectory, *nftBinary)
	}
	pendingApply := uistate.PendingApply(saved, active.Generation, active.RuntimeDigest, active.LastApply)
	writeJSON(uiLifecycleState{Saved: saved, Active: active, PendingApply: pendingApply})
	return nil
}

func runDiagnostics(args []string) error {
	flags := flag.NewFlagSet("_diagnostics", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "/etc/config/steer", "UCI configuration file")
	runDirectory := flags.String("run-dir", "/run/steer", "runtime state directory")
	stateDirectory := flags.String("state-dir", "/var/lib/steer", "generated state directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("_diagnostics accepts flags only")
	}
	writeJSON(openwrt.ReadDiagnostics(*configPath, *runDirectory, *stateDirectory))
	return nil
}

func runProbeResults(args []string) error {
	flags := flag.NewFlagSet("_probe-results", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "/etc/config/steer", "UCI configuration file")
	runDirectory := flags.String("run-dir", "/run/steer", "runtime state directory")
	stateDirectory := flags.String("state-dir", "/var/lib/steer", "generated state directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("_probe-results accepts flags only")
	}
	writeJSON(openwrt.ReadLatestProbeResults(*configPath, *runDirectory, *stateDirectory))
	return nil
}

func runExportIntent(args []string) error {
	flags := flag.NewFlagSet("_export-intent", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "/etc/config/steer", "UCI configuration file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("_export-intent accepts flags only")
	}
	value, decodeValidation := loadIntent(*configPath)
	if !decodeValidation.OK {
		writeJSON(decodeValidation)
		return errors.New("configuration decode failed")
	}
	writeJSON(value)
	return nil
}

func runExportNode(args []string) error {
	flags := flag.NewFlagSet("_export-node", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "/etc/config/steer", "UCI configuration file")
	nodeID := flags.String("id", "", "node ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *nodeID == "" {
		return errors.New("_export-node requires --id")
	}
	value, decodeValidation := loadIntent(*configPath)
	if !decodeValidation.OK {
		writeJSON(map[string]any{"ok": false, "error_code": "NODE_EXPORT_FAILED", "error": "Unable to decode the node configuration."})
		return nil
	}
	for _, node := range value.Nodes {
		if node.ID != *nodeID {
			continue
		}
		uri, err := subscription.EncodeURI(node)
		if err != nil {
			writeJSON(map[string]any{"ok": false, "error_code": "NODE_EXPORT_UNSUPPORTED", "error": err.Error()})
			return nil
		}
		writeJSON(map[string]any{"ok": true, "uri": uri})
		return nil
	}
	writeJSON(map[string]any{"ok": false, "error_code": "NODE_NOT_FOUND", "error": "The requested Node does not exist."})
	return nil
}

func runRuntime(args []string) error {
	flags := flag.NewFlagSet("_runtime", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	seedDirectory := flags.String("geodata", "/usr/share/steer/geodata-seed", "package Geo seed")
	singBoxBinary := flags.String("sing-box", "/usr/bin/sing-box", "sing-box executable")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("_runtime accepts flags only")
	}
	result := map[string]any{"steer": version, "canonical_schema": model.SchemaVersion}
	if output, err := exec.Command(*singBoxBinary, "version").CombinedOutput(); err == nil {
		report := capability.Parse(string(output), nil)
		result["sing_box"] = map[string]any{"version": report.Version, "tags": report.Tags}
	} else {
		result["sing_box"] = map[string]any{"error": strings.TrimSpace(string(output))}
	}
	if manifest, err := geodata.ReadManifest(*seedDirectory); err == nil {
		result["geodata"] = map[string]any{"version": manifest.Upstream.Version, "rule_count": len(manifest.Rules)}
	} else {
		result["geodata_error"] = err.Error()
	}
	writeJSON(result)
	return nil
}

func runParseNodes(args []string) error {
	flags := flag.NewFlagSet("_parse-nodes", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	outputPath := flags.String("output", "", "private JSON result path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *outputPath == "" || !filepath.IsAbs(*outputPath) {
		return errors.New("_parse-nodes requires an absolute --output path")
	}
	content, err := io.ReadAll(io.LimitReader(os.Stdin, (16<<20)+1))
	if err != nil {
		return fmt.Errorf("read node import: %w", err)
	}
	if len(content) > 16<<20 {
		return errors.New("node import exceeds 16 MiB")
	}
	parsed, err := subscription.ParseList(string(content))
	if err != nil {
		return err
	}
	if len(parsed.Nodes) == 0 {
		return fmt.Errorf("node import contained no valid nodes (%d skipped)", parsed.Skipped)
	}
	encoded, err := json.Marshal(struct {
		Nodes          []model.Node                 `json:"nodes"`
		Skipped        int                          `json:"skipped"`
		SkippedReasons []subscription.SkippedReason `json:"skipped_reasons,omitempty"`
	}{Nodes: parsed.Nodes, Skipped: parsed.Skipped, SkippedReasons: parsed.SkippedReasons})
	if err != nil {
		return err
	}
	file, err := os.OpenFile(*outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create private node import result: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write private node import result: %w", err)
	}
	return nil
}

func runValidate(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/config/steer", "UCI configuration file")
	seedDirectory := flags.String("seed-dir", "/usr/share/steer/geodata-seed", "package-owned Geo seed directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("validate accepts flags only")
	}
	value, decodeValidation := loadIntent(*configPath)
	if !decodeValidation.OK {
		writeJSON(decodeValidation)
		return errors.New("configuration decode failed")
	}
	validation := openwrt.ValidateWithGeoDataDirectory(value, *seedDirectory)
	writeJSON(validation)
	if !validation.OK {
		return errors.New("configuration validation failed")
	}
	return nil
}

func runApply(args []string) error {
	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	enabledText := flags.String("enabled", "", "change only the saved enabled state before applying")
	configPath := flags.String("config", "/etc/config/steer", "UCI configuration file")
	singBoxPath := flags.String("sing-box", "/usr/bin/sing-box", "sing-box binary")
	runDirectory := flags.String("run-dir", "/run/steer", "runtime state directory")
	stateDirectory := flags.String("state-dir", "/var/lib/steer", "generated state directory")
	seedDirectory := flags.String("seed-dir", "/usr/share/steer/geodata-seed", "package-owned Geo seed directory")
	nftBinary := flags.String("nft", "/usr/sbin/nft", "nft binary")
	initScript := flags.String("init", "/etc/init.d/steer", "procd init script")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("apply accepts flags only")
	}
	if *enabledText != "" && *enabledText != "true" && *enabledText != "false" {
		return errors.New("enabled must be true or false")
	}
	saved := false
	return runLockedApplyOutput(*runDirectory, func() (coreapply.Result, error) {
		value, decodeValidation := loadIntent(*configPath)
		if !decodeValidation.OK {
			return coreapply.Result{Validation: &decodeValidation}, errors.New("configuration decode failed")
		}
		if *enabledText != "" {
			value.Main.Enabled = *enabledText == "true"
		}
		validation := openwrt.ValidateWithGeoDataDirectory(value, *seedDirectory)
		if !validation.OK {
			return coreapply.Result{Validation: &validation}, openwrt.ValidationError{Validation: validation}
		}
		if *enabledText != "" {
			if err := openwrt.SetSavedEnabled(context.Background(), *configPath, value.Main.Enabled); err != nil {
				return coreapply.Result{}, err
			}
			saved = true
		}
		backend := openwrt.NewBackend(openwrt.ExecRunner{}, value, openwrt.BackendOptions{
			RunDirectory: *runDirectory, StateDirectory: *stateDirectory, SingBoxBinary: *singBoxPath,
			GeoDataDirectory: *seedDirectory, NFTBinary: *nftBinary, InitScript: *initScript,
		})
		return coreapply.Run(context.Background(), value, backend.CompilerOptions(), backend)
	}, func(result coreapply.Result) {
		if *enabledText == "" {
			writeJSON(result)
			return
		}
		writeJSON(struct {
			coreapply.Result
			Saved   bool `json:"saved"`
			Applied bool `json:"applied"`
		}{Result: result, Saved: saved, Applied: result.OK})
	})
}

func runLockedApply(runDirectory string, operation func() (coreapply.Result, error)) error {
	return runLockedApplyOutput(runDirectory, operation, func(result coreapply.Result) { writeJSON(result) })
}

func runLockedApplyOutput(runDirectory string, operation func() (coreapply.Result, error), output func(coreapply.Result)) error {
	lock, err := acquireApplyLock(runDirectory)
	if err != nil {
		return err
	}
	defer lock.Close()
	result, err := operation()
	if err != nil {
		result.OK = false
		result.Error = err.Error()
	}
	if recordErr := writeApplyRecord(runDirectory, coreapply.Record{Sequence: strconv.FormatInt(time.Now().UnixNano(), 10), Result: result}); recordErr != nil {
		combined := recordErr
		if err != nil {
			combined = errors.Join(err, recordErr)
		}
		result.OK = false
		result.Error = combined.Error()
		output(result)
		return combined
	}
	output(result)
	return err
}

func acquireApplyLock(runDirectory string) (*os.File, error) {
	ctx, cancel := context.WithTimeout(context.Background(), applyLockTimeout)
	defer cancel()
	return acquireApplyLockContext(ctx, runDirectory)
}

func acquireApplyLockContext(ctx context.Context, runDirectory string) (*os.File, error) {
	if err := os.MkdirAll(runDirectory, 0o755); err != nil {
		return nil, fmt.Errorf("create runtime directory for Apply lock: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(runDirectory, "apply.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open Apply lock: %w", err)
	}
	for {
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			lock.Close()
			return nil, fmt.Errorf("lock Apply transaction: %w", err)
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			lock.Close()
			return nil, fmt.Errorf("lock Apply transaction: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func runServiceStart(args []string) error {
	flags := flag.NewFlagSet("_start", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/config/steer", "UCI configuration file")
	singBoxPath := flags.String("sing-box", "/usr/bin/sing-box", "sing-box binary")
	stateDirectory := flags.String("state-dir", "/var/lib/steer", "generated state directory")
	seedDirectory := flags.String("seed-dir", "/usr/share/steer/geodata-seed", "package-owned Geo seed directory")
	runDirectory := flags.String("run-dir", "/run/steer", "runtime state directory")
	nftBinary := flags.String("nft", "/usr/sbin/nft", "nft binary")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("_start accepts flags only")
	}
	lock, err := acquireApplyLock(*runDirectory)
	if err != nil {
		return err
	}
	defer lock.Close()
	value, decodeValidation := loadIntent(*configPath)
	if !decodeValidation.OK {
		return errors.New(decodeValidation.Errors[0].Message)
	}
	validation := openwrt.ValidateWithGeoDataDirectory(value, *seedDirectory)
	if !validation.OK {
		issue := validation.Errors[0]
		return fmt.Errorf("configuration validation failed at %s %q option %q: %s", issue.ObjectType, issue.ObjectID, issue.Option, issue.Message)
	}
	if !value.Main.Enabled {
		return errors.New("cannot start a disabled configuration")
	}
	backend := openwrt.NewBackend(openwrt.ExecRunner{}, value, openwrt.BackendOptions{
		RunDirectory: *runDirectory, StateDirectory: *stateDirectory, GeoDataDirectory: *seedDirectory, SingBoxBinary: *singBoxPath, NFTBinary: *nftBinary,
	})
	compiled := compiler.Compile(value, backend.CompilerOptions())
	candidate, err := backend.Prepare(context.Background(), value, compiled)
	if err != nil {
		return err
	}
	if err := backend.ActivateForServiceStart(context.Background(), candidate); err != nil {
		return err
	}
	return backend.Finalize(context.Background(), candidate)
}

func runStatus(args []string) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	runDirectory := flags.String("run-dir", "/run/steer", "runtime state directory")
	nftBinary := flags.String("nft", "/usr/sbin/nft", "nft binary")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("status accepts flags only")
	}
	writeJSON(openwrt.ReadStatus(context.Background(), openwrt.ExecRunner{}, *runDirectory, *nftBinary))
	return nil
}

func runHealth(args []string) error {
	flags := flag.NewFlagSet("health", flag.ContinueOnError)
	runDirectory := flags.String("run-dir", "/run/steer", "runtime state directory")
	nftBinary := flags.String("nft", "/usr/sbin/nft", "nft binary")
	timeout := flags.Duration("timeout", 10*time.Second, "local readiness deadline")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("health accepts flags only")
	}
	return openwrt.WaitCurrentHealthy(context.Background(), openwrt.ExecRunner{}, *runDirectory, *nftBinary, *timeout)
}

func runProbe(args []string) error {
	flags := flag.NewFlagSet("probe", flag.ContinueOnError)
	runDirectory := flags.String("run-dir", "/run/steer", "runtime state directory")
	configPath := flags.String("config", "/etc/config/steer", "UCI configuration file")
	stateDirectory := flags.String("state-dir", "/var/lib/steer", "generated state directory")
	singBoxPath := flags.String("sing-box", "/usr/bin/sing-box", "sing-box binary")
	kind := flags.String("kind", "direct", "probe kind: direct, proxy or speedtest")
	nodeID := flags.String("node", "", "run a temporary test through this node")
	routeID := flags.String("route", "", "run a temporary test through this route and its detour chain")
	download := flags.Bool("download", false, "download the complete speed-test response")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("probe accepts flags only")
	}
	if *nodeID != "" && *routeID != "" {
		return errors.New("--node and --route are mutually exclusive")
	}
	if *nodeID != "" || *routeID != "" {
		if *kind != "speedtest" {
			return errors.New("--node and --route require --kind speedtest")
		}
		var err error
		if *nodeID != "" {
			_, err = openwrt.SpeedTestNode(context.Background(), *configPath, *stateDirectory, *singBoxPath, *nodeID, *download)
		} else {
			_, err = openwrt.SpeedTestRoute(context.Background(), *configPath, *stateDirectory, *singBoxPath, *routeID, *download)
		}
		scope, objectID := "nodes", *nodeID
		if *routeID != "" {
			scope, objectID = "routes", *routeID
		}
		testKind := "connect"
		if *download {
			testKind = "download"
		}
		results := openwrt.ReadLatestProbeResults(*configPath, *runDirectory, *stateDirectory)
		if latest, ok := coreprobe.FindLatestProbeResult(results, scope, objectID, testKind); ok {
			writeJSON(latest)
			return nil
		}
		if err == nil {
			err = errors.New("latest probe result was not persisted")
		}
		return err
	}
	_, err := openwrt.ProbeOverview(context.Background(), *configPath, *runDirectory, *stateDirectory, *kind, nil)
	results := openwrt.ReadLatestProbeResults(*configPath, *runDirectory, *stateDirectory)
	if latest, ok := coreprobe.FindLatestProbeResult(results, "overview", "", *kind); ok {
		writeJSON(latest)
		return nil
	}
	if err == nil {
		err = errors.New("latest probe result was not persisted")
	}
	return err
}

func runSubscription(args []string) error {
	if len(args) == 0 || (args[0] != "update" && args[0] != "status" && args[0] != "clean") {
		return errors.New("usage: steer subscription update|status [--id ID] | clean --id ID --node NODE_ID")
	}
	command := args[0]
	flags := flag.NewFlagSet("subscription", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/config/steer", "UCI configuration file")
	stateDirectory := flags.String("state-dir", "/var/lib/steer", "subscription snapshot directory")
	id := flags.String("id", "", "only update this subscription ID")
	nodeID := flags.String("node", "", "subscription node ID for clean")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("subscription subcommands accept flags only")
	}
	if command == "status" {
		statuses, err := openwrt.ReadSubscriptionStatus(*configPath, *stateDirectory)
		if err != nil {
			return err
		}
		writeJSON(struct {
			OK            bool                         `json:"ok"`
			Subscriptions []openwrt.SubscriptionStatus `json:"subscriptions"`
		}{true, statuses})
		return nil
	}
	if command == "clean" {
		if *id == "" || *nodeID == "" {
			return errors.New("subscription clean requires --id and --node")
		}
		_, err := openwrt.CleanSubscriptionNode(*configPath, *stateDirectory, *id, *nodeID)
		if err != nil {
			writeJSON(struct {
				OK    bool   `json:"ok"`
				Error string `json:"error"`
			}{false, err.Error()})
			return err
		}
		statuses, err := openwrt.ReadSubscriptionStatus(*configPath, *stateDirectory)
		if err != nil {
			return err
		}
		writeJSON(struct {
			OK            bool                         `json:"ok"`
			Subscriptions []openwrt.SubscriptionStatus `json:"subscriptions"`
		}{true, statuses})
		return nil
	}
	_, err := openwrt.UpdateConfiguredSubscriptions(context.Background(), &http.Client{Timeout: 30 * time.Second}, *configPath, *stateDirectory, *id)
	if err != nil {
		writeJSON(struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}{false, err.Error()})
		return err
	}
	statuses, err := openwrt.ReadSubscriptionStatus(*configPath, *stateDirectory)
	if err != nil {
		return err
	}
	writeJSON(struct {
		OK            bool                         `json:"ok"`
		Subscriptions []openwrt.SubscriptionStatus `json:"subscriptions"`
	}{true, statuses})
	return nil
}

func runGeoCatalog(args []string) error {
	flags := flag.NewFlagSet("geo-catalog", flag.ContinueOnError)
	kind := flags.String("kind", "", "geosite or geoip")
	seedDirectory := flags.String("seed-dir", "/usr/share/steer/geodata-seed", "package-owned Geo seed directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("geo-catalog accepts flags only")
	}
	names, err := openwrt.GeoCatalog(*seedDirectory, *kind)
	if err != nil {
		return err
	}
	writeJSON(struct {
		Kind  string   `json:"kind"`
		Names []string `json:"names"`
	}{*kind, names})
	return nil
}

func runCleanup(args []string) error {
	flags := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	nftBinary := flags.String("nft", "/usr/sbin/nft", "nft binary")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("cleanup accepts flags only")
	}
	return openwrt.CleanupPlatform(context.Background(), openwrt.ExecRunner{}, *nftBinary)
}

func writeApplyRecord(runDirectory string, record coreapply.Record) error {
	if err := os.MkdirAll(runDirectory, 0o755); err != nil {
		return fmt.Errorf("create runtime directory for Apply result: %w", err)
	}
	temporary, err := os.CreateTemp(runDirectory, ".last-apply.*")
	if err != nil {
		return fmt.Errorf("create Apply result: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(record); err != nil {
		temporary.Close()
		return fmt.Errorf("encode Apply result: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync Apply result: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close Apply result: %w", err)
	}
	if err := os.Rename(temporaryPath, filepath.Join(runDirectory, "last-apply.json")); err != nil {
		return fmt.Errorf("publish Apply result: %w", err)
	}
	return nil
}

func readJSON(path string, value any) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	if err := json.NewDecoder(file).Decode(value); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func loadIntent(path string) (model.Intent, model.Validation) {
	file, err := os.Open(path)
	if err != nil {
		return model.Intent{}, decodeFailure(fmt.Sprintf("open configuration: %v", err))
	}
	defer file.Close()
	value, err := openwrt.Decode(file)
	if err != nil {
		return model.Intent{}, decodeFailure(err.Error())
	}
	return value, model.Validation{OK: true, Errors: []model.Issue{}, Warnings: []model.Issue{}}
}

func decodeFailure(message string) model.Validation {
	return model.Validation{Errors: []model.Issue{{Code: "DECODE_FAILED", ObjectType: "uci", Message: message}}, Warnings: []model.Issue{}}
}

func writeJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func usage() error {
	return errors.New("usage: steer version|validate|apply|health|status|probe|subscription|geo-catalog|cleanup [flags]")
}
