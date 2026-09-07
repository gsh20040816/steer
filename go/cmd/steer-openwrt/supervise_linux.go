// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"os/exec"
	"syscall"
)

func setCoreParentDeath(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
