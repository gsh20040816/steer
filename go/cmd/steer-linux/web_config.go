// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	coreapply "github.com/gsh20040816/steer/go/internal/apply"
	"github.com/gsh20040816/steer/go/internal/geodata"
	linuxplatform "github.com/gsh20040816/steer/go/internal/platform/linux"
)

func (app webApplication) handleConfig(writer http.ResponseWriter, request *http.Request) {
	store := linuxplatform.IntentStore{Path: app.ConfigPath, GeoDataDirectory: app.seedDirectory()}
	switch request.Method {
	case http.MethodGet:
		value, revision, err := store.Load()
		if err != nil {
			writeWebError(writer, err, http.StatusInternalServerError)
			return
		}
		writer.Header().Set("ETag", revision)
		writeWebJSON(writer, map[string]any{"intent": value, "revision": revision})
	case http.MethodPut:
		body, err := io.ReadAll(io.LimitReader(request.Body, 2<<20))
		if err != nil {
			writeWebError(writer, err, http.StatusBadRequest)
			return
		}
		value, apply, err := decodeIntentPayload(body)
		if err != nil {
			writeWebError(writer, err, http.StatusBadRequest)
			return
		}
		expectedRevision := request.Header.Get("If-Match")
		var revision string
		var saveErr error
		var applyResult coreapply.Result
		var applyErr error
		lockErr := withOperationLock(app.RunDirectory, func() error {
			if expectedRevision == "" {
				if _, statErr := os.Stat(store.Path); statErr == nil {
					saveErr = errors.New("If-Match is required for an existing configuration")
					return nil
				}
			}
			revision, saveErr = store.Save(value, expectedRevision)
			if saveErr != nil || !apply {
				return nil
			}
			applyResult, applyErr = app.applyValue(value)
			applyResult, applyErr = recordApplyResult(app.RunDirectory, applyResult, applyErr)
			return nil
		})
		if lockErr != nil {
			writeWebError(writer, lockErr, http.StatusInternalServerError)
			return
		}
		if errors.Is(saveErr, linuxplatform.ErrRevisionConflict) {
			writeWebError(writer, saveErr, http.StatusConflict)
			return
		}
		if saveErr != nil {
			var validationErr linuxplatform.ValidationError
			if errors.As(saveErr, &validationErr) {
				writeWebJSONStatus(writer, map[string]any{
					"saved": false, "validation": validationErr.Validation, "error": errorDetails(saveErr),
				}, http.StatusUnprocessableEntity)
				return
			}
			status := http.StatusUnprocessableEntity
			if strings.Contains(saveErr.Error(), "If-Match is required") {
				status = http.StatusPreconditionRequired
			}
			writeWebError(writer, saveErr, status)
			return
		}
		writer.Header().Set("ETag", revision)
		validation := linuxplatform.ValidateWithGeoDataDirectory(value, app.seedDirectory())
		response := map[string]any{"saved": true, "applied": false, "revision": revision, "validation": validation}
		if apply {
			response["apply_result"] = applyResult
			if applyErr == nil {
				response["applied"] = true
			}
		}
		if applyErr != nil {
			writeWebJSONStatus(writer, response, http.StatusUnprocessableEntity)
			return
		}
		writeWebJSON(writer, response)
	default:
		writer.Header().Set("Allow", "GET, PUT")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type webErrorDetails struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Kind     string `json:"kind,omitempty"`
	Category string `json:"category,omitempty"`
	Path     string `json:"path,omitempty"`
}

func errorDetails(err error) *webErrorDetails {
	details := &webErrorDetails{Code: "OPERATION_FAILED", Message: err.Error()}
	var geoErr *geodata.Error
	if errors.As(err, &geoErr) {
		details.Code = geoErr.Code
		details.Kind = geoErr.Kind
		details.Category = geoErr.Category
		details.Path = geoErr.Path
	}
	return details
}
