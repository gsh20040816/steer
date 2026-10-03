// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"strings"

	model "github.com/gsh20040816/steer/go/internal/intent"
	platformvalidation "github.com/gsh20040816/steer/go/internal/validation"
)

// Validate combines the canonical contract with macOS platform limits.
// Source-MAC policy describes gateway traffic and has no reliable meaning for
// a local Darwin TUN, so it is rejected explicitly.
func Validate(value model.Intent) model.Validation {
	return ValidateWithGeoDataDirectory(value, DefaultGeoDataDirectory)
}

func ValidateWithGeoDataDirectory(value model.Intent, seedDirectory string) model.Validation {
	seedDirectory = normalizeGeoDataDirectory(seedDirectory)
	validation := platformvalidation.Validate(value, platformvalidation.Options{
		WireGuard:             true,
		IPv6WildcardDualStack: true,
		GeoDataDirectory:      seedDirectory,
	})
	if value.Main.DirectBypass != "" && value.Main.DirectBypass != "off" {
		validation.Errors = append(validation.Errors, model.Issue{
			Code: "PLATFORM_UNSUPPORTED_DIRECT_BYPASS", ObjectType: "steer", ObjectID: value.Main.ID,
			Option: "direct_bypass", Message: "macOS does not support kernel direct bypass",
		})
	}
	// The control daemon runs sing-box as root on behalf of any admin-group
	// user without a password, so a Tor node must not choose what root runs.
	for _, node := range value.Nodes {
		if node.Type != "tor" {
			continue
		}
		for option, set := range map[string]bool{
			"executable_path": node.ExecutablePath != "",
			"extra_args":      len(node.ExtraArgs) > 0,
			"data_directory":  node.DataDirectory != "",
		} {
			if set {
				validation.Errors = append(validation.Errors, model.Issue{
					Code: "PLATFORM_UNSUPPORTED_TOR_OPTION", ObjectType: "node", ObjectID: node.ID,
					Option: option, Message: "macOS does not allow custom Tor executables, arguments or data directories",
				})
			}
		}
	}
	for _, rule := range value.Rules {
		if !rule.Enabled {
			continue
		}
		if len(rule.SourceMACAddress) > 0 {
			validation.Errors = append(validation.Errors, model.Issue{
				Code: "PLATFORM_UNSUPPORTED_SOURCE_MAC", ObjectType: "rule", ObjectID: rule.ID,
				Option: "source_mac_address", Message: "macOS does not support source MAC rules",
			})
		}
	}
	validation.OK = len(validation.Errors) == 0
	return validation
}

func normalizeGeoDataDirectory(value string) string {
	if strings.TrimSpace(value) == "" {
		return DefaultGeoDataDirectory
	}
	return value
}
