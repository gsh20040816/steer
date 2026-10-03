// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	coreapply "github.com/gsh20040816/steer/go/internal/apply"
	"github.com/gsh20040816/steer/go/internal/geodata"
	model "github.com/gsh20040816/steer/go/internal/intent"
	linuxplatform "github.com/gsh20040816/steer/go/internal/platform/linux"
	coreprobe "github.com/gsh20040816/steer/go/internal/probe"
	"github.com/gsh20040816/steer/go/internal/subscription"
)

func (app webApplication) handleOverview(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	store := linuxplatform.IntentStore{Path: app.ConfigPath, GeoDataDirectory: app.seedDirectory()}
	value, revision, loadErr := store.Load()
	validation := model.Validation{OK: false}
	if loadErr == nil {
		validation = linuxplatform.ValidateWithGeoDataDirectory(value, app.seedDirectory())
	}
	runner := app.runner()
	options := linuxplatform.BackendOptions{
		RunDirectory: app.RunDirectory, StateDirectory: app.StateDirectory, GeoDataDirectory: app.seedDirectory(),
	}
	status := linuxplatform.ReadStatus(request.Context(), runner, options)
	pendingApply := loadErr == nil && validation.OK && linuxplatform.HasPendingApply(value, status, options)
	writeWebJSON(writer, map[string]any{
		"saved_revision": revision, "saved_valid": loadErr == nil && validation.OK, "pending_apply": pendingApply,
		"saved_enabled": loadErr == nil && value.Main.Enabled,
		"validation":    validation, "status": status, "error": errorString(loadErr),
	})
}

func (app webApplication) handleValidate(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 2<<20))
	if err != nil {
		writeWebError(writer, err, http.StatusBadRequest)
		return
	}
	value, _, err := decodeIntentPayload(body)
	if err != nil {
		writeWebError(writer, err, http.StatusBadRequest)
		return
	}
	validation := linuxplatform.ValidateWithGeoDataDirectory(value, app.seedDirectory())
	writeWebJSON(writer, validation)
}

func (app webApplication) handleApply(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	result, err := app.applySaved()
	if err != nil {
		writeWebJSONStatus(writer, result, http.StatusUnprocessableEntity)
		return
	}
	writeWebJSON(writer, result)
}

