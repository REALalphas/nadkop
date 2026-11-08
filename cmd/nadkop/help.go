package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

var commandHelp = map[string]string{
	"validate": "validate [-config FILE] [-output DIR]\nValidate configuration syntax and settings without fetching subscriptions or writing files.\nDoes not check fetched servers or compile geodata. -output is accepted and ignored.",
	"nodes":    "nodes [-config FILE] [-output DIR]\nFetch sources, synchronize servers.json under the configuration lock, and print node tags.\nThis can update the persistent server catalogue. -output is accepted and ignored.",
	"generate": "generate [-config FILE] [-output DIR]\nFetch sources, synchronize servers.json, compile, and publish a new private generation directory.\nThe output directory must not exist. Does not activate the generation or change network rules.",
	"apply":    "apply\nFetch sources, synchronize the catalogue, compile, and ask the service to activate a prepared generation.\nCan replace running processes and network/DNS rules after preflight succeeds.",
	"updater":  "updater\nRun the scheduled subscription updater until SIGINT/SIGTERM.\nAt the configured interval, perform apply, including source fetches and service/network changes.",
	"monitor":  "monitor [-manifest FILE]\nRun health probes and apply Xray balancer choices until SIGINT/SIGTERM.\nDefault manifest: /var/run/nadkop/current/health.json.",
	"network":  "network check|apply|stop|rollback|commit [-generation DIR]\ncheck: validate prerequisites, plan, and listeners.\napply: install rules for a generation, retaining rollback state.\nstop: remove Nadkop network rules and restore managed DNS settings.\nrollback: restore the previous network publication.\ncommit: finalize publication and remove rollback state.\nAll actions acquire a runtime lock; check/apply/stop can recover a pending transaction and change network state.\nDefault generation: /var/run/nadkop/current.",
	"rpc":      "rpc list|call METHOD\nInternal rpcd executable protocol: list prints method signatures; call reads one JSON object from stdin.\nEffects depend on METHOD: save/addsource/nodes can update persistent state; service changes the service.\nUse LuCI/rpcd ACLs for authorized access.",
}

// Parse help before any file, lock, stdin, network, or service operation. A help
// flag does not excuse invalid arguments, and flag values may themselves be -h.
func cliHelp(args []string, out io.Writer) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		if len(args) == 1 {
			fmt.Fprintln(out, "Usage: nadkop COMMAND [OPTIONS]\nCommands: validate, nodes, generate, apply, updater, network, monitor, rpc\nUse nadkop COMMAND --help for command details. Help performs no operations.")
			return true, nil
		}
		if args[0] != "help" {
			return true, fmt.Errorf("unexpected arguments")
		}
		args = append(append([]string{}, args[1:]...), "--help")
	}
	requested := false
	for _, arg := range args[1:] {
		if arg == "-h" || arg == "--help" {
			requested = true
		}
	}
	if !requested {
		return false, nil
	}
	command := args[0]
	usage, known := commandHelp[command]
	if !known {
		return true, fmt.Errorf("unknown command")
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	short := fs.Bool("h", false, "help")
	long := fs.Bool("help", false, "help")
	rest := args[1:]
	switch command {
	case "validate", "nodes", "generate":
		fs.String("config", "/etc/nadkop/config.json", "configuration file")
		fs.String("output", "/var/run/nadkop/current", "new output directory")
	case "monitor":
		fs.String("manifest", "/var/run/nadkop/current/health.json", "monitor manifest")
	case "network":
		fs.String("generation", "/var/run/nadkop/current", "generation directory")
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			switch rest[0] {
			case "check", "apply", "stop", "rollback", "commit":
				rest = rest[1:]
			default:
				return true, fmt.Errorf("unknown network action")
			}
		}
	case "rpc":
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			switch rest[0] {
			case "list":
				rest = rest[1:]
			case "call":
				rest = rest[1:]
				if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
					rest = rest[1:] // METHOD is opaque to the CLI.
				}
			default:
				return true, fmt.Errorf("unknown rpc action")
			}
		}
	}
	if err := fs.Parse(rest); err != nil {
		return true, err
	}
	if fs.NArg() != 0 {
		return true, fmt.Errorf("unexpected arguments")
	}
	if !*short && !*long {
		return false, nil
	}
	fmt.Fprintln(out, "Usage: nadkop "+usage)
	if command == "validate" || command == "nodes" || command == "generate" {
		fmt.Fprintln(out, "Default config: /etc/nadkop/config.json. Default output: /var/run/nadkop/current.")
	}
	return true, nil
}
