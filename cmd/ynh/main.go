package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Help is answered before anything else happens — before auto-migration,
	// before dispatch, before any command sees its arguments.
	//
	// `--help` used to be unimplemented on all 31 commands: an error on 27,
	// and on four the flag was ignored and the command ran. `ynh prune --help`
	// deleted run directories; `ynh run <name> --help` started a vendor
	// session. Asking what a command does must never be the same as doing it.
	//
	// Intercepting here rather than in each command is the whole point: with
	// 31 commands there is no version of "remember to check --help first" that
	// stays true, and the next command added would inherit the bug. Control
	// never reaching the action is not something an author can get wrong.
	//
	// It also precedes auto-migration deliberately. Reading help must not
	// rewrite the user's home directory.
	if len(os.Args) > 2 && wantsHelp(os.Args[2:]) {
		if printCommandHelp(os.Stdout, os.Args[1]) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Error: %s\n", helpForUnknown(os.Args[1]))
		os.Exit(1)
	}

	// Auto-migration gate: every command except a few that must remain
	// callable on a legacy home (migrate itself, version, help, paths)
	// runs schema-2 migration first if the home is at schema 1. This is
	// the SINGLE place legacy schema-1 layout is touched; all other code
	// speaks schema 2 only. Failure here aborts the command unless
	// --skip-broken was passed (handled inside cmdMigrate's own path).
	if needsAutoMigrate(os.Args[1]) {
		if err := autoMigrate(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	var err error
	switch os.Args[1] {
	case "init":
		err = cmdInit()
	case "install":
		err = cmdInstall(os.Args[2:])
	case "uninstall", "remove":
		err = cmdUninstall(os.Args[2:])
	case "update":
		err = cmdUpdate(os.Args[2:])
	case "run":
		err = cmdRun(os.Args[2:])
	case "ls", "list":
		err = cmdList(os.Args[2:])
	case "info":
		err = cmdInfo(os.Args[2:])
	case "installed":
		err = cmdInstalled(os.Args[2:])
	case "schema":
		err = cmdSchema(os.Args[2:])
	case "vendors":
		err = cmdVendors(os.Args[2:])
	case "sources":
		err = cmdSources(os.Args[2:])
	case "paths":
		err = cmdPaths(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "search":
		err = cmdSearch(os.Args[2:])
	case "registry":
		err = cmdRegistry(os.Args[2:])
	case "backend":
		err = cmdBackend(os.Args[2:])
	case "delegate":
		err = cmdDelegate(os.Args[2:])
	case "fork":
		err = cmdFork(os.Args[2:])
	case "include":
		err = cmdInclude(os.Args[2:])
	case "focus":
		err = cmdFocus(os.Args[2:])
	case "profile":
		err = cmdProfile(os.Args[2:])
	case "hook":
		err = cmdHook(os.Args[2:])
	case "doctor":
		err = cmdDoctor(os.Args[2:])
	case "mcp":
		err = cmdMCP(os.Args[2:])
	case "sensors":
		err = cmdSensors(os.Args[2:])
	case "trust":
		err = cmdTrust(os.Args[2:])
	case "check":
		err = cmdCheck(os.Args[2:], os.Stdout, os.Stderr)
	case "baseline":
		err = cmdBaseline(os.Args[2:], os.Stdout, os.Stderr)
	case "agent":
		err = cmdAgent(os.Args[2:])
	case "image":
		err = cmdImage(os.Args[2:])
	case "prune":
		err = cmdPrune()
	case "migrate":
		err = cmdMigrate(os.Args[2:])
	case "quarantine":
		err = cmdQuarantine(os.Args[2:])
	case "version", "--version":
		err = cmdVersion(os.Args[2:])
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		// `ynh check` distinguishes its two failure modes by exit code so a
		// gate is unambiguous: 1 means a blocking sensor failed (the report
		// is already on stdout), 2 means ynh could not run the check.
		if errors.Is(err, errCheckBlocked) {
			os.Exit(1)
		}
		if errors.Is(err, errCheckExec) {
			if !errors.Is(err, errStructuredReported) {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
			os.Exit(2)
		}
		// errStructuredReported means the command has already emitted a JSON
		// error envelope to stderr — print nothing more to keep structured
		// consumer stdout/stderr clean. errors.Is so a wrapped sentinel still
		// suppresses the "Error: ..." line.
		if !errors.Is(err, errStructuredReported) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
	}
}
