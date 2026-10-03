// SPDX-License-Identifier: GPL-3.0-or-later
package openwrt

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gsh20040816/steer/go/internal/generation"
)

// These identifiers belong only to the pre-1.14 MAC firewall implementation.
// Cleanup keeps upgrades from leaving its policy rules behind; new generations
// never create this table or any rules that reference it.
const (
	legacyMACTable               = 2023
	legacyMACPriority            = 8999
	legacyMACMark                = 0x2026
	legacyAutoRedirectOutputMark = 0x2024
)

func ActivateGeneration(ctx context.Context, runner Runner, candidate generation.Candidate, runDirectory, nftBinary string) error {
	if err := CleanupPlatform(ctx, runner, nftBinary); err != nil {
		return err
	}
	if _, err := runner.Output(ctx, nftBinary, "-f", filepath.Join(candidate.Directory, "firewall.nft")); err != nil {
		return fmt.Errorf("load Steer nftables shim: %w", err)
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

func CleanupPlatform(ctx context.Context, runner Runner, nftBinary string) error {
	tablesOutput, err := runner.Output(ctx, nftBinary, "-j", "list", "tables")
	if err != nil {
		return fmt.Errorf("list nftables tables: %w", err)
	}
	var tables struct {
		NFTables []map[string]map[string]any `json:"nftables"`
	}
	if err := json.Unmarshal(tablesOutput, &tables); err != nil {
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
	for _, family := range []string{"-4", "-6"} {
		output, err := runner.Output(ctx, "ip", "-json", family, "rule", "show")
		if err != nil {
			return fmt.Errorf("list %s policy rules: %w", family, err)
		}
		var rules []map[string]any
		if err := json.Unmarshal(output, &rules); err != nil {
			return fmt.Errorf("decode %s policy rules: %w", family, err)
		}
		for _, rule := range rules {
			mark := jsonInt(rule["fwmark"])
			if jsonInt(rule["priority"]) != legacyMACPriority || jsonInt(rule["table"]) != legacyMACTable || (mark != legacyMACMark && mark != legacyAutoRedirectOutputMark) {
				continue
			}
			args := []string{"rule", "del", "priority", fmt.Sprint(legacyMACPriority)}
			if iif := fmt.Sprint(rule["iif"]); iif != "" && iif != "<nil>" {
				args = append(args, "iif", iif)
			}
			args = append(args, "fwmark", fmt.Sprintf("0x%x", mark), "lookup", fmt.Sprint(legacyMACTable))
			if _, err := runner.Output(ctx, "ip", append([]string{family}, args...)...); err != nil {
				return fmt.Errorf("delete Steer MAC policy rule: %w", err)
			}
		}
		routesOutput, err := runner.Output(ctx, "ip", "-json", family, "route", "show", "table", "all")
		if err != nil {
			return fmt.Errorf("list %s route tables: %w", family, err)
		}
		var routes []map[string]any
		if err := json.Unmarshal(routesOutput, &routes); err != nil {
			return fmt.Errorf("decode %s route tables: %w", family, err)
		}
		for _, route := range routes {
			if jsonInt(route["table"]) != legacyMACTable {
				continue
			}
			if _, err := runner.Output(ctx, "ip", family, "route", "flush", "table", fmt.Sprint(legacyMACTable)); err != nil {
				return fmt.Errorf("flush Steer MAC route table: %w", err)
			}
			break
		}
	}
	return nil
}

func jsonInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case string:
		value := typed
		if slash := strings.IndexByte(value, '/'); slash >= 0 {
			value = value[:slash]
		}
		parsed, _ := strconv.ParseInt(value, 0, 64)
		return int(parsed)
	default:
		return 0
	}
}
