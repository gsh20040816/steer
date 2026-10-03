// SPDX-License-Identifier: GPL-3.0-or-later

package linux

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gsh20040816/steer/go/internal/generation"
)

func ActivateGeneration(ctx context.Context, runner Runner, candidate generation.Candidate, runDirectory, nftBinary string) error {
	if err := CleanupPlatform(ctx, runner, nftBinary); err != nil {
		return err
	}
	if _, err := runner.Output(ctx, nftBinary, "-f", filepath.Join(candidate.Directory, "firewall.nft")); err != nil {
		return fmt.Errorf("load Steer nftables shim: %w", err)
	}
	if err := os.MkdirAll(runDirectory, 0o750); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	link := filepath.Join(runDirectory, "current")
	temporary := filepath.Join(runDirectory, ".current."+strconv.Itoa(os.Getpid()))
	_ = os.Remove(temporary)
	if err := os.Symlink(candidate.Directory, temporary); err != nil {
		return fmt.Errorf("create current generation link: %w", err)
	}
	if err := os.Rename(temporary, link); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("publish current generation: %w", err)
	}
	return nil
}

func EnsureCurrentFirewall(ctx context.Context, runner Runner, runDirectory, nftBinary string) error {
	if err := CleanupPlatform(ctx, runner, nftBinary); err != nil {
		return err
	}
	if _, err := runner.Output(ctx, nftBinary, "-f", filepath.Join(runDirectory, "current", "firewall.nft")); err != nil {
		return fmt.Errorf("load current Steer nftables shim: %w", err)
	}
	return nil
}

func CleanupPlatform(ctx context.Context, runner Runner, nftBinary string) error {
	output, err := runner.Output(ctx, nftBinary, "-j", "list", "tables")
	if err != nil {
		return fmt.Errorf("list nftables tables: %w", err)
	}
	var tables struct {
		NFTables []map[string]map[string]any `json:"nftables"`
	}
	if err := json.Unmarshal(output, &tables); err != nil {
		return fmt.Errorf("decode nftables table list: %w", err)
	}
	for _, item := range tables.NFTables {
		table := item["table"]
		if table == nil || table["family"] != "inet" || table["name"] != "steer" {
			continue
		}
		if _, err := runner.Output(ctx, nftBinary, "delete", "table", "inet", "steer"); err != nil {
			return fmt.Errorf("delete Steer nftables table: %w", err)
		}
		break
	}
	return nil
}

func readCurrentPlan(runDirectory string) (Plan, error) {
	var plan Plan
	file, err := os.Open(filepath.Join(runDirectory, "current", "platform.json"))
	if err != nil {
		return plan, fmt.Errorf("open current Linux plan: %w", err)
	}
	defer file.Close()
	if err := json.NewDecoder(file).Decode(&plan); err != nil {
		return plan, fmt.Errorf("decode current Linux plan: %w", err)
	}
	return plan, nil
}
