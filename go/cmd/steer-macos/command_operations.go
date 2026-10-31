// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gsh20040816/steer/go/internal/geodata"
	macosplatform "github.com/gsh20040816/steer/go/internal/platform/macos"
	"github.com/gsh20040816/steer/go/internal/probe"
	"github.com/gsh20040816/steer/go/internal/subscription"
)

type probeSelection struct {
	Kind     string
	NodeID   string
	RouteID  string
	Download bool
}

// probeResponse is the stable helper-to-GUI payload.
type probeResponse struct {
	macosplatform.TestReport
}

func runDiagnostics(args []string, stdoutWriter interface{ Write([]byte) (int, error) }) error {
	flags := flag.NewFlagSet("_diagnostics", flag.ContinueOnError)
	socketPath := flags.String("socket", defaultControlSocket, "root control service socket")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("_diagnostics accepts flags only")
	}
	response, err := sendControlRequest(*socketPath, controlRequest{SchemaVersion: controlSchemaVersion, Operation: "diagnostics"})
	if err != nil {
		return err
	}
	if !response.OK {
		return errors.New(response.Error)
	}
	var diagnostics probe.Diagnostics
	if err := decodeStrictJSON(response.Payload, &diagnostics); err != nil {
		return fmt.Errorf("decode diagnostics response: %w", err)
	}
	return writeJSON(stdoutWriter, diagnostics)
}

func runProbeResults(args []string, stdoutWriter interface{ Write([]byte) (int, error) }) error {
	flags := flag.NewFlagSet("_probe-results", flag.ContinueOnError)
	socketPath := flags.String("socket", defaultControlSocket, "root control service socket")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("_probe-results accepts flags only")
	}
	response, err := sendControlRequest(*socketPath, controlRequest{SchemaVersion: controlSchemaVersion, Operation: "probe-results"})
	if err != nil {
		return err
	}
	if !response.OK {
		return errors.New(response.Error)
	}
	var results probe.LatestProbeResults
	if err := decodeStrictJSON(response.Payload, &results); err != nil {
		return fmt.Errorf("decode latest probe results response: %w", err)
	}
	return writeJSON(stdoutWriter, results)
}

func runProbe(args []string, stdoutWriter interface{ Write([]byte) (int, error) }) error {
	flags := flag.NewFlagSet("probe", flag.ContinueOnError)
	socketPath := flags.String("socket", defaultControlSocket, "root control service socket")
	kind := flags.String("kind", "direct", "direct, proxy or speedtest")
	nodeID := flags.String("node", "", "test one node")
	routeID := flags.String("route", "", "test one route chain")
	download := flags.Bool("download", false, "download the complete response")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || (*nodeID != "" && *routeID != "") {
		return errors.New("probe accepts flags only and node/route are mutually exclusive")
	}
	response, err := sendControlRequest(*socketPath, controlRequest{
		SchemaVersion: controlSchemaVersion,
		Operation:     "probe",
		Kind:          *kind,
		NodeID:        *nodeID,
		RouteID:       *routeID,
		Download:      *download,
	})
	if err != nil {
		return err
	}
	if !response.OK {
		if response.Error == "" {
			response.Error = "probe operation failed"
		}
		return errors.New(response.Error)
	}
	var result probe.LatestProbeResult
	if err := decodeStrictJSON(response.Payload, &result); err != nil {
		return fmt.Errorf("decode latest probe result response: %w", err)
	}
	if err := writeJSON(stdoutWriter, result); err != nil {
		return err
	}
	if !result.OK {
		return errors.New("HTTPS probe failed")
	}
	return nil
}

func performProbe(ctx context.Context, configPath string, options macosplatform.BackendOptions, selection probeSelection) (probeResponse, error) {
	if selection.NodeID != "" && selection.RouteID != "" {
		return probeResponse{}, errors.New("probe node and route are mutually exclusive")
	}
	if selection.Kind != "direct" && selection.Kind != "proxy" && selection.Kind != "speedtest" {
		return probeResponse{}, fmt.Errorf("unsupported probe kind %q", selection.Kind)
	}
	if selection.NodeID == "" && selection.RouteID == "" {
		report, err := macosplatform.ProbeOverview(ctx, configPath, options.StateDirectory, options.RunDirectory, selection.Kind, nil)
		if err != nil {
			return probeResponse{}, err
		}
		return probeResponse{TestReport: report}, nil
	}
	if selection.Kind != "speedtest" {
		return probeResponse{}, errors.New("node and route probes require kind speedtest")
	}
	var report macosplatform.TestReport
	var err error
	if selection.NodeID != "" {
		report, err = macosplatform.SpeedTestNode(ctx, configPath, options.StateDirectory, options.SingBoxBinary, selection.NodeID, selection.Download)
	} else {
		report, err = macosplatform.SpeedTestRoute(ctx, configPath, options.StateDirectory, options.SingBoxBinary, selection.RouteID, selection.Download)
	}
	if err != nil {
		return probeResponse{}, err
	}
	return probeResponse{TestReport: report}, nil
}

