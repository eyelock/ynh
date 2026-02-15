# ynh

A persona manager for AI coding assistants. Bundle skills, agents, rules, and commands into named personas, then launch them with any vendor CLI.

> **Note**: This is a personal project developed in my spare time. It's an exploration of the varying approaches to marketplace/distribution across AI vendors.

```bash
ynh install github.com/david/my-persona
david                                    # interactive session
david "review this PR"                   # non-interactive mode
david -v codex                           # same persona, different vendor
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

### 2. Create a persona

Create a directory with a `.claude-plugin/plugin.json` and your artifacts:

```
my-persona/
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
  "description": "David's personal coding persona"
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
ynh install ./my-persona
david
```

## Project Instructions

A persona can include an `instructions.md` that maps to the vendor's project instructions file:

```
my-persona/
├── .claude-plugin/
│   └── plugin.json
└── instructions.md       # becomes CLAUDE.md, codex.md, or .cursorrules
```

If multiple sources provide `instructions.md`, the persona's own file takes priority. See the [Artifacts Guide](docs/artifacts.md#project-instructions) for details.

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

Artifacts can live **directly in your persona** (embedded) or be **pulled from Git repos** (included):

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
      {"git": "github.com/eyelock/team-dev-persona"}
    ]
  }
}
```

Embedded artifacts (files in the persona directory) are always included. External artifacts are fetched from Git at runtime. Shorthand URLs (`github.com/...`) and `git@` URLs both use SSH. Use `https://` explicitly for HTTPS.

## Vendor Support

ynh supports multiple AI vendors. The vendor determines which CLI is launched and where config files are placed. Run `ynh vendors` to see what's available.

Each vendor gets the launch strategy that matches its capabilities:
- **Claude** uses `--plugin-dir` for native plugin loading (clean `exec`, no running process)
- **Codex/Cursor** use symlink-based installation (managed child process with signal forwarding)

Vendor resolution order: **CLI flag > persona default > global config**.

```bash
david                    # uses persona's default_vendor
david -v codex           # override to codex
```

All flags except `-v`, `--install`, and `--clean` are passed through to the vendor CLI. Use `--` to separate vendor flags from the prompt:

```bash
david --model opus -- "fix this bug"
david -v codex --full-auto -- "refactor auth"
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

| Command | Description |
|---------|-------------|
| `ynh init` | Show ynh home path and setup instructions |
| `ynh install <source>` | Install persona from Git URL or local path |
| `ynh uninstall <name>` | Remove an installed persona and its launcher |
| `ynh update <name>` | Refresh cached Git repos for a persona |
| `ynh run <name> [flags] [prompt]` | Launch a persona session |
| `ynh ls` | List installed personas |
| `ynh vendors` | List supported vendor adapters |
| `ynh status` | Show symlink installations across projects |
| `ynh prune` | Clean orphaned symlink installations |
| `ynh version` | Print version |

### Run Flags

| Flag | Description |
|------|-------------|
| `-v <vendor>` | Override vendor (claude, codex, cursor) |
| `--install` | Install symlinks for the vendor in the current project |
| `--clean` | Remove symlinks for the vendor in the current project |
| `--` | Separator between vendor flags and the prompt |

## License

MIT
