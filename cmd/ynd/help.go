package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Per-command help for ynd.
//
// `ynh` gained this in #260; `ynd` did not, so 11 of its 13 commands answered
// `--help` with the entire global usage page. `migrate` had real help and
// `create` printed a usage line as an error. A reader looking for one
// command's flags had to read the whole page and hope (#321).
//
// Unlike ynh, this was never a data-loss bug: every ynd command that writes or
// deletes returns errHelp from its argument parser, before any path is
// resolved. The interception still happens once, in main, before dispatch, for
// the reason #260 gives: doing it per command leaves thirteen opportunities to
// forget, and the next command added starts life with the gap.

// commandHelp is the per-command help text, keyed by the canonical command
// name. Aliases resolve through canonicalCommand first.
//
// Every command in main's dispatch switch must have an entry, asserted by
// TestEveryDispatchedCommandHasHelp.
var commandHelp = map[string]string{
	"create": `ynd create <type> <name> [flags]

Scaffold a new artifact.

Types:
  skill <name>       skills/<name>/SKILL.md
  agent <name>       agents/<name>.md with frontmatter
  harness <name>     a full harness directory structure
  rule <name>        rules/<name>.md
  command <name>     commands/<name>.md

Flags:
  --description <text>   Set the artifact's description
  -v, --vendor <vendor>  Target vendor layout (falls back to $YNH_VENDOR)

Scaffolded skills carry only name and description, which is deliberate:
the other spec fields cause Claude Code's plugin loader to demote a skill.`,

	"lint": `ynd lint [path...]

Lint markdown, shell blocks and config files.

Every positional is a root, not just the first. Reports trailing whitespace,
blank runs, missing final newlines, frontmatter problems, shell syntax, and
manifest structure.

Flags:
  -n                     Report without rewriting anything
  --harness <source>     Harness to resolve against (falls back to $YNH_HARNESS)`,

	"validate": `ynd validate [path]

Validate harness structure and artifacts.

Checks required files, frontmatter fields, directory layout, and JSON Schema
conformance: plugin.json against plugin.schema.json, and any
.ynh-plugin/marketplace.json against marketplace.schema.json.

Flags:
  --harness <source>     Harness to resolve against (falls back to $YNH_HARNESS)`,

	"fmt": `ynd fmt [path...]

Format markdown files in place.

Normalises whitespace and trailing newlines. Run it before committing
artifacts; lint reports what fmt fixes.`,

	"compress": `ynd compress [path] [flags]

Compress prompts using LLM-powered SudoLang techniques.

Writes a backup to ~/.ynd/backups before changing anything, mirroring the
absolute path. Override the location with $YND_BACKUP_DIR.

Flags:
  -y, --yes              Skip the confirmation prompt (also $YNH_YES, or CI)
  -v, --vendor <vendor>  Target vendor (falls back to $YNH_VENDOR)
  --pick                 Choose which files to compress
  --list-backups         Show backups for the given path
  --restore              Restore a file from its backup`,

	"inspect": `ynd inspect [flags]

Interactive codebase walkthrough that generates or updates skills and agents.

Writes to .<vendor>/ by default.

Flags:
  -o, --output-dir <dir> Write here instead of .<vendor>/
  -v, --vendor <vendor>  Target vendor (falls back to $YNH_VENDOR)
  -y, --yes              Skip confirmation prompts (also $YNH_YES, or CI)`,

	"export": `ynd export <source> [flags]

Export a harness as vendor-native plugin directories.

Flags:
  -o, --output <dir>     Destination directory
  -v, --vendor <vendor>  Target vendor (falls back to $YNH_VENDOR)
  --profile <name>       Apply a named profile (falls back to $YNH_PROFILE)
  --focus <name>         Apply a named focus
  --merged               Write one merged tree rather than per-vendor trees
  --path <subdir>        Export only this subdirectory of the source
  --harness <source>     Harness to resolve against (falls back to $YNH_HARNESS)
  --clean                Remove the output directory first
  -y, --yes              Skip the confirmation prompt (also $YNH_YES, or CI)

--clean refuses outright to delete the filesystem root, $HOME, the current
directory, any ancestor of it, or a git working copy. That refusal ignores
-y: skipping a question is not a licence to delete a home directory.`,

	"compose": `ynd compose <source> [flags]

Show the resolved composition before vendor assembly.

Reports what the harness resolves to once includes, delegates and profiles
are applied, with each artifact attributed to its source.

Flags:
  --format <text|json>   Output format
  --profile <name>       Apply a named profile (falls back to $YNH_PROFILE)
  --harness <source>     Harness to resolve against (falls back to $YNH_HARNESS)`,

	"preview": `ynd preview <source> [flags]

Show assembled vendor output without installing it.

This is what the vendor actually receives. Use it to confirm a change reaches
the vendor in the shape you expect.

Flags:
  -o, --output <dir>     Write the preview here instead of a temp directory
  -v, --vendor <vendor>  Target vendor (falls back to $YNH_VENDOR)
  --profile <name>       Apply a named profile (falls back to $YNH_PROFILE)
  --focus <name>         Apply a named focus
  --harness <source>     Harness to resolve against (falls back to $YNH_HARNESS)`,

	"diff": `ynd diff <source> [vendor...] [flags]

Compare assembled output across vendors.

With no vendors named, compares every vendor the harness supports. Naming two
compares just those.

Flags:
  -v, --vendor <vendor>  Target vendor (falls back to $YNH_VENDOR)
  --profile <name>       Apply a named profile (falls back to $YNH_PROFILE)
  --focus <name>         Apply a named focus
  --harness <source>     Harness to resolve against (falls back to $YNH_HARNESS)`,

	"marketplace": `ynd marketplace build [flags]

Build a vendor-native marketplace from marketplace.json.

Flags:
  -o, --output <dir>     Destination directory
  -v, --vendor <vendor>  Target vendor (falls back to $YNH_VENDOR)
  --clean                Remove the output directory first
  -y, --yes              Skip the confirmation prompt (also $YNH_YES, or CI)

--clean carries the same hard refusals as ynd export --clean.`,

	"migrate": `ynd migrate <path>

Convert .harness.json to .ynh-plugin/plugin.json in place.

Extracts install-time provenance into .ynh-plugin/installed.json, writes
plugin.json without that field, and removes .harness.json. Safe to run more
than once: it does nothing once the new format exists.

Given a directory, migrates every harness beneath it.`,

	"validate-output": `ynd validate-output --schema <name> [< file.json]

Validate JSON on stdin against a named CLI schema.

Reads the schemas ynh embeds, so this checks a response against the contract
the binary actually carries rather than a copy on disk.

  ynh ls --format json | ynd validate-output --schema list

Flags:
  --schema <name>        Schema to validate against (see: ynh schema --all)`,

	"version": `ynd version

Print the ynd version.

Use --format json for the machine-readable form; it is the canonical
wire-contract probe for consumers that gate on capabilities.`,

	"help": `ynd help

Show the global usage page.

For one command's detail, run 'ynd <command> --help'.`,
}

// canonicalCommand maps an alias to the name commandHelp is keyed by.
func canonicalCommand(name string) string {
	switch name {
	case "--version", "-v":
		return "version"
	case "--help", "-h":
		return "help"
	}
	return name
}

// wantsHelp reports whether args ask for help.
//
// It stops at `--`: everything after that belongs to the thing being invoked,
// so `ynd fmt -- --help` formats a file called --help rather than printing a
// help page.
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
	if _, err := fmt.Fprintln(w, text); err != nil {
		return false
	}
	return true
}

// helpTopics returns the commands with help, sorted. Used by tests.
func helpTopics() []string {
	topics := make([]string, 0, len(commandHelp))
	for k := range commandHelp {
		topics = append(topics, k)
	}
	sort.Strings(topics)
	return topics
}

// firstLine returns a help text's first line, which must name its command.
func firstLine(text string) string {
	if i := strings.Index(text, "\n"); i >= 0 {
		return text[:i]
	}
	return text
}
