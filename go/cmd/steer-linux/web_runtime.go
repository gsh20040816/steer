// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/gsh20040816/steer/go/internal/capability"
	"github.com/gsh20040816/steer/go/internal/geodata"
	model "github.com/gsh20040816/steer/go/internal/intent"
	linuxplatform "github.com/gsh20040816/steer/go/internal/platform/linux"
)

type runtimeTool struct {
	Version string   `json:"version,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type runtimeInfo struct {
	Steer           string         `json:"steer"`
	SingBox         runtimeTool    `json:"sing_box"`
	GeoData         runtimeGeoData `json:"geodata"`
	CanonicalSchema int            `json:"canonical_schema"`
	WebListen       string         `json:"web_listen"`
}

type runtimeGeoData struct {
	Version   string `json:"version,omitempty"`
	RuleCount int    `json:"rule_count,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (app webApplication) handleRuntime(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeWebJSON(writer, app.runtimeInfo(request.Context()))
}

func (app webApplication) handleLogs(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	runner := app.runner()
	output, err := runner.Output(request.Context(), "/usr/bin/journalctl",
		"-u", "steer.service", "-u", "steer-web.service", "-u", "steer-subscription.service",
		"-n", "300", "--no-pager", "--output=short-iso")
	if err != nil {
		writeWebError(writer, err, http.StatusUnprocessableEntity)
		return
	}
	writeWebJSON(writer, map[string]any{"output": string(output)})
}

func (app webApplication) handleDiagnostics(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeWebJSON(writer, linuxplatform.ReadDiagnostics(app.ConfigPath, app.RunDirectory, app.StateDirectory))
}

func (app webApplication) handleProbeResults(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeWebJSON(writer, linuxplatform.ReadLatestProbeResults(app.ConfigPath, app.RunDirectory, app.StateDirectory))
}

func (app webApplication) runtimeInfo(ctx context.Context) runtimeInfo {
	info := runtimeInfo{
		Steer:           version,
		CanonicalSchema: model.SchemaVersion,
		WebListen:       app.ListenAddress,
	}
	runner := app.runner()

	singBoxOutput, err := runner.Output(ctx, "/usr/bin/sing-box", "version")
	if err != nil {
		info.SingBox.Error = err.Error()
	} else {
		report := capability.Parse(string(singBoxOutput), nil)
		info.SingBox.Version = report.Version
		info.SingBox.Tags = report.Tags
		if len(report.Errors) > 0 {
			info.SingBox.Error = strings.Join(report.Errors, "; ")
		}
	}

	manifest, err := geodata.ReadManifest(app.seedDirectory())
	if err != nil {
		info.GeoData.Error = err.Error()
	} else {
		info.GeoData.Version = manifest.Upstream.Version
		info.GeoData.RuleCount = len(manifest.Rules)
	}
	return info
}
