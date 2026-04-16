package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/eyelock/ynh/internal/config"
)

// errHelp is returned by arg parsers when -h/--help is passed.
var errHelp = errors.New("help requested")

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
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
	case "version", "--version", "-v":
		fmt.Println(config.Version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	if errors.Is(err, errHelp) {
		printUsage()
	} else if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`ynd - ynh developer tools (%s)

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
  version                    Print version
  help                       Show this help

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
