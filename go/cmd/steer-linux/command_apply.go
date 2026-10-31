// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	coreapply "github.com/gsh20040816/steer/go/internal/apply"
	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/intent"
	linuxplatform "github.com/gsh20040816/steer/go/internal/platform/linux"
)

func runValidate(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/steer/config.json", "canonical JSON configuration file")
	stateDirectory := flags.String("state-dir", "/var/lib/steer", "state directory holding the subscription node store")
	seedDirectory := flags.String("seed-dir", "/usr/share/steer/geodata-seed", "package-owned Geo seed directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("validate accepts flags only")
	}
	value, validation, err := loadIntent(*configPath, *stateDirectory)
	if err != nil {
		writeJSON(validation)
		return err
	}
	validation = linuxplatform.ValidateWithGeoDataDirectory(value, *seedDirectory)
	writeJSON(validation)
	if !validation.OK {
		return errors.New("configuration validation failed")
	}
	return nil
}

func runApply(args []string) error {
	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/steer/config.json", "canonical JSON configuration file")
	singBoxPath := flags.String("sing-box", "/usr/bin/sing-box", "sing-box binary")
	stateDirectory := flags.String("state-dir", "/var/lib/steer", "generated state directory")
	runDirectory := flags.String("run-dir", "/run/steer", "runtime state directory")
	nftBinary := flags.String("nft", "/usr/sbin/nft", "nft binary")
	systemctl := flags.String("systemctl", "/usr/bin/systemctl", "systemctl binary")
	serviceName := flags.String("service", "steer.service", "systemd service name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("apply accepts flags only")
	}
	return runLockedApply(*runDirectory, func() (coreapply.Result, error) {
		value, validation, err := loadIntent(*configPath, *stateDirectory)
		if err != nil {
			return coreapply.Result{Validation: &validation}, err
		}
		if !validation.OK {
			return coreapply.Result{Validation: &validation}, linuxplatform.ValidationError{Validation: validation}
		}
		backend := linuxplatform.NewBackend(linuxplatform.ExecRunner{}, value, linuxplatform.BackendOptions{
			RunDirectory: *runDirectory, StateDirectory: *stateDirectory, SingBoxBinary: *singBoxPath,
			NFTBinary: *nftBinary, SystemctlBinary: *systemctl, ServiceName: *serviceName,
		})
		return applyAndPrune(value, backend, *configPath, *stateDirectory)
	})
}

func runService(args []string) error {
	flags := flag.NewFlagSet("_run", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/steer/config.json", "canonical JSON configuration file")
	singBoxPath := flags.String("sing-box", "/usr/bin/sing-box", "sing-box binary")
	stateDirectory := flags.String("state-dir", "/var/lib/steer", "generated state directory")
	runDirectory := flags.String("run-dir", "/run/steer", "runtime state directory")
	nftBinary := flags.String("nft", "/usr/sbin/nft", "nft binary")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("_run accepts flags only")
	}
	options := linuxplatform.BackendOptions{RunDirectory: *runDirectory, StateDirectory: *stateDirectory, SingBoxBinary: *singBoxPath, NFTBinary: *nftBinary}
	if err := migrateLocked(*configPath, *runDirectory, *stateDirectory); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(*runDirectory, "current", "sing-box.json")); err != nil {
		lock, lockErr := acquireApplyLock(*runDirectory)
		if lockErr != nil {
			return lockErr
		}
		value, validation, loadErr := loadIntent(*configPath, *stateDirectory)
		if loadErr == nil {
			validation = linuxplatform.Validate(value)
			if !validation.OK {
				loadErr = linuxplatform.ValidationError{Validation: validation}
			}
		}
		disabled := loadErr == nil && !value.Main.Enabled
		if disabled {
			loadErr = linuxplatform.CleanupPlatform(context.Background(), linuxplatform.ExecRunner{}, *nftBinary)
		}
		if loadErr == nil && !disabled {
			backend := linuxplatform.NewBackend(linuxplatform.ExecRunner{}, value, options)
			compiled := compiler.Compile(value, backend.CompilerOptions())
			candidate, prepareErr := backend.Prepare(context.Background(), value, compiled)
			if prepareErr == nil {
				prepareErr = backend.ActivateForServiceStart(context.Background(), candidate)
			}
			if prepareErr == nil {
				prepareErr = backend.Finalize(context.Background(), candidate)
			}
			loadErr = prepareErr
		}
		lock.Close()
		if loadErr != nil {
			return loadErr
		}
		if disabled {
			return nil
		}
	}
	if err := linuxplatform.EnsureCurrentFirewall(context.Background(), linuxplatform.ExecRunner{}, *runDirectory, *nftBinary); err != nil {
		return err
	}
	return syscall.Exec(*singBoxPath, []string{filepath.Base(*singBoxPath), "run", "-c", filepath.Join(*runDirectory, "current", "sing-box.json")}, os.Environ())
}

