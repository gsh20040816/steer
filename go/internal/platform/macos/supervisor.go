// SPDX-License-Identifier: GPL-3.0-or-later

package macos

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// RunSupervised owns the core and DNS lease together. DNS is restored before
// orderly shutdown; the independent control daemon recovers after SIGKILL.
func (backend *Backend) RunSupervised(ctx context.Context, config string) (result error) {
	return backend.runSupervised(ctx, config, backend.DNSManager(), backend.checkLocalDNS, backend.checkSystemDNS)
}

// A remote router may itself hijack port 53. A successful DNS reply alone is
// therefore not proof that our own core is ready to take over system DNS.
func (backend *Backend) checkLocalDNS(ctx context.Context) error {
	if err := backend.checkDNSRouting(ctx); err != nil {
		return err
	}
	return checkDNSReachable(ctx)
}

func (backend *Backend) checkDNSRouting(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	output, err := backend.runner.Output(ctx, backend.options.IfconfigBinary, "-a")
	if err != nil {
		return err
	}
	addresses := make(map[string]map[string]bool)
	current := ""
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			current = strings.TrimSuffix(fields[0], ":")
			addresses[current] = make(map[string]bool)
		} else if current != "" && len(fields) > 1 && (fields[0] == "inet" || fields[0] == "inet6") {
			addresses[current][strings.SplitN(fields[1], "%", 2)[0]] = true
		}
	}
	for i, server := range SystemDNSServers {
		args := []string{"-n", "get"}
		if strings.Contains(server, ":") {
			args = append(args, "-inet6")
		}
		output, err := backend.runner.Output(ctx, "/sbin/route", append(args, server)...)
		if err != nil {
			return err
		}
		device := ""
		for _, line := range strings.Split(string(output), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "interface:" {
				device = fields[1]
			}
		}
		local := strings.SplitN(backend.plan.Resources.TunAddresses[i], "/", 2)[0]
		if !strings.HasPrefix(device, "utun") || !addresses[device][local] {
			return fmt.Errorf("DNS address %s is not routed to Steer's local utun (interface %s)", server, device)
		}
	}
	return nil
}

func (backend *Backend) runSupervised(ctx context.Context, config string, manager DNSManager, probe func(context.Context) error, systemDNS func(context.Context) error) (result error) {
	if err := manager.Restore(ctx); err != nil {
		return err
	}
	readyPath := filepath.Join(backend.options.RunDirectory, "dns-ready.json")
	_ = os.Remove(readyPath)
	cmd := exec.Command(backend.options.SingBoxBinary, "run", "-c", config)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	exited := false
	defer func() {
		_ = os.Remove(readyPath)
		restoreCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		result = errors.Join(result, manager.Restore(restoreCtx))
		cancel()
		if !exited {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
		}
	}()
	// Readiness is diagnostic, not permission for the core to keep running.
	// Retry DNS takeover after transient failures without tearing down traffic.
	data, _ := json.Marshal(struct {
		Config string `json:"config"`
	}{Config: config})
	lastError := ""
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case err := <-done:
			exited = true
			return err
		case <-ctx.Done():
			return nil
		case <-timer.C:
			checkCtx, cancel := context.WithTimeout(ctx, backend.options.HealthTimeout)
			err := probe(checkCtx)
			if err == nil {
				// Never point system DNS at an entrance we have not verified.
				err = manager.Acquire(checkCtx)
			}
			if err == nil {
				err = systemDNS(checkCtx)
			}
			cancel()
			if err == nil {
				err = atomicWriteMode(readyPath, data, 0o644)
			} else {
				failure, _ := json.Marshal(struct {
					Config string `json:"config"`
					Error  string `json:"error"`
				}{config, err.Error()})
				if writeErr := atomicWriteMode(readyPath, failure, 0o644); writeErr != nil {
					_ = os.Remove(readyPath)
					err = errors.Join(err, writeErr)
				}
			}
			if err != nil {
				if err.Error() != lastError {
					fmt.Fprintf(os.Stderr, "DNS health check failed (core remains running): %v\n", err)
				}
				lastError = err.Error()
			} else if lastError != "" {
				fmt.Fprintln(os.Stderr, "DNS health check recovered")
				lastError = ""
			}
			timer.Reset(5 * time.Second)
		}
	}
}

