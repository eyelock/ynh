# ynh

A harness template manager for AI coding assistants. Bundle skills, agents, rules, and commands into named harnesses, then launch them with any vendor CLI.

> **Note**: This is a personal project developed in my spare time. It's an exploration of the varying approaches to marketplace/distribution across AI vendors.

**[Full Documentation](https://eyelock.github.io/ynh)**

```bash
ynh install github.com/myorg/david
david                                    # interactive session
david "review this PR"                   # non-interactive mode
david -v codex                           # same harness, different vendor
david --model opus -- "fix this bug"     # pass flags through to vendor CLI
```

## Quick Start

### 1. Install

```bash
brew tap eyelock/tap
brew install ynh
```

Add the launcher directory to your PATH (one-time):

```bash
echo 'export PATH="$HOME/.ynh/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

### 2. Create a harness

Create a directory with a `.claude-plugin/plugin.json` and your artifacts:

```
david/
├── .claude-plugin/
│   └── plugin.json           # required - name, version
├── metadata.json             # optional - vendor, includes, delegates
├── instructions.md           # optional - project-level instructions
├── skills/
│   └── review/
│       └── SKILL.md
├── agents/
│   └── code-reviewer.md
├── rules/
│   └── always-test.md
└── commands/
    └── check.md
```

The plugin manifest (`.claude-plugin/plugin.json`):

```json
{
  "name": "david",
  "version": "0.1.0",
  "description": "David's personal coding harness"
}
```

Optional metadata (`metadata.json`):

```json
{
  "ynh": {
    "default_vendor": "claude"
  }
}
```

### 3. Install and run

```bash
ynh install ./david
ynh install github.com/org/monorepo --path harnesses/david
david
```

## Project Instructions

A harness can include an `instructions.md` that maps to the vendor's project instructions file:

```
david/
├── .claude-plugin/
│   └── plugin.json
└── instructions.md       # becomes CLAUDE.md, codex.md, or .cursorrules
```

If multiple sources provide `instructions.md`, the harness's own file takes priority. See the [Artifacts Guide](docs/artifacts.md#project-instructions) for details.

## Adding Artifacts

Artifacts are standard-format files. No build step, no wrapper. A skill from [skills.sh](https://skills.sh) or any Git repo works as-is.

### Skills

A directory with a `SKILL.md` file following the [Agent Skills](https://agentskills.io) open specification:

```
skills/review/SKILL.md
```

### Agents

A markdown file with YAML frontmatter:

```
agents/code-reviewer.md
```

### Rules

A markdown file loaded as context:

```
rules/always-test.md
```

### Commands

A markdown file describing a command:

```
commands/check.md
```

### Embedding vs Including

Artifacts can live **directly in your harness** (embedded) or be **pulled from Git repos** (included):

```json
{
  "ynh": {
    "default_vendor": "claude",
    "includes": [
      {
        "git": "github.com/eyelock/claude-config-toolkit",
        "ref": "v2.0.0",
        "pick": ["skills/commit", "skills/tdd"]
      },
      {
        "git": "git@github.com:company/internal-tools.git",
        "path": "packages/ai-config",
        "pick": ["agents/reviewer"]
      }
    ],
    "delegates_to": [
      {"git": "github.com/eyelock/team-dev-harness"}
    ]
  }
}
```

Embedded artifacts (files in the harness directory) are always included. External artifacts are fetched from Git at runtime. Shorthand URLs (`github.com/...`) and `git@` URLs both use SSH. Use `https://` explicitly for HTTPS.

## Vendor Support

ynh supports multiple AI vendors. The vendor determines which CLI is launched and where config files are placed. Run `ynh vendors` to see what's available.

Each vendor gets the launch strategy that matches its capabilities:
- **Claude** uses `--plugin-dir` for native plugin loading (clean `exec`, no running process)
- **Codex/Cursor** use symlink-based installation (managed child process with signal forwarding)

Vendor resolution order: **CLI flag > harness default > global config**.

```bash
david                    # uses harness's default_vendor
david -v codex           # override to codex
```

All flags except `-v`, `--install`, and `--clean` are passed through to the vendor CLI. Use `--` to separate vendor flags from the prompt:

```bash
david --model opus -- "fix this bug"
david -v codex -- "refactor auth"
```

### Symlink Installation (Codex/Cursor)

Vendors that don't support plugin directories use symlinks installed into your project:

```bash
david -v cursor --install     # creates .cursor/ symlinks in current project
david -v cursor               # launches normally
david -v cursor --clean       # removes symlinks
```

Symlink installations are tracked in `~/.ynh/symlinks.json`. Use `ynh status` to see all installations and `ynh prune` to clean up orphaned ones.

Global default in `~/.ynh/config.json`:

```json
{"default_vendor": "claude"}
```

## Commands

### ynh (Harness Template Manager)

| Command | Description |
|---------|-------------|
| `ynh init` | Show ynh home path and setup instructions |
| `ynh install <source> [--path <subdir>]` | Install harness from Git URL or local path |
| `ynh uninstall <name>` | Remove an installed harness and its launcher (alias: `ynh remove`) |
| `ynh update <name>` | Refresh cached Git repos for a harness |
| `ynh run <name> [flags] [prompt]` | Launch a harness session |
| `ynh ls` | List installed harnesses (alias: `ynh list`) |
| `ynh info <name>` | Show detailed harness information |
| `ynh search <query>` | Search registries for harnesses by name or keyword |
| `ynh registry <subcommand>` | Manage harness registries (add, remove, list) |
| `ynh vendors` | List supported vendor adapters |
| `ynh status` | Show symlink installations across projects |
| `ynh prune` | Clean orphaned symlink installations |
| `ynh version` | Print version |

### ynd (Developer Tools)

| Command | Description |
|---------|-------------|
| `ynd create <type> <name>` | Scaffold a harness, skill, agent, rule, or command |
| `ynd lint [files]` | Lint markdown, shell blocks, and config files |
| `ynd validate [path]` | Validate harness structure and required fields |
| `ynd fmt [files]` | Format markdown files |
| `ynd compress [files]` | LLM-powered prompt compression with backup/restore |
| `ynd inspect` | Interactive codebase analysis to generate skills and agents |
| `ynd export <source> [flags]` | Export harness as vendor-native plugins |
| `ynd marketplace build [flags]` | Build a marketplace from harnesses and plugins |

See the [full command reference](https://eyelock.github.io/ynh/#/ynd) for all flags and options.

### Run Flags

| Flag | Description |
|------|-------------|
| `-v <vendor>` | Override vendor (claude, codex, cursor) |
| `--install` | Install symlinks for the vendor in the current project |
| `--clean` | Remove symlinks for the vendor in the current project |
| `--` | Separator between vendor flags and the prompt |

## Documentation

Full documentation is available at **[eyelock.github.io/ynh](https://eyelock.github.io/ynh)**, including:

- [Getting Started](https://eyelock.github.io/ynh/#/getting-started) — create and run your first harness
- [Harness Reference](https://eyelock.github.io/ynh/#/harnesses) — plugin manifest, metadata, includes, delegates
- [Artifacts Guide](https://eyelock.github.io/ynh/#/artifacts) — skills, agents, rules, commands, and project instructions
- [Vendor Support](https://eyelock.github.io/ynh/#/vendors) — Claude, Codex, Cursor capabilities and launch strategies
- [Agent Skills Standard](https://eyelock.github.io/ynh/#/skills-standard) — cross-platform spec, discovery paths, catalog budget
- [Marketplace & Distribution](https://eyelock.github.io/ynh/#/marketplace) — cross-vendor marketplace systems and ynh's marketplace builder
- [Docker](https://eyelock.github.io/ynh/#/docker) — containerized harnesses and Docker image baking
- [Tutorials](https://eyelock.github.io/ynh/#/tutorial/) — 8 progressive tutorials from first harness to marketplace generation

## License

MIT
