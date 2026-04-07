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
| harness.json      | .claude-plugin/plugin.json       | .codex-plugin/plugin.json        | .cursor-plugin/plugin.json       |
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
| harness.json      | hooks/hooks.json (plugin)        | .codex/hooks.json                | hooks/hooks.json (plugin)        |
|   hooks: {}       | .claude/settings.json (project)  | ~/.codex/hooks.json (user)       | .cursor/settings.json (project)  |
+-------------------+----------------------------------+----------------------------------+----------------------------------+
| Format:           | Three-level nesting:             | Three-level nesting:             | Two formats:                     |
|                   | event > matcher > hooks[]        | event > matcher > hooks[]        | - Plugin: flat (legacy names)    |
|                   |                                  |                                  | - Settings: three-level          |
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
| harness.json      | .claude/.mcp.json (plugin)       | .mcp.json (plugin root)          | .cursor/mcp.json (project)       |
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
| HIGH | Codex plugin manifest not generated       | Codex      |
| HIGH | Codex skills export path wrong            | Codex      |
|      |   (.agents/skills/ should be skills/)    |            |
| HIGH | Codex MCP format wrong                   | Codex      |
|      |   (TOML should be JSON .mcp.json)        |            |
| HIGH | Codex marketplace not generated           | Codex      |
| ---  | Claude AGENTS.md: RESOLVED               | Claude     |
|      |   (CLAUDE.md with @AGENTS.md import)     |            |
| MED  | Cursor plugin hooks format               | Cursor     |
|      |   (flat legacy vs three-level settings)  |            |
| MED  | Cursor .mdc rules format                 | Cursor     |
|      |   (ynh writes .md, Cursor wants .mdc     |            |
|      |    with globs/alwaysApply frontmatter)    |            |
| LOW  | SessionStart canonical event             | All        |
|      |   (Codex + Claude support it, not mapped) |            |
| LOW  | Hook types beyond "command"              | Claude,    |
|      |   (prompt, http, agent not mapped)       | Cursor     |
| LOW  | Cursor subagent/delegation support       | Cursor     |
|      |   (needs research)                       |            |
| LOW  | Cursor CLI flags for non-interactive     | Cursor     |
|      |   (needs research)                       |            |
+------+------------------------------------------+------------+
```

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
