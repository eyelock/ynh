package main

import (
	"fmt"

	"github.com/eyelock/ynh/internal/config"
)

// printUsage renders the help text for `ynh` and `ynh --help`. Kept in its
// own file so the dispatcher in main.go stays focused on routing.
func printUsage() {
	fmt.Printf(`ynh - ynh harness template manager (%s)

Usage:
  ynh <command> [arguments]

Commands:
  init                         Show ynh home path and setup instructions
  install <source> [--path <subdir>] [--ref <ref>]  Install a harness from Git URL or local path
  uninstall <name>             Remove an installed harness and its launcher
  update <name>                Refresh cached Git repos for a harness
  run <name> [flags] [prompt]  Launch a harness session (add --agent --task <text> for an autonomous loop)
  ls                           List installed harnesses (supports --format json)
  info <name> [--installed]    Show detailed harness information (supports --format json)
  schema <name>                Print the embedded JSON schema for a CLI command
  schema --all --format json   Print every embedded schema as one manifest
  vendors                      List supported vendor adapters (supports --format json)
  search <term>                Search registries and sources (supports --format json)
  sources add <path>           Add a local harness source directory
  sources list                 Show configured sources (supports --format json)
  sources remove <name>        Remove a source
  fork <name> [-o <path>]      Fork an installed harness to a local directory
  delegate add <harness> <url>      Add a Git delegate to a harness
  delegate remove <harness> <url>   Remove a Git delegate from a harness
  delegate update <harness> <url>   Update a Git delegate's options
  include add <harness> <url> [--profile <p>]     Add a Git include (top-level or profile-scoped)
  include remove <harness> <url> [--profile <p>]  Remove a Git include
  include update <harness> <url> [--profile <p>]  Update a Git include's options
  focus add <harness> <name> <prompt> [--profile <name>]    Add a named focus
  focus remove <harness> <name>                             Remove a named focus
  focus update <harness> <name> [flags]                     Update a focus (--clear profile to drop)
  profile add <harness> <name>                              Add a named profile
  profile remove <harness> <name>                           Remove a named profile
  hook add <harness> <event> <command> [--profile <p>] [--matcher <pattern>]  Add a hook
  hook remove <harness> <event> <index> [--profile <p>]                       Remove a hook by index
  mcp add <harness> <name> [--profile <p>] [flags]          Add an MCP server
  mcp remove <harness> <name> [--profile <p>]               Remove an MCP server
  mcp update <harness> <name> [--profile <p>] [flags]       Update an MCP server (--clear field repeatable)
  sensors ls <harness>           List declared sensors (supports --format json)
  sensors show <harness> <name>  Resolve a sensor declaration (supports --format text|json)
  sensors run <harness> <name>   Run a sensor and emit a JSON result
  registry add <url>           Add a harness registry
  registry list                Show configured registries (supports --format json)
  registry remove <url>        Remove a registry
  registry update              Refresh all cached registries
  image <name> [flags]         Build a Docker image with a harness baked in
  paths                        Show resolved path roots (supports --format json)
  status [--prune]             Show symlink installations (--prune removes orphans)
  migrate [flags]              Run schema migration of ~/.ynh home
  quarantine <list|restore|drop>  Manage entries that migration could not convert
  version                      Print version
  help                         Show this help

Run flags:
  -v <vendor>                  Override vendor (claude, codex, cursor)
  --focus <name>               Load a named focus (sets prompt and profile; implies non-interactive)
  --profile <name>             Apply a named profile overlay (with a prompt, implies non-interactive)
  --interactive                Override non-interactive default — stay in session after focus or prompt
  --instructions "<text>"      Inject per-invocation context after harness instructions
  --session-name <name>        Session label (recorded by ynh, not forwarded to vendor CLI)
  --install                    Install symlinks for the vendor in current project
  --clean                      Remove symlinks for the vendor in current project
  --agent --task <text> [...]  Run an autonomous agent loop instead of launching the vendor CLI
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
  ynh run david --instructions "PR #22 in eyelock/assistants"
  ynh run david -v codex
  ynh run david --model opus -- "fix this bug"
  ynh run david -v cursor --install
  ynh run david --agent --task "ship feature X"
  ynh info david --installed
  ynh status --prune
  ynh hook add david after_tool "echo done" --profile staging
  ynh mcp add david notes --command "npx -y notes-mcp" --profile staging
  ynh include add david github.com/eyelock/skill --profile thorough
  ynh search "go development"
  ynh registry add github.com/org/registry
  ynh install david
  ynh install david@my-registry
`, config.Version)
}
