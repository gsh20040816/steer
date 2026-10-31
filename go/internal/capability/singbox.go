// SPDX-License-Identifier: GPL-3.0-or-later
// Package capability validates the installed sing-box runtime shared by all platforms.
package capability

import (
	"regexp"
	"sort"
	"strings"
)

type Report struct {
	OK      bool     `json:"ok"`
	Version string   `json:"version"`
	Tags    []string `json:"tags"`
	Errors  []string `json:"errors"`
}

var versionPattern = regexp.MustCompile(`(?m)^sing-box version ([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?)`)

func Parse(output string, required []string) Report {
	report := Report{}
	match := versionPattern.FindStringSubmatch(output)
	if len(match) != 2 {
		report.Errors = append(report.Errors, "cannot parse sing-box version output")
		return report
	}
	report.Version = match[1]
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "Tags:") {
			continue
		}
		report.Tags = append(report.Tags, strings.FieldsFunc(strings.TrimSpace(strings.TrimPrefix(line, "Tags:")), func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })...)
	}
	sort.Strings(report.Tags)
	tagSet := map[string]bool{}
	for _, tag := range report.Tags {
		tagSet[tag] = true
	}
	for _, capability := range required {
		requiredTag := ""
		switch capability {
		case "with_quic", "dns_quic":
			requiredTag = "with_quic"
		case "with_utls", "with_wireguard", "with_gvisor":
			requiredTag = capability
		}
		if requiredTag != "" && !tagSet[requiredTag] {
			report.Errors = append(report.Errors, "sing-box build is missing tag "+requiredTag+" required by current intent")
		}
	}
	report.Errors = unique(report.Errors)
	report.OK = len(report.Errors) == 0
	return report
}

func unique(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