func runGeoCatalog(args []string, stdoutWriter interface{ Write([]byte) (int, error) }) error {
	flags := flag.NewFlagSet("geo-catalog", flag.ContinueOnError)
	kind := flags.String("kind", "", "geosite or geoip")
	seedDirectory := flags.String("seed-dir", defaultGeoDataDir, "package-owned Geo seed")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("geo-catalog accepts flags only")
	}
	names, err := geodata.Catalog(*seedDirectory, *kind)
	if err != nil {
		return err
	}
	return writeJSON(stdoutWriter, struct {
		Kind  string   `json:"kind"`
		Names []string `json:"names"`
	}{*kind, names})
}

func runSubscription(args []string, stdoutWriter interface{ Write([]byte) (int, error) }) error {
	commands := map[string]bool{"update": true, "status": true, "clean": true, "nodes": true, "node": true}
	if len(args) == 0 || !commands[args[0]] {
		return errors.New("usage: steer-macos subscription update|status|nodes [--id ID] | clean --id ID --node NODE_ID | node --id ID --node NODE_ID --enabled 0|1")
	}
	command := args[0]
	flags := flag.NewFlagSet("subscription", flag.ContinueOnError)
	configPath := flags.String("config", defaultConfigPath, "canonical JSON configuration")
	stateDirectory := flags.String("state-dir", defaultStateDirectory, "subscription state and node store directory")
	runDirectory := flags.String("run-dir", defaultRunDirectory, "operation lock directory")
	id := flags.String("id", "", "subscription ID")
	nodeID := flags.String("node", "", "subscription node ID")
	enabledText := flags.String("enabled", "", "node enabled state for node: 0 or 1")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("subscription subcommands accept flags only")
	}
	if (command == "clean" || command == "node") && (*id == "" || *nodeID == "") {
		return fmt.Errorf("subscription %s requires --id and --node", command)
	}
	if command == "node" && *enabledText != "0" && *enabledText != "1" {
		return errors.New("subscription node requires --enabled 0 or 1")
	}
	if command != "node" && *enabledText != "" {
		return errors.New("--enabled is only accepted by subscription node")
	}
	fail := func(err error) error {
		if writeErr := writeJSON(stdoutWriter, subscription.ErrorPayloadOf(err)); writeErr != nil {
			return writeErr
		}
		return err
	}
	switch command {
	case "status":
		statuses, err := macosplatform.ReadSubscriptionStatus(*configPath, *stateDirectory)
		if err != nil {
			return fail(err)
		}
		return writeSubscriptionStatuses(stdoutWriter, statuses)
	case "nodes":
		list, err := macosplatform.ListSubscriptionNodes(*configPath, *stateDirectory, *id)
		if err != nil {
			return fail(err)
		}
		return writeJSON(stdoutWriter, list)
	}
	lock, err := acquireLock(*runDirectory)
	if err != nil {
		return fail(err)
	}
	defer lock.Close()
	var operationErr error
	var snapshots []macosplatform.SubscriptionSnapshot
	switch command {
	case "node":
		operationErr = macosplatform.SetSubscriptionNodeEnabled(*configPath, *stateDirectory, *id, *nodeID, *enabledText == "1")
	case "clean":
		operationErr = macosplatform.CleanSubscriptionNode(*configPath, *stateDirectory, *id, *nodeID)
	default:
		snapshots, operationErr = macosplatform.UpdateConfiguredSubscriptions(context.Background(), &http.Client{Timeout: 30 * time.Second}, *configPath, *stateDirectory, *id)
	}
	if os.Geteuid() == 0 {
		adminGID, err := lookupAdminGID()
		if err != nil {
			return fail(err)
		}
		if err := setSubscriptionUpdatePermissions(*stateDirectory, adminGID, snapshots, operationErr); err != nil {
			return fail(err)
		}
	}
	if operationErr != nil {
		return fail(operationErr)
	}
	if command == "node" {
		list, err := macosplatform.ListSubscriptionNodes(*configPath, *stateDirectory, "")
		if err != nil {
			return fail(err)
		}
		return writeJSON(stdoutWriter, list)
	}
	statuses, err := macosplatform.ReadSubscriptionStatus(*configPath, *stateDirectory)
	if err != nil {
		return fail(err)
	}
	return writeSubscriptionStatuses(stdoutWriter, statuses)
}

func writeSubscriptionStatuses(stdoutWriter interface{ Write([]byte) (int, error) }, statuses []macosplatform.SubscriptionStatus) error {
	return writeJSON(stdoutWriter, struct {
		OK            bool                               `json:"ok"`
		Subscriptions []macosplatform.SubscriptionStatus `json:"subscriptions"`
	}{true, statuses})
}
