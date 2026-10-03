// SPDX-License-Identifier: GPL-3.0-or-later

package openwrt

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gsh20040816/steer/go/internal/generation"
)

func (backend *Backend) Activate(ctx context.Context, candidate generation.Candidate) error {
	if _, err := backend.runner.Output(ctx, backend.options.InitScript, "stop"); err != nil {
		return fmt.Errorf("stop current generation: %w", err)
	}
	if err := ActivateGeneration(ctx, backend.runner, candidate, backend.options.RunDirectory, backend.options.NFTBinary); err != nil {
		return err
	}
	if _, err := backend.runner.Output(ctx, "/usr/bin/env", "STEER_USE_CURRENT=1", backend.options.InitScript, "start"); err != nil {
		return fmt.Errorf("start candidate generation: %w", err)
	}
	return nil
}

// ActivateForServiceStart installs a prepared generation while procd is
// already constructing the sing-box instance. It is an init-script hook, not
// part of the public Apply lifecycle.
func (backend *Backend) ActivateForServiceStart(ctx context.Context, candidate generation.Candidate) error {
	return ActivateGeneration(ctx, backend.runner, candidate, backend.options.RunDirectory, backend.options.NFTBinary)
}

func (backend *Backend) Healthy(ctx context.Context, _ generation.Candidate) error {
	return waitHealthy(ctx, backend.runner, backend.plan, backend.options.HealthTimeout, backend.options.CheckListeners, backend.options.NFTBinary)
}

func (backend *Backend) Finalize(_ context.Context, candidate generation.Candidate) error {
	if err := pruneGenerations(backend.options.RunDirectory, candidate.Directory); err != nil {
		return fmt.Errorf("prune obsolete runtime generations: %w", err)
	}
	return nil
}

func (backend *Backend) Disable(ctx context.Context) error {
	if _, err := backend.runner.Output(ctx, backend.options.InitScript, "stop_runtime"); err != nil {
		return fmt.Errorf("stop Steer runtime while disabling: %w", err)
	}
	// stop_runtime keeps the procd object, so service_stopped never runs the
	// cleanup hook; drop the DNS shim here or LAN DNS points at a dead port.
	if err := CleanupPlatform(ctx, backend.runner, backend.options.NFTBinary); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(backend.options.RunDirectory, "current")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove disabled current generation: %w", err)
	}
	if err := os.RemoveAll(filepath.Join(backend.options.RunDirectory, "generations")); err != nil {
		return fmt.Errorf("remove disabled runtime generations: %w", err)
	}
	return nil
}

func waitHealthy(ctx context.Context, runner Runner, plan Plan, timeout time.Duration, listenerCheck func([]int) error, nftBinary string) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if err := checkHealthOnce(ctx, runner, plan, listenerCheck, nftBinary); err == nil {
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

func WaitCurrentHealthy(ctx context.Context, runner Runner, runDirectory, nftBinary string, timeout time.Duration) error {
	plan, err := readCurrentPlan(runDirectory)
	if err != nil {
		return err
	}
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return waitHealthy(ctx, runner, plan, timeout, checkListenerPorts, nftBinary)
}

func readCurrentPlan(runDirectory string) (Plan, error) {
	var plan Plan
	file, err := os.Open(filepath.Join(runDirectory, "current", "platform.json"))
	if err != nil {
		return plan, fmt.Errorf("open current OpenWrt plan: %w", err)
	}
	defer file.Close()
	if err := json.NewDecoder(file).Decode(&plan); err != nil {
		return plan, fmt.Errorf("decode current OpenWrt plan: %w", err)
	}
	return plan, nil
}

func planListenerPorts(plan Plan) []int {
	return []int{plan.Resources.DNSPort}
}

func checkHealthOnce(ctx context.Context, runner Runner, plan Plan, listenerCheck func([]int) error, nftBinary string) error {
	serviceOutput, err := runner.Output(ctx, "ubus", "call", "service", "list", `{"name":"steer"}`)
	if err != nil {
		return err
	}
	var services map[string]struct {
		Instances map[string]struct {
			Running bool `json:"running"`
			PID     int  `json:"pid"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(serviceOutput, &services); err != nil {
		return fmt.Errorf("decode procd status: %w", err)
	}
	service, exists := services["steer"]
	if !exists || !service.Instances["sing-box"].Running || service.Instances["sing-box"].PID <= 0 {
		return fmt.Errorf("procd sing-box instance is not running")
	}
	if _, err := runner.Output(ctx, "ip", "-json", "link", "show", "dev", plan.Resources.TunInterface); err != nil {
		return fmt.Errorf("TUN interface is not ready: %w", err)
	}
	if _, err := runner.Output(ctx, nftBinary, "-j", "list", "table", "inet", "steer"); err != nil {
		return fmt.Errorf("Steer nftables shim is not ready: %w", err)
	}
	if err := listenerCheck(planListenerPorts(plan)); err != nil {
		return err
	}
	return nil
}

func checkListenerPorts(ports []int) error {
	// The OpenWrt runtime is Linux-only. Keep shared Go tests runnable on the
	// macOS development host without pretending that macOS exposes Linux's
	// /proc listener tables.
	if runtime.GOOS != "linux" {
		return nil
	}
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

func pruneGenerations(runDirectory, keep string) error {
	entries, err := os.ReadDir(filepath.Join(runDirectory, "generations"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(runDirectory, "generations", entry.Name())
		if path != keep {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
	}
	return nil
}