func runHealth(args []string) error {
	flags := flag.NewFlagSet("health", flag.ContinueOnError)
	runDirectory := flags.String("run-dir", "/run/steer", "runtime state directory")
	nftBinary := flags.String("nft", "/usr/sbin/nft", "nft binary")
	systemctl := flags.String("systemctl", "/usr/bin/systemctl", "systemctl binary")
	serviceName := flags.String("service", "steer.service", "systemd service name")
	timeout := flags.Duration("timeout", 10*time.Second, "local readiness deadline")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("health accepts flags only")
	}
	return linuxplatform.WaitCurrentHealthy(context.Background(), linuxplatform.ExecRunner{}, linuxplatform.BackendOptions{RunDirectory: *runDirectory, NFTBinary: *nftBinary, SystemctlBinary: *systemctl, ServiceName: *serviceName}, *timeout)
}

func runStatus(args []string) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	runDirectory := flags.String("run-dir", "/run/steer", "runtime state directory")
	nftBinary := flags.String("nft", "/usr/sbin/nft", "nft binary")
	systemctl := flags.String("systemctl", "/usr/bin/systemctl", "systemctl binary")
	serviceName := flags.String("service", "steer.service", "systemd service name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("status accepts flags only")
	}
	writeJSON(linuxplatform.ReadStatus(context.Background(), linuxplatform.ExecRunner{}, linuxplatform.BackendOptions{RunDirectory: *runDirectory, NFTBinary: *nftBinary, SystemctlBinary: *systemctl, ServiceName: *serviceName}))
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
	if err := linuxplatform.CleanupPlatform(context.Background(), linuxplatform.ExecRunner{}, *nftBinary); err != nil {
		return err
	}
	return nil
}

// loadIntent returns the complete Saved Intent (config.json plus stored
// subscription nodes) and its validation. Subscription nodes written into
// config.json are reported as validation errors.
func loadIntent(path, stateDirectory string) (intent.Intent, intent.Validation, error) {
	value, _, err := linuxplatform.SavedStore(path, stateDirectory).LoadSaved()
	var validationErr linuxplatform.ValidationError
	if errors.As(err, &validationErr) {
		return intent.Intent{}, validationErr.Validation, err
	}
	if err != nil {
		return intent.Intent{}, intent.Validation{Errors: []intent.Issue{{Code: "DECODE_FAILED", ObjectType: "json", Message: err.Error()}}, Warnings: []intent.Issue{}}, err
	}
	return value, linuxplatform.Validate(value), nil
}

// applyAndPrune runs Apply and, after a success, drops the node store files
// of subscriptions that config.json no longer has.
func applyAndPrune(value intent.Intent, backend *linuxplatform.Backend, configPath, stateDirectory string) (coreapply.Result, error) {
	result, err := coreapply.Run(context.Background(), value, backend.CompilerOptions(), backend)
	if err == nil {
		// Orphans are never attached; a failed removal is retried later.
		_ = linuxplatform.RemoveOrphanNodes(configPath, stateDirectory)
	}
	return result, err
}

// migrateLocked moves schema 9 subscription nodes into the node store under
// the operation lock. The lock is taken only for schema 9: an Apply holds it
// while it starts this very service. Every other schema is left to validation.
func migrateLocked(configPath, runDirectory, stateDirectory string) error {
	if !linuxplatform.NeedsMigration(configPath) {
		return nil
	}
	return withOperationLock(runDirectory, func() error {
		if _, err := linuxplatform.Migrate(configPath, stateDirectory); err != nil {
			return fmt.Errorf("migrate configuration to schema %d: %w", intent.SchemaVersion, err)
		}
		return nil
	})
}

// runMigrate is the internal, idempotent schema 9 to 10 migration used by the
// Arch upgrade hook.
func runMigrate(args []string) error {
	flags := flag.NewFlagSet("_migrate", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/steer/config.json", "canonical JSON configuration file")
	stateDirectory := flags.String("state-dir", "/var/lib/steer", "state directory holding the subscription node store")
	runDirectory := flags.String("run-dir", "/run/steer", "shared operation lock directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("_migrate accepts flags only")
	}
	changed := false
	err := withOperationLock(*runDirectory, func() error {
		var err error
		changed, err = linuxplatform.Migrate(*configPath, *stateDirectory)
		return err
	})
	if err != nil {
		return err
	}
	writeJSON(struct {
		OK      bool `json:"ok"`
		Changed bool `json:"changed"`
	}{true, changed})
	return nil
}
