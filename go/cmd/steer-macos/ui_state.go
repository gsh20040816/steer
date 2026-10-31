// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"io"

	"github.com/gsh20040816/steer/go/internal/compiler"
	model "github.com/gsh20040816/steer/go/internal/intent"
	macosplatform "github.com/gsh20040816/steer/go/internal/platform/macos"
	"github.com/gsh20040816/steer/go/internal/uistate"
)

type macUILifecycleState struct {
	Saved        uistate.IntentState  `json:"saved"`
	Active       macosplatform.Status `json:"active"`
	PendingApply bool                 `json:"pending_apply"`
}

func runUIState(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("_state", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultConfigPath, "canonical JSON configuration")
	options := bindBackendFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("_state accepts flags only")
	}

	return writeJSON(stdout, readUIState(*configPath, options.value()))
}

func readUIState(configPath string, options macosplatform.BackendOptions) macUILifecycleState {
	value, loadErr := loadSaved(configPath, options.StateDirectory)
	validation := model.Validation{OK: false, Errors: []model.Issue{}, Warnings: []model.Issue{}, WarningGroups: []model.WarningGroup{}}
	saved := uistate.IntentState{Counts: uistate.Counts{}, Validation: validation}
	backend := macosplatform.NewBackend(macosplatform.ExecRunner{}, value, options)
	var validationErr macosplatform.ValidationError
	if errors.As(loadErr, &validationErr) {
		saved.Validation = validationErr.Validation
	} else if loadErr == nil {
		validation = macosplatform.ValidateWithGeoDataDirectory(value, options.GeoDataDirectory)
		saved = uistate.FromIntent(value, validation, compiler.Options{
			StateDirectory: options.StateDirectory, GeoDataDirectory: options.GeoDataDirectory,
			Target: macosplatform.NewPlan(value).CompilerTarget(),
		})
	} else {
		saved.Validation.Errors = append(saved.Validation.Errors, model.Issue{
			Code: "CONFIG_READ_FAILED", ObjectType: "steer", Message: "The saved configuration could not be read.",
		})
	}
	active := backend.ReadStatus(context.Background())
	pendingApply := uistate.PendingApply(saved, active.GenerationID, active.RuntimeDigest, active.LastApply)
	return macUILifecycleState{Saved: saved, Active: active, PendingApply: pendingApply}
}
