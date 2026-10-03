// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"embed"
	"fmt"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"

	linuxplatform "github.com/gsh20040816/steer/go/internal/platform/linux"
)

//go:embed web/*
var webAssets embed.FS

type webApplication struct {
	WebConfigPath  string
	ConfigPath     string
	RunDirectory   string
	StateDirectory string
	SeedDirectory  string
	ListenAddress  string
	Runner         linuxplatform.Runner
}

func (app webApplication) runner() linuxplatform.Runner {
	if app.Runner == nil {
		return linuxplatform.ExecRunner{}
	}
	return app.Runner
}

func serveWeb(listen, webConfigPath, configPath, runDirectory, stateDirectory, seedDirectory string) error {
	listen, err := normalizeWebListen(listen)
	if err != nil {
		return err
	}
	if _, err := configuredWebToken(webConfigPath); err != nil {
		return fmt.Errorf("read Web credentials: %w", err)
	}
	app := webApplication{
		WebConfigPath: webConfigPath, ConfigPath: configPath, RunDirectory: runDirectory,
		StateDirectory: stateDirectory, SeedDirectory: seedDirectory, ListenAddress: listen,
	}
	return (&http.Server{Addr: listen, Handler: webHandler(app)}).ListenAndServe()
}

func normalizeWebListen(value string) (string, error) {
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return "", fmt.Errorf("web listen address must be a loopback IP and port: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("web listen address must use a loopback IP, got %q", host)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("web listen port must be between 1 and 65535, got %q", portText)
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(port)), nil
}

func webHandler(app webApplication) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", app.handleIndex)
	mux.HandleFunc("/app.js", app.handleAsset)
	mux.HandleFunc("/style.css", app.handleAsset)
	mux.HandleFunc("/js/", app.handleAsset)
	mux.HandleFunc("/api/v1/enabled", app.auth(app.handleEnabled))
	mux.HandleFunc("/api/v1/config", app.auth(app.handleConfig))
	mux.HandleFunc("/api/v1/runtime", app.auth(app.handleRuntime))
	mux.HandleFunc("/api/v1/logs", app.auth(app.handleLogs))
	mux.HandleFunc("/api/v1/diagnostics", app.auth(app.handleDiagnostics))
	mux.HandleFunc("/api/v1/probe-results", app.auth(app.handleProbeResults))
	mux.HandleFunc("/api/v1/overview", app.auth(app.handleOverview))
	mux.HandleFunc("/api/v1/validate", app.auth(app.handleValidate))
	mux.HandleFunc("/api/v1/apply", app.auth(app.handleApply))
	mux.HandleFunc("/api/v1/nodes/import", app.auth(app.handleNodeImport))
	mux.HandleFunc("/api/v1/nodes/export", app.auth(app.handleNodeExport))
	mux.HandleFunc("/api/v1/probes/", app.auth(app.handleProbes))
	mux.HandleFunc("/api/v1/subscriptions", app.auth(app.handleSubscriptions))
	mux.HandleFunc("/api/v1/subscriptions/", app.auth(app.handleSubscriptions))
	mux.HandleFunc("/api/v1/geodata/", app.auth(app.handleGeoData))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(writer, request)
	})
}

func (app webApplication) seedDirectory() string {
	if app.SeedDirectory == "" {
		return "/usr/share/steer/geodata-seed"
	}
	return app.SeedDirectory
}

func (app webApplication) handleIndex(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	content, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		http.Error(writer, "web asset is unavailable", http.StatusInternalServerError)
		return
	}
	_, _ = writer.Write(content)
}

func (app webApplication) handleAsset(writer http.ResponseWriter, request *http.Request) {
	cleanPath := path.Clean(request.URL.Path)
	asset := "web" + cleanPath
	allowed := cleanPath == "/app.js" || cleanPath == "/style.css" || (strings.HasPrefix(cleanPath, "/js/") && strings.HasSuffix(cleanPath, ".js"))
	if !allowed {
		http.NotFound(writer, request)
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	content, err := webAssets.ReadFile(asset)
	if err != nil {
		http.Error(writer, "web asset is unavailable", http.StatusInternalServerError)
		return
	}
	if strings.HasSuffix(asset, ".js") {
		writer.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	} else {
		writer.Header().Set("Content-Type", "text/css; charset=utf-8")
	}
	if request.Method == http.MethodGet {
		_, _ = writer.Write(content)
	}
}
