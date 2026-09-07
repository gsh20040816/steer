//go:build !linux

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import "os/exec"

func setCoreParentDeath(cmd *exec.Cmd) {}
