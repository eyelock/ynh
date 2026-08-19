---
name: vendor-adapters
description: Maintain ynh vendor adapters against current vendor plugin specs. Use when updating adapters, checking spec compliance, or adding new vendor support.
---

# Vendor Adapter Maintenance

Use this skill when updating ynh vendor adapters, verifying spec compliance, or researching vendor plugin format changes. This is the single source of truth for all vendor documentation links and format mappings.

## Vendor Documentation Links

### Claude Code (Anthropic)

| Area | URL |
|------|-----|
| CLI Reference | https://code.claude.com/docs/en/cli-reference |
| Plugins Overview | https://code.claude.com/docs/en/plugins |
| Plugins Reference | https://code.claude.com/docs/en/plugins-reference |
| Plugin Marketplaces | https://code.claude.com/docs/en/plugin-marketplaces |
| Hooks Guide | https://code.claude.com/docs/en/hooks-guide |
| MCP Servers | https://code.claude.com/docs/en/mcp |
| Settings Reference | https://code.claude.com/docs/en/settings |
| Subagents | https://code.claude.com/docs/en/sub-agents |
| Official Plugins Repo | https://github.com/anthropics/claude-plugins-official |

### OpenAI Codex

| Area | URL |
|------|-----|
| Plugins Overview | https://developers.openai.com/codex/plugins |
| Plugin Build Guide | https://developers.openai.com/codex/plugins/build |
| Hooks | https://developers.openai.com/codex/hooks |
| CLI Reference | https://developers.openai.com/codex |
| GitHub Repo | https://github.com/openai/codex |

### Cursor

| Area | URL |
|------|-----|
| Plugin Template | https://github.com/cursor/plugin-template |
| Official Plugins Repo | https://github.com/cursor/plugins |
| Marketplace | https://cursor.com/marketplace |
| MCP Servers | https://docs.cursor.com/advanced/mcp |
| Rules (.mdc) | https://docs.cursor.com/advanced/rules |
| CLI | https://cursor.com/cli |
| Forum: .agents/ support | https://forum.cursor.com/t/support-for-agent-folder-compatibility/154167 |

### GitHub Copilot CLI

| Area | URL |
|------|-----|
| Install | https://docs.github.com/en/copilot/how-tos/set-up/install-copilot-cli |
| CLI Reference | https://docs.github.com/en/copilot/reference/copilot-cli-reference |
| Configure CLI | https://docs.github.com/en/copilot/how-tos/copilot-cli/set-up-copilot-cli/configure-copilot-cli |
| Custom Instructions | https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-custom-instructions |
| Agent Skills | https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-skills |
| Custom Agents | https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/create-custom-agents-for-cli |
| MCP Servers | https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-mcp-servers |
| Hooks | https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/use-hooks |
| Plugins & Marketplace | https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/plugins-marketplace |
| Plugin Reference | https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-plugin-reference |
| GitHub Repo | https://github.com/github/copilot-cli |
| Changelog | https://github.com/github/copilot-cli/blob/main/changelog.md |

**Note:** Copilot CLI ships weekly; GitHub's own docs warn "commands, flags,
and available models change often." Verify flags against `copilot help`
output before hardcoding into the adapter, don't trust docs/blog snapshots
alone.

### Cross-Vendor Standards

| Area | URL |
|------|-----|
| Agent Skills (agentskills.io) | https://agentskills.io |
| AGENTS.md Spec | https://github.com/agentsmd/agents.md |
| .agents/ Folder Spec | https://github.com/agentsfolder/spec |

## ynh-to-Vendor Format Mapping

What ynh calls each concept vs what each vendor calls it and where it lives.

### Plugin Manifest

```
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| ynh               | Claude Code                      | Codex                            | Cursor                           |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| .harness.json      | .claude-plugin/plugin.json       | .codex-plugin/plugin.json        | .cursor-plugin/plugin.json       |
| (source format)   | (distribution format)            | (distribution format)            | (distribution format)            |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Required fields:  | name                             | name, version, description       | name, version, description       |
| name, version     |                                  |                                  |                                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
```

### Skills

