// SPDX-License-Identifier: GPL-3.0-or-later

package linux

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gsh20040816/steer/go/internal/generation"
)

// runtimeFiles are the Linux generation files besides sing-box.json that
// change how the running service behaves.
var runtimeFiles = []generation.RuntimeFile{{Name: "platform.json"}, {Name: "firewall.nft"}}

func (backend *Backend) Activate(ctx context.Context, candidate generation.Candidate) (bool, error) {
	current := filepath.Join(backend.options.RunDirectory, "current")
	if generation.Unchanged(ctx, current, candidate, runtimeFiles, func(ctx context.Context) error {
		return checkHealthOnce(ctx, backend.runner, backend.plan, backend.options, "")
	}) {
		return false, generation.PublishLink(backend.options.RunDirectory, candidate)
	}
	if _, err := backend.runner.Output(ctx, backend.options.SystemctlBinary, "stop", backend.options.ServiceName); err != nil {
		return true, fmt.Errorf("stop current Linux generation: %w", err)
	}
	if err := ActivateGeneration(ctx, backend.runner, candidate, backend.options.RunDirectory, backend.options.NFTBinary); err != nil {
		return true, err
	}
	if _, err := backend.runner.Output(ctx, backend.options.SystemctlBinary, "start", backend.options.ServiceName); err != nil {
		return true, fmt.Errorf("start candidate Linux generation: %w", err)
	}
	return true, nil
}

func (backend *Backend) ActivateForServiceStart(ctx context.Context, candidate generation.Candidate) error {
	return ActivateGeneration(ctx, backend.runner, candidate, backend.options.RunDirectory, backend.options.NFTBinary)
}

func (backend *Backend) Healthy(ctx context.Context, candidate generation.Candidate) error {
	if err := waitHealthy(ctx, backend.runner, backend.plan, backend.options, candidate.Directory); err != nil {
		return err
	}
	return nil
}

func (backend *Backend) Finalize(_ context.Context, candidate generation.Candidate) error {
	if err := pruneGenerations(backend.options.RunDirectory, candidate.Directory); err != nil {
		return fmt.Errorf("prune obsolete runtime generations: %w", err)
	}
	return nil
}

func (backend *Backend) Disable(ctx context.Context) error {
	if _, err := backend.runner.Output(ctx, backend.options.SystemctlBinary, "stop", backend.options.ServiceName); err != nil {
		return fmt.Errorf("stop Steer while disabling: %w", err)
	}
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

func pruneGenerations(runDirectory, keep string) error {
	entries, err := os.ReadDir(filepath.Join(runDirectory, "generations"))
	if err != nil {
		return err
	}
	keepInfo, err := os.Stat(keep)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(runDirectory, "generations", entry.Name())
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if os.SameFile(info, keepInfo) {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}
