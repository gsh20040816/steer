// SPDX-License-Identifier: GPL-3.0-or-later
package compiler

import (
	model "github.com/gsh20040816/steer/go/internal/intent"
	"reflect"
	"testing"
)

func TestDirectMACExclusionsPreserveOrderedPolicy(t *testing.T) {
	const mac = "02:aa:00:00:00:10"
	direct := model.Rule{Enabled: true, Route: "direct", SourceMACAddress: []string{"02:AA:00:00:00:10", mac}}
	for _, tc := range []struct {
		name   string
		before []model.Rule
		change func(*model.Rule)
		want   bool
	}{
		{name: "whole device", want: true},
		{name: "earlier direct", before: []model.Rule{{Enabled: true, Route: "direct", DomainMatch: []string{"domain:lan"}}}, want: true},
		{name: "earlier proxy", before: []model.Rule{{Enabled: true, Route: "proxy", DomainMatch: []string{"domain:example.com"}}}},
		{name: "earlier reject", before: []model.Rule{{Enabled: true, Route: "block", Port: []int{443}}}},
		{name: "other MAC proxy", before: []model.Rule{{Enabled: true, Route: "proxy", SourceMACAddress: []string{"02:aa:00:00:00:11"}}}, want: true},
		{name: "same MAC proxy", before: []model.Rule{{Enabled: true, Route: "proxy", SourceMACAddress: []string{"02:AA:00:00:00:10"}}}},
		{name: "local proxy selector", before: []model.Rule{{Enabled: true, Route: "proxy", Inbound: []string{"local"}}}, want: true},
		{name: "disabled proxy", before: []model.Rule{{Route: "proxy"}}, want: true},
		{name: "default remains fallback", before: []model.Rule{{Enabled: true, Default: true, Route: "proxy"}}, want: true},
		{name: "disabled", change: func(r *model.Rule) { r.Enabled = false }},
		{name: "domain", change: func(r *model.Rule) { r.DomainMatch = []string{"domain:example.com"} }},
		{name: "destination", change: func(r *model.Rule) { r.IPMatch = []string{"1.1.1.1/32"} }},
		{name: "source", change: func(r *model.Rule) { r.SourceIPCIDR = []string{"172.30.50.10/32"} }},
		{name: "network", change: func(r *model.Rule) { r.Network = []string{"udp"} }},
		{name: "protocol", change: func(r *model.Rule) { r.Protocol = []string{"bittorrent"} }},
		{name: "port", change: func(r *model.Rule) { r.Port = []int{443} }},
		{name: "inbound", change: func(r *model.Rule) { r.Inbound = []string{"local"} }},
		{name: "allowed IPs", change: func(r *model.Rule) { r.AllowedIPs = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := representativeIntent()
			r := direct
			if tc.change != nil {
				tc.change(&r)
			}
			value.Rules = append(append([]model.Rule{}, tc.before...), r, model.Rule{Enabled: true, Route: "proxy"})
			var want []string
			if tc.want {
				want = []string{mac}
			}
			if got := DirectMACExclusions(value); !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}
