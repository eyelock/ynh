# ynd Developer Tools

`ynd` is a companion CLI for authoring and maintaining ynh harnesses. It scaffolds artifacts, validates structure, formats markdown, compresses prompts, inspects codebases to generate new skills and agents, exports harnesses as vendor-native plugins, and builds marketplaces.

## Install

ynd ships alongside ynh — both binaries are included in every release.

```bash
brew tap eyelock/tap && brew install ynh
# or download from GitHub releases
```

## Commands

### create

Scaffold a new artifact or full harness.

```bash
ynd create harness my-team     # full harness directory structure (harness.json + artifacts)
ynd create skill commit        # skills/commit/SKILL.md
ynd create agent reviewer      # agents/reviewer.md
ynd create rule be-nice        # rules/be-nice.md
ynd create command deploy      # commands/deploy.md
```

### lint

Lint markdown, shell blocks, and config files for common issues.

```bash
ynd lint                       # all files under CWD
ynd lint path/to/harness       # specific directory
ynd lint --harness ./my-harness  # explicit harness flag
```

### validate

Validate harness structure: required files, frontmatter fields, directory layout.

```bash
ynd validate                   # current harness directory
ynd validate path/to/harness   # specific harness
ynd validate --harness ./my-harness  # explicit harness flag
```

### fmt

Format markdown files — normalise headings, whitespace, and list markers.

```bash
ynd fmt                        # all .md files under CWD
ynd fmt skills/                # specific directory
ynd fmt --harness ./my-harness   # explicit harness flag
```

### compress

Compress prompt/instruction text using LLM-powered SudoLang-style techniques. Requires `claude` or `codex` CLI on PATH.

A backup of every file is saved to `~/.ynd/backups/` before overwriting. Use `--restore` and `--list-backups` to manage backups.

```bash
ynd compress                   # discover and compress all .md files
ynd compress AGENTS.md         # specific file
ynd compress -y verbose.md     # skip confirmation prompt
ynd compress -v claude         # use specific vendor CLI
YNH_YES=1 ynd compress file.md # skip confirmation via env var
CI=true ynd compress file.md   # also skip confirmation (CI convention)

# Backup management
ynd compress --list-backups AGENTS.md   # show backup history
ynd compress --restore AGENTS.md        # restore latest backup
ynd compress --restore --pick 2 AGENTS.md  # restore specific backup
```

### inspect

Interactive codebase walkthrough. Detects project signals and uses an LLM to suggest skills and agents tailored to your stack.

Generated artifacts are written into the vendor-specific config directory by default (e.g. `.claude/skills/`, `.claude/agents/`). Use `--output-dir` to override.

```bash
ynd inspect                    # auto-detect vendor CLI, write to .{vendor}/
ynd inspect -v claude          # use specific vendor
ynd inspect -o .               # write artifacts to project root instead
ynd inspect -o /tmp/out        # write artifacts to a custom directory
YNH_YES=1 ynd inspect          # skip prompts via env var
YNH_VENDOR=cursor ynd inspect  # vendor via env var
```

### preview

Show the assembled vendor-native output for a harness without installing it. Useful for verifying hook config, MCP config, and artifact layout before shipping.

```bash
ynd preview ./my-harness                    # default: Claude vendor, stdout
ynd preview ./my-harness -v cursor          # specific vendor
ynd preview ./my-harness -v claude -o ./out # write to directory
ynd preview ./my-harness --profile strict   # preview with a specific profile
ynd preview --harness ./my-harness          # explicit harness flag
```

| Flag | Description |
|------|-------------|
| `-v, --vendor <name>` | Vendor to assemble for. Default: `claude` |
| `-o, --output <dir>` | Write output to directory instead of stdout |
| `--harness <dir>` | Harness source directory (alternative to positional arg) |
| `--profile <name>` | Profile to apply during assembly |

When no `-o` flag is given, preview prints a tree with file contents to stdout. With `-o`, it writes the full assembled output to the specified directory.

Preview supports the same source types as export: local directories with `harness.json` or bare `AGENTS.md` directories.

See [Tutorial 8: Developer Preview](tutorial/12-developer-preview.md) for a guided walkthrough.

### diff

Compare assembled harness output across two or more vendors. Shows which files are unique to each vendor, which have different content, and which are identical.

```bash
ynd diff ./my-harness                       # compare all vendors (claude, codex, cursor)
ynd diff ./my-harness claude cursor         # compare specific vendors (positional)
ynd diff ./my-harness -v claude,cursor      # compare specific vendors (flag)
ynd diff ./my-harness claude cursor codex   # three-way comparison
ynd diff ./my-harness --profile strict      # diff with a specific profile applied
ynd diff --harness ./my-harness             # explicit harness flag
```