```
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| ynh               | Claude Code                      | Codex                            | Cursor                           |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Source:            |                                  |                                  |                                  |
| skills/<name>/    | skills/<name>/SKILL.md           | skills/<name>/SKILL.md           | skills/<name>/SKILL.md           |
|   SKILL.md        |                                  |                                  |                                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Runtime:          | .claude/skills/<name>/SKILL.md   | .codex/skills/ (or .agents/      | .cursor/skills/<name>/SKILL.md   |
|                   |                                  |  skills/ standalone)             |                                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Plugin export:    | skills/<name>/SKILL.md           | skills/<name>/SKILL.md           | skills/<name>/SKILL.md           |
|                   | (at plugin root)                 | (at plugin root)                 | (at plugin root)                 |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Invocation:       | /plugin-name:skill-name          | @plugin-name skill-name          | /plugin-name:skill-name          |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Format:           | YAML frontmatter + markdown      | YAML frontmatter + markdown      | YAML frontmatter + markdown      |
|                   | (name, description)              | (name, description)              | (name, description)              |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
```

### Agents / Subagents

```
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| ynh               | Claude Code                      | Codex                            | Cursor                           |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Source:            |                                  |                                  |                                  |
| agents/<name>.md  | agents/<name>.md                 | NOT SUPPORTED in plugins         | agents/<name>.md                 |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Frontmatter:      | name, description, model,        |                                  | name, description                |
|                   | tools, disallowedTools, skills,  |                                  |                                  |
|                   | maxTurns, effort, memory,        |                                  |                                  |
|                   | background, isolation            |                                  |                                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Delegation:       | Native subagent system           | NOT SUPPORTED                    | NEEDS RESEARCH                   |
| (delegates_to)    | via agent .md files              |                                  |                                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
```

### Rules

```
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| ynh               | Claude Code                      | Codex                            | Cursor                           |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Source:            |                                  |                                  |                                  |
| rules/<name>.md   | .claude/rules/<name>.md          | NOT SUPPORTED in plugins         | .cursor/rules/<name>.mdc         |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Format:           | Plain markdown                   |                                  | .mdc (markdown + frontmatter     |
|                   |                                  |                                  |  with description, globs,        |
|                   |                                  |                                  |  alwaysApply)                    |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Legacy:           |                                  |                                  | .cursorrules (project root,      |
|                   |                                  |                                  |  deprecated but still read)      |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
```

### Commands

```
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| ynh               | Claude Code                      | Codex                            | Cursor                           |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Source:            |                                  |                                  |                                  |
| commands/         | commands/<name>.md               | NOT SUPPORTED in plugins         | commands/<name>.md               |
|   <name>.md       | (LEGACY -- use skills instead)   |                                  |                                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
```

### Hooks

```
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| ynh               | Claude Code                      | Codex                            | Cursor                           |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Source:            |                                  |                                  |                                  |
| .harness.json      | hooks/hooks.json (plugin)        | .codex/hooks.json                | hooks/hooks.json (plugin)        |
|   hooks: {}       | .claude/settings.json (project)  | ~/.codex/hooks.json (user)       | .cursor/settings.json (project)  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Format:           | Three-level nesting:             | Three-level nesting:             | Flat format, same at both paths: |
|                   | event > matcher > hooks[]        | event > matcher > hooks[]        | {event: [{command}]} — CONFIRMED |
|                   |                                  |                                  | ynh writes both paths identically|
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Events            | 25 events (see Claude docs)      | 5 events: SessionStart,          | 25 events (same as Claude)       |
| (vendor-native):  | Key: PreToolUse, PostToolUse,    | PreToolUse, PostToolUse,         |                                  |
|                   | UserPromptSubmit, Stop,          | UserPromptSubmit, Stop           |                                  |
|                   | SessionStart, ...                |                                  |                                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| ynh canonical     | before_tool -> PreToolUse        | before_tool -> PreToolUse        | before_tool -> beforeShellExec   |
| event mapping:    | after_tool  -> PostToolUse       | after_tool  -> PostToolUse       | after_tool  -> afterFileEdit     |
|                   | before_prompt -> UserPromptSubmit| before_prompt -> UserPromptSubmit| before_prompt -> beforeSubmit    |
|                   | on_stop -> Stop                  | on_stop -> Stop                  | on_stop -> stop                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Hook types:       | command, http, prompt, agent     | command only                     | command, prompt, http, agent     |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| --plugin-dir      | Skills: YES                      | N/A (uses symlinks)              | N/A (uses symlinks)              |
| auto-activation:  | Hooks: NO (need /plugin enable)  |                                  |                                  |
|                   | MCP: NO (need /plugin enable)    |                                  |                                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
```

