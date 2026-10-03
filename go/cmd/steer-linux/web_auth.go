// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

func (app webApplication) auth(handler http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if !app.authorized(request) {
			writer.Header().Set("WWW-Authenticate", `Bearer realm="steer"`)
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		if request.Method != http.MethodGet && request.Method != http.MethodHead && !sameOrigin(request) {
			http.Error(writer, "origin is not allowed", http.StatusForbidden)
			return
		}
		handler(writer, request)
	}
}

func (app webApplication) authorized(request *http.Request) bool {
	stored, err := configuredWebToken(app.WebConfigPath)
	if err != nil {
		return false
	}
	header := request.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	value := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	return subtle.ConstantTimeCompare([]byte(value), stored) == 1
}

func sameOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	return origin == "" || strings.HasSuffix(origin, "://"+request.Host)
}