The diff output groups files into four categories:
- **Only in \<vendor\>** — files unique to that vendor (e.g., `.claude/settings.json` for Claude hooks)
- **Different content** — files present in both but with different content
- **Identical** — files present in both with the same content

At least two vendors are required for comparison. If no vendors are specified, all registered vendors are compared.

See [Tutorial 8: Developer Preview](tutorial/12-developer-preview.md) for a guided walkthrough.

### export

Export a harness as vendor-native plugins. Resolves all remote includes, flattens artifacts, and writes distributable output per vendor.

```bash
ynd export ./my-harness                          # all vendors → ./dist/my-harness/
ynd export ./my-harness -v claude,cursor          # specific vendors only
ynd export ./my-harness -o ./out                  # custom output directory
ynd export ./my-harness --merged                  # single dir with dual manifests
ynd export ./my-harness --clean                   # remove output dir before export
ynd export ./my-harness --profile strict          # export with a specific profile applied
ynd export --harness ./my-harness                 # explicit harness flag
ynd export github.com/user/repo --path harnesses/david  # from a monorepo
```

| Flag | Description |
|------|-------------|
| `-o, --output <dir>` | Output directory. Default: `./dist/<harness-name>/` |
| `-v, --vendor <names>` | Comma-separated vendors. Default: all registered (`claude,codex,cursor`) |
| `--harness <dir>` | Harness source directory (alternative to positional arg) |
| `--path <subdir>` | Subdirectory within source (for monorepos) |
| `--profile <name>` | Profile to apply during assembly |
| `--merged` | Single output dir with all vendor manifests (for CI/marketplace use) |
| `--clean` | Remove entire output dir before export |

**Output structure** (per-vendor mode):

```
dist/my-harness/
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
    ├── .codex-plugin/plugin.json
    ├── skills/<name>/SKILL.md
    └── AGENTS.md
```

Key differences from runtime layout:

- Export places artifacts at the plugin root (`skills/`), not inside the vendor config dir (`.claude/skills/`)
- Claude export writes `AGENTS.md` for instructions, not `CLAUDE.md` (which would conflict with the installing project's own `CLAUDE.md`)
- Codex is limited to skills only — agents, rules, commands, and delegates are excluded with warnings
- `--merged` produces one directory with both `.claude-plugin/` and `.cursor-plugin/` manifests; Codex is excluded from merged mode

See [Tutorial 10: Export](tutorial/05-export.md) for a guided walkthrough.

### marketplace build

Build a marketplace from a collection of harnesses and pre-built plugins. Each entry is exported with dual vendor manifests and indexed.

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
    { "type": "harness", "source": "./harnesses/bar" },
    { "type": "harness", "source": "github.com/user/repo", "path": "harnesses/baz" }
  ]
}
```

- `plugin` entries are copied as-is (already in vendor-native format)
- `harness` entries are fully exported — includes resolved, artifacts flattened
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

See [Tutorial 11: Marketplace](tutorial/06-marketplace.md) for a guided walkthrough.

## Common Options

| Flag | Commands | Description |
|------|----------|-------------|
| `-v, --vendor <name(s)>` | compress, inspect, export, preview, diff, marketplace | Vendor to use. Comma-separated for export/diff/marketplace. Single value for preview. |
| `-y, --yes` | compress, inspect | Skip confirmation prompts. Also honored via `YNH_YES` or `CI` env vars. |
| `-o, --output <path>` | inspect, export, preview, marketplace | Output directory. Defaults vary by command. |
| `--harness <dir>` | preview, diff, export, validate, lint, fmt | Harness source directory. Alternative to positional arg. Also honored via `YNH_HARNESS` env var. |
| `--clean` | export, marketplace | Remove output directory before writing. |
| `--merged` | export | Single output dir with dual vendor manifests. |
| `--profile <name>` | preview, diff, export | Profile to apply during assembly. |
| `--path <subdir>` | export | Subdirectory within source (for monorepos). |
| `--restore` | compress | Restore a file from its latest backup. |
| `--list-backups` | compress | Show backup history for a file. |
| `--pick <N>` | compress | With `--restore`, pick a specific backup by number from the list. |

## Examples

```bash
# Bootstrap a new harness and validate it
ynd create harness ops-team
cd ops-team
ynd validate

# Add artifacts, lint, and format
ynd create skill deploy
ynd create agent reviewer
ynd lint
ynd fmt

# Compress verbose instructions before shipping
ynd compress -y AGENTS.md

# Inspect a project to generate skills
cd /path/to/my-app
ynd inspect

# Preview assembled output for a specific vendor
ynd preview ./ops-team -v cursor
ynd preview ./ops-team -v claude -o ./preview-output

# Compare assembled output across vendors
ynd diff ./ops-team claude cursor

# Export a harness as vendor-native plugins
ynd export ./ops-team -v claude,cursor

# Build a marketplace from multiple harnesses
ynd marketplace build marketplace.json -o ./dist --clean
```
