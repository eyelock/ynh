package main

import (
	"fmt"
	"io"
	"strings"
)

// Per-subcommand help for the editing groups: include, mcp, delegate, profile,
// hook, focus, sources, registry and backend.
//
// `ynh include update --help` used to print the group's one-line summary and no
// flags, so `--path`, `--pick`, `--ref` and `--from-path` were discoverable only
// from docs/reference.md (#587).
//
// The CLI is hand-rolled, so there is no flag definition for help to be
// generated from. Rewriting nine groups of parsers onto a shared table would
// be a far larger and riskier change than the bug. Instead each subcommand has
// one entry here, naming the parser functions that implement it, and
// TestSubcommandHelpMatchesParsers reads those functions' source and fails when
// a flag the parser accepts is missing from the entry, or the entry lists a
// flag the parser does not accept. Help and behaviour cannot drift without a
// red build, which is the guarantee the issue asks for.

// argHelp describes one positional argument.
type argHelp struct{ Name, Desc string }

// flagHelp describes one flag. Flag is the display form, e.g. "--path <subdir>"
// or "-v, --vendor <vendor>"; the names are read back out of it by the test.
type flagHelp struct{ Flag, Desc string }

// subHelp is one subcommand's help.
type subHelp struct {
	Usage   string // first line; must start with "ynh <key>"
	Summary string
	Args    []argHelp
	Flags   []flagHelp
	Notes   string
	// Parsers names the functions in cmd/ynh whose `case "--flag"` clauses
	// implement this subcommand. Used only by the drift test.
	Parsers []string
}

var (
	harnessArg = argHelp{"<harness>", "Installed harness id, or path to a harness directory"}
	profileArg = argHelp{"<profile>", "Profile name"}
	urlArg     = argHelp{"<url>", "Git URL of the source"}

	formatFlag = flagHelp{"--format text|json", "Output format (default text)"}
	pathFlag   = flagHelp{"--path <subdir>", "Subdirectory of the repository holding the content"}
	refFlag    = flagHelp{"--ref <ref>", "Pin to a tag, branch or commit"}
	hooksFlag  = flagHelp{"--hooks", "Let the included harness's hooks run in the session (off by default)"}

	mcpAddFlags = []flagHelp{
		{"--command <cmd>", "Command to launch a stdio server"},
		{"--url <url>", "URL of a remote server"},
		{"--type <type>", "Transport: stdio, streamable-http or sse"},
		{"--arg <value>", "Argument for the command (repeatable)"},
		{"--env K=V", "Environment variable for the server (repeatable)"},
		{"--cwd <dir>", "Working directory for the server"},
		{"--header K=V", "HTTP header for a remote server (repeatable)"},
	}
	mcpUpdateFlags = append(append([]flagHelp{}, mcpAddFlags...),
		flagHelp{"--clear-args", "Remove all args (with --arg, replace them)"},
		flagHelp{"--clear-env", "Remove all env vars (with --env, replace them)"},
		flagHelp{"--clear-headers", "Remove all headers (with --header, replace them)"},
	)

	includeUpdateNote = "At least one of the flags must be given. Flags not given are left as they are."
)

