# Marketplace & Distribution

ynh can generate vendor-native marketplace indexes from collections of personas and plugins. This page covers how each vendor's marketplace system works, the state of cross-vendor distribution, and how ynh bridges the differences.

## The Landscape

AI coding assistants are converging on a plugin/marketplace model for distributing skills, agents, and configuration. All three vendors ynh supports have marketplace systems, though they differ significantly in maturity, openness, and format.

| | Claude Code | Cursor | Codex |
|---|---|---|---|
| **Status** | GA | GA (Feb 2026) | Beta (Mar 2026) |
| **Model** | Open — anyone hosts a marketplace | Curated — official + team | Curated — official catalog |
| **Plugin format** | `.claude-plugin/plugin.json` | `.cursor-plugin/plugin.json` | No plugin manifest |
| **Marketplace index** | `.claude-plugin/marketplace.json` | `.cursor-plugin/marketplace.json` | `marketplace.json` (evolving) |
| **Install mechanism** | TUI: `/plugin install`. CLI: `--plugin-dir` (direct load) | IDE one-click / CLI | CLI `plugin install` |
| **Private marketplaces** | Yes (any Git repo) | Yes (Teams/Enterprise) | Not documented |
| **Ecosystem size** | Large (40+ marketplaces, 800+ plugins) | Growing (30+ curated plugins) | Small (curated set) |

## Claude Code

Claude Code has the most mature and open marketplace system. Any Git repository containing a `.claude-plugin/marketplace.json` can act as a marketplace.

### Key Concepts

- **Marketplace** — a Git repo with a `.claude-plugin/marketplace.json` index listing available plugins
- **Plugin** — a directory with a `.claude-plugin/plugin.json` manifest, containing skills, agents, rules, commands, hooks, MCP servers, and LSP servers
- **Source** — where the plugin lives: relative path within the marketplace repo, GitHub repo, Git URL, Git subdirectory, or npm package

### marketplace.json Format

Located at `.claude-plugin/marketplace.json` in the repository root:

```json
{
  "name": "my-marketplace",
  "owner": { "name": "My Org", "email": "team@example.com" },
  "metadata": {
    "description": "Optional marketplace description",
    "version": "1.0.0",
    "pluginRoot": "./plugins"
  },
  "plugins": [
    {
      "name": "my-plugin",
      "source": "./plugins/my-plugin",
      "description": "What this plugin does",
      "version": "1.0.0"
    },
    {
      "name": "external-plugin",
      "source": {
        "source": "github",
        "repo": "org/plugin-repo",
        "ref": "v2.0.0"
      }
    }
  ]
}
```

**Required fields:**

| Field | Type | Notes |
|-------|------|-------|
| `name` | string | Marketplace identifier (kebab-case). Users see this when installing: `name@marketplace` |
| `owner.name` | string | Publisher name |
| `plugins` | array | List of available plugins |

**Optional metadata:**

| Field | Type | Notes |
|-------|------|-------|
| `owner.email` | string | Contact email |
| `metadata.description` | string | Marketplace description |
| `metadata.version` | string | Marketplace version |
| `metadata.pluginRoot` | string | Base directory prepended to relative plugin source paths |

**Plugin entry fields:**

| Field | Required | Notes |
|-------|----------|-------|
| `name` | Yes | Plugin identifier (kebab-case) |
| `source` | Yes | String (relative path) or object (see plugin sources below) |
| `description` | No | Plugin description |
| `version` | No | Semver string |
| `author` | No | Object with `name` and optional `email` |
| `category` | No | Plugin category for organization |
| `tags` | No | Array of tags for searchability |
| `keywords` | No | Array of tags for discovery |
| `strict` | No | Whether `plugin.json` is the authority for component definitions (default: `true`) |
| `commands` | No | Custom paths to command files or directories |
| `agents` | No | Custom paths to agent files |
| `hooks` | No | Hooks configuration or path to hooks file |
| `mcpServers` | No | MCP server configurations |

**Plugin source types:**

