// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"errors"
	"fmt"
)

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "version":
		fmt.Println(version)
		return nil
	case "validate":
		return runValidate(args[1:])
	case "apply":
		return runApply(args[1:])
	case "health":
		return runHealth(args[1:])
	case "status":
		return runStatus(args[1:])
	case "probe":
		return runProbe(args[1:])
	case "cleanup":
		return runCleanup(args[1:])
	case "geo-catalog":
		return runGeoCatalog(args[1:])
	case "subscription":
		return runSubscription(args[1:])
	case "web":
		return runWeb(args[1:])
	case "web-token":
		return runWebToken(args[1:])
	case "_run":
		return runService(args[1:])
	case "_migrate":
		return runMigrate(args[1:])
	default:
		return usage()
	}
}

func usage() error {
	return errors.New("usage: steer version|validate|apply|health|status|probe|cleanup|geo-catalog|subscription|web|web-token|_run [flags]")
}
