// SPDX-License-Identifier: GPL-3.0-or-later

package linux

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	coreapply "github.com/gsh20040816/steer/go/internal/apply"
	"github.com/gsh20040816/steer/go/internal/compiler"
	"github.com/gsh20040816/steer/go/internal/generation"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

type Status struct {
	Healthy       bool              `json:"healthy"`
	Generation    string            `json:"generation,omitempty"`
	IntentDigest  string            `json:"intent_digest,omitempty"`
	RuntimeDigest string            `json:"runtime_digest,omitempty"`
	LastApply     *coreapply.Record `json:"last_apply,omitempty"`
}

func WaitCurrentHealthy(ctx context.Context, runner Runner, options BackendOptions, timeout time.Duration) error {
	options = normalizeBackendOptions(options)
	plan, err := readCurrentPlan(options.RunDirectory)
	if err != nil {
		return err
	}
	if timeout > 0 {
		options.HealthTimeout = timeout
	}
	return waitHealthy(ctx, runner, plan, options)
}

func waitHealthy(ctx context.Context, runner Runner, plan Plan, options BackendOptions) error {
	deadline := time.Now().Add(options.HealthTimeout)
	var lastErr error
	for {
		if err := checkHealthOnce(ctx, runner, plan, options); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("candidate did not become locally healthy: %w", lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func checkHealthOnce(ctx context.Context, runner Runner, plan Plan, options BackendOptions) error {
	output, err := runner.Output(ctx, options.SystemctlBinary, "show", options.ServiceName, "--property=ActiveState", "--property=MainPID")
	if err != nil {
		return err
	}
	properties := map[string]string{}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			properties[key] = value
		}
	}
	pid, _ := strconv.Atoi(properties["MainPID"])
	if properties["ActiveState"] != "active" || pid <= 0 {
		return fmt.Errorf("systemd sing-box service is not active")
	}
	if _, err := runner.Output(ctx, "ip", "-json", "link", "show", "dev", plan.Resources.TunInterface); err != nil {
		return fmt.Errorf("TUN interface is not ready: %w", err)
	}
	if _, err := runner.Output(ctx, options.NFTBinary, "-j", "list", "table", "inet", "steer"); err != nil {
		return fmt.Errorf("Steer nftables DNS shim is not ready: %w", err)
	}
	return options.CheckListeners([]int{plan.Resources.DNSPort, plan.Resources.DNSPort6})
}

func checkListenerPorts(ports []int) error {
	found := map[int]bool{}
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6", "/proc/net/udp", "/proc/net/udp6"} {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read listeners from %s: %w", path, err)
		}
		for _, line := range strings.Split(string(content), "\n")[1:] {
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			parts := strings.Split(fields[1], ":")
			if len(parts) != 2 {
				continue
			}
			decoded, decodeErr := hex.DecodeString(parts[1])
			if decodeErr != nil || len(decoded) != 2 {
				continue
			}
			port := int(decoded[0])<<8 | int(decoded[1])
			if strings.Contains(path, "tcp") && fields[3] != "0A" {
				continue
			}
			found[port] = true
		}
	}
	for _, port := range ports {
		if !found[port] {
			return fmt.Errorf("expected listener port %d is not ready", port)
		}
	}
	return nil
}

func ReadStatus(ctx context.Context, runner Runner, options BackendOptions) Status {
	options = normalizeBackendOptions(options)
	status := Status{}
	if file, err := os.Open(filepath.Join(options.RunDirectory, "last-apply.json")); err == nil {
		var record coreapply.Record
		if json.NewDecoder(file).Decode(&record) == nil {
			status.LastApply = &record
		}
		file.Close()
	}
	if generationID, _, current, compiled, err := readCurrentIdentity(options); err == nil && current.Main.Enabled {
		status.Generation = generationID
		status.IntentDigest = compiled.IntentDigest
		status.RuntimeDigest = compiled.RuntimeDigest
	}
	plan, err := readCurrentPlan(options.RunDirectory)
	if status.Generation != "" && err == nil && checkHealthOnce(ctx, runner, plan, options) == nil {
		status.Healthy = true
	}
	return status
}

func readCurrentIdentity(options BackendOptions) (string, string, model.Intent, compiler.Output, error) {
	currentPath := filepath.Join(options.RunDirectory, "current")
	info, err := os.Lstat(currentPath)
	if err != nil {
		return "", "", model.Intent{}, compiler.Output{}, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", "", model.Intent{}, compiler.Output{}, fmt.Errorf("current generation is not a symbolic link")
	}
	resolved, err := filepath.EvalSymlinks(currentPath)
	if err != nil {
		return "", "", model.Intent{}, compiler.Output{}, err
	}
	generationRoot, err := filepath.EvalSymlinks(filepath.Join(options.RunDirectory, "generations"))
	if err != nil {
		return "", "", model.Intent{}, compiler.Output{}, err
	}
	relative, err := filepath.Rel(generationRoot, resolved)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", model.Intent{}, compiler.Output{}, fmt.Errorf("current generation is outside the generation root")
	}
	current, err := generation.ReadIntent(resolved)
	if err != nil {
		return "", "", model.Intent{}, compiler.Output{}, err
	}
	singBoxFile, err := os.Open(filepath.Join(resolved, "sing-box.json"))
	if err != nil {
		return "", "", model.Intent{}, compiler.Output{}, err
	}
	defer singBoxFile.Close()
	var singBox map[string]any
	if err := json.NewDecoder(singBoxFile).Decode(&singBox); err != nil {
		return "", "", model.Intent{}, compiler.Output{}, err
	}
	identity := compiler.Output{IntentDigest: compiler.IntentDigest(current), RuntimeDigest: compiler.RuntimeDigest(current, singBox)}
	return filepath.Base(resolved), resolved, current, identity, nil
}

// HasPendingApply compares the saved runtime projection with the actual
// current generation. Canonical inventory that is not referenced by the
// compiled runtime therefore does not manufacture a pending Apply.
func HasPendingApply(value model.Intent, status Status, options BackendOptions) bool {
	options = normalizeBackendOptions(options)
	plan := NewPlan(value)
	compiled := compiler.Compile(value, compiler.Options{
		StateDirectory: options.StateDirectory, GeoDataDirectory: options.GeoDataDirectory, Target: plan.CompilerTarget(),
	})
	if value.Main.Enabled {
		if status.Generation == "" || status.RuntimeDigest != compiled.RuntimeDigest {
			return true
		}
	} else if status.Generation != "" {
		return true
	}
	lastResult := status.LastApply
	return lastResult != nil && !lastResult.Result.OK && lastResult.Result.RuntimeDigest != "" && lastResult.Result.RuntimeDigest == compiled.RuntimeDigest
}
