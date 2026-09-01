// The `ynh help` page.
//
// Grouped by intent rather than listed flat: the commands fall into five
// things a reader might be trying to do, and a newcomer needs two of them.

package main

import (
	"fmt"
	"io"
	"os"

	"github.com/eyelock/ynh/internal/config"
)

func printUsage() {
	printUsageTo(os.Stdout)
}

// printUsageTo writes the usage page to w. Split from printUsage so a test can
// assert every dispatched command is listed — the check that would have caught
// `migrate` and `quarantine` being dispatched for releases while the page never
// mentioned them. Mirrors the cmdXTo pattern used elsewhere in this package.
func printUsageTo(w io.Writer) {
	// Usage goes to a terminal or a test buffer; a write failure means stdout
	// is gone and there is nowhere left to report it.
	_, _ = fmt.Fprintf(w, `ynh - ynh harness template manager (%s)

Usage:
  ynh <command> [arguments]

Commands:

  Getting a harness
    install <source> [--path <subdir>] [--ref <ref>]  Install from a Git URL or local path
    uninstall <name>                       Remove an installed harness and its launcher
    update <name>                          Refresh a harness's cached Git repos
    fork <name> [--to <path>]              Copy an installed harness somewhere you can edit
    search <term>                          Search registries and sources
    registry <add|list|remove|update>      Manage harness registries
    sources <add|list|remove>              Manage local harness source directories

  Using one
    run <name> [flags] [prompt]            Launch a harness session
    agent run --task <text> [flags]        Run an autonomous agent loop
    ls                                     List installed harnesses
    info <name>                            Show a harness's resolved configuration
    installed <name>                       Show where a harness was installed from
    vendors                                List vendor adapters, and which CLIs are on PATH

  Authoring one
    focus <ls|add|remove|update>           Named prompts, each optionally applying a profile
    profile <ls|add|remove|hook|mcp|include>  Named configuration overlays
    hook <add|remove|export>               Commands the vendor runs at lifecycle events
    mcp <add|remove|update>                MCP servers the harness declares
    include <add|remove|update>            Artifacts pulled in from Git or a local path
    delegate <add|remove|update>           Harnesses this one hands off to
    sensors <ls|show|run>                  Observation surfaces a loop driver consumes

  Gating
    check <harness-id|path>                Run every declared sensor and gate on the result
    baseline <harness>                     Report what the ratchet currently forgives
    trust <ls|show|accept>                 What a harness will execute, and whether it changed

  Operating
    init                                   Show the ynh home path and setup instructions
    status                                 Show symlink installations across projects
    paths                                  Show resolved path roots
    doctor                                 Diagnose a broken setup: vendors, harnesses, symlinks, PATH
    prune                                  Clean orphaned symlinks and stale run dirs
    image <name> [flags]                   Build a Docker image with a harness baked in
    backend <add|list|remove>              Local model backends a vendor CLI can target
    migrate                                Migrate the ynh home to the current schema
    quarantine <list|restore|drop>         Harnesses a failed migration set aside
    schema <name> | --all                  Print a published JSON schema
    version                                Print version
    help                                   Show this help

Run 'ynh <command> --help' for one command's detail, and 'ynh schema --all'
for the published JSON shape of every command that emits structured output.

Run flags:
  -v <vendor>                  Override vendor (claude, codex, cursor, copilot), or "<backend>/<vendor>[/<model>]" to redirect at a local model backend (see: ynh backend)
  --focus <name>               Load a named focus (sets prompt and profile; implies non-interactive)
  --profile <name>             Apply a named profile overlay (with a prompt, implies non-interactive)
  --interactive                Override non-interactive default — stay in session after focus or prompt
  --instructions "<text>"      Inject per-invocation context after harness instructions
  --session-name <name>        Session label (recorded by ynh, not forwarded to vendor CLI)
  --resume                     Continue the previous session in this directory
  --resume=<id>                Continue one specific session by id
  --install                    Install symlinks for the vendor in current project
  --clean                      Remove symlinks for the vendor in current project
  All other flags are passed through to the vendor CLI.
  Use -- to separate vendor flags from the prompt.

Examples:
  ynh init
  ynh install github.com/myorg/david
  ynh install ./my-local-harness
  ynh install github.com/org/monorepo --path harnesses/david
  ynh install github.com/org/repo --ref v1.2.0
  ynh run david
  ynh run david "review this PR"
  ynh run david --focus code-review
  ynh run david --focus code-review --interactive
  ynh run david --profile thorough -- "audit this module"
  ynh run david --resume
  ynh run david --resume=6187c041-ee79-4646-b101-82f89c3e50ca
  ynh run david --instructions "PR #22 in eyelock/assistants"
  ynh run david --focus code-review --instructions "PR #22 in eyelock/assistants"
  ynh run david -v codex
  ynh run david --model opus -- "fix this bug"
  ynh run david -v codex -- "refactor auth"
  ynh run david -v cursor --install
  ynh run david -v cursor --clean
  ynh search "go development"
  ynh registry add github.com/org/registry
  ynh install david
  ynh install david@my-registry
`, config.Version)
}
