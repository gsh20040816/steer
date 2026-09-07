// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gsh20040816/steer/go/internal/platform/openwrt"
)

func runSupervise(args []string) error {
	flags := flag.NewFlagSet("_supervise", flag.ContinueOnError)
	core := flags.String("core", "/usr/bin/sing-box", "core executable")
	config := flags.String("config", "/run/steer/current/sing-box.json", "active configuration")
	state := flags.String("recovery-state", "/run/steer/netlink-recovery.json", "recovery cooldown")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("_supervise accepts flags only")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	var last time.Time
	if data, err := os.ReadFile(*state); err == nil {
		if err = json.Unmarshal(data, &last); err != nil {
			return fmt.Errorf("read recovery cooldown: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	for ctx.Err() == nil {
		recoverCore, err := superviseCore(ctx, *core, *config, *state, last)
		if err != nil {
			return err
		}
		if !recoverCore {
			return nil
		}
		last = time.Now()
	}
	return nil
}

func superviseCore(ctx context.Context, core, config, state string, last time.Time) (bool, error) {
	restore, err := openwrt.RaiseReceiveDefault("/proc/sys")
	if err != nil {
		fmt.Fprintln(os.Stderr, "netlink guard: receive buffer adjustment unavailable:", err)
	}
	defer restore()
	cmd := exec.Command(core, "run", "-c", config)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	setCoreParentDeath(cmd)
	if err := cmd.Start(); err != nil {
		return false, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stop := func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}
	// Netlink subscriptions are created before the core finishes startup. Keep
	// the larger default through this short window, then restore the host value.
	startup := time.NewTimer(5 * time.Second)
	defer startup.Stop()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	guard := openwrt.NetlinkGuard{LastRecovery: last}
	for {
		select {
		case <-ctx.Done():
			stop()
			return false, nil
		case err := <-done:
			if err == nil {
				err = fmt.Errorf("core exited unexpectedly")
			}
			return false, err
		case <-startup.C:
			restore()
		case now := <-ticker.C:
			s, e := openwrt.ReadNetlinkSample("/proc", cmd.Process.Pid, now)
			if e != nil {
				guard = openwrt.NetlinkGuard{LastRecovery: last}
				continue
			}
			if !guard.Observe(s) {
				continue
			}
			// Persist before stopping, so procd restarts cannot bypass the cooldown.
			data, _ := json.Marshal(now)
			if e = os.MkdirAll(filepath.Dir(state), 0700); e == nil {
				e = os.WriteFile(state+".tmp", data, 0600)
			}
			if e == nil {
				e = os.Rename(state+".tmp", state)
			}
			if e != nil {
				fmt.Fprintln(os.Stderr, "netlink guard: recovery skipped; cannot save cooldown:", e)
				guard.LastRecovery = now
				continue
			}
			fmt.Fprintf(os.Stderr, "netlink guard: recovering core pid=%d route_socket=%s queue=%d drops=%d after sustained CPU and stalled notifications\n", cmd.Process.Pid, s.Inode, s.Queue, s.Drops)
			stop()
			return ctx.Err() == nil, nil
		}
	}
}
