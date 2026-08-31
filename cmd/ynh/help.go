package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Per-command help.
//
// Before this existed, `--help` was unimplemented on all 31 commands. On 27 it
// was an error; on four the flag was ignored and **the command ran** — so
// `ynh prune --help` deleted run directories and `ynh run <name> --help`
// started a vendor session. A documentation lookup that performs an action is
// a defect of a different order from a missing help page, and it is the reason
// this is handled centrally rather than command by command.
//
// The interception happens once, in main, before dispatch. Adding it to each
// command would leave 31 opportunities to forget, and the next command added
// would start life with the same bug. Here, reaching a command's action with
// --help present is not something an author can get wrong, because control
// never arrives.

// commandHelp is the per-command help text, keyed by the canonical command
// name. Aliases resolve through canonicalCommand first.
//
// Every command in main's dispatch switch must have an entry — asserted by
// TestEveryDispatchedCommandHasHelp, which is what stops this drifting the way
// printUsage did when `migrate` and `quarantine` were added without it.
var commandHelp = map[string]string{
	"init": `ynh init

Show the resolved ynh home path and setup instructions.

Creates ~/.ynh and its subdirectories if they do not exist.`,

	"install": `ynh install <source> [flags]

Install a harness from a Git URL, a registry id, or a local path.

Flags:
  --path <subdir>    Install from a subdirectory of the repository
  --ref <ref>        Pin to a tag, branch or commit

Examples:
  ynh install github.com/myorg/david
  ynh install ./my-local-harness
  ynh install github.com/org/monorepo --path harnesses/david
  ynh install github.com/org/repo --ref v1.2.0
  ynh install david@my-registry`,

	"uninstall": `ynh uninstall <name>

Remove an installed harness and its launcher.

The source tree is left in place; only ynh's record and launcher are removed.`,

	"update": `ynh update <name>

Refresh the cached Git repositories a harness resolves from.`,

	"run": `ynh run <name> [flags] [prompt]

Launch a harness session with the vendor CLI.

Flags:
  -v <vendor>              Override vendor (claude, codex, cursor, copilot), or
                           "<backend>/<vendor>[/<model>]" to redirect at a local
                           model backend (see: ynh backend)
  --focus <name>           Load a named focus (sets prompt and profile; implies
                           non-interactive)
  --profile <name>         Apply a named profile overlay
  --interactive            Stay in session after a focus or prompt
  --instructions "<text>"  Inject per-invocation context after harness instructions
  --harness-file <path>    Load a harness from an explicit manifest path
  --session-name <name>    Session label, recorded by ynh and not forwarded
  --resume                 Continue the previous session in this directory
  --resume=<id>            Continue one specific session by id
  --install                Install symlinks for the vendor in this project
  --clean                  Remove symlinks for the vendor in this project

All other flags are passed through to the vendor CLI. Use -- to separate the
prompt from flags.

Examples:
  ynh run david
  ynh run david "review this PR"
  ynh run david --focus code-review
  ynh run david -v codex -- "refactor auth"`,

	"ls": `ynh ls [flags]

List installed harnesses.

Flags:
  --format text|json   Output format (default text)
  --check-updates      Check each harness's source for newer content`,

	"info": `ynh info <name> [flags]

Show a harness's resolved configuration: vendor, includes, delegates, hooks,
MCP servers, profiles, focuses and sensors.

Flags:
  --format text|json   Output format (default text)`,

	"installed": `ynh installed <name> [flags]

Show recorded install provenance for a harness — where it came from, at what
ref, and when.

Flags:
  --format text|json   Output format (default text)`,

	"schema": `ynh schema <name> [flags]
ynh schema --all --format json

Print an embedded JSON schema for a CLI command's response shape.

Flags:
  --all                Print every embedded schema as one manifest
  --format text|json   Output format`,

	"vendors": `ynh vendors [flags]

List supported vendor adapters and whether each vendor's CLI is on PATH.

Flags:
  --format text|json   Output format (default text)`,

	"sources": `ynh sources <add|list|remove> [args]

Manage local harness source directories.

  ynh sources add <path>      Add a local source directory
  ynh sources list            Show configured sources (supports --format json)
  ynh sources remove <name>   Remove a source`,

	"paths": `ynh paths [flags]

Show the resolved path roots ynh uses: home, cache, run and bin.

Flags:
  --format text|json   Output format (default text)`,

	"status": `ynh status [flags]

Show symlink installations across projects.

Flags:
  --format text|json   Output format (default text)`,

	"search": `ynh search <term> [flags]

Search configured registries and sources for a harness.

Flags:
  --format text|json   Output format (default text)`,

	"registry": `ynh registry <add|list|remove|update> [args]

Manage harness registries.

  ynh registry add <url>      Add a registry
  ynh registry list           Show configured registries (supports --format json)
  ynh registry remove <url>   Remove a registry
  ynh registry update         Refresh all cached registries`,

	"backend": `ynh backend <add|list|remove> [args]

Manage local model backends that a vendor CLI can be redirected at.

  ynh backend add <name> <vendor> --base-url <url> [--auth-token <token>] [--type <type>]
  ynh backend list                      Show configured backends (supports --format json)
  ynh backend remove <name> [<vendor>]  Remove a backend, or one vendor's connection within it`,

	"delegate": `ynh delegate <add|remove|update> <harness> <url> [flags]

Manage a harness's Git delegates.`,

	"fork": `ynh fork <name> [flags]

Fork an installed harness into a local directory you can edit.

Flags:
  --to <path>          Destination directory
  --format text|json   Output format (default text)`,

	"include": `ynh include <add|remove|update> <harness> <url> [flags]

Manage a harness's Git includes.`,

	"focus": `ynh focus <add|remove|update> [args]

Edit a harness's named focuses. There is no list verb — use
"ynh info <name>" to see the focuses a harness declares.

  ynh focus add <harness> <name> <prompt> [--profile <name>]
  ynh focus remove <harness> <name>
  ynh focus update <harness> <name> [--prompt <text>] [--profile <name>] [--clear-profile]`,

	"profile": `ynh profile <add|remove|hook|mcp|include> [args]

Edit a harness's named profiles. There is no list verb — use
"ynh info <name>" to see the profiles a harness declares.

  ynh profile add <harness> <name>
  ynh profile remove <harness> <name>
  ynh profile hook <add|remove> <harness> <profile> ...
  ynh profile mcp <add|remove|update> <harness> <profile> <name> [flags]
  ynh profile include <add|remove|update> <harness> <profile> <url> [flags]`,

	"hook": `ynh hook <add|remove|export> [args]

Manage a harness's top-level hooks.

  ynh hook add <harness> <event> <command> [--matcher <pattern>]
  ynh hook remove <harness> <event> <index>
  ynh hook export <harness> --target <settings|local> [--dry-run]`,

	"doctor": `ynh doctor

Check Claude hook wiring in the current project.

This checks one thing: whether Claude's settings.json hook configuration is
correctly wired. It is not a general setup diagnostic — for those, see
"ynd validate", "ynd preview", "ynh info", "ynh status" and "ynh paths".`,

	"mcp": `ynh mcp <add|remove|update> <harness> <name> [flags]

Manage a harness's top-level MCP servers.`,

	"sensors": `ynh sensors <ls|show|run> [args]

Inspect the sensors a harness declares.

  ynh sensors ls <harness>          List declared sensors (supports --format json)
  ynh sensors show <harness> <name> Resolve one sensor declaration
  ynh sensors run <harness> <name>  Run a sensor and emit a JSON result`,

	"check": `ynh check <harness-id|path> [flags]

Run every declared sensor and gate on the result.

Takes an installed id ("local/demo") or a path to a harness directory
(".", "./my-harness", an absolute path). A path needs no prior install, which
matters while you are still authoring the harness.

Flags:
  --only a,b            Run only the named sensors
  --cwd <dir>           Run against a directory other than the current one
  --update-baseline     Accept current failures so only new ones gate
  --no-baseline         Ignore the baseline for this run
  --calibrate           Run each sensor against its reference fixture instead
  --sensor-overlay <j>  Substitute sensor declarations from a JSON object
  --format text|json    Output format (default text)

Exit codes:
  0  every sensor behaved as declared
  1  a blocking sensor failed
  2  the gate could not run`,

	"baseline": `ynh baseline <harness> [flags]

Report what a harness's ratchet currently forgives.

Flags:
  --explain            Re-run the sensors to resolve fingerprints into findings
  --cwd <dir>          Read the baseline in another directory
  --format text|json   Output format (default text)`,

	"agent": `ynh agent run [flags]

Run an autonomous agent loop session.

Flags:
  --harness <name>            Harness to run under
  --task <text|@file>         The task to work on
  --focus <name>              Load a named focus instead of a task
  --profile <name>            Apply a named profile overlay
  --backend <name>            Model backend to use
  --model <name>              Model override
  --worktree <path>           Run against a specific worktree
  --max-turns <n>             Stop after n turns
  --max-wall <duration>       Stop after a wall-clock budget
  --max-tokens <n>            Stop after a token budget
  --max-plan-iterations <n>   Cap plan revision rounds
  --no-plan                   Skip the planning phase
  --interactive               Stay in session
  --sandbox                   Run inside the harness's image
  --convergence-sensor <name> Sensor that decides the run is done
  --sensor-overlay <json>     Substitute sensor declarations
  --emit-jsonl <path>         Write the trajectory as NDJSON
  --auto-commit               Commit the result (opt-in; off by default)
  --resume <id>               Continue a previous run
  --format text|json          Output format (default text)`,

	"image": `ynh image <name> [flags]

Build a Docker image with a harness baked in.`,

	"prune": `ynh prune

Clean orphaned symlink installations and stale run directories.

This deletes. Run "ynh status" first to see what is currently installed.`,

	"migrate": `ynh migrate

Migrate the ynh home directory to the current schema version.

Most commands migrate automatically on first use; this runs it explicitly.`,

	"quarantine": `ynh quarantine <list|restore|drop> [args]

Manage harnesses quarantined by a failed migration.

  ynh quarantine list             Show quarantined harnesses
  ynh quarantine restore <name>   Restore one from quarantine
  ynh quarantine drop <name>      Delete one permanently`,

	"version": `ynh version [flags]

Print the ynh version.

Flags:
  --format text|json   Output format (default text)`,

	"help": `ynh help

Show the command list. For one command's detail, use "ynh <command> --help".`,
}

