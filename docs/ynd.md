# ynd Developer Tools

`ynd` is a companion CLI for authoring and maintaining ynh personas. It scaffolds artifacts, validates structure, formats markdown, compresses prompts, and inspects codebases to generate new skills and agents.

## Install

ynd ships alongside ynh — both binaries are included in every release.

```bash
brew tap eyelock/tap && brew install ynh
# or download from GitHub releases
```

## Commands

### create

Scaffold a new artifact or full persona.

```bash
ynd create persona my-team     # full persona directory structure
ynd create skill commit        # skills/commit/SKILL.md
ynd create agent reviewer      # agents/reviewer.md
ynd create rule be-nice        # rules/be-nice.md
ynd create command deploy      # commands/deploy.md
```

### lint

Lint markdown, shell blocks, and config files for common issues.

```bash
ynd lint                       # all files under CWD
ynd lint agents/reviewer.md    # single file
```

### validate

Validate persona structure: required files, frontmatter fields, directory layout.

```bash
ynd validate                   # current persona directory
ynd validate path/to/persona   # specific persona
```

### fmt

Format markdown files — normalise headings, whitespace, and list markers.

```bash
ynd fmt                        # all .md files under CWD
ynd fmt skills/                # specific directory
```

### compress

Compress prompt/instruction text using LLM-powered SudoLang-style techniques. Requires `claude` or `codex` CLI on PATH.

A backup of every file is saved to `~/.ynd/backups/` before overwriting. Use `--restore` and `--list-backups` to manage backups.

```bash
ynd compress                   # discover and compress all .md files
ynd compress instructions.md   # specific file
ynd compress -y verbose.md     # skip confirmation prompt
ynd compress -v claude         # use specific vendor CLI

# Backup management
ynd compress --list-backups instructions.md   # show backup history
ynd compress --restore instructions.md        # restore latest backup
ynd compress --restore --pick 2 instructions.md  # restore specific backup
```

### inspect

Interactive codebase walkthrough. Detects project signals and uses an LLM to suggest skills and agents tailored to your stack.

Generated artifacts are written into the vendor-specific config directory by default (e.g. `.claude/skills/`, `.claude/agents/`). Use `--output-dir` to override.

```bash
ynd inspect                    # auto-detect vendor CLI, write to .{vendor}/
ynd inspect -v claude          # use specific vendor
ynd inspect -o .               # write artifacts to project root instead
ynd inspect -o /tmp/out        # write artifacts to a custom directory
```

## Common Options

| Flag | Commands | Description |
|------|----------|-------------|
| `-v, --vendor <name>` | compress, inspect | Vendor CLI to use (`claude`, `codex`). Default: auto-detect. |
| `-y, --yes` | compress, inspect | Skip confirmation prompts. |
| `-o, --output-dir <path>` | inspect | Directory to write generated skills/agents into. Default: `.{vendor}/` (e.g. `.claude/`). |
| `--restore` | compress | Restore a file from its latest backup. |
| `--list-backups` | compress | Show backup history for a file. |
| `--pick <N>` | compress | With `--restore`, pick a specific backup by number from the list. |

## Examples

```bash
# Bootstrap a new persona and validate it
ynd create persona ops-team
cd ops-team
ynd validate

# Add artifacts, lint, and format
ynd create skill deploy
ynd create agent reviewer
ynd lint
ynd fmt

# Compress verbose instructions before shipping
ynd compress -y instructions.md

# Inspect a project to generate skills
cd /path/to/my-app
ynd inspect
```
