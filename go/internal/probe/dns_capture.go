// SPDX-License-Identifier: GPL-3.0-or-later

package probe

import (
	"encoding/json"
	"os"
	"strings"
)

// DNSCaptureDiagnostic reports only what can be verified from the published
// Active generation. It does not claim that arbitrary application traffic was
// observed or that encrypted DNS was blocked.
type DNSCaptureDiagnostic struct {
	Mode             string `json:"mode"`
	ActiveGeneration string `json:"active_generation,omitempty"`
	Configured       bool   `json:"configured"`
	Detail           string `json:"detail"`
}

func InspectDNSCapture(mode, generation, singBoxPath, firewallPath string) DNSCaptureDiagnostic {
	result := DNSCaptureDiagnostic{Mode: mode, ActiveGeneration: generation}
	if generation == "" {
		result.Detail = "no Active generation is available for port-53 capture inspection"
		return result
	}
	content, err := os.ReadFile(singBoxPath)
	if err != nil || !hasDNSHijackRule(content, mode) {
		result.Detail = "the Active generation is missing the expected port-53 DNS hijack rule"
		return result
	}
	if firewallPath != "" {
		firewall, readErr := os.ReadFile(firewallPath)
		firewallText := string(firewall)
		if readErr != nil || !strings.Contains(firewallText, "dport 53") ||
			(!strings.Contains(firewallText, "redirect") && !strings.Contains(firewallText, "dnat")) {
			result.Detail = "the Active generation is missing the expected port-53 firewall redirect"
			return result
		}
	}
	result.Configured = true
	result.Detail = "the published Active generation contains the expected port-53 capture artifacts"
	if mode == "native_hijack_with_local_shim" {
		result.Detail = "the Active generation enables native DNS hijacking and retains the local-destination compatibility shim"
	}
	return result
}

func hasDNSHijackRule(content []byte, mode string) bool {
	var document map[string]any
	if json.Unmarshal(content, &document) != nil {
		return false
	}
	if mode == "native_hijack_with_local_shim" {
		inbounds, _ := document["inbounds"].([]any)
		found := false
		for _, value := range inbounds {
			inbound, _ := value.(map[string]any)
			if inbound["type"] == "tun" && inbound["dns_mode"] == "hijack" && inbound["auto_redirect"] == true {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	route, _ := document["route"].(map[string]any)
	rules, _ := route["rules"].([]any)
	for _, value := range rules {
		rule, _ := value.(map[string]any)
		if rule["action"] != "hijack-dns" {
			continue
		}
		switch mode {
		case "tun_port53_hijack":
			if containsPort53(rule["port"]) && containsString(rule["network"], "tcp") && containsString(rule["network"], "udp") {
				return true
			}
		case "dedicated_shim", "native_hijack_with_local_shim":
			if containsStringPrefix(rule["inbound"], "steer-dns") {
				return true
			}
		default:
			return true
		}
	}
	return false
}

func containsString(value any, expected string) bool {
	values, _ := value.([]any)
	for _, item := range values {
		if item == expected {
			return true
		}
	}
	return false
}

func containsStringPrefix(value any, prefix string) bool {
	values, _ := value.([]any)
	for _, item := range values {
		if text, ok := item.(string); ok && strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func containsPort53(value any) bool {
	ports, _ := value.([]any)
	for _, port := range ports {
		if number, ok := port.(float64); ok && number == 53 {
			return true
		}
	}
	return false
}