// subcommandHelp is keyed "<group> <sub>" or "<group> <sub> <sub>", with the
// canonical subcommand names (ls, not list; except where list is canonical).
var subcommandHelp = map[string]subHelp{
	"include add": {
		Usage:   "ynh include add <harness> <url|path> [flags]",
		Summary: "Add an include to a harness: a Git URL, or a path to a directory on disk (recorded as a local include). With --pick, the picks are checked against the content before anything is written.",
		Args:    []argHelp{harnessArg, {"<url|path>", "Git URL of the source, or a filesystem path (absolute, ./, ../, ~/, or an existing directory)"}},
		Flags: []flagHelp{
			pathFlag,
			{"--pick <items>", "Comma-separated artifacts to take, e.g. skills/commit,agents/reviewer.md, or a top-level skill folder by name"},
			refFlag,
			{"--as <alias>", "Namespace for the included harness's focuses and profiles (alias:name)"},
			{"--git", "Treat a path as a Git repository on disk instead of a local include"},
			hooksFlag,
			{"--replace", "Replace an existing include of the same url and path"},
		},
		Parsers: []string{"cmdIncludeAdd"},
	},
	"include remove": {
		Usage:   "ynh include remove <harness> <url> [flags]",
		Summary: "Remove a Git include from a harness.",
		Args:    []argHelp{harnessArg, urlArg},
		Flags:   []flagHelp{{"--path <subdir>", "Remove only the include with this path (needed when a url is included more than once)"}},
		Parsers: []string{"cmdIncludeRemove"},
	},
	"include update": {
		Usage:   "ynh include update <harness> <url> [flags]",
		Summary: "Change the path, picks or ref of an existing include without editing the manifest by hand.",
		Args:    []argHelp{harnessArg, urlArg},
		Flags: []flagHelp{
			{"--from-path <subdir>", "Select which include to change, when the url appears with several paths"},
			{"--path <subdir>", "Set the include's path"},
			{"--pick <items>", "Replace the picks with this comma-separated list (empty clears them)"},
			{"--ref <ref>", "Set the pinned tag, branch or commit"},
		},
		Notes:   includeUpdateNote + "\n\nExample:\n  ynh include update my-harness https://github.com/eyelock/ynm --path integrations --pick skills/ynm-memory",
		Parsers: []string{"cmdIncludeUpdate"},
	},

	"mcp add": {
		Usage:   "ynh mcp add <harness> <name> [flags]",
		Summary: "Declare an MCP server on a harness. Give --command for a stdio server or --url for a remote one.",
		Args:    []argHelp{harnessArg, {"<name>", "Name of the MCP server"}},
		Flags:   mcpAddFlags,
		Parsers: []string{"cmdMCPAdd"},
	},
	"mcp remove": {
		Usage:   "ynh mcp remove <harness> <name>",
		Summary: "Remove an MCP server from a harness.",
		Args:    []argHelp{harnessArg, {"<name>", "Name of the MCP server"}},
		Parsers: []string{"cmdMCPRemove"},
	},
	"mcp update": {
		Usage:   "ynh mcp update <harness> <name> [flags]",
		Summary: "Change fields of an existing MCP server. Fields not given are left as they are.",
		Args:    []argHelp{harnessArg, {"<name>", "Name of the MCP server"}},
		Flags:   mcpUpdateFlags,
		Parsers: []string{"cmdMCPUpdate"},
	},

	"delegate add": {
		Usage:   "ynh delegate add <harness> <url> [flags]",
		Summary: "Add a Git delegate: another harness this one hands off to.",
		Args:    []argHelp{harnessArg, urlArg},
		Flags: []flagHelp{
			refFlag,
			{"--path <subdir>", "Subdirectory of the repository holding the delegate harness"},
			{"--profile <name>", "Run the delegate under one of its own profiles"},
			{"--focus <name>", "Run the delegate under one of its own focuses (its prompt becomes standing instructions)"},
		},
		Notes:   "--profile and --focus are mutually exclusive: a focus already carries its profile. The name is checked against the delegate when it can be read, as ynd validate checks it.",
		Parsers: []string{"cmdDelegateAdd"},
	},
	"delegate remove": {
		Usage:   "ynh delegate remove <harness> <url> [flags]",
		Summary: "Remove a delegate from a harness.",
		Args:    []argHelp{harnessArg, urlArg},
		Flags:   []flagHelp{{"--path <subdir>", "Remove only the delegate with this path"}},
		Parsers: []string{"cmdDelegateRemove"},
	},
	"delegate update": {
		Usage:   "ynh delegate update <harness> <url> [flags]",
		Summary: "Change the path or ref of an existing delegate.",
		Args:    []argHelp{harnessArg, urlArg},
		Flags: []flagHelp{
			{"--from-path <subdir>", "Select which delegate to change, when the url appears with several paths"},
			{"--path <subdir>", "Set the delegate's path"},
			{"--ref <ref>", "Set the pinned tag, branch or commit"},
		},
		Notes:   "At least one of --path or --ref must be given.",
		Parsers: []string{"cmdDelegateUpdate"},
	},

	"profile ls": {
		Usage:   "ynh profile ls <harness> [flags]",
		Summary: "List a harness's profiles.",
		Args:    []argHelp{harnessArg},
		Flags:   []flagHelp{formatFlag},
		Parsers: []string{"lsArgs"},
	},
	"profile add": {
		Usage:   "ynh profile add <harness> <name>",
		Summary: "Add an empty profile to a harness.",
		Args:    []argHelp{harnessArg, {"<name>", "Name of the new profile"}},
		Parsers: []string{"cmdProfileAdd"},
	},
	"profile remove": {
		Usage:   "ynh profile remove <harness> <name>",
		Summary: "Remove a profile from a harness.",
		Args:    []argHelp{harnessArg, {"<name>", "Profile name"}},
		Parsers: []string{"cmdProfileRemove"},
	},
	"profile hook add": {
		Usage:   "ynh profile hook add <harness> <profile> <event> <command> [flags]",
		Summary: "Add a hook to a profile.",
		Args:    []argHelp{harnessArg, profileArg, {"<event>", "Lifecycle event"}, {"<command>", "Command the vendor runs"}},
		Flags: []flagHelp{
			{"--matcher <pattern>", "Only fire for tools matching this pattern"},
			{"--timeout <seconds>", "Most seconds the vendor lets the hook run, 1 to 3600; a vendor without a timeout field warns and uses its default"},
		},
		Parsers: []string{"cmdProfileHookAdd"},
	},
	"profile hook remove": {
		Usage:   "ynh profile hook remove <harness> <profile> <event> <index>",
		Summary: "Remove a hook from a profile.",
		Args:    []argHelp{harnessArg, profileArg, {"<event>", "Lifecycle event"}, {"<index>", "Position of the hook within the event, from 0"}},
		Parsers: []string{"cmdProfileHookRemove"},
	},
	"profile mcp add": {
		Usage:   "ynh profile mcp add <harness> <profile> <name> [flags]",
		Summary: "Declare an MCP server in a profile. Give --command, --url, or --null.",
		Args:    []argHelp{harnessArg, profileArg, {"<name>", "Name of the MCP server"}},
		Flags:   append(append([]flagHelp{}, mcpAddFlags...), flagHelp{"--null", "Disable this server while the profile is active"}),
		Parsers: []string{"cmdProfileMCPAdd"},
	},
	"profile mcp remove": {
		Usage:   "ynh profile mcp remove <harness> <profile> <name>",
		Summary: "Remove an MCP server from a profile.",
		Args:    []argHelp{harnessArg, profileArg, {"<name>", "Name of the MCP server"}},
		Parsers: []string{"cmdProfileMCPRemove"},
	},
	"profile mcp update": {
		Usage:   "ynh profile mcp update <harness> <profile> <name> [flags]",
		Summary: "Change fields of an MCP server in a profile. Fields not given are left as they are.",
		Args:    []argHelp{harnessArg, profileArg, {"<name>", "Name of the MCP server"}},
		Flags:   mcpUpdateFlags,
		Parsers: []string{"cmdProfileMCPUpdate"},
	},
	"profile include add": {
		Usage:   "ynh profile include add <harness> <profile> <url|path> [flags]",
		Summary: "Add an include to a profile: a Git URL, or a path to a directory on disk (recorded as a local include).",
		Args:    []argHelp{harnessArg, profileArg, {"<url|path>", "Git URL of the source, or a filesystem path (absolute, ./, ../, ~/, or an existing directory)"}},
		Flags:   []flagHelp{pathFlag, refFlag, {"--git", "Treat a path as a Git repository on disk instead of a local include"}, hooksFlag, {"--replace", "Replace an existing include of the same url and path"}},
		Parsers: []string{"cmdProfileIncludeAdd"},
	},
	"profile include remove": {
		Usage:   "ynh profile include remove <harness> <profile> <url> [flags]",
		Summary: "Remove a Git include from a profile.",
		Args:    []argHelp{harnessArg, profileArg, urlArg},
		Flags:   []flagHelp{{"--path <subdir>", "Remove only the include with this path"}},
		Parsers: []string{"cmdProfileIncludeRemove"},
	},
	"profile include update": {
		Usage:   "ynh profile include update <harness> <profile> <url> [flags]",
		Summary: "Change the path or ref of an include in a profile.",
		Args:    []argHelp{harnessArg, profileArg, urlArg},
		Flags: []flagHelp{
			{"--from-path <subdir>", "Select which include to change, when the url appears with several paths"},
			{"--path <subdir>", "Set the include's path"},
			{"--ref <ref>", "Set the pinned tag, branch or commit"},
		},
		Notes:   "At least one of --path or --ref must be given.",
		Parsers: []string{"cmdProfileIncludeUpdate"},
	},

	"hook add": {
		Usage:   "ynh hook add <harness> <event> <command> [flags]",
		Summary: "Add a hook the vendor runs at a lifecycle event.",
		Args:    []argHelp{harnessArg, {"<event>", "Lifecycle event"}, {"<command>", "Command the vendor runs"}},
		Flags: []flagHelp{
			{"--matcher <pattern>", "Only fire for tools matching this pattern"},
			{"--timeout <seconds>", "Most seconds the vendor lets the hook run, 1 to 3600; a vendor without a timeout field warns and uses its default"},
		},
		Parsers: []string{"cmdHookAdd"},
	},
	"hook remove": {
		Usage:   "ynh hook remove <harness> <event> <index>",
		Summary: "Remove a hook from a harness.",
		Args:    []argHelp{harnessArg, {"<event>", "Lifecycle event"}, {"<index>", "Position of the hook within the event, from 0"}},
		Parsers: []string{"cmdHookRemove"},
	},
	"hook export": {
		Usage:   "ynh hook export <harness> --target <settings|local> [flags]",
		Summary: "Merge a harness's hooks into Claude's project settings file, for a plain claude session.",
		Args:    []argHelp{harnessArg},
		Flags: []flagHelp{
			{"--target <settings|local>", "settings: .claude/settings.json (committed); local: .claude/settings.local.json (personal)"},
			{"-v, --vendor <vendor>", "Vendor to export for (only claude is meaningful)"},
			{"--dry-run", "Print the resulting file and write nothing"},
		},
		Parsers: []string{"cmdHookExport"},
	},

	"focus ls": {
		Usage:   "ynh focus ls <harness> [flags]",
		Summary: "List a harness's focuses.",
		Args:    []argHelp{harnessArg},
		Flags:   []flagHelp{formatFlag},
		Parsers: []string{"lsArgs"},
	},
	"focus add": {
		Usage:   "ynh focus add <harness> <name> <prompt> [flags]",
		Summary: "Add a named focus: a prompt, optionally applying a profile.",
		Args:    []argHelp{harnessArg, {"<name>", "Name of the focus"}, {"<prompt>", "The prompt the focus sends"}},
		Flags:   []flagHelp{{"--profile <name>", "Apply this profile when the focus runs"}},
		Parsers: []string{"cmdFocusAdd"},
	},
	"focus remove": {
		Usage:   "ynh focus remove <harness> <name>",
		Summary: "Remove a focus from a harness.",
		Args:    []argHelp{harnessArg, {"<name>", "Name of the focus"}},
		Parsers: []string{"cmdFocusRemove"},
	},
	"focus update": {
		Usage:   "ynh focus update <harness> <name> [flags]",
		Summary: "Change a focus's prompt or profile.",
		Args:    []argHelp{harnessArg, {"<name>", "Name of the focus"}},
		Flags: []flagHelp{
			{"--prompt <text>", "Set the prompt"},
			{"--profile <name>", "Set the profile to apply"},
			{"--clear-profile", "Stop applying a profile (not with --profile)"},
		},
		Notes:   "At least one of the flags must be given.",
		Parsers: []string{"cmdFocusUpdate"},
	},

	"sources add": {
		Usage:   "ynh sources add <path> [flags]",
		Summary: "Add a local directory of harnesses as a source.",
		Args:    []argHelp{{"<path>", "Directory to add; stored as an absolute path"}},
		Flags: []flagHelp{
			{"--name <name>", "Name for the source (default: the directory name)"},
			{"--description <text>", "One-line description shown in listings"},
		},
		Parsers: []string{"cmdSourcesAdd"},
	},
	"sources list": {
		Usage:   "ynh sources list [flags]",
		Summary: "Show configured sources.",
		Flags:   []flagHelp{formatFlag},
		Parsers: []string{"cmdSourcesListTo"},
	},
	"sources remove": {
		Usage:   "ynh sources remove <name>",
		Summary: "Remove a source.",
		Args:    []argHelp{{"<name>", "Name of the source"}},
		Parsers: []string{"cmdSourcesRemove"},
	},

	"registry add": {
		Usage:   "ynh registry add <url>",
		Summary: "Add a harness registry.",
		Args:    []argHelp{{"<url>", "Git URL of the registry"}},
		Parsers: []string{"cmdRegistryAdd"},
	},
	"registry list": {
		Usage:   "ynh registry list [flags]",
		Summary: "Show configured registries.",
		Flags:   []flagHelp{formatFlag},
		Parsers: []string{"cmdRegistryList"},
	},
	"registry remove": {
		Usage:   "ynh registry remove <url>",
		Summary: "Remove a registry.",
		Args:    []argHelp{{"<url>", "Git URL of the registry"}},
		Parsers: []string{"cmdRegistryRemove"},
	},
	"registry update": {
		Usage:   "ynh registry update",
		Summary: "Refresh every cached registry.",
		Parsers: []string{"cmdRegistryUpdate"},
	},

	"backend add": {
		Usage:   "ynh backend add <name> <vendor> --base-url <url> [flags]",
		Summary: "Connect a vendor CLI to a local model backend.",
		Args:    []argHelp{{"<name>", "Name of the backend"}, {"<vendor>", "Vendor to connect"}},
		Flags: []flagHelp{
			{"--base-url <url>", "Endpoint of the backend (required)"},
			{"--auth-token <token>", "Token the vendor sends to the backend"},
			{"--type <type>", "Backend type"},
			{"--env KEY=VALUE", "Extra environment variable for the vendor (repeatable)"},
		},
		Parsers: []string{"cmdBackendAdd"},
	},
	"backend list": {
		Usage:   "ynh backend list [flags]",
		Summary: "Show configured backends.",
		Flags:   []flagHelp{formatFlag},
		Parsers: []string{"cmdBackendList"},
	},
	"backend remove": {
		Usage:   "ynh backend remove <name> [<vendor>]",
		Summary: "Remove a backend, or one vendor's connection within it.",
		Args:    []argHelp{{"<name>", "Name of the backend"}, {"[<vendor>]", "Remove only this vendor's connection"}},
		Parsers: []string{"cmdBackendRemove"},
	},
}