### MCP Servers

```
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| ynh               | Claude Code                      | Codex                            | Cursor                           |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Source:            |                                  |                                  |                                  |
| .harness.json      | .claude/.mcp.json (plugin)       | .mcp.json (plugin root)          | .cursor/mcp.json (project)       |
|   mcp_servers: {} | .mcp.json (project root)         |                                  | mcp.json (plugin root)           |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Format:           | JSON: {"mcpServers": {...}}      | JSON: {"mcpServers": {...}}      | JSON: {"mcpServers": {...}}      |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Server types:     | stdio (command+args)             | stdio (command+args)             | stdio, SSE, streamable HTTP      |
|                   | HTTP (url+headers)               |                                  | OAuth supported                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
```

### Marketplace

```
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| ynh               | Claude Code                      | Codex                            | Cursor                           |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Index file:       | .claude-plugin/marketplace.json  | .agents/plugins/                 | .cursor-plugin/marketplace.json  |
|                   |                                  |   marketplace.json               |                                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Format:           | name, owner, plugins[]           | name, interface.displayName,     | name, owner, metadata,           |
|                   |   (name, source, description,    |   plugins[] (name, source,       |   plugins[] (name, source,       |
|                   |    version)                      |    policy, category)             |    description)                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Plugin source:    | "./plugins/name" (relative)      | {"source":"local",               | "plugin-name" (relative dir)     |
|                   |                                  |  "path":"./plugins/name"}        |                                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Install cmd:      | /plugin install name@marketplace | codex (via Plugin Directory)     | /add-plugin                      |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Official:         | anthropics/claude-plugins-       | OpenAI Plugin Directory          | cursor.com/marketplace           |
|                   |   official (GitHub)              |   (coming soon)                  |                                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
```

### GitHub Copilot CLI Mapping

Copilot CLI wasn't part of the original three-vendor comparison; it's kept as
its own markdown table (not folded into the ASCII boxes above) since it's
newer and its docs move fast. Fold it into the boxes above once an adapter
lands and the shape stabilizes.

