// SPDX-License-Identifier: GPL-3.0-or-later

package macos

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

// Runner is deliberately narrow so launchd/ifconfig/sing-box interactions can
// be tested without a Darwin machine or a running LaunchDaemon.
type Runner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

type ExecRunner struct {
	Timeout time.Duration
	// DNS mutations must stay in launchd's process group so a killed owner
	// cannot leave a late networksetup write racing recovery.
	JoinProcessGroup bool
}

const (
	defaultCommandTimeout = 2 * time.Minute
	commandWaitDelay      = 5 * time.Second
)

func (runner ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	timeout := runner.Timeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(commandCtx, name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: !runner.JoinProcessGroup}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		if runner.JoinProcessGroup {
			return command.Process.Kill()
		}
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	command.WaitDelay = commandWaitDelay
	output, err := command.CombinedOutput()
	if err != nil {
		if commandCtx.Err() != nil {
			err = commandCtx.Err()
		}
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func runtimePaths(runDirectory, stateDirectory string) Paths {
	root := filepath.Clean(runDirectory)
	return Paths{
		Root:                 root,
		ConfigDirectory:      filepath.Join(root, "config"),
		ConfigPath:           filepath.Join(root, "config", "config.json"),
		GenerationsDirectory: filepath.Join(root, "generations"),
		StateDirectory:       stateDirectory,
		LogsDirectory:        filepath.Join(root, "logs"),
	}
}
