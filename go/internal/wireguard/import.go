// SPDX-License-Identifier: GPL-3.0-or-later
// Package wireguard imports portable WireGuard configuration without executing
// wg-quick hooks or mutating system DNS/routes.
package wireguard

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"

	model "github.com/gsh20040816/steer/go/internal/intent"
)

type Import struct {
	Tunnel   model.WireGuardTunnel `json:"tunnel"`
	DNS      []string              `json:"dns,omitempty"`
	Warnings []string              `json:"warnings,omitempty"`
}

func Parse(r io.Reader) (Import, error) {
	result := Import{Tunnel: model.WireGuardTunnel{Enabled: true}}
	section := ""
	interfaces := 0
	peer := -1
	data, readErr := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if readErr != nil {
		return Import{}, readErr
	}
	if len(data) > 1<<20 {
		return Import{}, fmt.Errorf("WireGuard configuration exceeds 1 MiB")
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	seen := map[string]bool{}
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(strings.SplitN(scanner.Text(), "#", 2)[0])
		if text == "" {
			continue
		}
		fail := func() (Import, error) {
			return Import{}, fmt.Errorf("WireGuard line %d: unsupported, duplicate or malformed field", line)
		}
		if strings.HasPrefix(text, "[") {
			section = text
			seen = map[string]bool{}
			switch section {
			case "[Interface]":
				interfaces++
				if interfaces != 1 || peer >= 0 {
					return fail()
				}
			case "[Peer]":
				if interfaces != 1 {
					return fail()
				}
				result.Tunnel.Peers = append(result.Tunnel.Peers, model.WireGuardPeer{})
				peer++
			default:
				return fail()
			}
			continue
		}
		key, val, ok := strings.Cut(text, "=")
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if !ok || val == "" {
			return fail()
		}
		list := func() []string {
			values := strings.Split(val, ",")
			for i := range values {
				values[i] = strings.TrimSpace(values[i])
			}
			return values
		}
		if seen[key] && key != "Address" && key != "DNS" && key != "AllowedIPs" {
			return fail()
		}
		seen[key] = true
		number := func() (int, error) {
			n, e := strconv.Atoi(val)
			if e != nil || n < 0 {
				return 0, fmt.Errorf("WireGuard line %d: invalid number", line)
			}
			return n, nil
		}
		if section == "[Interface]" {
			switch key {
			case "PrivateKey":
				result.Tunnel.PrivateKey = val
			case "Address":
				for _, raw := range list() {
					p, e := netip.ParsePrefix(raw)
					if e != nil {
						return fail()
					}
					if p.Bits() != p.Addr().BitLen() {
						result.Warnings = append(result.Warnings, "Tunnel address "+raw+" converted to a host prefix; remote networks remain in AllowedIPs")
					}
					result.Tunnel.Address = append(result.Tunnel.Address, netip.PrefixFrom(p.Addr(), p.Addr().BitLen()).String())
				}
			case "ListenPort":
				n, e := number()
				if e != nil {
					return Import{}, e
				}
				result.Tunnel.ListenPort = n
			case "MTU":
				n, e := number()
				if e != nil {
					return Import{}, e
				}
				result.Tunnel.MTU = n
			case "DNS":
				result.DNS = append(result.DNS, list()...)
				result.Warnings = append(result.Warnings, "DNS entries are suggestions only; system DNS and DNS policy are not changed")
			default:
				return fail()
			}
		} else if section == "[Peer]" {
			p := &result.Tunnel.Peers[peer]
			switch key {
			case "PublicKey":
				p.PublicKey = val
			case "PresharedKey":
				p.PreSharedKey = val
			case "AllowedIPs":
				p.AllowedIPs = append(p.AllowedIPs, list()...)
			case "Endpoint":
				host, port, e := net.SplitHostPort(val)
				if e != nil {
					return fail()
				}
				n, e := strconv.Atoi(port)
				if e != nil {
					return fail()
				}
				p.Server = host
				p.ServerPort = n
			case "PersistentKeepalive":
				n, e := number()
				if e != nil {
					return Import{}, e
				}
				p.PersistentKeepalive = n
			default:
				return fail()
			}
		} else {
			return fail()
		}
	}
	if e := scanner.Err(); e != nil {
		return Import{}, e
	}
	if interfaces != 1 || len(result.Tunnel.Peers) == 0 {
		return Import{}, fmt.Errorf("WireGuard requires one Interface and at least one Peer")
	}
	// Use the same field validator as Save/Apply; synthetic identity is not
	// returned and no key material is interpolated into diagnostics.
	t := result.Tunnel
	t.ID = "wg-import"
	validation := model.Validate(model.Intent{WireGuardTunnels: []model.WireGuardTunnel{t}})
	for _, issue := range validation.Errors {
		if issue.ObjectType == "wireguard_tunnel" {
			return Import{}, fmt.Errorf("WireGuard %s: %s", issue.Option, issue.Message)
		}
	}
	return result, nil
}
