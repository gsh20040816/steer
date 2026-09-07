// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	coreapply "github.com/gsh20040816/steer/go/internal/apply"
	"github.com/gsh20040816/steer/go/internal/compiler"
)

func (backend *Backend) launchDaemonLoaded(ctx context.Context) (bool, error) {
	_, err := backend.runner.Output(ctx, backend.options.LaunchctlBinary, "print", "system/"+backend.options.LaunchDaemonLabel)
	if err != nil {
		// launchctl uses a non-zero exit status for an unloaded label. The next
		// bootstrap operation remains the authoritative error if launchctl itself
		// is unavailable or permissions are insufficient.
		return false, nil
	}
	// A successful print means launchd still owns the label even when its
	// process has exited and the state is "not running". Such a label must be
	// booted out before bootstrap; otherwise launchctl reports EIO (exit 5).
	return true, nil
}

func (backend *Backend) stopLaunchDaemon(ctx context.Context) error {
	loaded, err := backend.launchDaemonLoaded(ctx)
	if err != nil {
		return err
	}
	if !loaded {
		return backend.DNSManager().Restore(ctx)
	}
	output, err := backend.runner.Output(ctx, backend.options.LaunchctlBinary, "print", "system/"+backend.options.LaunchDaemonLabel)
	if err == nil && launchdOutputIsRunning(string(output)) {
		// Signal the supervisor first so it can restore DNS while the core is
		// still alive. bootout may terminate the whole process group at once.
		if _, err := backend.runner.Output(ctx, backend.options.LaunchctlBinary, "kill", "SIGTERM", "system/"+backend.options.LaunchDaemonLabel); err != nil {
			return fmt.Errorf("request graceful macOS shutdown: %w", err)
		}
		deadline := time.Now().Add(25 * time.Second)
		for {
			output, err := backend.runner.Output(ctx, backend.options.LaunchctlBinary, "print", "system/"+backend.options.LaunchDaemonLabel)
			if err != nil || !launchdOutputIsRunning(string(output)) {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("macOS supervisor did not finish DNS restoration before shutdown")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if _, err := backend.runner.Output(ctx, backend.options.LaunchctlBinary, "bootout", "system/"+backend.options.LaunchDaemonLabel); err != nil {
		return fmt.Errorf("stop macOS LaunchDaemon: %w", err)
	}
	return backend.DNSManager().Restore(ctx)
}

func (backend *Backend) startLaunchDaemon(ctx context.Context) error {
	if _, err := backend.runner.Output(ctx, backend.options.LaunchctlBinary, "bootstrap", "system", backend.options.LaunchDaemonPlist); err != nil {
		return fmt.Errorf("start macOS LaunchDaemon: %w", err)
	}
	return nil
}

func (backend *Backend) waitHealthy(ctx context.Context, expectedDirectory string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = backend.options.HealthTimeout
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if err := backend.checkHealthyOnce(ctx, expectedDirectory); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("macOS candidate did not become locally healthy: %w", lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (backend *Backend) checkHealthyOnce(ctx context.Context, expectedDirectory string) error {
	output, err := backend.runner.Output(ctx, backend.options.LaunchctlBinary, "print", "system/"+backend.options.LaunchDaemonLabel)
	if err != nil {
		return fmt.Errorf("inspect macOS LaunchDaemon: %w", err)
	}
	if !launchdOutputIsRunning(string(output)) {
		return fmt.Errorf("macOS LaunchDaemon is not running")
	}
	current, err := backend.paths().LoadCurrent()
	if err != nil {
		return err
	}
	if filepath.Base(expectedDirectory) != current.Directory {
		return fmt.Errorf("active macOS generation does not match the candidate")
	}
	if backend.options.CheckTUN != nil {
		if err := backend.options.CheckTUN(backend.plan.Resources.TunAddresses); err != nil {
			return err
		}
	} else {
		ifconfig, err := backend.runner.Output(ctx, backend.options.IfconfigBinary, "-a")
		if err != nil {
			return fmt.Errorf("inspect macOS utun interface: %w", err)
		}
		for _, address := range backend.plan.Resources.TunAddresses {
			ip := strings.SplitN(address, "/", 2)[0]
			if !strings.Contains(string(ifconfig), ip) {
				return fmt.Errorf("macOS utun address %s is not ready", ip)
			}
		}
	}
	return backend.checkDNSReady(ctx, expectedDirectory)
}

func (backend *Backend) ReadStatus(ctx context.Context) Status {
	status := DefaultStatus()
	if file, err := os.Open(filepath.Join(backend.options.RunDirectory, "last-apply.json")); err == nil {
		var record coreapply.Record
		if json.NewDecoder(file).Decode(&record) == nil && record.Sequence != "" {
			status.LastApply = &record
		}
		file.Close()
	}
	current, value, err := backend.paths().LoadCurrentIntent()
	if err == nil {
		status.GenerationID = current.GenerationID
		status.IntentDigest = current.IntentDigest
		if file, openErr := os.Open(filepath.Join(backend.options.RunDirectory, "generations", current.Directory, "sing-box.json")); openErr == nil {
			var singBox map[string]any
			if json.NewDecoder(file).Decode(&singBox) == nil {
				status.RuntimeDigest = compiler.RuntimeDigest(value, singBox)
			}
			file.Close()
		}
		activeBackend := NewBackend(backend.runner, value, backend.options)
		if healthErr := activeBackend.checkHealthyOnce(ctx, filepath.Join(backend.options.RunDirectory, "generations", current.Directory)); healthErr == nil {
			status.Healthy = true
		} else {
			status.Error = healthErr.Error()
		}
	} else {
		// Only a missing current pointer means stopped. Missing/corrupt generation
		// files and permission failures are status errors.
		if _, pointerErr := os.Stat(filepath.Join(backend.options.RunDirectory, "current.json")); !os.IsNotExist(pointerErr) {
			status.Error = err.Error()
		}
	}
	return status
}

func (backend *Backend) WaitCurrentHealthy(ctx context.Context, timeout time.Duration) error {
	current, err := backend.paths().LoadCurrent()
	if err != nil {
		return err
	}
	return backend.waitHealthy(ctx, filepath.Join(backend.options.RunDirectory, "generations", current.Directory), timeout)
}

func (backend *Backend) CurrentConfigPath() (string, error) {
	current, err := backend.paths().LoadCurrent()
	if err != nil {
		return "", err
	}
	path := filepath.Join(backend.options.RunDirectory, "generations", current.Directory, "sing-box.json")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("current macOS sing-box config is unavailable: %w", err)
	}
	return path, nil
}

func (backend *Backend) Cleanup(ctx context.Context) error {
	if err := backend.stopLaunchDaemon(ctx); err != nil {
		return err
	}
	return nil
}