| Source | Format | Notes |
|--------|--------|-------|
| Relative path | `"./plugins/my-plugin"` | Must start with `./`. Only works with Git-based marketplaces |
| GitHub | `{"source": "github", "repo": "owner/repo"}` | Optional `ref`, `sha` for pinning |
| Git URL | `{"source": "url", "url": "https://..."}` | Any Git host. Optional `ref`, `sha` |
| Git subdirectory | `{"source": "git-subdir", "url": "...", "path": "..."}` | Sparse clone for monorepos. Optional `ref`, `sha` |
| npm | `{"source": "npm", "package": "@scope/pkg"}` | Optional `version`, `registry` |

### CLI vs TUI Commands

Claude Code has two distinct interfaces for working with plugins:

**CLI flags** (used when launching `claude` from the terminal):

```bash
# Load plugins directly from a directory (bypasses marketplace)
claude --plugin-dir ./my-plugin
claude --plugin-dir ./plugin-a --plugin-dir ./plugin-b

# Validate marketplace and plugin structure
claude plugin validate .
```

The `--plugin-dir` flag loads a plugin for the current session only. It does not install from a marketplace — it takes a direct path to a plugin directory. Repeat the flag for multiple plugins.

**TUI slash commands** (used inside an interactive Claude Code session):

```
# Marketplace management
/plugin marketplace add <url-or-repo>
/plugin marketplace list
/plugin marketplace update <name>
/plugin marketplace remove <name>

# Plugin installation
/plugin install <name>@<marketplace>
/plugin disable <name>@<marketplace>
/reload-plugins
```

These are the primary way users discover and install marketplace plugins. The marketplace is cloned, the plugin is copied to `~/.claude/plugins/cache/`, and it persists across sessions.

**Settings-based configuration** (for teams and CI):

Marketplaces can also be configured via `.claude/settings.json` for automatic prompting:

```json
{
  "extraKnownMarketplaces": {
    "company-tools": {
      "source": { "source": "github", "repo": "org/claude-plugins" }
    }
  },
  "enabledPlugins": {
    "formatter@company-tools": true
  }
}
```

For containers and CI, use `CLAUDE_CODE_PLUGIN_SEED_DIR` to pre-populate plugins at image build time without cloning at runtime.

### Enterprise Controls

Administrators can restrict which marketplaces users are allowed to add via the `strictKnownMarketplaces` managed setting. This supports exact matching on GitHub repos, Git URLs, regex patterns on hostnames (`hostPattern`), and filesystem paths (`pathPattern`).

### Official Resources