| Concept | ynh | Copilot CLI |
|---|---|---|
| Plugin manifest | `.harness.json` | **CONFIRMED by hand-testing** (v1.0.75): `plugin.json` at `.plugin/`, repo root, `.github/plugin/`, or `.claude-plugin/` (compat path) — all four verified to work identically via `--plugin-dir <dir>`. Only `name` required. Bundled `skills/<name>/SKILL.md` inside the plugin dir load and activate correctly regardless of which manifest path is used (verified: asked the live model whether the skill was available — yes, both times). **Caveat:** `copilot skill list`'s static table does NOT show `--plugin-dir`-bundled skills at all (only Project/Personal/Builtin categories) — this is a listing/display gap in that specific command, not a functional one; don't take `skill list` output as the source of truth for whether a plugin's skills are actually loaded. **CONFIRMED: the manifest is required, not optional** — removing it entirely (skills present, no manifest anywhere) made a previously-working skill stop loading; skills silently fail with no error when the manifest is missing or misplaced. **Shipped adapter bug caught by this**: `GeneratePluginManifest` is called with a different root by `ynh run` (skills nested under `.copilot/`, matching `--plugin-dir`) than by `ynd export` (skills flattened to the export root) — a fixed manifest path was correct for one caller and silently broken for the other. Fixed via `copilotRunDirLayout(outputDir)`, which detects the caller by checking whether `outputDir/.copilot` exists, verified against a real export + reload. |
| Skills | `skills/<name>/SKILL.md` | `SKILL.md` under `.github/skills/`, `.claude/skills/`, or `.agents/skills/` (project); `~/.copilot/skills/` (user). Reads Claude's `.claude/skills/` natively. Native support, not just markdown-by-convention. |
| Agents / subagents | `agents/<name>.md` | `.agent.md` under `.github/agents/` (project), `~/.copilot/agents/` (user, overrides same-named project agent). 6 built-in agents ship by default. Invoked via `/agent`, `--agent NAME`, or auto-inferred. |
| Rules | `rules/<name>.md` | No "rules" concept by that name. Closest analog: `NAME.instructions.md` under `.github/instructions/` with `applyTo: <glob>` frontmatter — no defined precedence when multiple files match. |
| Commands | `commands/<name>.md` | **Not supported.** No user-definable `.prompt.md`-style custom slash commands as of 2026-07-29 (open feature requests: github/copilot-cli#618, #942, #1113). Only a fixed built-in command set. Plugin manifests can declare a "commands" component, but how that surfaces isn't confirmed. |
| Hooks | `.harness.json` `hooks: {}` | **CONFIRMED, complete (14 events).** Canonical source: `docs.github.com/en/copilot/reference/hooks-reference`. See the dedicated Hooks section below for the full event table and canonical-event mapping. | Config file: `.github/hooks/*.json` (repo, any filename) or `~/.copilot/hooks/*.json` (user). Entry shape: `{"type": "command", "bash": "...", "powershell": "...", "cwd": "...", "timeoutSec": 30, "env": {...}, "matcher": "regex"}`. Top-level also supports `"disableAllHooks": false`. |
| MCP servers | `.harness.json` `mcp_servers: {}` | **CONFIRMED exact schema by hand-testing** (`copilot mcp add`/`get`/`list`, v1.0.75). `.mcp.json` (project root) or `.github/mcp.json` (repo-shared) — both work, both live-read (no restart), both labeled "Workspace" source, verified interchangeable by moving the file between them mid-session. `~/.copilot/mcp-config.json` is user scope, lower precedence. SSE flagged by GitHub's own docs as legacy/deprecated. No documented OAuth — static credentials only. **CONFIRMED (adapter shipped): a `.mcp.json` bundled inside a `--plugin-dir`-loaded plugin is NOT read** (`copilot mcp list` shows nothing, tested both `.mcp.json` and `.github/mcp.json` placements inside the plugin dir). The Copilot adapter's `GenerateMCPConfig` still writes `.copilot/.mcp.json` for interface consistency, but `buildCopilotArgs` re-reads that file and projects it into the real project's `.github/mcp.json` — the path that actually works. |
| Marketplace | n/a (ynh-generated) | `marketplace.json` at `.github/plugin/` for repos acting as a marketplace. Two pre-registered defaults: `copilot-plugins`, `awesome-copilot`. Install via `copilot plugin install`, `/plugin install`, or declarative `enabledPlugins` in `settings.json`. |
| Instructions file | `instructions.md` | Reads **multiple formats simultaneously**, no defined precedence: `AGENTS.md`, `.github/copilot-instructions.md`, `.github/instructions/*.instructions.md`, `CLAUDE.md`/`.claude/CLAUDE.md`, `GEMINI.md`, plus `~/.copilot/copilot-instructions.md` (user-level, cross-repo). Reads `AGENTS.md` natively — no workaround needed (contrast Claude's `@AGENTS.md` import hack). Never shell out to `copilot init` — it's an LLM-invoking scaffolder that infers its own content from the repo (confirmed via `copilot init --help`, v1.0.75), fundamentally incompatible with ynh's model of authoritative harness-authored instructions. **CONFIRMED (adapter shipped): an `AGENTS.md` bundled inside a `--plugin-dir`-loaded plugin is NOT read as instructions** — verified with a behavioral canary test (positive control: same file at cwd/repo root, adopted; negative: same file at the plugin-dir root, ignored — tested twice with `AGENTS.md` and `.github/copilot-instructions.md`, and again with `--add-dir` added, all negative). `GenerateSystemPrompt` (used only by `ynd export`) still emits `AGENTS.md` for consistency with the other three adapters, but the actual `ynh run` delivery mechanism is different: `buildCopilotArgs` reads the assembled `AGENTS.md` from the staging dir and projects it into the real project directory as `.github/instructions/ynh-harness.instructions.md` with `applyTo: "**/*"` frontmatter (confirmed required — Copilot's path-scoped instructions files do nothing without it). This file is uniquely-namespaced and fully ynh-owned, so it's safe to overwrite on every run without touching anything the user authored themselves. |
| Config dir | `.claude`/`.codex`/`.cursor` | No single project dotfolder — spread across `.github/*` subpaths plus root files. User-level home is `~/.copilot/` (override: `COPILOT_HOME`). |
| Launch — interactive | `syscall.Exec` or child process | **CONFIRMED (`copilot help`, 2026-07-29): native `--plugin-dir <directory>` (repeatable), same pattern as Claude.** `syscall.Exec`, `NeedsSymlinks() == false`. |
| Launch — non-interactive | `-p "prompt"` equivalent | **CONFIRMED:** `-p, --prompt <text>`. Docs note `--allow-all-tools` is *required* for non-interactive mode — must be appended, or the run hangs on a permission prompt with no TTY. |
| Launch — initial prompt into interactive session | vendor-specific | **CONFIRMED:** `-i, --interactive <prompt>` — "Start interactive mode and automatically execute this prompt." |
| Model selection | vendor-specific flag | `--model <name>` (e.g. `gpt-5.4`) / in-session `/model`. |
| Auto-approve / yolo | vendor-specific flag | `--allow-all` / `--yolo` (equivalent to `--allow-all-tools --allow-all-paths --allow-all-urls`), plus granular `--allow-all-tools`, `--allow-all-paths`, `--allow-all-urls`, `--allow-tool`, `--deny-tool`, `--allow-url`, `--deny-url`. |
| Add-dir / cwd | vendor-specific flag | `--add-dir <dir>` (repeatable); `-C <directory>` to change cwd before startup. |
| Session resume | vendor-specific | **CONFIRMED, caller-chosen ID:** `--session-id <id>` sets the UUID for a *new* session; `-r, --resume[=id]` resumes by session ID, task ID, ID prefix (7+ hex chars), or exact case-insensitive name; `--continue` resumes the most recent session. Better than Cursor's backend (which must generate and track its own ID) — ynh can choose the UUID up front. |
| AGENTS.md toggle | n/a | `--no-custom-instructions` disables loading from `AGENTS.md` and related files — confirms native `AGENTS.md` reading end-to-end. |
| Additional MCP config | n/a | `--additional-mcp-config <json-or-@file>` — session-scoped MCP servers passed as a launch arg, additive to `~/.copilot/mcp-config.json`. A third MCP-wiring option beyond writing `.mcp.json`/`.github/mcp.json` into the staging dir — compare both in Phase 1 of the adapter work. |
| Sandbox | n/a | **CONFIRMED ABSENT** (`copilot help permissions`, 2026-07-29) — no `--sandbox`/`--no-sandbox` flag exists. Permission model is tool/URL/path-scoped instead (see next row), not a sandbox toggle. Drop this row from the adapter design — there's nothing to map. |
| Permission model detail | n/a | `--allow-tool`/`--deny-tool` take `kind(argument)` patterns: `shell(cmd:*)`, `write(path?)`, `<mcp-server-name>(tool-name?)`, `url(domain-or-url?)`. **Deny always wins over allow**, even against `--allow-all-tools`. `--available-tools`/`--excluded-tools` filter which tools the model *sees* at all (separate from the approval-prompt layer). Doc explicitly says wildcard matching is expected to expand "in the very near future" — don't over-fit the adapter to today's exact pattern syntax if ynh ever exposes fine-grained permission passthrough. |

### Copilot CLI MCP Config Schema (confirmed by hand-testing, 2026-07-29, v1.0.75)

Verified by writing `.mcp.json` and `.github/mcp.json` by hand and confirming
`copilot mcp list`/`get` pick them up correctly — not inferred from docs.

```json
{
  "mcpServers": {
    "local-example": {
      "type": "local",
      "command": "npx",
      "args": ["-y", "@upstash/context7-mcp"],
      "env": { "SOME_KEY": "value" },
      "tools": ["*"]
    },
    "remote-example": {
      "type": "http",
      "url": "https://mcp.example.com/mcp",
      "headers": { "Authorization": "Bearer xyz" },
      "tools": ["*"]
    }
  }
}
```

Key differences from ynh's existing `GenerateMCPConfig` output for
Claude/Cursor (which just marshals `plugin.MCPServer` with no `type` field,
inferring stdio-vs-remote from whether `command` or `url` is set):

- Copilot's schema **requires an explicit `"type"` field**: `"local"` for
  stdio, `"http"` or `"sse"` for remote. The Copilot adapter's
  `GenerateMCPConfig` must add this field — translate `command present →
  "local"`, `url present → "http"` (default) unless the harness's own MCP
  server declaration specifies SSE.
- `tools` defaults to `["*"]` if omitted when set via `copilot mcp add`, but
  wasn't tested for omission entirely from a hand-written file — include it
  explicitly (`["*"]`) to be safe rather than assuming a default applies to
  files not written through the CLI.
- **`copilot mcp add` only writes to user-level `~/.copilot/mcp-config.json`**
  — there is no flag to target the workspace file. This confirms (same
  pattern as the `copilot init` finding above): **ynh's adapter must write
  `.mcp.json`/`.github/mcp.json` directly as a file, not shell out to
  `copilot mcp add`**, exactly like it already does for Claude and Cursor.

### Copilot CLI Hook Events (confirmed complete, 2026-07-29)

Source: `docs.github.com/en/copilot/reference/hooks-reference` — the page's
own table header states it lists "every supported event." All 14 keys are
**camelCase**, nested under top-level `"hooks"`: `{"version": 1, "hooks": {"<eventName>": [...]}}`.

**CRITICAL, hand-tested finding (v1.0.75): hooks silently no-op in an
untrusted folder, with no error and no warning.** A `.github/hooks/
copilot-cli-policy.json` with a `preToolUse` hook did not fire at all
(verified via a marker-file side effect that never appeared) when run
against a brand-new scratch git repo — despite `--allow-all-tools`,
`--allow-all-paths`, and `--experimental` all being set. The hook only
started firing after the directory was added to `trustedFolders`.

**Follow-up, also hand-tested: no CLI flag grants this trust.** Tried
`--add-dir <the-same-dir>` (both alone and combined with
`--allow-all-paths`) against a fresh untrusted scratch repo — confirmed via
a live shell tool call (`ls -la`, verified to have actually executed) that
the `preToolUse` hook still did not fire, and no global config file was
touched. **`--add-dir` extends path *access*, it does not grant the
trust-folder status hooks require** — these are two separate gates.

One more wrinkle: the trust grant did not stay where it was written.
`trustedFolders` was set by hand-editing `~/.copilot/settings.json`, but the
CLI's own auto-managed `~/.copilot/config.json` (which says "managed
automatically" in its own header comment) picked it up and became the
persisted source — `settings.json` itself was later found empty/deleted
while `config.json` still carried the entry. Treat `config.json` as the
actual runtime-authoritative trust store, not `settings.json`, but be wary of
writing to a file explicitly marked app-managed.

**This directly threatens the adapter's `NeedsSymlinks() == false` /
`--plugin-dir` launch strategy**: ynh's staging dir is not the user's own
git checkout, so it will not be a pre-trusted folder the first time a
harness runs there, and hooks will silently fail with no error surfaced to
ynh or the user — and there is no known per-invocation flag to work around
it. **Before Phase 1 ships hook support, resolve how ynh gets the staging
dir trusted** — candidates: write directly to `trustedFolders` in
`~/.copilot/config.json` (mutates global user config marked app-managed,
needs explicit user consent per this repo's action-care norms, and may not
be a supported integration point), or accept that Copilot hooks require a
one-time manual trust grant the way Claude Code's `/plugin enable` does. Do
not ship `GenerateHookConfig` for Copilot without addressing this — a hook
config that silently never fires is worse than an honest "not supported."

| JSON key | Fires when | Output processed? |
|---|---|---|
| `sessionStart` | New or resumed session begins | **CONFIRMED by hand-testing (v1.0.75): output is ignored.** The hook fires (verified via a marker-file side effect) but neither a flat `{"additionalContext": "..."}` nor a Claude-style nested `{"hookSpecificOutput": {"additionalContext": "..."}}` made it into the model's context — tested both, the model reported no such string either way. The CLI-specific tutorial page was right; the newer reference page's claim is either wrong for this version, describes a future release, or applies only to the cloud agent. Don't rely on `sessionStart` for context injection in the ynh adapter. |
| `sessionEnd` | Session terminates | No |
| `userPromptSubmitted` | User submits a prompt | No |
| `userPromptTransformed` | After the runtime rewrites the submitted prompt into model-facing content | Yes — can rewrite model-facing content |
| `preToolUse` | Before each tool executes | Yes — allow/deny/modify |
| `postToolUse` | After a tool completes **successfully** | Yes — can modify result / inject `additionalContext` |
| `postToolUseFailure` | After a tool completes with a **failure** | Yes — recovery guidance via `additionalContext` |
| `agentStop` | Main agent finishes a turn | Yes — `decision: "block"` can force continuation |
| `subagentStart` | A subagent is spawned | Optional — `additionalContext` prepended to the subagent's prompt |
| `subagentStop` | A subagent completes | Yes — block/force continuation |
| `errorOccurred` | An error occurs during execution | No |
| `preCompact` | Context compaction about to begin | No — notification only |
| `permissionRequest` | Before the permission service runs | Yes — `behavior: "allow"`/`"deny"` short-circuits. **CLI only**, no effect under the cloud agent. |
| `notification` | CLI emits a system notification | Optional — `additionalContext` injection. **CLI only.** |

**Canonical event mapping for `GenerateHookConfig`:**

- `before_tool` → `preToolUse`
- `after_tool` → **both** `postToolUse` (success) and `postToolUseFailure`
  (failure) — Copilot splits what Claude/Codex/Cursor combine into one
  after-tool hook into two distinct events by outcome. Decide whether ynh's
  single canonical `after_tool` fans out to both keys, or whether ynh should
  eventually grow a failure-specific canonical event — fan-out to both is the
  simpler starting point and matches "don't lose information" better than
  picking one.
- `before_prompt` → `userPromptSubmitted` (not `userPromptTransformed` — that
  one fires later, on already-rewritten content, and is closer to a rewrite
  hook than a `before_prompt` analog; leave unmapped for now).
- `on_stop` → `agentStop`
- No canonical equivalent exists today for `sessionEnd`, `subagentStart`,
  `subagentStop`, `errorOccurred`, `preCompact`, `permissionRequest`, or
  `notification` — leave unmapped, same precedent as Cursor's comment noting
  "there is no afterShellExecution event." Don't invent canonical events to
  soak these up without a concrete ynh use case driving it.

Do not confuse this JSON-config event list with the `@github/copilot-sdk`'s
TypeScript/Python/Go/.NET/Java callback method names (PascalCase/`onXxx`
forms documented separately under `copilot-sdk/hooks/hooks-overview`) — that
page describes the SDK for building custom agents, not the CLI's hook config
file. The 14 events above are what goes in `.github/hooks/*.json`.

**Open questions before writing the adapter** (see `add-vendor-adapter` skill
for the full scaffolding checklist once these are resolved):

- Exact flag spellings need reconfirming against a live `copilot help` — several
  above are corroborated by secondary sources (DeepWiki, blogs) rather than a
  single canonical fetched CLI reference page. (Update: launch/permission
  flags have since been directly confirmed against `copilot help` and
  `copilot help permissions` — see the CLI Flags rows above. This caveat now
  applies mainly to flags not yet directly verified, e.g. `--effort` value
  semantics, `--context` tier behavior.)
- Whether Copilot's `SKILL.md` frontmatter is byte-for-byte compatible with
  the agentskills.io spec ynh uses (relevant to `docs/skills-standard.md`'s
  known-issues notes) is unconfirmed — required fields (`name`, `description`)
  match, but `allowed-tools` and other fields haven't been diffed against the
  spec.
- No commands support means ynh's `commands/` artifact type has nowhere to
  go for this vendor — same situation as Codex today (see Commands row in
  the ASCII table above).

### Instructions File

```
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| ynh               | Claude Code                      | Codex                            | Cursor                           |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Source:            |                                  |                                  |                                  |
| instructions.md   | CLAUDE.md (project root)         | AGENTS.md (project root)         | .cursorrules (project root,      |
|                   |                                  |                                  |  deprecated)                     |
|                   |                                  |                                  | .cursor/rules/*.mdc (current)    |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| ynh runtime:      | --append-system-prompt           | Written as codex.md in           | Written as .cursorrules in       |
|                   | (injected, no file conflict)     |   staging dir                    |   staging dir                    |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| ynh export:       | AGENTS.md + CLAUDE.md            | AGENTS.md                        | .cursorrules + AGENTS.md         |
|                   |  (CLAUDE.md contains @AGENTS.md  |                                  |                                  |
|                   |   import — see workaround below) |                                  |                                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
```

**Claude AGENTS.md workaround:** Claude Code does not read `AGENTS.md` natively
(see https://code.claude.com/docs/en/memory). ynh exports a `CLAUDE.md` containing
just `@AGENTS.md` which uses Claude's `@`-import syntax to pull in the cross-vendor
instructions file. This avoids duplicating content while ensuring Claude reads the
instructions. The plugin's `CLAUDE.md` lives inside the plugin directory, so it does
not conflict with the project's own `CLAUDE.md`.

### Launch Strategy

```
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| ynh               | Claude Code                      | Codex                            | Cursor                           |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Binary:           | claude                           | codex                            | agent                            |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Launch:           | syscall.Exec (process replace)   | exec.Command (child process)     | exec.Command (child process)     |
|                   | --plugin-dir + --add-dir +       | cmd.Dir = stagingDir             | cmd.Dir = stagingDir             |
|                   | --append-system-prompt           |                                  |                                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Symlinks:         | No (uses --plugin-dir)           | Yes (into project .codex/)       | Yes (into project .cursor/)      |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Non-interactive:  | claude -p "prompt"               | codex exec "prompt"              | agent -p "prompt"                |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Vendor-specific   | --dangerously-skip-permissions   | --skip-git-repo-check            | NEEDS RESEARCH                   |
| flags of note:    | --model, --permission-mode       | --model, --full-auto             |                                  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
```

## Known Gaps and Research Needed

```
+------+------------------------------------------+------------+
| Prio | Gap                                      | Vendor     |
+------+------------------------------------------+------------+
| ---  | Claude AGENTS.md: RESOLVED               | Claude     |
|      |   (CLAUDE.md with @AGENTS.md import)     |            |
| ---  | Codex plugin manifest: RESOLVED          | Codex      |
| ---  | Codex skills export path: RESOLVED       | Codex      |
| ---  | Codex MCP format: RESOLVED               | Codex      |
| ---  | Codex marketplace: RESOLVED              | Codex      |
| ---  | Cursor .mdc rules format: RESOLVED       | Cursor     |
|      |   (see internal/vendor/cursor.go,        |            |
|      |    Cursor.TransformArtifact)              |            |
| ---  | Cursor plugin hooks path: RESOLVED       | Cursor     |
|      |   (writes both .cursor/hooks.json and    |            |
|      |    hooks/hooks.json — same format/names)  |            |
| ---  | Cursor plugin MCP path: RESOLVED         | Cursor     |
|      |   (writes both .cursor/mcp.json and      |            |
|      |    mcp.json at plugin root)                |            |
| ---  | Cursor subagent/delegation support:      | Cursor     |
|      |   RESOLVED — confirmed working, ynh's    |            |
|      |   name+description frontmatter matches   |            |
|      |   Cursor's requirements                   |            |
| LOW  | Hook types beyond "command"              | Claude,    |
|      |   (prompt, http, agent not mapped)       | Cursor     |
| LOW  | Cursor CLI flags for non-interactive     | Cursor     |
|      |   (needs research)                       |            |
+------+------------------------------------------+------------+
```

Note: `on_session_start` canonical event mapping (#199) is tracked as a separate
in-flight PR, not listed above as a gap — see the stacked-PR plan for eyelock/ynh#199.

## Workflow

When updating a vendor adapter:

1. **Fetch current docs** from the URLs above
2. **Compare** against the format mapping tables
3. **Update adapter code** in `internal/vendor/<vendor>.go`
4. **Update exporter** in `internal/exporter/exporter.go` if export format changed
5. **Update manifest** in `internal/exporter/manifest.go` if plugin.json format changed
6. **Update marketplace** in `internal/marketplace/` if marketplace format changed
7. **Update tests** for all changed files
8. **Update docs** in `docs/vendors.md`, `docs/hooks.md`, `docs/mcp.md`, `docs/marketplace.md`
9. **Run `make check`** to verify everything passes
10. **Manual test** with the actual vendor CLI to confirm the output works
