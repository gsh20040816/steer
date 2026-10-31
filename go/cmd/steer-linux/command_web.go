// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

func runWeb(args []string) error {
	flags := flag.NewFlagSet("web", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/steer/config.json", "canonical JSON configuration file")
	runDirectory := flags.String("run-dir", "/run/steer", "runtime state directory")
	stateDirectory := flags.String("state-dir", "/var/lib/steer", "generated state directory")
	seedDirectory := flags.String("seed-dir", "/usr/share/steer/geodata-seed", "package-owned Geo seed directory")
	listen := flags.String("listen", "127.0.0.1:9080", "loopback listen address")
	webConfigPath := flags.String("web-config", defaultWebCredentialsPath, "Web credentials configuration file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("web accepts flags only")
	}
	// A failed migration leaves schema 9 in place; the page still has to
	// load to show the validation errors.
	if err := migrateLocked(*configPath, *runDirectory, *stateDirectory); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return serveWeb(*listen, *webConfigPath, *configPath, *runDirectory, *stateDirectory, *seedDirectory)
}

func runWebToken(args []string) error {
	flags := flag.NewFlagSet("web-token", flag.ContinueOnError)
	path := flags.String("config", defaultWebCredentialsPath, "Web credentials configuration file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("web-token accepts flags only")
	}
	token, err := configuredWebToken(*path)
	if err != nil {
		return err
	}
	fmt.Println(string(token))
	return nil
}
