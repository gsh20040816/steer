// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"time"

	linuxplatform "github.com/gsh20040816/steer/go/internal/platform/linux"
	"github.com/gsh20040816/steer/go/internal/subscription"
)

func runSubscription(args []string) error {
	commands := map[string]bool{"update": true, "status": true, "clean": true, "nodes": true, "node": true}
	if len(args) == 0 || !commands[args[0]] {
		return errors.New("usage: steer subscription update|status|nodes [--id ID] | clean --id ID --node NODE_ID | node --id ID --node NODE_ID --enabled 0|1")
	}
	command := args[0]
	flags := flag.NewFlagSet("subscription", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/steer/config.json", "canonical JSON configuration file")
	stateDirectory := flags.String("state-dir", "/var/lib/steer", "subscription snapshot and node store directory")
	runDirectory := flags.String("run-dir", "/run/steer", "shared operation lock directory")
	id := flags.String("id", "", "subscription ID")
	nodeID := flags.String("node", "", "subscription node ID")
	enabledText := flags.String("enabled", "", "node enabled state for node: 0 or 1")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("subscription subcommands accept flags only")
	}
	if (command == "clean" || command == "node") && (*id == "" || *nodeID == "") {
		return fmt.Errorf("subscription %s requires --id and --node", command)
	}
	if command == "node" && *enabledText != "0" && *enabledText != "1" {
		return errors.New("subscription node requires --enabled 0 or 1")
	}
	if command != "node" && *enabledText != "" {
		return errors.New("--enabled is only accepted by subscription node")
	}
	fail := func(err error) error {
		writeJSON(subscription.ErrorPayloadOf(err))
		return err
	}
	switch command {
	case "status":
		statuses, err := linuxplatform.ReadSubscriptionStatus(*configPath, *stateDirectory)
		if err != nil {
			return fail(err)
		}
		writeSubscriptionStatuses(statuses)
		return nil
	case "nodes":
		list, err := linuxplatform.ListSubscriptionNodes(*configPath, *stateDirectory, *id)
		if err != nil {
			return fail(err)
		}
		writeJSON(list)
		return nil
	case "node":
		err := withOperationLock(*runDirectory, func() error {
			return linuxplatform.SetSubscriptionNodeEnabled(*configPath, *stateDirectory, *id, *nodeID, *enabledText == "1")
		})
		if err != nil {
			return fail(err)
		}
		list, err := linuxplatform.ListSubscriptionNodes(*configPath, *stateDirectory, "")
		if err != nil {
			return fail(err)
		}
		writeJSON(list)
		return nil
	case "clean":
		err := withOperationLock(*runDirectory, func() error {
			return linuxplatform.CleanSubscriptionNode(*configPath, *stateDirectory, *id, *nodeID)
		})
		if err != nil {
			return fail(err)
		}
	default:
		err := withOperationLock(*runDirectory, func() error {
			_, err := linuxplatform.UpdateConfiguredSubscriptions(context.Background(), &http.Client{Timeout: 30 * time.Second}, *configPath, *stateDirectory, *id)
			return err
		})
		if err != nil {
			return fail(err)
		}
	}
	statuses, err := linuxplatform.ReadSubscriptionStatus(*configPath, *stateDirectory)
	if err != nil {
		return fail(err)
	}
	writeSubscriptionStatuses(statuses)
	return nil
}

func writeSubscriptionStatuses(statuses []linuxplatform.SubscriptionStatus) {
	writeJSON(struct {
		OK            bool                               `json:"ok"`
		Subscriptions []linuxplatform.SubscriptionStatus `json:"subscriptions"`
	}{true, statuses})
}
