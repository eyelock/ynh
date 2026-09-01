package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/eyelock/ynh/internal/config"
)

// errHelp is returned by arg parsers when -h/--help is passed.
var errHelp = errors.New("help requested")

// errDeclined marks an operation the operator refused, or one that ended with
// nothing done because every item was skipped.
//
// It is not a failure, so main prints it without the "Error:" prefix. It must
// still exit non-zero. `ynd migrate` used to print "Aborted." and exit 0, so a
// caller could not tell a completed migration from a refused one, and since
// promptAction returns its refusing answer on EOF, every piped invocation took
// the refusing branch and reported success for doing nothing.
//
// Exit 1, per docs/cli-structured.md: 1 for user and runtime errors, 2 for
// usage errors. Declining is a user outcome, not a usage mistake.
var errDeclined = errors.New("declined")

// declinedError carries the sentence shown to the operator while still
// matching errDeclined under errors.Is.
type declinedError struct{ msg string }

func (e *declinedError) Error() string { return e.msg }

func (e *declinedError) Is(target error) bool { return target == errDeclined }

// declined reports that the operator was asked and said no, or that a run
// finished having written nothing. The message is shown as-is, so write it as
// a sentence addressed to the operator.
func declined(format string, a ...any) error {
	return &declinedError{msg: fmt.Sprintf(format, a...)}
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Per-command help is intercepted once, here, before dispatch. Doing it
	// inside each command would leave thirteen opportunities to forget, and
	// the next command added would start life without it (#321).
	if len(os.Args) > 2 && wantsHelp(os.Args[2:]) {
		if printCommandHelp(os.Stdout, os.Args[1]) {
			return
		}
	}

	var err error
	switch os.Args[1] {
	case "create":
		err = cmdCreate(os.Args[2:])
	case "lint":
		err = cmdLint(os.Args[2:])
	case "validate":
		err = cmdValidate(os.Args[2:])
	case "fmt":
		err = cmdFmt(os.Args[2:])
	case "compress":
		err = cmdCompress(os.Args[2:])
	case "inspect":
		err = cmdInspect(os.Args[2:])
	case "export":
		err = cmdExport(os.Args[2:])
	case "compose":
		err = cmdCompose(os.Args[2:])
	case "preview":
		err = cmdPreview(os.Args[2:])
	case "diff":
		err = cmdDiff(os.Args[2:])
	case "marketplace":
		err = cmdMarketplace(os.Args[2:])
	case "migrate":
		err = cmdMigrate(os.Args[2:])
	case "validate-output":
		err = cmdValidateOutput(os.Args[2:])
	case "version", "--version", "-v":
		err = cmdVersion(os.Args[2:])
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	switch {
	case errors.Is(err, errHelp):
		printUsage()
	case errors.Is(err, errDeclined):
		// No "Error:" prefix: nothing went wrong, the operator said no. The
		// non-zero status is what tells a script the work did not happen.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	case err != nil:
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	printUsageTo(os.Stdout)
}

// printUsageTo writes the usage page to w. Split from printUsage so a test can
// assert every dispatched command is listed, mirroring cmd/ynh.
func printUsageTo(w io.Writer) {
	// Usage goes to a terminal or a test buffer; a write failure means stdout
	// is gone and there is nowhere left to report it.
	_, _ = fmt.Fprintf(w, `ynd - ynh developer tools (%s)

Usage:
  ynd <command> [arguments]

Commands:
  create <type> <name>       Scaffold a new skill, agent, rule, command, or harness
  lint [file]                Lint markdown, shell, and config files
  validate [file]            Validate harness structure and artifacts
  fmt [file]                 Format markdown files
  compress [file]            Compress prompts using LLM-powered SudoLang techniques
  inspect                    Interactive codebase walkthrough to generate/update skills and agents
  compose <source>           Show resolved composition before vendor assembly
  export <source>            Export harness as vendor-native plugin directories
  preview <source>           Show assembled vendor output without installing
  diff <source> [vendors]    Compare assembled output across vendors
  marketplace build          Build a vendor-native marketplace from marketplace.json
  migrate <path>             Convert .harness.json → .ynh-plugin/plugin.json in-place
  validate-output --schema <name|path> [< file.json]
                             Validate JSON on stdin against the named CLI schema
  version                    Print version
  help                       Show this help

Run 'ynd <command> --help' for one command's detail.

Create types:
  skill <name>               Create skills/<name>/SKILL.md
  agent <name>               Create agents/<name>.md with frontmatter
  harness <name>             Create full harness directory structure
  rule <name>                Create rules/<name>.md
  command <name>             Create commands/<name>.md

Options:
  [file]                     Target a specific file (default: recurse CWD)
  -v, --vendor <name>        Vendor CLI for compress/inspect (default: auto-detect)
  -y, --yes                  Skip confirmation prompts
  -o, --output-dir <path>    Output directory for inspect artifacts (default: .{vendor}/)
  --restore                  Restore a file from its latest compress backup
  --list-backups             Show compress backup history for a file
  --pick <N>                 With --restore, pick a specific backup by number

Examples:
  ynd create skill commit
  ynd create harness my-team
  ynd lint
  ynd lint agents/reviewer.md
  ynd validate
  ynd fmt skills/
  ynd compress -v claude
  ynd compress instructions.md
  ynd compress --list-backups instructions.md
  ynd compress --restore instructions.md
  ynd inspect
  ynd inspect -v claude
  ynd inspect -o .
  ynd compose ./my-harness
  ynd compose ./my-harness --profile staging
  ynd compose ./my-harness --format text
  ynd export ./my-harness
  ynd export ./my-harness -v claude,cursor -o ./dist
  ynd export ./my-harness --merged
  ynd export github.com/user/repo --path harnesses/david
  ynd preview ./my-harness
  ynd preview ./my-harness -v cursor
  ynd preview ./my-harness -v claude -o ./output
  ynd diff ./my-harness claude cursor
  ynd diff ./my-harness
`, config.Version)
}
