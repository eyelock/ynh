# Vendor Support

ynh works with multiple AI coding assistants. The vendor determines which CLI is launched and where config files are placed. Your harnesses and artifacts stay the same regardless of vendor.

## Supported Vendors

Run `ynh vendors` to see what's available:

```
$ ynh vendors
NAME     CLI      CONFIG DIR
claude   claude   .claude
codex    codex    .codex
cursor   agent    .cursor
```

## Launch Strategies

Each vendor gets the strategy that matches its capabilities:

| Vendor | Launch | Artifacts | Process |
|--------|--------|-----------|---------|
| **Claude** | `syscall.Exec` with `--plugin-dir` | Native plugin loading | Clean handoff (ynh exits) |
| **Codex** | Child process with `cmd.Dir` | Symlinks into project | Managed (signal forwarding) |
| **Cursor** | Child process with `cmd.Dir` | Symlinks into project | Managed (signal forwarding) |

Claude supports `--plugin-dir` natively, so ynh can exec directly into it. Codex and Cursor don't have plugin loading, so ynh installs symlinks and manages the child process for signal forwarding.

### Symlink Installation (Codex/Cursor)

For vendors that need symlinks:

```bash
david -v cursor --install     # creates .cursor/ symlinks in current project
david -v cursor               # launches normally
david -v cursor --clean       # removes symlinks
```

Installations are tracked in `~/.ynh/symlinks.json`. Use `ynh status` to see all installations and `ynh prune` to clean up orphaned ones.

## Choosing a Vendor

**Per-harness** (in `harness.json`):

```json
{
  "default_vendor": "claude"
}
```

**Per-session** (CLI flag overrides everything):

```bash
david -v codex
```

**Global default** (in `~/.ynh/config.json`):

```json
{"default_vendor": "claude"}
```

Resolution order: **CLI flag (`-v`) > `YNH_VENDOR` env var > harness default > global config**.

`YNH_VENDOR` is honored by both `ynh` commands (`ynh run`) and `ynd` commands (`preview`, `export`, `create`, `compress`, `inspect`, `marketplace`).

## Vendor Notes

**Claude Code** - Full interactive and non-interactive support. Uses `--plugin-dir` for artifact loading and `--append-system-prompt` for harness instructions. Requires `claude` CLI installed. See [claude.ai/code](https://claude.ai/code).

**OpenAI Codex** - Full interactive and non-interactive support. Uses symlink-based artifact installation. Requires `codex` CLI installed. See [openai.com/codex](https://openai.com/codex).

**Cursor Agent** - Full interactive and non-interactive support. Uses symlink-based artifact installation. Requires `agent` CLI installed (`curl https://cursor.com/install -fsS | bash`). Uses `-p` for non-interactive prompts. See [cursor.com/cli](https://cursor.com/cli).

## Export Output by Vendor

`ynd export` produces vendor-native plugin layouts. Each vendor has a different file structure:

| | Claude | Cursor | Codex |
|---|---|---|---|
| **Manifest** | `.claude-plugin/plugin.json` | `.cursor-plugin/plugin.json` | — |
| **Skills** | `skills/<name>/SKILL.md` | `skills/<name>/SKILL.md` | `.agents/skills/<name>/SKILL.md` |
| **Agents** | `agents/<name>.md` | `agents/<name>.md` | *excluded* |
| **Rules** | `rules/<name>.md` | `rules/<name>.md` | *excluded* |
| **Commands** | `commands/<name>.md` | `commands/<name>.md` | *excluded* |
| **Instructions** | `AGENTS.md` | `.cursorrules` + `AGENTS.md` | `AGENTS.md` |
| **Marketplace** | `.claude-plugin/marketplace.json` | `.cursor-plugin/marketplace.json` | *excluded* |

Key differences between runtime and export:

- **Runtime** places artifacts inside the vendor config directory (e.g., `.claude/skills/`)
- **Export** places artifacts at the plugin root (e.g., `skills/`) — the standard distributable layout
- Claude export writes `AGENTS.md` for instructions, not `CLAUDE.md` (which would conflict with the installing project's own)
- Codex export is limited to skills — agents, rules, commands, and delegates are excluded with warnings
- Codex is excluded from marketplace and merged export modes (no marketplace system)

See [ynd export](ynd.md#export) for full command reference.

## Adding a New Vendor

See [CONTRIBUTING.md](https://github.com/eyelock/ynh/blob/main/.github/CONTRIBUTING.md) for how to implement a vendor adapter.
