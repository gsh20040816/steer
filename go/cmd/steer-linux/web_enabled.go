// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// The switch updates only main.enabled in the latest saved configuration, under
// the same operation lock as subscription updates and normal configuration saves.
func (app webApplication) handleEnabled(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", "POST")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.Enabled == nil {
		writeWebError(writer, errors.New("enabled must be a boolean"), http.StatusBadRequest)
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		writeWebError(writer, errors.New("unexpected trailing data"), http.StatusBadRequest)
		return
	}
	response := map[string]any{"saved": false, "applied": false}
	err := withOperationLock(app.RunDirectory, func() error {
		store := app.store()
		value, revision, err := store.Load()
		if err != nil {
			return err
		}
		value.Main.Enabled = *input.Enabled
		saved, err := store.Complete(value)
		if err != nil {
			return err
		}
		revision, err = store.Save(value, revision)
		if err != nil {
			return err
		}
		response["saved"], response["revision"], response["intent"] = true, revision, value
		result, applyErr := app.applyValue(saved)
		result, applyErr = recordApplyResult(app.RunDirectory, result, applyErr)
		response["apply_result"] = result
		response["applied"] = applyErr == nil
		return applyErr
	})
	if err != nil {
		response["error"] = errorDetails(err)
		writeWebJSONStatus(writer, response, http.StatusUnprocessableEntity)
		return
	}
	writeWebJSON(writer, response)
}