// subcommandAliases are the spellings a group's dispatch accepts for the same
// subcommand, so help follows whichever one the user typed.
var subcommandAliases = map[string]string{"list": "ls", "rm": "remove"}

// subcommandKey resolves the arguments after the command name to the longest
// subcommandHelp key they spell, or "" when they spell none. Resolution stops at
// the first word that extends no key, so a harness named like a subcommand
// ("ynh include add add ...") is not misread.
func subcommandKey(cmd string, rest []string) string {
	key := ""
	prefix := canonicalCommand(cmd)
	for _, w := range rest {
		if strings.HasPrefix(w, "-") {
			break
		}
		next := ""
		for _, cand := range []string{w, subcommandAliases[w], aliasOf(w)} {
			if cand == "" {
				continue
			}
			if hasKeyPrefix(prefix + " " + cand) {
				next = prefix + " " + cand
				break
			}
		}
		if next == "" {
			break
		}
		prefix = next
		if _, ok := subcommandHelp[prefix]; ok {
			key = prefix
		}
	}
	return key
}

// aliasOf maps a canonical subcommand back to the alias spelled "list".
func aliasOf(w string) string {
	for alias, canon := range subcommandAliases {
		if canon == w {
			return alias
		}
	}
	return ""
}

// hasKeyPrefix reports whether p is a key, or the start of one ("profile hook"
// leads to "profile hook add").
func hasKeyPrefix(p string) bool {
	for k := range subcommandHelp {
		if k == p || strings.HasPrefix(k, p+" ") {
			return true
		}
	}
	return false
}

