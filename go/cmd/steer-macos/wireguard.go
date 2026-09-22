// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"github.com/gsh20040816/steer/go/internal/wireguard"
	"io"
	"os"
)

func runParseWireGuard(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("parse-wireguard", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	input := flags.String("input", "", "WireGuard configuration file")
	if e := flags.Parse(args); e != nil {
		return e
	}
	if *input == "" || flags.NArg() != 0 {
		return errors.New("parse-wireguard requires --input")
	}
	f, e := os.Open(*input)
	if e != nil {
		return e
	}
	defer f.Close()
	parsed, e := wireguard.Parse(f)
	if e != nil {
		return e
	}
	return json.NewEncoder(stdout).Encode(parsed)
}
