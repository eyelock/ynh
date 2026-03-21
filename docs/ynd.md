# ynd Developer Tools

`ynd` is a companion CLI for authoring and maintaining ynh personas. It scaffolds artifacts, validates structure, formats markdown, compresses prompts, inspects codebases to generate new skills and agents, exports personas as vendor-native plugins, and builds marketplaces.

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

### export

Export a persona as vendor-native plugins. Resolves all remote includes, flattens artifacts, and writes distributable output per vendor.

```bash
ynd export ./my-persona                          # all vendors → ./dist/my-persona/
ynd export ./my-persona -v claude,cursor          # specific vendors only
ynd export ./my-persona -o ./out                  # custom output directory
ynd export ./my-persona --merged                  # single dir with dual manifests
ynd export ./my-persona --clean                   # remove output dir before export
ynd export github.com/user/repo --path personas/david  # from a monorepo
```

| Flag | Description |
|------|-------------|
| `-o, --output <dir>` | Output directory. Default: `./dist/<persona-name>/` |
| `-v, --vendor <names>` | Comma-separated vendors. Default: all registered (`claude,codex,cursor`) |
| `--path <subdir>` | Subdirectory within source (for monorepos) |
| `--merged` | Single output dir with all vendor manifests (for CI/marketplace use) |
| `--clean` | Remove entire output dir before export |

**Output structure** (per-vendor mode):

```
dist/my-persona/
├── claude/
│   ├── .claude-plugin/plugin.json
│   ├── skills/<name>/SKILL.md
│   ├── agents/<name>.md
│   ├── rules/<name>.md
│   ├── commands/<name>.md
│   └── AGENTS.md
├── cursor/
│   ├── .cursor-plugin/plugin.json
│   ├── skills/
│   ├── agents/
│   ├── rules/
│   ├── commands/
│   ├── .cursorrules
│   └── AGENTS.md
└── codex/
    ├── .agents/skills/<name>/SKILL.md
    └── AGENTS.md
```

Key differences from runtime layout:

- Export places artifacts at the plugin root (`skills/`), not inside the vendor config dir (`.claude/skills/`)
- Claude export writes `AGENTS.md` for instructions, not `CLAUDE.md` (which would conflict with the installing project's own `CLAUDE.md`)
- Codex is limited to skills only — agents, rules, commands, and delegates are excluded with warnings
- `--merged` produces one directory with both `.claude-plugin/` and `.cursor-plugin/` manifests; Codex is excluded from merged mode

See [Tutorial 5: Export](tutorial/05-export.md) for a guided walkthrough.

### marketplace build

Build a marketplace from a collection of personas and pre-built plugins. Each entry is exported with dual vendor manifests and indexed.

```bash
ynd marketplace build                             # uses ./marketplace.json
ynd marketplace build config/marketplace.json     # custom config path
ynd marketplace build -o ./marketplace-dist       # custom output directory
ynd marketplace build -v claude,cursor            # specific vendors
ynd marketplace build --clean                     # remove output dir before build
```

| Flag | Description |
|------|-------------|
| `-o, --output <dir>` | Output directory. Default: `./dist` |
| `-v, --vendor <names>` | Comma-separated vendors. Default: `claude,cursor` |
| `--clean` | Remove output dir before building |

**Config format** (`marketplace.json`):

```json
{
  "name": "my-marketplace",
  "owner": { "name": "My Org" },
  "entries": [
    { "type": "plugin", "source": "./plugins/foo" },
    { "type": "persona", "source": "./personas/bar" },
    { "type": "persona", "source": "github.com/user/repo", "path": "personas/baz" }
  ]
}
```

- `plugin` entries are copied as-is (already in vendor-native format)
- `persona` entries are fully exported — includes resolved, artifacts flattened
- Codex is excluded (no marketplace system)

**Output structure:**

```
dist/
├── plugins/
│   └── <name>/
│       ├── .claude-plugin/plugin.json
│       ├── .cursor-plugin/plugin.json
│       ├── skills/
│       └── ...
├── .claude-plugin/marketplace.json
├── .cursor-plugin/marketplace.json
└── README.md
```

See [Tutorial 6: Marketplace](tutorial/06-marketplace.md) for a guided walkthrough.

## Common Options

| Flag | Commands | Description |
|------|----------|-------------|
| `-v, --vendor <name(s)>` | compress, inspect, export, marketplace | Vendor to use. Comma-separated for export/marketplace. |
| `-y, --yes` | compress, inspect | Skip confirmation prompts. |
| `-o, --output <path>` | inspect, export, marketplace | Output directory. Defaults vary by command. |
| `--clean` | export, marketplace | Remove output directory before writing. |
| `--merged` | export | Single output dir with dual vendor manifests. |
| `--path <subdir>` | export | Subdirectory within source (for monorepos). |
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

# Export a persona as vendor-native plugins
ynd export ./ops-team -v claude,cursor

# Build a marketplace from multiple personas
ynd marketplace build marketplace.json -o ./dist --clean
```