- **Plugin docs:** [code.claude.com/docs/en/plugins](https://code.claude.com/docs/en/plugins)
- **Plugin reference:** [code.claude.com/docs/en/plugins-reference](https://code.claude.com/docs/en/plugins-reference)
- **Creating marketplaces:** [code.claude.com/docs/en/plugin-marketplaces](https://code.claude.com/docs/en/plugin-marketplaces)
- **Discovering plugins:** [code.claude.com/docs/en/discover-plugins](https://code.claude.com/docs/en/discover-plugins)
- **CLI reference:** [code.claude.com/docs/en/cli-reference](https://code.claude.com/docs/en/cli-reference)
- **Official marketplace:** [github.com/anthropics/claude-plugins-official](https://github.com/anthropics/claude-plugins-official)

### Key Design Points

- **Git-native** — marketplaces are repos, plugins are directories within repos. No central registry with auth gates.
- **Relative source paths** — `source` fields use `./plugins/name` paths that resolve within the Git working tree. This is why Claude Code requires marketplaces to be Git repos (not just directories). URL-based marketplaces must use external source types (GitHub, npm, Git URL) instead.
- **Plugin caching** — installed plugins are copied to `~/.claude/plugins/cache/`, not used in-place. Files outside the plugin directory (e.g. `../shared-utils`) won't be available.
- **Plugin scope** — plugins can be installed at user scope (`~/.claude/plugins/`) or project scope (`.claude/plugins/`).

## Cursor

Cursor launched its plugin marketplace in February 2026 alongside Cursor 2.5. The system bundles five primitives into installable packages: MCP servers, skills, subagents, hooks, and rules.

### Plugin Format

Each plugin has a `.cursor-plugin/` directory containing:
- `plugin.json` — per-plugin manifest (same schema as Claude Code's `plugin.json`)
- Skills, rules, MCP server configs, hooks, subagent definitions

The marketplace index uses `.cursor-plugin/marketplace.json` at the repository root with the same format as Claude Code (name, owner, plugins array).

### Discovery

The primary discovery surface is the Cursor Marketplace (in-IDE and web). Initial launch partners included Amplitude, AWS, Figma, Linear, and Stripe. The catalog has grown to 30+ plugins.

On Teams/Enterprise plans, admins can create private team marketplaces with controlled distribution.

### Format Compatibility

Cursor and Claude Code share nearly identical plugin and marketplace formats:

| Aspect | Claude Code | Cursor |
|--------|------------|--------|
| Plugin manifest dir | `.claude-plugin/` | `.cursor-plugin/` |
| Manifest file | `plugin.json` | `plugin.json` |
| Marketplace index | `.claude-plugin/marketplace.json` | `.cursor-plugin/marketplace.json` |
| Schema | Same | Same |

This is why ynh's merged export mode works — one set of artifacts with both `.claude-plugin/` and `.cursor-plugin/` directories serves both vendors from the same physical plugin directory.

### Official Resources

- **Plugin docs:** [cursor.com/docs/plugins](https://cursor.com/docs/plugins)
- **Building plugins:** [cursor.com/docs/plugins/building](https://cursor.com/docs/plugins/building)
- **Marketplace:** [cursor.com/marketplace](https://cursor.com/marketplace)
- **Marketplace launch blog:** [cursor.com/blog/marketplace](https://cursor.com/blog/marketplace)

## OpenAI Codex

Codex added marketplace support in early March 2026. The system is the newest of the three and is still evolving.

### Current State

Codex's marketplace is curated rather than open. It supports:
- Plugin discovery from the official catalog
- Install/uninstall via CLI
- Auth checks at install time
- Automatic prompting to install missing plugins

### Limitations for Distribution

Codex has significant differences from Claude Code and Cursor:

- **No self-hosted marketplaces** — the catalog is centrally managed (as of March 2026)
- **Skills-only distribution** — Codex discovers skills via `.agents/skills/` but has no loading mechanism for agents, rules, or commands as separate artifacts
- **No plugin manifest** — skills are self-describing via SKILL.md frontmatter
- **Limited marketplace.json** — the format exists but the schema is still evolving

For these reasons, ynh excludes Codex from marketplace generation. When building a marketplace with `ynd marketplace build`, Codex is silently skipped. Individual Codex export (`ynd export -v codex`) produces the `.agents/skills/` layout.

### Official Resources

- **Codex CLI docs:** [developers.openai.com/codex/cli](https://developers.openai.com/codex/cli)
- **CLI reference:** [developers.openai.com/codex/cli/reference](https://developers.openai.com/codex/cli/reference)
- **Changelog:** [developers.openai.com/codex/changelog](https://developers.openai.com/codex/changelog)
- **GitHub:** [github.com/openai/codex](https://github.com/openai/codex)

## ynh and Marketplaces

ynh acts as the translation layer between your persona definition and vendor-native distribution formats. The `ynd marketplace build` command takes a `marketplace.json` config, resolves all persona includes, and produces a Git-ready directory with dual vendor indexes.

### What ynh Does

1. **Reads** your `marketplace.json` config listing personas and plugins
2. **Resolves** all remote includes (Git repos, pick filtering, monorepo subpaths)
3. **Exports** each entry as a merged plugin with both `.claude-plugin/` and `.cursor-plugin/` manifests
4. **Generates** vendor-native `marketplace.json` indexes for Claude Code and Cursor
5. **Initializes** the output as a Git repo (required by Claude Code for relative source path resolution)

### marketplace.json (ynh Config)

ynh's `marketplace.json` is a build config — it describes *what* to include in the marketplace. The vendor-native `marketplace.json` files (in `.claude-plugin/` and `.cursor-plugin/`) are generated output.

```json
{
  "name": "my-marketplace",
  "owner": { "name": "My Org" },
  "description": "Skills and personas for the team",
  "entries": [
    {
      "type": "plugin",
      "source": "./plugins/formatter"
    },
    {
      "type": "persona",
      "source": "./personas/reviewer",
      "description": "Override description for the marketplace"
    },
    {
      "type": "persona",
      "source": "github.com/user/repo",
      "path": "personas/baz"
    }
  ]
}
```

| Field | Required | Notes |
|-------|----------|-------|
| `name` | Yes | Marketplace name |
| `owner.name` | Yes | Publisher name |
| `owner.email` | No | Contact email |
| `description` | No | Marketplace description |
| `entries[].type` | Yes | `"persona"` or `"plugin"` |
| `entries[].source` | Yes | Local path or Git URL |
| `entries[].description` | No | Overrides plugin.json description |
| `entries[].version` | No | Overrides plugin.json version |
| `entries[].path` | No | Monorepo subdirectory |

### Entry Types

- **`plugin`** — a self-contained plugin directory (already has `.claude-plugin/plugin.json`). Copied as-is with missing vendor manifests generated.
- **`persona`** — a ynh persona (has `metadata.json` with includes). Fully exported: remote includes resolved, pick filtering applied, delegates generated, dual manifests written.

### Output Structure

```
dist/
├── plugins/
│   ├── formatter/
│   │   ├── .claude-plugin/plugin.json
│   │   ├── .cursor-plugin/plugin.json
│   │   └── skills/auto-format/SKILL.md
│   └── reviewer/
│       ├── .claude-plugin/plugin.json
│       ├── .cursor-plugin/plugin.json
│       ├── .cursorrules
│       ├── AGENTS.md
│       └── skills/...
├── .claude-plugin/marketplace.json    # Claude Code index
├── .cursor-plugin/marketplace.json    # Cursor index
├── .git/                              # auto-initialized
└── README.md                          # auto-generated
```

### CLI Usage

```bash
# Build from marketplace.json in current directory
ynd marketplace build

# Custom config, output, and vendor targeting
ynd marketplace build config/marketplace.json -o ./dist -v claude,cursor

# Clean rebuild
ynd marketplace build --clean
```

### Distribution

Once built, push the output directory to a Git repository. Users can then:

**Claude Code:**
```bash
/plugin marketplace add github.com/org/my-marketplace
/plugin install formatter@my-marketplace
```

**Cursor:**
Add the marketplace URL in Cursor's plugin settings, then install from the IDE marketplace browser.

### Design Decisions

**Why two marketplace.json files?** Vendor formats reject unknown fields. Claude Code requires `.claude-plugin/marketplace.json`, Cursor requires `.cursor-plugin/marketplace.json`. ynh generates both from the same source config, producing one physical plugin directory that serves both vendors.

**Why exclude Codex?** Codex has no self-hosted marketplace system and limited artifact support (skills only). Generating a Codex marketplace index would have no consumer. Individual Codex export is still supported via `ynd export -v codex`.

**Why auto-init Git?** Claude Code's plugin loader resolves relative `source` paths (e.g., `./plugins/formatter`) within the Git working tree. A marketplace directory that isn't a Git repo causes path resolution failures at install time.

## Cross-Vendor Compatibility Matrix

| Feature | Claude Code | Cursor | Codex |
|---------|------------|--------|-------|
| Skills | Yes | Yes | Yes |
| Agents | Yes | Yes | No |
| Rules | Yes | Yes | No |
| Commands | Yes | Yes | No |
| Instructions | AGENTS.md | .cursorrules + AGENTS.md | AGENTS.md |
| Plugin manifest | .claude-plugin/ | .cursor-plugin/ | None |
| Marketplace index | .claude-plugin/marketplace.json | .cursor-plugin/marketplace.json | N/A |
| Delegates | Yes (subagent) | Yes (subagent) | No |
| Merged export | Yes | Yes | Excluded |

## References

- Agent Skills spec: [agentskills.io](https://agentskills.io)
- Claude Code plugins: [code.claude.com/docs/en/plugins](https://code.claude.com/docs/en/plugins)
- Claude Code marketplaces: [code.claude.com/docs/en/plugin-marketplaces](https://code.claude.com/docs/en/plugin-marketplaces)
- Cursor marketplace: [cursor.com/marketplace](https://cursor.com/marketplace)
- Cursor plugins: [cursor.com/docs/plugins](https://cursor.com/docs/plugins)
- Codex CLI: [developers.openai.com/codex/cli](https://developers.openai.com/codex/cli)
- ynh marketplace tutorial: [Tutorial 6: Marketplace](tutorial/06-marketplace.md)
- ynd marketplace command: [ynd Developer Tools](ynd.md#marketplace-build)
