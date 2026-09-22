// SPDX-License-Identifier: GPL-3.0-or-later

package compiler

import model "github.com/gsh20040816/steer/go/internal/intent"

// proofExpr is a compile-time boolean expression over pre-match metadata.
// Constants are kept out of sing-box JSON: an empty native rule means true,
// while false must never accidentally become an empty/match-all rule.
type proofExpr struct {
	constant *bool
	match    map[string]any
}

func proofConstant(value bool) proofExpr        { return proofExpr{constant: &value} }
func proofMatch(value map[string]any) proofExpr { return proofExpr{match: value} }

func proofCombine(mode string, expressions ...proofExpr) proofExpr {
	var matches []map[string]any
	for _, expression := range expressions {
		if expression.constant != nil {
			if *expression.constant == (mode == "or") {
				return expression
			}
			continue
		}
		matches = append(matches, expression.match)
	}
	if len(matches) == 0 {
		return proofConstant(mode == "and")
	}
	return proofMatch(combine(matches, mode))
}

func proofNot(expression proofExpr) proofExpr {
	if expression.constant != nil {
		return proofConstant(!*expression.constant)
	}
	// Always negate the entire expression, including nested AND/OR groups.
	return proofMatch(map[string]any{"type": "logical", "mode": "and", "invert": true, "rules": []any{expression.match}})
}

// ruleProof is an internal rule IR: yes and no are independently sufficient
// proofs. Neither being true means UNKNOWN, never FALSE. Canonical condition
// groups are ANDed; values within each group retain compileRuleMatch semantics.
type ruleProof struct{ yes, no proofExpr }

func preMatchProof(rule model.Rule, allowDNS bool) ruleProof {
	var yes, no []proofExpr
	add := func(match map[string]any, known proofExpr) {
		predicate := proofMatch(match)
		yes = append(yes, proofCombine("and", known, predicate))
		no = append(no, proofCombine("and", known, proofNot(predicate)))
	}
	// IP-literal IPv6 traffic is the only scope compiled below. Local
	// proxy inbound selectors cannot match that scope.
	if len(rule.Inbound) > 0 {
		return ruleProof{proofConstant(false), proofConstant(true)}
	}
	if len(rule.DomainMatch) > 0 {
		known := proofConstant(false)
		if allowDNS {
			known = proofMatch(map[string]any{"domain_regex": []string{".+"}})
		}
		add(compileDomainGroup(rule.DomainMatch), known)
	}
	if len(rule.IPMatch) > 0 {
		add(compileIPGroup(rule.IPMatch), proofConstant(true))
	}
	if len(rule.SourceIPCIDR) > 0 {
		add(map[string]any{"source_ip_cidr": rule.SourceIPCIDR}, proofConstant(true))
	}
	if len(rule.Network) > 0 {
		add(map[string]any{"network": rule.Network}, proofConstant(true))
	}
	if len(rule.Port) > 0 {
		add(map[string]any{"port": rule.Port}, proofConstant(true))
	}
	if len(rule.Protocol) > 0 {
		// Application protocol is unavailable at pre-match. Keep it UNKNOWN.
		yes = append(yes, proofConstant(false))
		no = append(no, proofConstant(false))
	}
	if len(rule.SourceMACAddress) > 0 {
		// A positive native match proves the neighbor was found. A negative
		// match does not distinguish an absent neighbor from a different MAC.
		yes = append(yes, proofMatch(compileRuleMatch(model.Rule{SourceMACAddress: rule.SourceMACAddress}, false)))
		no = append(no, proofConstant(false))
	}
	return ruleProof{proofCombine("and", yes...), proofCombine("or", no...)}
}

func compileDirectBypass(intent model.Intent, target Target) []any {
	mode := intent.Main.DirectBypass
	if (mode != "static" && mode != "dns") || len(target.BypassInboundTags) == 0 {
		return nil
	}
	routes := indexRoutes(intent.Routes)
	// Earlier Direct rules cannot change the routing outcome. Only possible
	// non-Direct winners block bypass. A known later Direct can therefore
	// bypass even when an earlier Direct domain/protocol condition is unknown.
	guard := proofConstant(true)
	var result []any
	var defaultRule *model.Rule
	emit := func(rule model.Rule) {
		proof := preMatchProof(rule, mode == "dns")
		candidate := proofCombine("and", guard, proof.yes)
		if candidate.constant != nil && !*candidate.constant {
			return
		}
		scope := proofMatch(map[string]any{
			"inbound": target.BypassInboundTags, "ip_version": 6,
		})
		// Port 53 belongs to the platform DNS capture path, including when
		// the user has a catch-all Direct rule.
		match := proofCombine("and", scope, proofNot(proofMatch(map[string]any{"port": []int{53}})), candidate).match
		// No outbound: skip this rule outside auto_redirect pre-match. The
		// complete original rules below sniff own every captured connection.
		match["action"] = "bypass"
		result = append(result, match)
	}
	for _, rule := range intent.Rules {
		if !rule.Enabled {
			continue
		}
		if rule.Default {
			copy := rule
			defaultRule = &copy
			continue
		}
		if routes[rule.Route].Kind == "direct" {
			emit(rule)
		} else {
			guard = proofCombine("and", guard, preMatchProof(rule, mode == "dns").no)
		}
	}
	if defaultRule != nil && routes[defaultRule.Route].Kind == "direct" {
		emit(*defaultRule)
	}
	return result
}