func (app webApplication) handleNodeImport(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		Document string `json:"document"`
	}
	if err := json.NewDecoder(io.LimitReader(request.Body, 16<<20)).Decode(&payload); err != nil {
		writeWebError(writer, errors.New("request requires a node document"), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(payload.Document) == "" {
		writeWebError(writer, errors.New("request requires a node document"), http.StatusBadRequest)
		return
	}
	parsed := subscription.ParseList(payload.Document)
	if len(parsed.Nodes) == 0 {
		writeWebError(writer, fmt.Errorf("node import contained no valid nodes (%d skipped)", parsed.Skipped), http.StatusUnprocessableEntity)
		return
	}
	writeWebJSON(writer, map[string]any{
		"nodes": parsed.Nodes, "skipped": parsed.Skipped, "skipped_reasons": parsed.SkippedReasons,
	})
}

func (app webApplication) handleNodeExport(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		Node model.Node `json:"node"`
	}
	decoder := json.NewDecoder(io.LimitReader(request.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		writeWebError(writer, errors.New("request requires a Canonical node"), http.StatusBadRequest)
		return
	}
	uri, err := subscription.EncodeURI(payload.Node)
	if err != nil {
		writeWebError(writer, err, http.StatusUnprocessableEntity)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeWebJSON(writer, map[string]any{"uri": uri})
}

func (app webApplication) handleProbes(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/api/v1/probes/")
	body, _ := io.ReadAll(io.LimitReader(request.Body, 64<<10))
	var payload struct {
		Kind     string `json:"kind"`
		Download bool   `json:"download"`
	}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			writeWebError(writer, err, http.StatusBadRequest)
			return
		}
	}
	if path == "overview" {
		kind := payload.Kind
		if kind == "" {
			kind = "direct"
		}
		if kind != "direct" && kind != "proxy" && kind != "speedtest" {
			writeWebError(writer, errors.New("probe kind must be direct, proxy or speedtest"), http.StatusBadRequest)
			return
		}
		_, probeErr := linuxplatform.ProbeOverviewWithState(request.Context(), app.ConfigPath, app.RunDirectory, app.StateDirectory, kind, nil)
		app.writeLatestProbeResult(writer, "overview", "", kind, probeErr)
		return
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 2 && (parts[0] == "nodes" || parts[0] == "routes") {
		var probeErr error
		if parts[0] == "nodes" {
			_, probeErr = linuxplatform.SpeedTestNode(request.Context(), app.ConfigPath, app.StateDirectory, "/usr/bin/sing-box", parts[1], payload.Download)
		} else {
			_, probeErr = linuxplatform.SpeedTestRoute(request.Context(), app.ConfigPath, app.StateDirectory, "/usr/bin/sing-box", parts[1], payload.Download)
		}
		kind := "connect"
		if payload.Download {
			kind = "download"
		}
		app.writeLatestProbeResult(writer, parts[0], parts[1], kind, probeErr)
		return
	}
	http.NotFound(writer, request)
}

func (app webApplication) writeLatestProbeResult(writer http.ResponseWriter, scope, objectID, kind string, probeErr error) {
	results := linuxplatform.ReadLatestProbeResults(app.ConfigPath, app.RunDirectory, app.StateDirectory)
	if result, ok := coreprobe.FindLatestProbeResult(results, scope, objectID, kind); ok {
		writeWebJSON(writer, result)
		return
	}
	if probeErr == nil {
		probeErr = errors.New("latest probe result was not persisted")
	}
	writeWebError(writer, probeErr, http.StatusUnprocessableEntity)
}

func (app webApplication) handleSubscriptions(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet && request.URL.Path == "/api/v1/subscriptions" {
		statuses, err := linuxplatform.ReadSubscriptionStatus(app.ConfigPath, app.StateDirectory)
		if err != nil {
			writeWebError(writer, err, http.StatusInternalServerError)
			return
		}
		writeWebJSON(writer, map[string]any{"subscriptions": statuses})
		return
	}
	prefix := "/api/v1/subscriptions/"
	if !strings.HasPrefix(request.URL.Path, prefix) {
		http.NotFound(writer, request)
		return
	}
	remainder := strings.TrimPrefix(request.URL.Path, prefix)
	parts := strings.Split(strings.TrimSuffix(remainder, "/"), "/")
	if len(parts) == 2 && parts[1] == "update" && request.Method == http.MethodPost {
		err := withOperationLock(app.RunDirectory, func() error {
			var err error
			_, err = linuxplatform.UpdateConfiguredSubscriptions(request.Context(), &http.Client{Timeout: 30 * time.Second}, app.ConfigPath, app.StateDirectory, parts[0])
			return err
		})
		if err != nil {
			writeWebError(writer, err, http.StatusUnprocessableEntity)
			return
		}
		statuses, err := linuxplatform.ReadSubscriptionStatus(app.ConfigPath, app.StateDirectory)
		if err != nil {
			writeWebError(writer, err, http.StatusInternalServerError)
			return
		}
		writeWebJSON(writer, map[string]any{"subscriptions": statuses})
		return
	}
	if len(parts) == 3 && parts[1] == "nodes" && request.Method == http.MethodDelete {
		err := withOperationLock(app.RunDirectory, func() error {
			var err error
			_, err = linuxplatform.CleanSubscriptionNode(app.ConfigPath, app.StateDirectory, parts[0], parts[2])
			return err
		})
		if err != nil {
			writeWebError(writer, err, http.StatusUnprocessableEntity)
			return
		}
		statuses, err := linuxplatform.ReadSubscriptionStatus(app.ConfigPath, app.StateDirectory)
		if err != nil {
			writeWebError(writer, err, http.StatusInternalServerError)
			return
		}
		writeWebJSON(writer, map[string]any{"subscriptions": statuses})
		return
	}
	http.NotFound(writer, request)
}

func (app webApplication) handleGeoData(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	kind := strings.TrimPrefix(request.URL.Path, "/api/v1/geodata/")
	if kind != "geosite" && kind != "geoip" {
		writeWebError(writer, errors.New("Geo kind must be geosite or geoip"), http.StatusBadRequest)
		return
	}
	writeWebJSON(writer, app.geoDataStatus(request.Context(), kind))
}

type geoDataStatus struct {
	Kind     string           `json:"kind"`
	Readable bool             `json:"readable"`
	Count    int              `json:"count"`
	Names    []string         `json:"names"`
	Error    *webErrorDetails `json:"error,omitempty"`
}

func (app webApplication) geoDataStatus(_ context.Context, kind string) geoDataStatus {
	status := geoDataStatus{Kind: kind, Names: []string{}}
	names, err := geodata.Catalog(app.seedDirectory(), kind)
	if err != nil {
		status.Error = errorDetails(err)
		return status
	}
	status.Readable = true
	status.Names = names
	status.Count = len(names)
	return status
}

func (app webApplication) applySaved() (coreapply.Result, error) {
	return runLockedApplyResult(app.RunDirectory, func() (coreapply.Result, error) {
		value, _, err := (linuxplatform.IntentStore{Path: app.ConfigPath, GeoDataDirectory: app.seedDirectory()}).Load()
		if err != nil {
			validation := model.Validation{Errors: []model.Issue{{Code: "DECODE_FAILED", ObjectType: "json", Message: err.Error()}}, Warnings: []model.Issue{}}
			return coreapply.Result{Validation: &validation}, err
		}
		return app.applyValue(value)
	})
}

func (app webApplication) applyValue(value model.Intent) (coreapply.Result, error) {
	validation := linuxplatform.ValidateWithGeoDataDirectory(value, app.seedDirectory())
	if !validation.OK {
		return coreapply.Result{Validation: &validation}, linuxplatform.ValidationError{Validation: validation}
	}
	runner := app.runner()
	backend := linuxplatform.NewBackend(runner, value, linuxplatform.BackendOptions{
		RunDirectory: app.RunDirectory, StateDirectory: app.StateDirectory, GeoDataDirectory: app.seedDirectory(),
	})
	return coreapply.Run(context.Background(), value, backend.CompilerOptions(), backend)
}

func decodeIntentPayload(body []byte) (model.Intent, bool, error) {
	var wrapper struct {
		Intent json.RawMessage `json:"intent"`
		Apply  bool            `json:"apply"`
	}
	if err := json.Unmarshal(body, &wrapper); err == nil && len(wrapper.Intent) > 0 {
		value, err := model.DecodeJSON(bytes.NewReader(wrapper.Intent))
		return value, wrapper.Apply, err
	}
	value, err := model.DecodeJSON(bytes.NewReader(body))
	return value, false, err
}

func writeWebJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}

func writeWebJSONStatus(writer http.ResponseWriter, value any, status int) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func writeWebError(writer http.ResponseWriter, err error, status int) {
	writeWebJSONStatus(writer, map[string]any{"error": err.Error()}, status)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
