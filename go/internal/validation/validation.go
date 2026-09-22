// SPDX-License-Identifier: GPL-3.0-or-later

// Package validation combines the platform-independent Canonical Intent
// contract with the lightweight runtime facts owned by platform adapters.
package validation

import (
	"net"
	"strconv"
	"strings"

	"github.com/gsh20040816/steer/go/internal/geodata"
	model "github.com/gsh20040816/steer/go/internal/intent"
)

type Options struct {
	WireGuard             bool
	ReservedListeners     []model.Listener
	IPv6WildcardDualStack bool
	GeoDataDirectory      string
}

// Validate reads only the installed Geo manifest. Prepare remains responsible
// for checking the size and SHA-256 of every required SRS seed file.
func Validate(value model.Intent, options Options) model.Validation {
	result := model.ValidateWithOptions(value, model.ValidationOptions{
		ReservedListeners:     options.ReservedListeners,
		IPv6WildcardDualStack: options.IPv6WildcardDualStack,
	})
	if !options.WireGuard {
		for _, t := range value.WireGuardTunnels {
			if t.Enabled {
				result.Errors = append(result.Errors, model.Issue{Code: "PLATFORM_UNSUPPORTED_WIREGUARD", ObjectType: "wireguard_tunnel", ObjectID: t.ID, Message: "managed WireGuard tunnels currently require macOS"})
			}
		}
		result.OK = len(result.Errors) == 0
	}
	references := geoReferences(value)
	if len(references) == 0 {
		return result
	}

	manifest, err := geodata.ReadManifest(options.GeoDataDirectory)
	if err != nil {
		seen := map[string]bool{}
		for _, reference := range references {
			key := reference.ruleID + "\x00" + reference.option
			if seen[key] {
				continue
			}
			seen[key] = true
			result.Errors = append(result.Errors, model.Issue{
				Code: geodata.ErrorManifestInvalid, ObjectType: "rule", ObjectID: reference.ruleID,
				Option: reference.option, Message: err.Error(),
			})
		}
		result.OK = false
		return result
	}

	available := make(map[string]struct{}, len(manifest.Rules))
	for _, rule := range manifest.Rules {
		available[rule.Kind+"\x00"+rule.Category] = struct{}{}
	}
	seen := map[string]bool{}
	for _, reference := range references {
		key := reference.kind + "\x00" + reference.category
		if _, exists := available[key]; exists {
			continue
		}
		issueKey := reference.ruleID + "\x00" + reference.option + "\x00" + key
		if seen[issueKey] {
			continue
		}
		seen[issueKey] = true
		geoErr := &geodata.Error{
			Code: geodata.ErrorCategoryNotFound, Kind: reference.kind, Category: reference.category,
		}
		result.Errors = append(result.Errors, model.Issue{
			Code: geoErr.Code, ObjectType: "rule", ObjectID: reference.ruleID,
			Option: reference.option, Message: geoErr.Error(),
		})
	}
	result.OK = len(result.Errors) == 0
	result.WarningGroups = model.GroupWarnings(result.Warnings)
	return result
}

type geoReference struct {
	kind, category, ruleID, option string
}

func geoReferences(value model.Intent) []geoReference {
	var references []geoReference
	for _, rule := range value.Rules {
		if !rule.Enabled {
			continue
		}
		for _, expression := range rule.DomainMatch {
			if category, ok := validReference(expression, "geosite"); ok {
				references = append(references, geoReference{kind: "geosite", category: category, ruleID: rule.ID, option: "domain_match"})
			}
		}
		for _, expression := range rule.IPMatch {
			if category, ok := validReference(expression, "geoip"); ok {
				references = append(references, geoReference{kind: "geoip", category: category, ruleID: rule.ID, option: "ip_match"})
			}
		}
	}
	return references
}

func validReference(expression, kind string) (string, bool) {
	prefix := kind + ":"
	if !strings.HasPrefix(expression, prefix) {
		return "", false
	}
	category := strings.TrimPrefix(expression, prefix)
	return category, model.ValidGeoCategory(category)
}

func ReservedListener(address string, port int, owner string) model.Listener {
	return model.Listener{Address: address, Port: port, Owner: owner + " listener " + net.JoinHostPort(address, strconv.Itoa(port))}
}
