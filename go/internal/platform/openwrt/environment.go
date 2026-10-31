// SPDX-License-Identifier: GPL-3.0-or-later
// Package openwrt implements the OpenWrt adapter for the shared Steer core.
package openwrt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Runner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

const (
	defaultCommandTimeout = 2 * time.Minute
	commandWaitDelay      = 5 * time.Second
)

type ExecRunner struct {
	Timeout time.Duration
}

func (runner ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	commandCtx, cancel := withCommandTimeout(ctx, runner.Timeout)
	defer cancel()
	command := newCommandContext(commandCtx, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		if commandCtx.Err() != nil {
			err = commandCtx.Err()
		}
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func newCommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
	command.WaitDelay = commandWaitDelay
	return command
}

func withCommandTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func atomicWrite(path string, content []byte) error {
	directory := filepath.Dir(path)
	info, err := os.Stat(path)
	mode := os.FileMode(0o600)
	if err == nil {
		mode = info.Mode().Perm()
	}
	temporary, err := os.CreateTemp(directory, ".steer.atomic.*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