// printSubcommandHelp writes the help for the subcommand args name, where args
// is os.Args[1:]. It reports whether there was one.
func printSubcommandHelp(w io.Writer, args []string) bool {
	if len(args) < 2 {
		return false
	}
	key := subcommandKey(args[0], args[1:])
	if key == "" {
		return false
	}
	h := subcommandHelp[key]

	var b strings.Builder
	b.WriteString(h.Usage + "\n\n" + h.Summary + "\n")
	if len(h.Args) > 0 {
		b.WriteString("\nArguments:\n")
		for _, a := range h.Args {
			fmt.Fprintf(&b, "  %-*s  %s\n", argWidth(h.Args), a.Name, a.Desc)
		}
	}
	if len(h.Flags) > 0 {
		b.WriteString("\nFlags:\n")
		for _, f := range h.Flags {
			fmt.Fprintf(&b, "  %-*s  %s\n", flagWidth(h.Flags), f.Flag, f.Desc)
		}
	}
	if h.Notes != "" {
		b.WriteString("\n" + h.Notes + "\n")
	}
	_, err := fmt.Fprint(w, b.String())
	return err == nil
}

func argWidth(as []argHelp) int {
	n := 0
	for _, a := range as {
		n = max(n, len(a.Name))
	}
	return n
}

func flagWidth(fs []flagHelp) int {
	n := 0
	for _, f := range fs {
		n = max(n, len(f.Flag))
	}
	return n
}

// printHelpFor answers `--help` for the arguments after the program name: the
// subcommand's own page when the arguments name one, otherwise the command's.
func printHelpFor(w io.Writer, args []string) bool {
	if printSubcommandHelp(w, args) {
		return true
	}
	return printCommandHelp(w, args[0])
}