// commandAliases maps an alias to its canonical command name.
var commandAliases = map[string]string{
	"remove": "uninstall",
	"list":   "ls",
}

// canonicalCommand resolves an alias to the name commandHelp is keyed by.
func canonicalCommand(name string) string {
	if c, ok := commandAliases[name]; ok {
		return c
	}
	return name
}

// wantsHelp reports whether args request help for the command rather than
// asking it to do anything.
//
// Only args before a `--` count. `ynh run x -- --help` is a prompt that
// happens to read "--help", not a request for ynh's help — the separator is
// the user saying explicitly that what follows is not for ynh.
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--help" || a == "-h" {
			return true
		}
	}
	return false
}

// printCommandHelp writes one command's help. It reports whether the command
// was known, so main can fall back to the full usage page rather than printing
// nothing for a typo.
func printCommandHelp(w io.Writer, name string) bool {
	text, ok := commandHelp[canonicalCommand(name)]
	if !ok {
		return false
	}
	// A write failure here is a closed stdout, not something the caller can
	// act on — but errcheck is strict and silently dropping it is the habit
	// this project does not want.
	if _, err := fmt.Fprintln(w, text); err != nil {
		return false
	}
	return true
}

// helpTopics lists the commands with help, for tests and diagnostics.
func helpTopics() []string {
	out := make([]string, 0, len(commandHelp))
	for k := range commandHelp {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// helpForUnknown formats the error for a command nobody recognises.
func helpForUnknown(name string) string {
	return fmt.Sprintf("unknown command: %s\n\nRun 'ynh help' for the command list, "+
		"or 'ynh <command> --help' for one command's detail.", strings.TrimSpace(name))
}