func (backend *Backend) checkSystemDNS(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	output, err := backend.runner.Output(ctx, "/usr/sbin/scutil", "--dns")
	if err != nil {
		return err
	}
	return validateSystemDNS(string(output))
}

func validateSystemDNS(output string) error {
	// Only inspect resolver #1 in the unscoped section. macOS can omit the
	// IPv6 server on IPv4-only networks even when both servers are configured.
	inDefault := false
	found := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "DNS configuration (") {
			break
		}
		if strings.HasPrefix(line, "resolver #") {
			if inDefault || line != "resolver #1" {
				break
			}
			inDefault = true
			continue
		}
		if !inDefault || !strings.HasPrefix(line, "nameserver[") {
			continue
		}
		_, server, ok := strings.Cut(line, " : ")
		server = strings.TrimSpace(server)
		if !ok || (server != SystemDNSServers[0] && server != SystemDNSServers[1]) {
			return fmt.Errorf("system default DNS contains unmanaged server %q", server)
		}
		found = true
	}
	if !found {
		return errors.New("system default DNS has no Steer nameserver")
	}
	return nil
}

func checkDNSReachable(ctx context.Context) error {
	errs := make(chan error, len(SystemDNSServers)*2)
	for _, server := range SystemDNSServers {
		for _, network := range []string{"udp", "tcp"} {
			go func() { errs <- probeDNS(ctx, network, net.JoinHostPort(server, "53")) }()
		}
	}
	var result error
	for range len(SystemDNSServers) * 2 {
		result = errors.Join(result, <-errs)
	}
	return result
}

func probeDNS(ctx context.Context, network, address string) error {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	// A normal root NS query, with RD set. An upstream timeout/SERVFAIL must
	// not count as a healthy DNS service just because the port is listening.
	query := []byte{0x53, 0x54, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 1}
	packet := query
	if network == "tcp" {
		packet = append([]byte{0, byte(len(query))}, query...)
	}
	if _, err := conn.Write(packet); err != nil {
		return err
	}
	response := make([]byte, 4096)
	var n int
	if network == "tcp" {
		length := make([]byte, 2)
		if _, err := io.ReadFull(conn, length); err != nil {
			return err
		}
		n = int(binary.BigEndian.Uint16(length))
		if n > len(response) {
			return errors.New("DNS health response too large")
		}
		_, err = io.ReadFull(conn, response[:n])
	} else {
		n, err = conn.Read(response)
	}
	if err != nil {
		return err
	}
	if n < 12 || response[0] != query[0] || response[1] != query[1] || response[2]&0x80 == 0 || response[3]&15 != 0 {
		return fmt.Errorf("invalid or unsuccessful DNS health response from %s", address)
	}
	return nil
}

func (backend *Backend) checkDNSReady(ctx context.Context, expectedDirectory string) error {
	if backend.options.CheckDNS != nil {
		return backend.options.CheckDNS()
	}
	data, err := os.ReadFile(filepath.Join(backend.options.RunDirectory, "dns-ready.json"))
	if err != nil {
		return fmt.Errorf("system DNS takeover is not ready: %w", err)
	}
	var marker struct {
		Config string `json:"config"`
		Error  string `json:"error"`
	}
	if json.Unmarshal(data, &marker) != nil || marker.Config != filepath.Join(expectedDirectory, "sing-box.json") {
		return errors.New("DNS takeover belongs to a different generation")
	}
	if marker.Error != "" {
		return fmt.Errorf("DNS health check failed: %s", marker.Error)
	}
	if err := backend.checkDNSRouting(ctx); err != nil {
		return err
	}
	return backend.checkSystemDNS(ctx)
}
